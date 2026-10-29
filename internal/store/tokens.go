package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	TokenStatic  = "static"
	TokenAccess  = "access"
	TokenRefresh = "refresh"
)

type Token struct {
	Hash      string
	GrantID   int64
	Kind      string
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (s *Store) InsertToken(ctx context.Context, t Token) error {
	return s.inTx(ctx, func(tx *sql.Tx) error { return insertToken(ctx, tx, t) })
}

type Bearer struct {
	Grant     Grant
	ExpiresAt time.Time
}

func (s *Store) GrantForBearer(ctx context.Context, hash string, now time.Time) (Bearer, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+prefixedGrantColumns+`, t.expires_at
FROM tokens t JOIN grants g ON g.id = t.grant_id
WHERE t.hash = ? AND t.kind IN ('static', 'access') AND g.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > ?)`, hash, millis(now))
	var expiresAt sql.NullInt64
	grant, err := scanGrantWith(row, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Bearer{}, ErrNotFound
	}
	return Bearer{Grant: grant, ExpiresAt: optionalTime(expiresAt)}, err
}

func insertToken(ctx context.Context, tx *sql.Tx, t Token) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO tokens (hash, grant_id, kind, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		t.Hash, t.GrantID, t.Kind, optionalMillis(t.ExpiresAt), millis(t.CreatedAt))
	return err
}

func (s *Store) RotateRefreshToken(ctx context.Context, hash string, now time.Time, issue func(Grant) ([]Token, error)) (Grant, error) {
	var grant Grant
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var (
			expiresAt sql.NullInt64
			rotatedAt sql.NullInt64
			err       error
		)
		grant, err = scanGrantWith(tx.QueryRowContext(ctx, `SELECT `+prefixedGrantColumns+`, t.expires_at, t.rotated_at
FROM tokens t JOIN grants g ON g.id = t.grant_id
WHERE t.hash = ? AND t.kind = 'refresh' AND g.revoked_at IS NULL`, hash), &expiresAt, &rotatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if rotatedAt.Valid {
			return commitAndFail(revokeReused(ctx, tx, sql.NullInt64{Int64: grant.ID, Valid: true}, now))
		}
		if !optionalTime(expiresAt).After(now) {
			return ErrNotFound
		}
		tokens, err := issue(grant)
		if err != nil {
			return err
		}
		return replaceRefreshToken(ctx, tx, grant.ID, hash, now, tokens)
	})
	return grant, err
}

func replaceRefreshToken(ctx context.Context, tx *sql.Tx, grantID int64, hash string, now time.Time, tokens []Token) error {
	if _, err := tx.ExecContext(ctx, `UPDATE tokens SET rotated_at = ? WHERE hash = ?`, millis(now), hash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tokens WHERE grant_id = ? AND expires_at IS NOT NULL AND expires_at <= ?`, grantID, millis(now)); err != nil {
		return err
	}
	for _, token := range tokens {
		token.GrantID = grantID
		if err := insertToken(ctx, tx, token); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RevokeGrantOfToken(ctx context.Context, hash string, at time.Time, allowed func(Grant) bool) (Grant, error) {
	var grant Grant
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		active, err := scanGrant(tx.QueryRowContext(ctx, `SELECT `+prefixedGrantColumns+`
FROM tokens t JOIN grants g ON g.id = t.grant_id
WHERE t.hash = ? AND t.kind IN ('access', 'refresh') AND g.kind = 'oauth' AND g.revoked_at IS NULL`, hash))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !allowed(active) {
			return ErrNotFound
		}
		grant, err = revokeGrant(ctx, tx, active.ID, at)
		return err
	})
	return grant, err
}
