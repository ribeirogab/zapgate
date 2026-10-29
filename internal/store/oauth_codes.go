package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const usedCodeRetention = 24 * time.Hour

type AuthCode struct {
	Hash          string
	ClientID      string
	GrantName     string
	AccountNames  []string
	CodeChallenge string
	RedirectURI   string
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

type ReusedError struct {
	Grant Grant
}

func (e *ReusedError) Error() string { return "the credential was already used" }

func (s *Store) CreateAuthCode(ctx context.Context, c AuthCode) error {
	names, err := json.Marshal(c.AccountNames)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM auth_codes WHERE expires_at < ?`, millis(c.CreatedAt.Add(-usedCodeRetention))); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO auth_codes (hash, client_id, grant_name, account_names, code_challenge, redirect_uri, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.Hash, c.ClientID, c.GrantName, string(names), c.CodeChallenge, c.RedirectURI, millis(c.ExpiresAt), millis(c.CreatedAt))
		return err
	})
}

func (s *Store) RedeemAuthCode(ctx context.Context, hash string, now time.Time, issue func(AuthCode) (NewGrant, error)) (Grant, error) {
	var grant Grant
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		code, usedGrant, used, err := lockAuthCode(ctx, tx, hash, now)
		if err != nil {
			return err
		}
		if used {
			return commitAndFail(revokeReused(ctx, tx, usedGrant, now))
		}
		n, err := issue(code)
		if err != nil {
			return commitAndFail(err)
		}
		grant, err = createGrant(ctx, tx, n)
		if errors.Is(err, ErrNameInUse) || errors.Is(err, ErrUnknownAccount) {
			return commitAndFail(err)
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE auth_codes SET grant_id = ? WHERE hash = ?`, grant.ID, hash)
		return err
	})
	return grant, err
}

func lockAuthCode(ctx context.Context, tx *sql.Tx, hash string, now time.Time) (AuthCode, sql.NullInt64, bool, error) {
	var (
		code      AuthCode
		names     string
		grantID   sql.NullInt64
		usedAt    sql.NullInt64
		expiresAt int64
		createdAt int64
	)
	err := tx.QueryRowContext(ctx, `SELECT hash, client_id, grant_id, grant_name, account_names, code_challenge, redirect_uri, expires_at, used_at, created_at FROM auth_codes WHERE hash = ?`, hash).
		Scan(&code.Hash, &code.ClientID, &grantID, &code.GrantName, &names, &code.CodeChallenge, &code.RedirectURI, &expiresAt, &usedAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return code, grantID, false, ErrNotFound
	}
	if err != nil {
		return code, grantID, false, err
	}
	code.ExpiresAt = fromMillis(expiresAt)
	code.CreatedAt = fromMillis(createdAt)
	if err := json.Unmarshal([]byte(names), &code.AccountNames); err != nil {
		return code, grantID, false, err
	}
	if usedAt.Valid {
		return code, grantID, true, nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE auth_codes SET used_at = ? WHERE hash = ?`, millis(now), hash)
	return code, grantID, false, err
}

func revokeReused(ctx context.Context, tx *sql.Tx, grantID sql.NullInt64, at time.Time) error {
	if !grantID.Valid {
		return &ReusedError{}
	}
	grant, err := revokeGrant(ctx, tx, grantID.Int64, at)
	if err != nil {
		return err
	}
	return &ReusedError{Grant: grant}
}
