package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type DeliverySummary struct {
	Pending     int
	Failed      int
	LastSuccess time.Time
	LastFailure AttemptFailure
}

type AttemptFailure struct {
	Status int
	Error  string
}

func (f AttemptFailure) Known() bool {
	return f.Status != 0 || f.Error != ""
}

func (s *Store) SummarizeDeliveries(ctx context.Context, webhookID int64) (DeliverySummary, error) {
	var (
		summary     DeliverySummary
		lastSuccess sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `SELECT
  coalesce(sum(state IN (?, ?)), 0),
  coalesce(sum(state = ?), 0),
  max(CASE WHEN state = ? THEN settled_at END)
FROM webhook_deliveries WHERE webhook_id = ?`,
		DeliveryOpen, DeliveryRetrying, DeliveryFailed, DeliveryDelivered, webhookID).Scan(&summary.Pending, &summary.Failed, &lastSuccess)
	if err != nil {
		return DeliverySummary{}, err
	}
	summary.LastSuccess = optionalTime(lastSuccess)
	summary.LastFailure, err = s.lastFailure(ctx, webhookID)
	return summary, err
}

func (s *Store) lastFailure(ctx context.Context, webhookID int64) (AttemptFailure, error) {
	var (
		status  sql.NullInt64
		message sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT last_status, last_error FROM webhook_deliveries
WHERE webhook_id = ? AND state IN (?, ?) ORDER BY id DESC LIMIT 1`, webhookID, DeliveryRetrying, DeliveryFailed).Scan(&status, &message)
	if errors.Is(err, sql.ErrNoRows) {
		return AttemptFailure{}, nil
	}
	return AttemptFailure{Status: int(status.Int64), Error: message.String}, err
}
