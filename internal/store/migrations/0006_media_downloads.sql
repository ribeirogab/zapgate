ALTER TABLE media ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE media ADD COLUMN retry_at INTEGER;

ALTER TABLE media ADD COLUMN error TEXT;

CREATE INDEX media_due ON media (state, retry_at);

CREATE UNIQUE INDEX media_path ON media (path) WHERE path IS NOT NULL;
