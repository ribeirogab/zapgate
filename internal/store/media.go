package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	MediaPending     = "pending"
	MediaDownloaded  = "downloaded"
	MediaFailed      = "failed"
	MediaUnavailable = "unavailable"
)

type MediaRef struct {
	AccountID int64
	ChatJID   string
	MessageID string
}

type MediaFile struct {
	MediaRef
	Kind     string
	Media    Media
	SentAt   time.Time
	Path     string
	Attempts int
	Error    string
	RetryAt  time.Time
}

type MediaFailure struct {
	State    string
	Attempts int
	Error    string
	RetryAt  time.Time
}

const mediaFileSelect = `SELECT d.account_id, d.chat_jid, d.message_id, m.kind, m.sent_at, d.mime, d.file_name, d.size, d.seconds, d.voice_note,
  d.media_key, d.direct_path, d.file_sha256, d.file_enc_sha256, d.state, d.path, d.attempts, d.error, d.retry_at
FROM media d
JOIN messages m ON m.account_id = d.account_id AND m.chat_jid = d.chat_jid AND m.id = d.message_id`

func (s *Store) MediaFile(ctx context.Context, ref MediaRef) (MediaFile, error) {
	return mediaFileWhere(ctx, s.db, `d.account_id = ? AND d.chat_jid = ? AND d.message_id = ?`, ref.AccountID, ref.ChatJID, ref.MessageID)
}

func (s *Store) MediaFileByPath(ctx context.Context, path string) (MediaFile, error) {
	return mediaFileWhere(ctx, s.db, `d.path = ? AND d.state = ?`, path, MediaDownloaded)
}

func (s *Store) DueMedia(ctx context.Context, now time.Time, limit int) ([]MediaFile, error) {
	rows, err := s.db.QueryContext(ctx, mediaFileSelect+` WHERE d.state IN (?, ?) AND coalesce(d.retry_at, 0) <= ?
ORDER BY coalesce(d.retry_at, 0), m.sent_at DESC LIMIT ?`, MediaPending, MediaFailed, millis(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []MediaFile
	for rows.Next() {
		file, err := scanMediaFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func (s *Store) SetMediaDownloaded(ctx context.Context, ref MediaRef, path string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE media SET state = ?, path = ?, error = NULL, retry_at = NULL
WHERE account_id = ? AND chat_jid = ? AND message_id = ? AND state != ?`, MediaDownloaded, path, ref.AccountID, ref.ChatJID, ref.MessageID, MediaDownloaded)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func (s *Store) SetMediaFailed(ctx context.Context, ref MediaRef, failure MediaFailure) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET state = ?, attempts = ?, error = ?, retry_at = ?
WHERE account_id = ? AND chat_jid = ? AND message_id = ? AND state != ?`,
		failure.State, failure.Attempts, optionalText(failure.Error), optionalMillis(failure.RetryAt), ref.AccountID, ref.ChatJID, ref.MessageID, MediaDownloaded)
	return err
}

func (s *Store) RetryFailedMediaNow(ctx context.Context, accountID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET retry_at = ? WHERE account_id = ? AND state = ?`, millis(now), accountID, MediaFailed)
	return err
}

func mediaFileWhere(ctx context.Context, q rowQueryer, where string, args ...any) (MediaFile, error) {
	file, err := scanMediaFile(q.QueryRowContext(ctx, mediaFileSelect+` WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return MediaFile{}, ErrNotFound
	}
	return file, err
}

func scanMediaFile(row scanner) (MediaFile, error) {
	var (
		file                       MediaFile
		sentAt                     int64
		mime, fileName, directPath sql.NullString
		path, failure              sql.NullString
		size, seconds, retryAt     sql.NullInt64
	)
	m := &file.Media
	err := row.Scan(&file.AccountID, &file.ChatJID, &file.MessageID, &file.Kind, &sentAt, &mime, &fileName, &size, &seconds, &m.VoiceNote,
		&m.MediaKey, &directPath, &m.FileSHA256, &m.FileEncSHA256, &m.State, &path, &file.Attempts, &failure, &retryAt)
	file.SentAt = fromMillis(sentAt)
	m.Mime = mime.String
	m.FileName = fileName.String
	m.Size = size.Int64
	m.Seconds = int(seconds.Int64)
	m.DirectPath = directPath.String
	file.Path = path.String
	file.Error = failure.String
	file.RetryAt = optionalTime(retryAt)
	return file, err
}
