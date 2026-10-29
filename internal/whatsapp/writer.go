package whatsapp

import (
	"context"
	"io"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

type Writer interface {
	SendMessage(ctx context.Context, to types.JID, msg *waE2E.Message) (whatsmeow.SendResponse, error)
	BuildEdit(chat types.JID, id types.MessageID, content *waE2E.Message) *waE2E.Message
	BuildRevoke(chat, sender types.JID, id types.MessageID) *waE2E.Message
	BuildReaction(chat, sender types.JID, id types.MessageID, emoji string) *waE2E.Message
	MarkRead(ctx context.Context, ids []types.MessageID, at time.Time, chat, sender types.JID) error
	Upload(ctx context.Context, plaintext io.Reader, scratch io.ReadWriteSeeker, kind whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
}
