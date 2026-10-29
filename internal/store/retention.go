package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const RetentionBatch = 1000

const expiringMessages = `SELECT rowid FROM messages WHERE account_id = ? AND sent_at < ? ORDER BY sent_at LIMIT ?`

type ExpiredBatch struct {
	Messages int
	Files    int
}

func (s *Store) DeleteMessagesBefore(ctx context.Context, accountID int64, before time.Time, removeFiles func(paths []string) error) (ExpiredBatch, error) {
	var batch ExpiredBatch
	args := []any{accountID, millis(before), RetentionBatch}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		paths, err := expiringMediaPaths(ctx, tx, args)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE rowid IN (`+expiringMessages+`)`, args...)
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if err := removeFiles(paths); err != nil {
			return err
		}
		batch = ExpiredBatch{Messages: int(deleted), Files: len(paths)}
		return nil
	})
	return batch, err
}

func expiringMediaPaths(ctx context.Context, tx *sql.Tx, args []any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT d.path FROM media d
JOIN messages m ON m.account_id = d.account_id AND m.chat_jid = d.chat_jid AND m.id = d.message_id
WHERE m.rowid IN (`+expiringMessages+`) AND d.path IS NOT NULL`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func (s *Store) CountMessages(ctx context.Context, accountID int64) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE account_id = ?`, accountID).Scan(&count)
	return count, err
}

func (s *Store) SetAccountRetention(ctx context.Context, name string, months int) (Account, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE accounts SET retention_months = ? WHERE name = ?`, sql.NullInt64{Int64: int64(months), Valid: months > 0}, name)
	if err != nil {
		return Account{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return Account{}, errors.Join(ErrNotFound, err)
	}
	return s.AccountByName(ctx, name)
}
