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
	ListedChats         func(accountID int64) []string
	CreatedAt           time.Time
}

type WebhookChat struct {
	AccountID int64
	ChatJID   string
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
	if err := linkWebhookChats(ctx, tx, id, accountIDs, n.ListedChats); err != nil {
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

func linkWebhookChats(ctx context.Context, tx *sql.Tx, webhookID int64, accountIDs []int64, listed func(accountID int64) []string) error {
	if listed == nil {
		return nil
	}
	for _, accountID := range accountIDs {
		for _, chat := range listed(accountID) {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO webhook_chats (webhook_id, account_id, chat_jid) VALUES (?, ?, ?)`, webhookID, accountID, chat); err != nil {
				return err
			}
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

type WebhookChange struct {
	Webhook             Webhook
	ChangeAccounts      bool
	AllExistingAccounts bool
	AccountNames        []string
	ListedChats         func(accountID int64) []string
}

func (s *Store) ChangeWebhook(ctx context.Context, c WebhookChange) (Webhook, error) {
	var webhook Webhook
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if err := updateWebhook(ctx, tx, c.Webhook); err != nil {
			return err
		}
		if c.ChangeAccounts {
			if err := replaceWebhookAccounts(ctx, tx, c); err != nil {
				return err
			}
		}
		if err := replaceWebhookChats(ctx, tx, c.Webhook.ID, c.ListedChats); err != nil {
			return err
		}
		var err error
		webhook, err = webhookWhere(ctx, tx, `w.id = ?`, c.Webhook.ID)
		return err
	})
	return webhook, err
}

func updateWebhook(ctx context.Context, tx *sql.Tx, w Webhook) error {
	headers, err := encodeHeaders(w.Headers)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE webhooks SET url = ?, format = ?, content = ?, events = ?, chats = ?, quiet_ms = ?, max_wait_ms = ?, headers = ? WHERE id = ?`,
		w.URL, w.Format, w.Content, strings.Join(w.Events, eventsSeparator), w.Chats, w.Quiet.Milliseconds(), w.MaxWait.Milliseconds(), headers, w.ID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return errors.Join(ErrNotFound, err)
	}
	return nil
}

func replaceWebhookAccounts(ctx context.Context, tx *sql.Tx, c WebhookChange) error {
	accountIDs, err := chosenAccountIDs(ctx, tx, c.AllExistingAccounts, c.AccountNames)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM webhook_accounts WHERE webhook_id = ?`, c.Webhook.ID); err != nil {
		return err
	}
	return linkWebhookAccounts(ctx, tx, c.Webhook.ID, accountIDs)
}

func replaceWebhookChats(ctx context.Context, tx *sql.Tx, webhookID int64, listed func(accountID int64) []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM webhook_chats WHERE webhook_id = ?`, webhookID); err != nil {
		return err
	}
	accountIDs, err := queryIDs(ctx, tx, `SELECT account_id FROM webhook_accounts WHERE webhook_id = ?`, webhookID)
	if err != nil {
		return err
	}
	return linkWebhookChats(ctx, tx, webhookID, accountIDs, listed)
}

func (s *Store) WebhookChats(ctx context.Context, w Webhook) ([]WebhookChat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account_id, chat_jid FROM webhook_chats WHERE webhook_id = ? ORDER BY chat_jid, account_id`, w.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chats []WebhookChat
	for rows.Next() {
		var chat WebhookChat
		if err := rows.Scan(&chat.AccountID, &chat.ChatJID); err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}
	return chats, rows.Err()
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

func (s *Store) RotateWebhookSecret(ctx context.Context, name, secret string, previousSecretUntil time.Time) (Webhook, error) {
	return s.updateWebhook(ctx, name, `previous_secret = secret, previous_secret_until = ?, secret = ?`, millis(previousSecretUntil), secret)
}

func (s *Store) EnableWebhook(ctx context.Context, name string) (Webhook, error) {
	return s.updateWebhook(ctx, name, `disabled_at = NULL,
  cursor = CASE WHEN disabled_at IS NULL THEN cursor ELSE max(cursor, (SELECT coalesce(max(seq), 0) FROM events)) END`)
}

func (s *Store) updateWebhook(ctx context.Context, name, set string, args ...any) (Webhook, error) {
	var webhook Webhook
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE webhooks SET `+set+` WHERE name = ?`, append(args, name)...)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed == 0 {
			return errors.Join(ErrNotFound, err)
		}
		webhook, err = webhookWhere(ctx, tx, `w.name = ?`, name)
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
