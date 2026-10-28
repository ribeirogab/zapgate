package store

import (
	"context"
	"time"
)

func (s *Store) SpendApproval(ctx context.Context, nonce string, expires, now time.Time) (bool, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM spent_approvals WHERE expires_at < ?`, millis(now)); err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO spent_approvals (nonce, expires_at) VALUES (?, ?)`, nonce, millis(expires))
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	return inserted == 1, err
}
