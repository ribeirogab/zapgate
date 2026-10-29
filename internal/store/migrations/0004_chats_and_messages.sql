CREATE TABLE chats (
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  jid TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('direct', 'group')),
  name TEXT,
  last_message_at INTEGER,
  unread_count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (account_id, jid)
);

CREATE INDEX chats_recent ON chats (account_id, last_message_at);

CREATE TABLE messages (
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  chat_jid TEXT NOT NULL,
  id TEXT NOT NULL,
  sender_jid TEXT NOT NULL,
  sender_name TEXT,
  from_me INTEGER NOT NULL,
  sent_at INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('text', 'image', 'video', 'audio', 'document', 'sticker', 'location', 'contact', 'poll')),
  text TEXT,
  quoted_id TEXT,
  edited_at INTEGER,
  revoked_at INTEGER,
  PRIMARY KEY (account_id, chat_jid, id),
  FOREIGN KEY (account_id, chat_jid) REFERENCES chats (account_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX messages_chat_time ON messages (account_id, chat_jid, sent_at, id);
CREATE INDEX messages_time ON messages (sent_at);
CREATE INDEX messages_sender ON messages (account_id, sender_jid);
