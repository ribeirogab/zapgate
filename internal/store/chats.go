package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	ChatDirect = "direct"
	ChatGroup  = "group"
)

type Chat struct {
	JID           string
	Kind          string
	Name          string
	LastMessageAt time.Time
	UnreadCount   int
}

type ChatCursor struct {
	LastMessageAt time.Time
	JID           string
}

type ChatQuery struct {
	UnreadOnly bool
	Match      func(Chat) bool
	After      *ChatCursor
	Limit      int
}

const chatColumns = `jid, kind, name, last_message_at, unread_count`

func (s *Store) Chat(ctx context.Context, accountID int64, jid string) (Chat, error) {
	chat, err := scanChat(s.db.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chats WHERE account_id = ? AND jid = ?`, accountID, jid))
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, ErrNotFound
	}
	return chat, err
}

func (s *Store) ListChats(ctx context.Context, accountID int64, q ChatQuery) ([]Chat, error) {
	query := `SELECT ` + chatColumns + ` FROM chats WHERE account_id = ?`
	args := []any{accountID}
	if q.UnreadOnly {
		query += ` AND unread_count > 0`
	}
	if q.After != nil {
		at := millis(q.After.LastMessageAt)
		query += ` AND (coalesce(last_message_at, 0) < ? OR (coalesce(last_message_at, 0) = ? AND jid > ?))`
		args = append(args, at, at, q.After.JID)
	}
	query += ` ORDER BY coalesce(last_message_at, 0) DESC, jid`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chats := []Chat{}
	for len(chats) < q.Limit && rows.Next() {
		chat, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		if q.Match == nil || q.Match(chat) {
			chats = append(chats, chat)
		}
	}
	return chats, rows.Err()
}

func (s *Store) SetChatName(ctx context.Context, accountID int64, jid, name string) error {
	if name == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET name = ? WHERE account_id = ? AND jid = ?`, name, accountID, jid)
	return err
}

func (s *Store) SaveChat(ctx context.Context, accountID int64, chat Chat) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		return upsertChat(ctx, tx, accountID, chat)
	})
}

func (s *Store) MarkChatReadUntil(ctx context.Context, accountID int64, jid string, readAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET unread_count = (
  SELECT count(*) FROM messages m WHERE m.account_id = chats.account_id AND m.chat_jid = chats.jid AND m.from_me = 0 AND m.sent_at > ?
) WHERE account_id = ? AND jid = ?`, millis(readAt), accountID, jid)
	return err
}

func (s *Store) SetUnreadCount(ctx context.Context, accountID int64, jid string, count int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET unread_count = ? WHERE account_id = ? AND jid = ?`, count, accountID, jid)
	return err
}

func (s *Store) MergeIdentity(ctx context.Context, accountID int64, lid, phone string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := mergeChat(ctx, tx, accountID, lid, phone); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET sender_jid = ? WHERE account_id = ? AND sender_jid = ?`, phone, accountID, lid); err != nil {
			return err
		}
		if err := mergeReactions(ctx, tx, accountID, lid, phone); err != nil {
			return err
		}
		return mergeWebhookChats(ctx, tx, accountID, lid, phone)
	})
}

func mergeWebhookChats(ctx context.Context, tx *sql.Tx, accountID int64, lid, phone string) error {
	for _, move := range []string{
		`UPDATE events SET chat_jid = ? WHERE account_id = ? AND chat_jid = ?`,
		`UPDATE webhook_deliveries SET chat_jid = ? WHERE account_id = ? AND chat_jid = ?`,
		`UPDATE OR IGNORE webhook_chats SET chat_jid = ? WHERE account_id = ? AND chat_jid = ?`,
	} {
		if _, err := tx.ExecContext(ctx, move, phone, accountID, lid); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM webhook_chats WHERE account_id = ? AND chat_jid = ?`, accountID, lid)
	return err
}

func mergeReactions(ctx context.Context, tx *sql.Tx, accountID int64, lid, phone string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE OR IGNORE reactions SET sender_jid = ? WHERE account_id = ? AND sender_jid = ?`, phone, accountID, lid); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM reactions WHERE account_id = ? AND sender_jid = ?`, accountID, lid)
	return err
}

func mergeChat(ctx context.Context, tx *sql.Tx, accountID int64, lid, phone string) error {
	old, err := scanChat(tx.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chats WHERE account_id = ? AND jid = ?`, accountID, lid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE OR IGNORE chats SET jid = ? WHERE account_id = ? AND jid = ?`, phone, accountID, lid)
	if err != nil {
		return err
	}
	if moved, err := result.RowsAffected(); err != nil || moved == 1 {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE OR IGNORE messages SET chat_jid = ? WHERE account_id = ? AND chat_jid = ?`, phone, accountID, lid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chats WHERE account_id = ? AND jid = ?`, accountID, lid); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE chats SET
  name = coalesce(name, ?),
  last_message_at = max(coalesce(last_message_at, 0), ?),
  unread_count = unread_count + ?
WHERE account_id = ? AND jid = ?`, optionalText(old.Name), millis(old.LastMessageAt), old.UnreadCount, accountID, phone)
	return err
}

func upsertChat(ctx context.Context, tx *sql.Tx, accountID int64, chat Chat) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO chats (account_id, jid, kind, name, last_message_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (account_id, jid) DO UPDATE SET
  name = coalesce(excluded.name, chats.name),
  last_message_at = nullif(max(coalesce(excluded.last_message_at, 0), coalesce(chats.last_message_at, 0)), 0)`,
		accountID, chat.JID, chat.Kind, optionalText(chat.Name), optionalMillis(chat.LastMessageAt))
	return err
}

func scanChat(row scanner) (Chat, error) {
	var (
		chat          Chat
		name          sql.NullString
		lastMessageAt sql.NullInt64
	)
	err := row.Scan(&chat.JID, &chat.Kind, &name, &lastMessageAt, &chat.UnreadCount)
	chat.Name = name.String
	chat.LastMessageAt = optionalTime(lastMessageAt)
	return chat, err
}
