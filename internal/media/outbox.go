package media

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ribeirogab/zapgate/internal/clock"
)

const (
	MaxFileBytes   = 100 << 20
	UploadTTL      = time.Hour
	UploadsDir     = "uploads"
	TempDir        = "tmp"
	dataSuffix     = ".data"
	metaSuffix     = ".json"
	sweepInterval  = time.Minute
	partFilePrefix = "upload-*"
)

var (
	ErrTooLarge      = fmt.Errorf("the file is larger than %d MB", MaxFileBytes>>20)
	ErrEmpty         = errors.New("the file is empty")
	ErrUnknownUpload = errors.New("unknown upload")
	uploadIDPattern  = regexp.MustCompile(`^[A-Z2-7]{26}$`)
)

type ExpiredUploadError struct {
	ExpiredAt time.Time
}

func (e *ExpiredUploadError) Error() string {
	return "the upload expired at " + e.ExpiredAt.UTC().Format(time.RFC3339)
}

type Upload struct {
	ID        string
	Size      int64
	ExpiresAt time.Time
}

type uploadMeta struct {
	GrantID   int64  `json:"grant_id"`
	Size      int64  `json:"size"`
	SHA256    []byte `json:"sha256"`
	Mime      string `json:"mime,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
}

type OutboxDeps struct {
	DataDir    string
	Clock      clock.Clock
	Transcoder Transcoder
	Fetch      FetchConfig
	Log        *slog.Logger
}

type Outbox struct {
	uploads string
	temp    string
	clock   clock.Clock
	codec   Transcoder
	fetcher *fetcher
	log     *slog.Logger
}

func NewOutbox(deps OutboxDeps) (*Outbox, error) {
	o := &Outbox{
		uploads: filepath.Join(deps.DataDir, UploadsDir),
		temp:    filepath.Join(deps.DataDir, TempDir),
		clock:   deps.Clock,
		codec:   deps.Transcoder,
		fetcher: newFetcher(deps.Fetch),
		log:     deps.Log,
	}
	if err := os.RemoveAll(o.temp); err != nil {
		return nil, fmt.Errorf("clean the temp dir: %w", err)
	}
	for _, dir := range []string{o.uploads, o.temp} {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := o.sweep(true); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *Outbox) Run(ctx context.Context) error {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := o.sweep(false); err != nil {
				o.log.Warn("cannot delete expired uploads", "error", err)
			}
		}
	}
}

func (o *Outbox) SaveUpload(grantID int64, declaredType string, body io.Reader) (Upload, error) {
	part, err := os.CreateTemp(o.temp, partFilePrefix)
	if err != nil {
		return Upload{}, err
	}
	defer os.Remove(part.Name())
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(part, hash), io.LimitReader(body, MaxFileBytes+1))
	err = errors.Join(err, part.Close())
	switch {
	case err != nil:
		return Upload{}, err
	case size > MaxFileBytes:
		return Upload{}, ErrTooLarge
	case size == 0:
		return Upload{}, ErrEmpty
	}
	upload := Upload{ID: rand.Text(), Size: size, ExpiresAt: o.clock.Now().Add(UploadTTL)}
	meta := uploadMeta{GrantID: grantID, Size: size, SHA256: hash.Sum(nil), Mime: declaredMime(declaredType), ExpiresAt: upload.ExpiresAt.UnixMilli()}
	if err := os.Rename(part.Name(), o.uploadPath(upload.ID, dataSuffix)); err != nil {
		return Upload{}, err
	}
	return upload, o.writeMeta(upload.ID, meta)
}

func (o *Outbox) writeMeta(id string, meta uploadMeta) error {
	body, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(o.temp, partFilePrefix)
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(body)
	if err := errors.Join(err, temp.Close()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), o.uploadPath(id, metaSuffix))
}

func (o *Outbox) openUpload(grantID int64, id string) (File, error) {
	if !uploadIDPattern.MatchString(id) {
		return File{}, ErrUnknownUpload
	}
	meta, err := o.readMeta(id)
	if errors.Is(err, os.ErrNotExist) || (err == nil && meta.GrantID != grantID) {
		return File{}, ErrUnknownUpload
	}
	if err != nil {
		return File{}, err
	}
	if expires := time.UnixMilli(meta.ExpiresAt); !o.clock.Now().Before(expires) {
		return File{}, &ExpiredUploadError{ExpiredAt: expires}
	}
	return File{Path: o.uploadPath(id, dataSuffix), Size: meta.Size, SHA256: meta.SHA256, declared: meta.Mime}, nil
}

func (o *Outbox) readMeta(id string) (uploadMeta, error) {
	body, err := os.ReadFile(o.uploadPath(id, metaSuffix))
	if err != nil {
		return uploadMeta{}, err
	}
	var meta uploadMeta
	return meta, json.Unmarshal(body, &meta)
}

func (o *Outbox) uploadPath(id, suffix string) string {
	return filepath.Join(o.uploads, id+suffix)
}

func (o *Outbox) sweep(startup bool) error {
	entries, err := os.ReadDir(o.uploads)
	if err != nil {
		return err
	}
	now := o.clock.Now()
	var errs []error
	for _, entry := range entries {
		id, isData := strings.CutSuffix(entry.Name(), dataSuffix)
		if isData {
			if _, err := os.Stat(o.uploadPath(id, metaSuffix)); startup && errors.Is(err, os.ErrNotExist) {
				errs = append(errs, os.Remove(o.uploadPath(id, dataSuffix)))
			}
			continue
		}
		id, isMeta := strings.CutSuffix(entry.Name(), metaSuffix)
		if !isMeta {
			continue
		}
		meta, err := o.readMeta(id)
		if err == nil && now.Before(time.UnixMilli(meta.ExpiresAt)) {
			continue
		}
		errs = append(errs, ignoreMissing(os.Remove(o.uploadPath(id, dataSuffix))), ignoreMissing(os.Remove(o.uploadPath(id, metaSuffix))))
	}
	return errors.Join(errs...)
}

func ignoreMissing(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (o *Outbox) tempFile() (*os.File, error) {
	return os.CreateTemp(o.temp, partFilePrefix)
}

func (o *Outbox) Scratch() (*os.File, func(), error) {
	file, err := o.tempFile()
	if err != nil {
		return nil, nil, err
	}
	return file, func() {
		file.Close()
		os.Remove(file.Name())
	}, nil
}
