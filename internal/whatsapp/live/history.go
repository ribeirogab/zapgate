package live

import (
	"context"

	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func (c *client) ParseWebMessage(chat types.JID, msg *waWeb.WebMessageInfo) (*events.Message, error) {
	return c.cli.ParseWebMessage(chat, msg)
}

func (c *client) RequestOlderHistory(ctx context.Context, oldest types.MessageInfo, count int) error {
	_, err := c.cli.SendPeerMessage(ctx, c.cli.BuildHistorySyncRequest(&oldest, count))
	return err
}
