package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const MessageText = "text"

type Message struct {
	ChatJID    string
	ID         string
	SenderJID  string
	SenderName string
	FromMe     bool
	SentAt     time.Time
	Kind       string
	Text       string
	QuotedID   string
	EditedAt   time.Time
	RevokedAt  time.Time
	Media      *Media
	Details    *Details
	Reactions  []Reaction
}

type MessageBound struct {
	SentAt time.Time
	ID     string
}

type MessageQuery struct {
	Before *MessageBound
	After  *MessageBound
	Limit  int
}

const messageSelect = `SELECT m.chat_jid, m.id, m.sender_jid, m.sender_name, m.from_me, m.sent_at, m.kind, m.text, m.quoted_id, m.edited_at, m.revoked_at,
  d.mime, d.file_name, d.size, d.seconds, d.voice_note, d.state, x.details
FROM messages m
LEFT JOIN media d ON d.account_id = m.account_id AND d.chat_jid = m.chat_jid AND d.message_id = m.id
LEFT JOIN message_details x ON x.account_id = m.account_id AND x.chat_jid = m.chat_jid AND x.message_id = m.id`

func (s *Store) SaveMessage(ctx context.Context, accountID int64, chat Chat, msg Message) error {
	return s.saveMessage(ctx, accountID, chat, msg, true)
}

func (s *Store) SaveHistoryMessage(ctx context.Context, accountID int64, chat Chat, msg Message) error {
	return s.saveMessage(ctx, accountID, chat, msg, false)
}

func (s *Store) saveMessage(ctx context.Context, accountID int64, chat Chat, msg Message, live bool) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		chat.LastMessageAt = msg.SentAt
		if err := upsertChat(ctx, tx, accountID, chat); err != nil {
			return err
		}
		inserted, err := insertMessage(ctx, tx, accountID, msg)
		if err != nil || !inserted {
			return err
		}
		if err := insertParts(ctx, tx, accountID, msg); err != nil {
			return err
		}
		if !live {
			return nil
		}
		return countUnread(ctx, tx, accountID, msg)
	})
}

func insertMessage(ctx context.Context, tx *sql.Tx, accountID int64, msg Message) (bool, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO messages (account_id, chat_jid, id, sender_jid, sender_name, from_me, sent_at, kind, text, quoted_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (account_id, chat_jid, id) DO NOTHING`,
		accountID, msg.ChatJID, msg.ID, msg.SenderJID, optionalText(msg.SenderName), msg.FromMe, millis(msg.SentAt), msg.Kind, optionalText(msg.Text), optionalText(msg.QuotedID))
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	return inserted == 1, err
}

func countUnread(ctx context.Context, tx *sql.Tx, accountID int64, msg Message) error {
	if msg.FromMe {
		_, err := tx.ExecContext(ctx, `UPDATE chats SET unread_count = 0 WHERE account_id = ? AND jid = ? AND last_message_at <= ?`, accountID, msg.ChatJID, millis(msg.SentAt))
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE chats SET unread_count = unread_count + 1 WHERE account_id = ? AND jid = ?`, accountID, msg.ChatJID)
	return err
}

func (s *Store) Message(ctx context.Context, accountID int64, chatJID, id string) (Message, error) {
	msg, err := scanMessage(s.db.QueryRowContext(ctx, messageSelect+` WHERE m.account_id = ? AND m.chat_jid = ? AND m.id = ?`, accountID, chatJID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	return msg, err
}

func (s *Store) Messages(ctx context.Context, accountID int64, chatJID string, q MessageQuery) ([]Message, error) {
	where, args := messageRange(accountID, chatJID, q)
	order := `DESC`
	if q.After != nil {
		order = `ASC`
	}
	messages, err := s.queryMessages(ctx, accountID, messageSelect+` WHERE `+where+
		` ORDER BY m.sent_at `+order+`, m.id `+order+` LIMIT ?`, append(args, q.Limit)...)
	if order == `DESC` {
		reverse(messages)
	}
	return messages, err
}

func (s *Store) OldestMessage(ctx context.Context, accountID int64, chatJID string) (Message, error) {
	messages, err := s.queryMessages(ctx, accountID, messageSelect+` WHERE m.account_id = ? AND m.chat_jid = ? ORDER BY m.sent_at, m.id LIMIT 1`, accountID, chatJID)
	if err != nil {
		return Message{}, err
	}
	if len(messages) == 0 {
		return Message{}, ErrNotFound
	}
	return messages[0], nil
}

func (s *Store) queryMessages(ctx context.Context, accountID int64, query string, args ...any) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	messages := []Message{}
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		messages = append(messages, msg)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return messages, s.attachReactions(ctx, accountID, messages)
}

func (s *Store) HasMessages(ctx context.Context, accountID int64, chatJID string, q MessageQuery) (bool, error) {
	where, args := messageRange(accountID, chatJID, q)
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM messages m WHERE `+where+`)`, args...).Scan(&found)
	return found, err
}

func (s *Store) EditMessage(ctx context.Context, accountID int64, chatJID, id, senderJID, text string, editedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET text = ?, edited_at = ?
WHERE account_id = ? AND chat_jid = ? AND id = ? AND sender_jid = ? AND coalesce(edited_at, 0) <= ?`,
		optionalText(text), millis(editedAt), accountID, chatJID, id, senderJID, millis(editedAt))
	return err
}

func (s *Store) RevokeMessage(ctx context.Context, accountID int64, chatJID, id, senderJID string, byAdmin bool, revokedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET revoked_at = ?
WHERE account_id = ? AND chat_jid = ? AND id = ? AND revoked_at IS NULL AND (sender_jid = ? OR ?)`,
		millis(revokedAt), accountID, chatJID, id, senderJID, byAdmin)
	return err
}

func messageRange(accountID int64, chatJID string, q MessageQuery) (string, []any) {
	where := `m.account_id = ? AND m.chat_jid = ?`
	args := []any{accountID, chatJID}
	if q.Before != nil {
		clause, boundArgs := boundClause(`<`, *q.Before)
		where += ` AND ` + clause
		args = append(args, boundArgs...)
	}
	if q.After != nil {
		clause, boundArgs := boundClause(`>`, *q.After)
		where += ` AND ` + clause
		args = append(args, boundArgs...)
	}
	return where, args
}

func boundClause(op string, bound MessageBound) (string, []any) {
	at := millis(bound.SentAt)
	if bound.ID == "" {
		return `m.sent_at ` + op + ` ?`, []any{at}
	}
	return `(m.sent_at ` + op + ` ? OR (m.sent_at = ? AND m.id ` + op + ` ?))`, []any{at, at, bound.ID}
}

func reverse(messages []Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

func scanMessage(row scanner) (Message, error) {
	var (
		msg                 Message
		senderName, text    sql.NullString
		quotedID            sql.NullString
		sentAt              int64
		editedAt, revokedAt sql.NullInt64
		parts               partColumns
	)
	err := row.Scan(&msg.ChatJID, &msg.ID, &msg.SenderJID, &senderName, &msg.FromMe, &sentAt, &msg.Kind, &text, &quotedID, &editedAt, &revokedAt,
		&parts.mime, &parts.fileName, &parts.size, &parts.seconds, &parts.voiceNote, &parts.state, &parts.details)
	if err != nil {
		return msg, err
	}
	msg.SenderName = senderName.String
	msg.Text = text.String
	msg.QuotedID = quotedID.String
	msg.SentAt = fromMillis(sentAt)
	msg.EditedAt = optionalTime(editedAt)
	msg.RevokedAt = optionalTime(revokedAt)
	return msg, parts.attachTo(&msg)
}
