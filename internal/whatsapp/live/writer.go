package live

import (
	"context"
	"io"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

func (c *client) SendMessage(ctx context.Context, to types.JID, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
	c.sending.Lock()
	defer c.sending.Unlock()
	return c.cli.SendMessage(ctx, to, msg)
}

func (c *client) BuildEdit(chat types.JID, id types.MessageID, content *waE2E.Message) *waE2E.Message {
	return c.cli.BuildEdit(chat, id, content)
}

func (c *client) BuildRevoke(chat, sender types.JID, id types.MessageID) *waE2E.Message {
	return c.cli.BuildRevoke(chat, sender, id)
}

func (c *client) BuildReaction(chat, sender types.JID, id types.MessageID, emoji string) *waE2E.Message {
	return c.cli.BuildReaction(chat, sender, id, emoji)
}

func (c *client) MarkRead(ctx context.Context, ids []types.MessageID, at time.Time, chat, sender types.JID) error {
	return c.cli.MarkRead(ctx, ids, at, chat, sender)
}

func (c *client) Upload(ctx context.Context, plaintext io.Reader, scratch io.ReadWriteSeeker, kind whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	return c.cli.UploadReader(ctx, plaintext, scratch, kind)
}
