ALTER TABLE oauth_clients ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_clients ADD COLUMN redirect_uris TEXT NOT NULL DEFAULT '[]';
ALTER TABLE oauth_clients ADD COLUMN auth_method TEXT NOT NULL DEFAULT 'none' CHECK (auth_method IN ('none', 'client_secret_post', 'client_secret_basic'));
ALTER TABLE oauth_clients ADD COLUMN secret_hash TEXT;
ALTER TABLE oauth_clients ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;

ALTER TABLE tokens ADD COLUMN rotated_at INTEGER;

CREATE INDEX tokens_grant ON tokens (grant_id);

CREATE TABLE auth_codes (
  hash TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
  grant_id INTEGER REFERENCES grants (id) ON DELETE CASCADE,
  grant_name TEXT NOT NULL,
  account_names TEXT NOT NULL,
  code_challenge TEXT NOT NULL,
  redirect_uri TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER,
  created_at INTEGER NOT NULL
);

CREATE INDEX auth_codes_expires ON auth_codes (expires_at);
