CREATE TABLE media (
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  chat_jid TEXT NOT NULL,
  message_id TEXT NOT NULL,
  mime TEXT,
  file_name TEXT,
  size INTEGER,
  seconds INTEGER,
  voice_note INTEGER NOT NULL DEFAULT 0,
  media_key BLOB,
  direct_path TEXT,
  file_sha256 BLOB,
  file_enc_sha256 BLOB,
  path TEXT,
  state TEXT NOT NULL DEFAULT 'pending',
  PRIMARY KEY (account_id, chat_jid, message_id),
  FOREIGN KEY (account_id, chat_jid, message_id) REFERENCES messages (account_id, chat_jid, id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX media_state ON media (account_id, state);

CREATE TABLE message_details (
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  chat_jid TEXT NOT NULL,
  message_id TEXT NOT NULL,
  details TEXT NOT NULL,
  PRIMARY KEY (account_id, chat_jid, message_id),
  FOREIGN KEY (account_id, chat_jid, message_id) REFERENCES messages (account_id, chat_jid, id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE reactions (
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  chat_jid TEXT NOT NULL,
  message_id TEXT NOT NULL,
  sender_jid TEXT NOT NULL,
  sender_name TEXT,
  from_me INTEGER NOT NULL,
  emoji TEXT NOT NULL,
  reacted_at INTEGER NOT NULL,
  PRIMARY KEY (account_id, chat_jid, message_id, sender_jid),
  FOREIGN KEY (account_id, chat_jid, message_id) REFERENCES messages (account_id, chat_jid, id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX reactions_sender ON reactions (account_id, sender_jid);

CREATE TABLE message_index (
  id INTEGER PRIMARY KEY,
  account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  chat_jid TEXT NOT NULL,
  message_id TEXT NOT NULL,
  UNIQUE (account_id, chat_jid, message_id),
  FOREIGN KEY (account_id, chat_jid, message_id) REFERENCES messages (account_id, chat_jid, id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE VIRTUAL TABLE messages_fts USING fts5 (
  text,
  content = '',
  contentless_delete = 1,
  tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER messages_index_insert AFTER INSERT ON messages BEGIN
  INSERT INTO message_index (account_id, chat_jid, message_id) VALUES (new.account_id, new.chat_jid, new.id);
  INSERT INTO messages_fts (rowid, text)
    SELECT id, new.text FROM message_index
    WHERE account_id = new.account_id AND chat_jid = new.chat_jid AND message_id = new.id AND new.text IS NOT NULL;
END;

CREATE TRIGGER messages_index_text AFTER UPDATE OF text ON messages BEGIN
  DELETE FROM messages_fts WHERE rowid = (
    SELECT id FROM message_index WHERE account_id = new.account_id AND chat_jid = new.chat_jid AND message_id = new.id
  );
  INSERT INTO messages_fts (rowid, text)
    SELECT id, new.text FROM message_index
    WHERE account_id = new.account_id AND chat_jid = new.chat_jid AND message_id = new.id AND new.text IS NOT NULL;
END;

CREATE TRIGGER message_index_delete AFTER DELETE ON message_index BEGIN
  DELETE FROM messages_fts WHERE rowid = old.id;
END;

INSERT INTO message_index (account_id, chat_jid, message_id)
  SELECT account_id, chat_jid, id FROM messages;

INSERT INTO messages_fts (rowid, text)
  SELECT i.id, m.text FROM message_index i
  JOIN messages m ON m.account_id = i.account_id AND m.chat_jid = i.chat_jid AND m.id = i.message_id
  WHERE m.text IS NOT NULL;
