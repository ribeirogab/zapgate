CREATE TABLE spent_approvals (
  nonce TEXT PRIMARY KEY,
  expires_at INTEGER NOT NULL
);

CREATE INDEX spent_approvals_expires ON spent_approvals (expires_at);
