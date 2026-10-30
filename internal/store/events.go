package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	EventMessageReceived = "message.received"
	EventAccountState    = "account.state"
)

type Event struct {
	Seq       int64
	AccountID int64
	Kind      string
	ChatJID   string
	MessageID string
	State     string
	CreatedAt time.Time
}

const eventColumns = `e.seq, e.account_id, e.kind, e.chat_jid, e.message_id, e.state, e.created_at`

func journalReceived(ctx context.Context, tx *sql.Tx, accountID int64, msg Message, at time.Time) error {
	if msg.FromMe {
		return nil
	}
	return insertEvent(ctx, tx, Event{AccountID: accountID, Kind: EventMessageReceived, ChatJID: msg.ChatJID, MessageID: msg.ID, CreatedAt: at})
}

func journalState(ctx context.Context, tx *sql.Tx, accountID int64, state string, at time.Time) error {
	if state == AccountPairing {
		return nil
	}
	return insertEvent(ctx, tx, Event{AccountID: accountID, Kind: EventAccountState, State: state, CreatedAt: at})
}

func insertEvent(ctx context.Context, tx *sql.Tx, e Event) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO events (account_id, kind, chat_jid, message_id, state, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		e.AccountID, e.Kind, optionalText(e.ChatJID), optionalText(e.MessageID), optionalText(e.State), millis(e.CreatedAt))
	return err
}

func (s *Store) EventsAfter(ctx context.Context, seq int64, limit int) ([]Event, error) {
	return queryEvents(ctx, s.db, `SELECT `+eventColumns+` FROM events e WHERE e.seq > ? ORDER BY e.seq LIMIT ?`, seq, limit)
}

func (s *Store) DeliveryEvents(ctx context.Context, d Delivery) ([]Event, error) {
	return queryEvents(ctx, s.db, `SELECT `+eventColumns+` FROM events e
WHERE e.account_id = ? AND e.chat_jid IS ? AND e.seq BETWEEN ? AND ? ORDER BY e.seq`,
		d.AccountID, optionalText(d.ChatJID), d.FirstSeq, d.LastSeq)
}

func queryEvents(ctx context.Context, q queryer, query string, args ...any) ([]Event, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var events []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, event)
	}
	return events, errors.Join(rows.Err(), rows.Close())
}

func scanEvent(row scanner) (Event, error) {
	var (
		e                         Event
		chatJID, messageID, state sql.NullString
		createdAt                 int64
	)
	err := row.Scan(&e.Seq, &e.AccountID, &e.Kind, &chatJID, &messageID, &state, &createdAt)
	e.ChatJID = chatJID.String
	e.MessageID = messageID.String
	e.State = state.String
	e.CreatedAt = fromMillis(createdAt)
	return e, err
}
