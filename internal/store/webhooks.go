package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Webhook struct {
	ID                  int64
	Name                string
	URL                 string
	Format              string
	Content             string
	Events              []string
	Chats               string
	Quiet               time.Duration
	MaxWait             time.Duration
	Secret              string
	PreviousSecret      string
	PreviousSecretUntil time.Time
	Headers             map[string]string
	Cursor              int64
	CreatedAt           time.Time
	DisabledAt          time.Time
}

func (w Webhook) Disabled() bool { return !w.DisabledAt.IsZero() }

type NewWebhook struct {
	Name                string
	URL                 string
	Format              string
	Content             string
	Events              []string
	Chats               string
	Quiet               time.Duration
	MaxWait             time.Duration
	Secret              string
	Headers             map[string]string
	AllExistingAccounts bool
	AccountNames        []string
	CreatedAt           time.Time
}

const (
	webhookColumns = `w.id, w.name, w.url, w.format, w.content, w.events, w.chats, w.quiet_ms, w.max_wait_ms, w.secret,
  w.previous_secret, w.previous_secret_until, w.headers, w.cursor, w.created_at, w.disabled_at`
	eventsSeparator = " "
)

func (s *Store) CreateWebhook(ctx context.Context, n NewWebhook) (Webhook, error) {
	var webhook Webhook
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		webhook, err = createWebhook(ctx, tx, n)
		return err
	})
	return webhook, err
}

func createWebhook(ctx context.Context, tx *sql.Tx, n NewWebhook) (Webhook, error) {
	if err := ensureWebhookNameFree(ctx, tx, n.Name); err != nil {
		return Webhook{}, err
	}
	accountIDs, err := chosenAccountIDs(ctx, tx, n.AllExistingAccounts, n.AccountNames)
	if err != nil {
		return Webhook{}, err
	}
	cursor, err := lastEventSeq(ctx, tx)
	if err != nil {
		return Webhook{}, err
	}
	id, err := insertWebhook(ctx, tx, n, cursor)
	if err != nil {
		return Webhook{}, err
	}
	if err := linkWebhookAccounts(ctx, tx, id, accountIDs); err != nil {
		return Webhook{}, err
	}
	return webhookWhere(ctx, tx, `w.id = ?`, id)
}

func ensureWebhookNameFree(ctx context.Context, tx *sql.Tx, name string) error {
	_, err := webhookWhere(ctx, tx, `w.name = ?`, name)
	if err == nil {
		return fmt.Errorf("%w: %s", ErrNameInUse, name)
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func lastEventSeq(ctx context.Context, q rowQueryer) (int64, error) {
	var seq int64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&seq)
	return seq, err
}

func insertWebhook(ctx context.Context, tx *sql.Tx, n NewWebhook, cursor int64) (int64, error) {
	headers, err := encodeHeaders(n.Headers)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO webhooks (name, url, format, content, events, chats, quiet_ms, max_wait_ms, secret, headers, cursor, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name, n.URL, n.Format, n.Content, strings.Join(n.Events, eventsSeparator), n.Chats,
		n.Quiet.Milliseconds(), n.MaxWait.Milliseconds(), n.Secret, headers, cursor, millis(n.CreatedAt))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func linkWebhookAccounts(ctx context.Context, tx *sql.Tx, webhookID int64, accountIDs []int64) error {
	for _, accountID := range accountIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO webhook_accounts (webhook_id, account_id) VALUES (?, ?)`, webhookID, accountID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) WebhookByName(ctx context.Context, name string) (Webhook, error) {
	return webhookWhere(ctx, s.db, `w.name = ?`, name)
}

func (s *Store) Webhook(ctx context.Context, id int64) (Webhook, error) {
	return webhookWhere(ctx, s.db, `w.id = ?`, id)
}

func (s *Store) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+webhookColumns+` FROM webhooks w ORDER BY w.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	webhooks := []Webhook{}
	for rows.Next() {
		webhook, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, webhook)
	}
	return webhooks, rows.Err()
}

func (s *Store) WebhookAccounts(ctx context.Context, w Webhook) ([]Account, error) {
	return queryAccounts(ctx, s.db, `SELECT `+accountColumns+` FROM accounts a JOIN webhook_accounts wa ON wa.account_id = a.id WHERE wa.webhook_id = ? ORDER BY a.name`, w.ID)
}

func (s *Store) DeleteWebhook(ctx context.Context, name string) (Webhook, error) {
	var webhook Webhook
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		webhook, err = webhookWhere(ctx, tx, `w.name = ?`, name)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, webhook.ID)
		return err
	})
	return webhook, err
}

func webhookWhere(ctx context.Context, q rowQueryer, where string, args ...any) (Webhook, error) {
	webhook, err := scanWebhook(q.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhooks w WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	return webhook, err
}

func scanWebhook(row scanner) (Webhook, error) {
	var (
		w                   Webhook
		events              string
		quietMS, maxWaitMS  int64
		previousSecret      sql.NullString
		previousSecretUntil sql.NullInt64
		headers             sql.NullString
		createdAt           int64
		disabledAt          sql.NullInt64
	)
	err := row.Scan(&w.ID, &w.Name, &w.URL, &w.Format, &w.Content, &events, &w.Chats, &quietMS, &maxWaitMS, &w.Secret,
		&previousSecret, &previousSecretUntil, &headers, &w.Cursor, &createdAt, &disabledAt)
	if err != nil {
		return Webhook{}, err
	}
	w.Events = strings.Fields(events)
	w.Quiet = time.Duration(quietMS) * time.Millisecond
	w.MaxWait = time.Duration(maxWaitMS) * time.Millisecond
	w.PreviousSecret = previousSecret.String
	w.PreviousSecretUntil = optionalTime(previousSecretUntil)
	w.CreatedAt = fromMillis(createdAt)
	w.DisabledAt = optionalTime(disabledAt)
	w.Headers, err = decodeHeaders(headers)
	return w, err
}

func encodeHeaders(headers map[string]string) (sql.NullString, error) {
	if len(headers) == 0 {
		return sql.NullString{}, nil
	}
	encoded, err := json.Marshal(headers)
	return sql.NullString{String: string(encoded), Valid: true}, err
}

func decodeHeaders(value sql.NullString) (map[string]string, error) {
	if !value.Valid {
		return nil, nil
	}
	var headers map[string]string
	err := json.Unmarshal([]byte(value.String), &headers)
	return headers, err
}
