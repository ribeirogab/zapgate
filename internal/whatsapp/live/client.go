package live

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	pairingDisplayName = "Chrome (Linux)"
	pairingUpdates     = 4
)

var errUnexpectedQRItem = errors.New("WhatsApp answered the pairing in an unexpected way")

type client struct {
	cli     *whatsmeow.Client
	life    context.Context
	lids    store.LIDStore
	sending sync.Mutex
}

func (c *client) Device() string {
	if c.cli.Store.ID == nil {
		return ""
	}
	return c.cli.Store.ID.String()
}

func (c *client) ProfileName() string {
	if c.cli.Store.PushName != "" {
		return c.cli.Store.PushName
	}
	return c.cli.Store.BusinessName
}

func (c *client) AddEventHandler(handler whatsapp.EventHandler) {
	c.cli.AddEventHandler(func(evt any) { handler(evt) })
}

func (c *client) Connect() error {
	return c.cli.ConnectContext(c.life)
}

func (c *client) Pair(ctx context.Context, req whatsapp.PairRequest) (<-chan whatsapp.PairUpdate, error) {
	items, err := c.cli.GetQRChannel(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.cli.ConnectContext(c.life); err != nil {
		return nil, err
	}
	updates := make(chan whatsapp.PairUpdate, pairingUpdates)
	go c.forwardPairing(ctx, req, items, updates)
	return updates, nil
}

func (c *client) forwardPairing(ctx context.Context, req whatsapp.PairRequest, items <-chan whatsmeow.QRChannelItem, updates chan<- whatsapp.PairUpdate) {
	defer close(updates)
	codeSent := false
	for item := range items {
		update, final := c.pairingUpdate(ctx, req, item, &codeSent)
		if update != nil && !send(ctx, updates, *update) {
			drain(items)
			return
		}
		if final {
			drain(items)
			return
		}
	}
}

func (c *client) pairingUpdate(ctx context.Context, req whatsapp.PairRequest, item whatsmeow.QRChannelItem, codeSent *bool) (*whatsapp.PairUpdate, bool) {
	switch {
	case item.Event == whatsmeow.QRChannelEventCode && req.QR:
		return &whatsapp.PairUpdate{QR: item.Code}, false
	case item.Event == whatsmeow.QRChannelEventCode && *codeSent:
		return nil, false
	case item.Event == whatsmeow.QRChannelEventCode:
		*codeSent = true
		code, err := c.cli.PairPhone(ctx, req.Phone, true, whatsmeow.PairClientChrome, pairingDisplayName)
		if err != nil {
			return &whatsapp.PairUpdate{Err: err}, true
		}
		return &whatsapp.PairUpdate{Code: code}, false
	case item == whatsmeow.QRChannelSuccess:
		return nil, true
	case item == whatsmeow.QRChannelTimeout:
		return &whatsapp.PairUpdate{Err: whatsapp.ErrPairingExpired}, true
	case item.Event == whatsmeow.QRChannelEventError:
		return &whatsapp.PairUpdate{Err: item.Error}, true
	}
	return &whatsapp.PairUpdate{Err: fmt.Errorf("%w: %s", errUnexpectedQRItem, item.Event)}, true
}

func send(ctx context.Context, updates chan<- whatsapp.PairUpdate, update whatsapp.PairUpdate) bool {
	select {
	case updates <- update:
		return true
	case <-ctx.Done():
		return false
	}
}

func drain(items <-chan whatsmeow.QRChannelItem) {
	go func() {
		for range items {
		}
	}()
}

func (c *client) Logout(ctx context.Context) error {
	if c.cli.Store.ID == nil {
		c.cli.Disconnect()
		return nil
	}
	err := c.cli.Logout(ctx)
	if err == nil {
		return nil
	}
	c.cli.Disconnect()
	if deleteErr := c.cli.Store.Delete(context.WithoutCancel(ctx)); deleteErr != nil {
		return errors.Join(err, deleteErr)
	}
	return fmt.Errorf("%w: %v", whatsapp.ErrUnlinkedLocallyOnly, err)
}

func (c *client) Disconnect() {
	c.cli.Disconnect()
}
