package fake

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

func (w *World) Emit(phone string, evt any) {
	w.update(phone, func(dev *device) any {
		w.learnFrom(dev, evt)
		return evt
	})
}

func (w *World) MapLID(lid, pn types.JID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lids[lid.ToNonAD()] = pn.ToNonAD()
}

func (w *World) SetContact(phone string, jid types.JID, info types.ContactInfo) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if dev, ok := w.devices[phone]; ok {
		info.Found = true
		dev.contacts[jid.ToNonAD()] = info
	}
}

func (w *World) SetGroup(phone string, info types.GroupInfo) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if dev, ok := w.devices[phone]; ok {
		dev.groups[info.JID] = info
	}
}

func (w *World) learnFrom(dev *device, evt any) {
	msg, ok := evt.(*events.Message)
	if !ok {
		return
	}
	source := msg.Info.MessageSource
	w.pairIdentities(source.Sender, source.SenderAlt)
	w.pairIdentities(source.Chat, source.RecipientAlt)
	if !source.IsFromMe && msg.Info.PushName != "" {
		for _, jid := range []types.JID{source.Sender, source.SenderAlt} {
			if !jid.IsEmpty() {
				contact := dev.contacts[jid.ToNonAD()]
				contact.Found = true
				contact.PushName = msg.Info.PushName
				dev.contacts[jid.ToNonAD()] = contact
			}
		}
	}
}

func (w *World) pairIdentities(first, second types.JID) {
	switch {
	case first.Server == types.HiddenUserServer && second.Server == types.DefaultUserServer:
		w.lids[first.ToNonAD()] = second.ToNonAD()
	case first.Server == types.DefaultUserServer && second.Server == types.HiddenUserServer:
		w.lids[second.ToNonAD()] = first.ToNonAD()
	}
}

func (c *Client) linkedDevice() (*device, error) {
	dev := c.world.deviceByJID(c.device())
	if dev == nil || !dev.local {
		return nil, whatsapp.ErrNotPaired
	}
	return dev, nil
}

func (c *Client) Contact(_ context.Context, jid types.JID) (types.ContactInfo, error) {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	dev, err := c.linkedDevice()
	if err != nil {
		return types.ContactInfo{}, err
	}
	return dev.contacts[jid.ToNonAD()], nil
}

func (c *Client) Contacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	dev, err := c.linkedDevice()
	if err != nil {
		return nil, err
	}
	all := make(map[types.JID]types.ContactInfo, len(dev.contacts))
	for jid, info := range dev.contacts {
		all[jid] = info
	}
	return all, nil
}

func (c *Client) GroupInfo(_ context.Context, group types.JID) (*types.GroupInfo, error) {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	dev, err := c.linkedDevice()
	if err != nil {
		return nil, err
	}
	if !dev.online {
		return nil, errOffline
	}
	info, ok := dev.groups[group]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoGroup, group)
	}
	return &info, nil
}

func (c *Client) PhoneForLID(_ context.Context, lid types.JID) (types.JID, error) {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	return c.world.lids[lid.ToNonAD()], nil
}
