package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ribeirogab/zapgate/internal/sqlite"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrNameInUse      = errors.New("name in use")
	ErrUnknownAccount = errors.New("unknown account")
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open(sqlite.Driver, sqlite.DSN(path))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrate(ctx, db, embeddedMigrations); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

type committedError struct {
	err error
}

func (e *committedError) Error() string { return e.err.Error() }

func commitAndFail(err error) error {
	return &committedError{err: err}
}

func (s *Store) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	err = fn(tx)
	var committed *committedError
	if errors.As(err, &committed) {
		return errors.Join(tx.Commit(), committed.err)
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func millis(t time.Time) int64 {
	return t.UnixMilli()
}

func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

func optionalTime(ms sql.NullInt64) time.Time {
	if !ms.Valid {
		return time.Time{}
	}
	return fromMillis(ms.Int64)
}

func optionalMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: millis(t), Valid: true}
}
