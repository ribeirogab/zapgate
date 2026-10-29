package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	AuthMethod   string
	SecretHash   string
	CreatedAt    time.Time
}

func (s *Store) CreateOAuthClient(ctx context.Context, c OAuthClient) error {
	uris, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO oauth_clients (id, name, redirect_uris, auth_method, secret_hash, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, string(uris), c.AuthMethod, optionalText(c.SecretHash), millis(c.CreatedAt))
	return err
}

func (s *Store) OAuthClient(ctx context.Context, id string) (OAuthClient, error) {
	return oauthClientWhere(ctx, s.db, id)
}

func oauthClientWhere(ctx context.Context, q rowQueryer, id string) (OAuthClient, error) {
	var (
		c          OAuthClient
		uris       string
		secretHash sql.NullString
		createdAt  int64
	)
	err := q.QueryRowContext(ctx, `SELECT id, name, redirect_uris, auth_method, secret_hash, created_at FROM oauth_clients WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &uris, &c.AuthMethod, &secretHash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthClient{}, ErrNotFound
	}
	if err != nil {
		return OAuthClient{}, err
	}
	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return OAuthClient{}, err
	}
	c.SecretHash = secretHash.String
	c.CreatedAt = fromMillis(createdAt)
	return c, nil
}

func (s *Store) ForgetUnusedOAuthClients(ctx context.Context, registeredBefore, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_clients
WHERE created_at < ?
  AND NOT EXISTS (SELECT 1 FROM grants WHERE grants.client_id = oauth_clients.id)
  AND NOT EXISTS (SELECT 1 FROM auth_codes WHERE auth_codes.client_id = oauth_clients.id AND auth_codes.expires_at > ?)`,
		millis(registeredBefore), millis(now))
	return err
}
