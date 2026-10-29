package store

import (
	"context"
	"time"
)

type SearchQuery struct {
	Match   string
	ChatJID string
	Since   time.Time
	Until   time.Time
	Before  *SearchPosition
	Limit   int
}

type SearchPosition struct {
	SentAt  time.Time
	ChatJID string
	ID      string
}

func (s *Store) SearchMessages(ctx context.Context, accountID int64, q SearchQuery) ([]Message, error) {
	query := messageSelect + `
JOIN message_index i ON i.account_id = m.account_id AND i.chat_jid = m.chat_jid AND i.message_id = m.id
WHERE i.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?) AND m.account_id = ?`
	args := []any{q.Match, accountID}
	if q.ChatJID != "" {
		query += ` AND m.chat_jid = ?`
		args = append(args, q.ChatJID)
	}
	if !q.Since.IsZero() {
		query += ` AND m.sent_at >= ?`
		args = append(args, millis(q.Since))
	}
	if !q.Until.IsZero() {
		query += ` AND m.sent_at < ?`
		args = append(args, millis(q.Until))
	}
	if q.Before != nil {
		at := millis(q.Before.SentAt)
		query += ` AND (m.sent_at < ? OR (m.sent_at = ? AND (m.id < ? OR (m.id = ? AND m.chat_jid < ?))))`
		args = append(args, at, at, q.Before.ID, q.Before.ID, q.Before.ChatJID)
	}
	query += ` ORDER BY m.sent_at DESC, m.id DESC, m.chat_jid DESC LIMIT ?`
	return s.queryMessages(ctx, accountID, query, append(args, q.Limit)...)
}
