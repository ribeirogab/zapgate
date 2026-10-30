package store

import (
	"context"
	"time"
)

const unsettledDeliveryEvent = `EXISTS (
  SELECT 1 FROM webhook_deliveries d
  WHERE d.state IN ('open', 'retrying') AND d.account_id = e.account_id AND d.chat_jid IS e.chat_jid
    AND e.seq BETWEEN d.first_seq AND d.last_seq)`

func (s *Store) DeleteEventsBefore(ctx context.Context, before time.Time) (int, error) {
	return s.deleteBatch(ctx, `DELETE FROM events WHERE seq IN (
  SELECT e.seq FROM events e WHERE e.created_at < ? AND NOT `+unsettledDeliveryEvent+` ORDER BY e.created_at LIMIT ?)`,
		millis(before), RetentionBatch)
}

func (s *Store) DeleteDeliveriesSettledBefore(ctx context.Context, before time.Time) (int, error) {
	return s.deleteBatch(ctx, `DELETE FROM webhook_deliveries WHERE id IN (
  SELECT id FROM webhook_deliveries WHERE settled_at < ? AND state IN ('delivered', 'failed') ORDER BY settled_at LIMIT ?)`,
		millis(before), RetentionBatch)
}

func (s *Store) deleteBatch(ctx context.Context, query string, args ...any) (int, error) {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	return int(deleted), err
}
