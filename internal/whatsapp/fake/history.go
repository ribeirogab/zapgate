package fake

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type HistoryRequest struct {
	Chat         types.JID
	OldestID     string
	OldestFromMe bool
	OldestAt     time.Time
	Count        int
}

func (w *World) HistoryRequests(phone string) []HistoryRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	if dev, ok := w.devices[phone]; ok {
		return append([]HistoryRequest(nil), dev.historyRequests...)
	}
	return nil
}

func (c *Client) ParseWebMessage(chat types.JID, msg *waWeb.WebMessageInfo) (*events.Message, error) {
	c.world.mu.Lock()
	own, err := types.ParseJID(c.device())
	lid := c.world.lidOf(own.ToNonAD())
	c.world.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return whatsmeow.NewClient(&store.Device{ID: &own, LID: lid}, nil).ParseWebMessage(chat, msg)
}

func (c *Client) RequestOlderHistory(_ context.Context, oldest types.MessageInfo, count int) error {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	dev, err := c.linkedDevice()
	if err != nil {
		return err
	}
	if !dev.online {
		return errOffline
	}
	dev.historyRequests = append(dev.historyRequests, HistoryRequest{
		Chat:         oldest.Chat,
		OldestID:     oldest.ID,
		OldestFromMe: oldest.IsFromMe,
		OldestAt:     oldest.Timestamp,
		Count:        count,
	})
	return nil
}

func (w *World) lidOf(pn types.JID) types.JID {
	for lid, phone := range w.lids {
		if phone == pn {
			return lid
		}
	}
	return types.JID{}
}
