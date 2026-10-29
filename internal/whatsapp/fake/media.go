package fake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"go.mau.fi/whatsmeow"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

var errWrongMediaKey = errors.New("the media key does not decrypt this file")

type cdnBlob struct {
	key     []byte
	content []byte
}

type cdn struct {
	mu        sync.Mutex
	blobs     map[string]cdnBlob
	downloads map[string][]whatsapp.MediaDownload
	held      chan struct{}
	inFlight  int
}

func (w *World) PublishMedia(directPath string, mediaKey, content []byte) {
	w.cdn.mu.Lock()
	defer w.cdn.mu.Unlock()
	if w.cdn.blobs == nil {
		w.cdn.blobs = map[string]cdnBlob{}
	}
	w.cdn.blobs[directPath] = cdnBlob{key: slices.Clone(mediaKey), content: slices.Clone(content)}
}

func (w *World) MediaDownloads(directPath string) []whatsapp.MediaDownload {
	w.cdn.mu.Lock()
	defer w.cdn.mu.Unlock()
	return slices.Clone(w.cdn.downloads[directPath])
}

func (w *World) HoldMediaDownloads() {
	w.cdn.mu.Lock()
	defer w.cdn.mu.Unlock()
	if w.cdn.held == nil {
		w.cdn.held = make(chan struct{})
	}
}

func (w *World) ReleaseMediaDownloads() {
	w.cdn.mu.Lock()
	defer w.cdn.mu.Unlock()
	if w.cdn.held != nil {
		close(w.cdn.held)
		w.cdn.held = nil
	}
}

func (w *World) MediaDownloadsInFlight() int {
	w.cdn.mu.Lock()
	defer w.cdn.mu.Unlock()
	return w.cdn.inFlight
}

func (c *Client) DownloadMedia(ctx context.Context, media whatsapp.MediaDownload, file whatsmeow.File) error {
	if err := c.mediaConnection(); err != nil {
		return err
	}
	blob, err := c.world.cdn.fetch(ctx, media)
	if err != nil {
		return err
	}
	_, err = file.Write(blob.content)
	return err
}

func (c *Client) mediaConnection() error {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	dev, err := c.linkedDevice()
	if err != nil {
		return err
	}
	if !dev.online {
		return whatsapp.ErrNotConnected
	}
	return nil
}

func (s *cdn) fetch(ctx context.Context, media whatsapp.MediaDownload) (cdnBlob, error) {
	held := s.begin(media)
	defer s.end()
	if held != nil {
		select {
		case <-held:
		case <-ctx.Done():
			return cdnBlob{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	blob, ok := s.blobs[media.DirectPath]
	if !ok {
		return cdnBlob{}, fmt.Errorf("%w: 404", whatsapp.ErrMediaGone)
	}
	if !bytes.Equal(blob.key, media.MediaKey) {
		return cdnBlob{}, errWrongMediaKey
	}
	return blob, nil
}

func (s *cdn) begin(media whatsapp.MediaDownload) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.downloads == nil {
		s.downloads = map[string][]whatsapp.MediaDownload{}
	}
	s.downloads[media.DirectPath] = append(s.downloads[media.DirectPath], media)
	s.inFlight++
	return s.held
}

func (s *cdn) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight--
}
