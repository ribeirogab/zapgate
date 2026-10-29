package fake

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"slices"

	"go.mau.fi/whatsmeow"
)

type Upload struct {
	Data     []byte
	Kind     whatsmeow.MediaType
	Response whatsmeow.UploadResponse
}

func (w *World) Uploads(phone string) []Upload {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.outboxOf(phone).uploads)
}

func (c *Client) Upload(_ context.Context, plaintext io.Reader, scratch io.ReadWriteSeeker, kind whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	data, err := io.ReadAll(plaintext)
	if err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	if _, err := scratch.Write(data); err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	dev, err := c.onlineDevice()
	if err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	box := c.world.outboxOf(dev.phone)
	if box.failure != nil {
		return whatsmeow.UploadResponse{}, box.failure
	}
	n := len(box.uploads) + 1
	plainHash := sha256.Sum256(data)
	encHash := sha256.Sum256(append([]byte("encrypted"), data...))
	key := sha256.Sum256(fmt.Appendf(nil, "media key %d", n))
	resp := whatsmeow.UploadResponse{
		URL:           fmt.Sprintf("https://mmg.whatsapp.net/fake/%d", n),
		DirectPath:    fmt.Sprintf("/v/t62.fake/%d", n),
		MediaKey:      key[:],
		FileSHA256:    plainHash[:],
		FileEncSHA256: encHash[:],
		FileLength:    uint64(len(data)),
	}
	box.uploads = append(box.uploads, Upload{Data: data, Kind: kind, Response: resp})
	return resp, nil
}
