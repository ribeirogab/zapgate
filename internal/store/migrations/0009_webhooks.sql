CREATE TABLE webhooks (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL,
  format TEXT NOT NULL DEFAULT 'standard' CHECK (format IN ('standard', 'claude-routine')),
  content TEXT NOT NULL DEFAULT 'minimal' CHECK (content IN ('minimal', 'full')),
  events TEXT NOT NULL,
  chats TEXT NOT NULL DEFAULT 'all' CHECK (chats IN ('all', 'direct', 'groups', 'listed')),
  quiet_ms INTEGER NOT NULL DEFAULT 5000,
  max_wait_ms INTEGER NOT NULL DEFAULT 30000,
  secret TEXT NOT NULL,
  previous_secret TEXT,
  previous_secret_until INTEGER,
  headers TEXT,
  cursor INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  disabled_at INTEGER
);

CREATE TABLE webhook_accounts (
  webhook_id INTEGER NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  PRIMARY KEY (webhook_id, account_id)
);

CREATE TABLE webhook_chats (
  webhook_id INTEGER NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  chat_jid TEXT NOT NULL,
  PRIMARY KEY (webhook_id, account_id, chat_jid)
);

CREATE TABLE events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('message.received', 'account.state')),
  chat_jid TEXT,
  message_id TEXT,
  state TEXT,
  created_at INTEGER NOT NULL
);

CREATE INDEX events_account ON events (account_id, seq);

CREATE INDEX events_created ON events (created_at);

CREATE TABLE webhook_deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  webhook_id INTEGER NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  chat_jid TEXT,
  first_seq INTEGER NOT NULL,
  last_seq INTEGER NOT NULL,
  event_count INTEGER NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('open', 'retrying', 'delivered', 'failed')),
  opened_at INTEGER NOT NULL,
  due_at INTEGER NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_status INTEGER,
  last_error TEXT,
  settled_at INTEGER
);

CREATE INDEX webhook_deliveries_due ON webhook_deliveries (state, due_at);

CREATE INDEX webhook_deliveries_chat ON webhook_deliveries (webhook_id, account_id, chat_jid, id);

CREATE INDEX webhook_deliveries_settled ON webhook_deliveries (settled_at) WHERE settled_at IS NOT NULL;
