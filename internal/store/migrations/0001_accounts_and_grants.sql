CREATE TABLE accounts (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  jid TEXT UNIQUE,
  phone TEXT NOT NULL,
  description TEXT,
  retention_months INTEGER,
  state TEXT NOT NULL CHECK (state IN ('pairing', 'connected', 'disconnected', 'logged_out')),
  created_at INTEGER NOT NULL
);

CREATE TABLE oauth_clients (
  id TEXT PRIMARY KEY
);

CREATE TABLE grants (
  id INTEGER PRIMARY KEY,
  client_id TEXT REFERENCES oauth_clients (id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('static', 'oauth')),
  read_only INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  revoked_at INTEGER
);

CREATE UNIQUE INDEX grants_active_name ON grants (name) WHERE revoked_at IS NULL;

CREATE TABLE grant_accounts (
  grant_id INTEGER NOT NULL REFERENCES grants (id) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  PRIMARY KEY (grant_id, account_id)
);

CREATE TABLE tokens (
  hash TEXT PRIMARY KEY,
  grant_id INTEGER NOT NULL REFERENCES grants (id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('static', 'access', 'refresh')),
  expires_at INTEGER,
  created_at INTEGER NOT NULL
);
