package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type Reaction struct {
	ChatJID    string
	MessageID  string
	SenderJID  string
	SenderName string
	FromMe     bool
	Emoji      string
	ReactedAt  time.Time
}

func (s *Store) React(ctx context.Context, accountID int64, r Reaction) error {
	if r.Emoji == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM reactions WHERE account_id = ? AND chat_jid = ? AND message_id = ? AND sender_jid = ? AND reacted_at <= ?`,
			accountID, r.ChatJID, r.MessageID, r.SenderJID, millis(r.ReactedAt))
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reactions (account_id, chat_jid, message_id, sender_jid, sender_name, from_me, emoji, reacted_at)
SELECT ?, ?, ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM messages WHERE account_id = ? AND chat_jid = ? AND id = ?)
ON CONFLICT (account_id, chat_jid, message_id, sender_jid) DO UPDATE SET
  emoji = excluded.emoji,
  sender_name = coalesce(excluded.sender_name, reactions.sender_name),
  reacted_at = excluded.reacted_at
WHERE excluded.reacted_at >= reactions.reacted_at`,
		accountID, r.ChatJID, r.MessageID, r.SenderJID, optionalText(r.SenderName), r.FromMe, r.Emoji, millis(r.ReactedAt),
		accountID, r.ChatJID, r.MessageID)
	return err
}

func (s *Store) attachReactions(ctx context.Context, accountID int64, messages []Message) error {
	if len(messages) == 0 {
		return nil
	}
	keys := make([]string, 0, len(messages))
	args := []any{accountID}
	index := map[[2]string]int{}
	for i, msg := range messages {
		keys = append(keys, `(?, ?)`)
		args = append(args, msg.ChatJID, msg.ID)
		index[[2]string{msg.ChatJID, msg.ID}] = i
	}
	rows, err := s.db.QueryContext(ctx, `SELECT chat_jid, message_id, sender_jid, sender_name, from_me, emoji, reacted_at FROM reactions
WHERE account_id = ? AND (chat_jid, message_id) IN (VALUES `+strings.Join(keys, `, `)+`)
ORDER BY reacted_at, sender_jid`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			r          Reaction
			senderName sql.NullString
			reactedAt  int64
		)
		if err := rows.Scan(&r.ChatJID, &r.MessageID, &r.SenderJID, &senderName, &r.FromMe, &r.Emoji, &reactedAt); err != nil {
			return err
		}
		r.SenderName = senderName.String
		r.ReactedAt = fromMillis(reactedAt)
		i := index[[2]string{r.ChatJID, r.MessageID}]
		messages[i].Reactions = append(messages[i].Reactions, r)
	}
	return rows.Err()
}
