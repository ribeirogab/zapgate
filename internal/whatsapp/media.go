package whatsapp

import (
	"context"
	"errors"

	"go.mau.fi/whatsmeow"
)

type MediaDownload struct {
	DirectPath    string
	MediaKey      []byte
	FileSHA256    []byte
	FileEncSHA256 []byte
	Type          whatsmeow.MediaType
}

type Media interface {
	DownloadMedia(ctx context.Context, media MediaDownload, file whatsmeow.File) error
}

var (
	ErrMediaGone    = errors.New("WhatsApp no longer keeps this media file")
	ErrNotConnected = errors.New("the account is not connected to WhatsApp")
)
