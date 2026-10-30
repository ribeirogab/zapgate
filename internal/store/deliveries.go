package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	DeliveryOpen      = "open"
	DeliveryRetrying  = "retrying"
	DeliveryDelivered = "delivered"
	DeliveryFailed    = "failed"
)

type Delivery struct {
	ID         int64
	WebhookID  int64
	AccountID  int64
	ChatJID    string
	FirstSeq   int64
	LastSeq    int64
	EventCount int
	State      string
	OpenedAt   time.Time
	DueAt      time.Time
	Attempts   int
	LastStatus int
	LastError  string
	SettledAt  time.Time
}

type Placement struct {
	Join  bool
	DueAt time.Time
}

type Queue struct {
	WebhookID int64
	Cursor    int64
	Events    []Event
	Place     func(e Event, newest *Delivery) Placement
}

type AttemptOutcome struct {
	State           string
	DueAt           time.Time
	Status          int
	Error           string
	SettledAt       time.Time
	DisablesWebhook bool
}

const (
	deliveryColumns = `d.id, d.webhook_id, d.account_id, d.chat_jid, d.first_seq, d.last_seq, d.event_count, d.state, d.opened_at, d.due_at,
  d.attempts, d.last_status, d.last_error, d.settled_at`
	sendableDelivery = `d.state IN ('open', 'retrying') AND NOT EXISTS (
  SELECT 1 FROM webhook_deliveries o
  WHERE o.webhook_id = d.webhook_id AND o.account_id = d.account_id AND o.chat_jid IS d.chat_jid
    AND o.id < d.id AND o.state IN ('open', 'retrying'))
  AND d.webhook_id IN (` + enabledWebhookIDs + `)`
	enabledWebhookIDs = `SELECT id FROM webhooks WHERE disabled_at IS NULL`
)

func (s *Store) QueueEvents(ctx context.Context, q Queue) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		takesEvents, err := moveCursor(ctx, tx, q.WebhookID, q.Cursor)
		if err != nil || !takesEvents {
			return err
		}
		for _, event := range q.Events {
			if err := queueEvent(ctx, tx, q, event); err != nil {
				return err
			}
		}
		return nil
	})
}

func moveCursor(ctx context.Context, tx *sql.Tx, webhookID, cursor int64) (takesEvents bool, err error) {
	err = tx.QueryRowContext(ctx, `UPDATE webhooks SET cursor = ? WHERE id = ? AND cursor < ? RETURNING disabled_at IS NULL`,
		cursor, webhookID, cursor).Scan(&takesEvents)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return takesEvents, err
}

func queueEvent(ctx context.Context, tx *sql.Tx, q Queue, e Event) error {
	if e.Kind == EventAccountState {
		return openDelivery(ctx, tx, q.WebhookID, e, e.CreatedAt)
	}
	newest, err := newestDelivery(ctx, tx, q.WebhookID, e)
	if err != nil {
		return err
	}
	placement := q.Place(e, newest)
	if placement.Join && newest != nil {
		return joinDelivery(ctx, tx, newest.ID, e, placement.DueAt)
	}
	return openDelivery(ctx, tx, q.WebhookID, e, placement.DueAt)
}

func newestDelivery(ctx context.Context, tx *sql.Tx, webhookID int64, e Event) (*Delivery, error) {
	delivery, err := deliveryWhere(ctx, tx, `d.webhook_id = ? AND d.account_id = ? AND d.chat_jid IS ? ORDER BY d.id DESC LIMIT 1`,
		webhookID, e.AccountID, optionalText(e.ChatJID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return &delivery, err
}

func joinDelivery(ctx context.Context, tx *sql.Tx, id int64, e Event, dueAt time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE webhook_deliveries SET last_seq = ?, event_count = event_count + 1, due_at = ? WHERE id = ?`,
		e.Seq, millis(dueAt), id)
	return err
}

func openDelivery(ctx context.Context, tx *sql.Tx, webhookID int64, e Event, dueAt time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO webhook_deliveries (webhook_id, account_id, chat_jid, first_seq, last_seq, event_count, state, opened_at, due_at)
VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		webhookID, e.AccountID, optionalText(e.ChatJID), e.Seq, e.Seq, DeliveryOpen, millis(e.CreatedAt), millis(dueAt))
	return err
}

func (s *Store) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deliveryColumns+` FROM webhook_deliveries d
WHERE `+sendableDelivery+` AND d.due_at <= ? ORDER BY d.due_at, d.id LIMIT ?`, millis(now), limit)
	if err != nil {
		return nil, err
	}
	var deliveries []Delivery
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, errors.Join(rows.Err(), rows.Close())
}

func (s *Store) NextDeliveryDue(ctx context.Context) (time.Time, bool, error) {
	var due sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MIN(d.due_at) FROM webhook_deliveries d WHERE `+sendableDelivery).Scan(&due)
	return optionalTime(due), due.Valid, err
}

func (s *Store) ClaimDelivery(ctx context.Context, id int64, leaseUntil time.Time) (Delivery, error) {
	var delivery Delivery
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE webhook_deliveries SET attempts = attempts + 1, due_at = ?
WHERE id = ? AND state IN (?, ?) AND webhook_id IN (`+enabledWebhookIDs+`)`,
			millis(leaseUntil), id, DeliveryOpen, DeliveryRetrying)
		if err != nil {
			return err
		}
		if claimed, err := result.RowsAffected(); err != nil || claimed == 0 {
			return errors.Join(ErrNotFound, err)
		}
		delivery, err = deliveryWhere(ctx, tx, `d.id = ?`, id)
		return err
	})
	return delivery, err
}

func (s *Store) RecordAttempt(ctx context.Context, id int64, a AttemptOutcome) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE webhook_deliveries SET state = ?, due_at = coalesce(?, due_at), last_status = ?, last_error = ?, settled_at = ?
WHERE id = ?`,
			a.State, optionalMillis(a.DueAt), optionalStatus(a.Status), optionalText(a.Error), optionalMillis(a.SettledAt), id)
		if err != nil || !a.DisablesWebhook {
			return err
		}
		return disableWebhookOf(ctx, tx, id, a.SettledAt)
	})
}

func disableWebhookOf(ctx context.Context, tx *sql.Tx, deliveryID int64, at time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE webhooks SET disabled_at = ?
WHERE id = (SELECT webhook_id FROM webhook_deliveries WHERE id = ?) AND disabled_at IS NULL`, millis(at), deliveryID)
	return err
}

func (s *Store) RedeliverFailed(ctx context.Context, webhookID int64, failedSince, dueAt time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries SET state = ?, attempts = 0, due_at = ?, settled_at = NULL
WHERE webhook_id = ? AND state = ? AND settled_at >= ?`,
		DeliveryRetrying, millis(dueAt), webhookID, DeliveryFailed, millis(failedSince))
	if err != nil {
		return 0, err
	}
	reopened, err := result.RowsAffected()
	return int(reopened), err
}

func optionalStatus(status int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(status), Valid: status != 0}
}

func deliveryWhere(ctx context.Context, q rowQueryer, where string, args ...any) (Delivery, error) {
	delivery, err := scanDelivery(q.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM webhook_deliveries d WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	return delivery, err
}

func scanDelivery(row scanner) (Delivery, error) {
	var (
		d                     Delivery
		chatJID, lastError    sql.NullString
		openedAt, dueAt       int64
		lastStatus, settledAt sql.NullInt64
	)
	err := row.Scan(&d.ID, &d.WebhookID, &d.AccountID, &chatJID, &d.FirstSeq, &d.LastSeq, &d.EventCount, &d.State, &openedAt, &dueAt,
		&d.Attempts, &lastStatus, &lastError, &settledAt)
	d.ChatJID = chatJID.String
	d.OpenedAt = fromMillis(openedAt)
	d.DueAt = fromMillis(dueAt)
	d.LastStatus = int(lastStatus.Int64)
	d.LastError = lastError.String
	d.SettledAt = optionalTime(settledAt)
	return d, err
}
