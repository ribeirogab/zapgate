package webhooks

import (
	"context"
	"errors"
	"slices"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

func parseChatList(ctx context.Context, value string) ([]types.JID, error) {
	var chats []types.JID
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		chat, err := whatsapp.ParseChat(ctx, nil, item)
		if errors.Is(err, whatsapp.ErrNotPhoneOrJID) {
			return nil, invalid("the chat %q is not valid: use all, direct, groups, or a comma-separated list of phone numbers with the country code and JIDs", item)
		}
		if err != nil {
			return nil, invalid("%v", err)
		}
		chats = append(chats, chat)
	}
	if len(chats) == 0 {
		return nil, invalid("the chat filter needs all, direct, groups, or a comma-separated list of phone numbers and JIDs")
	}
	return chats, nil
}

func (r *Registry) listedChats(ctx context.Context, c config) func(accountID int64) []string {
	if c.webhook.Chats != ChatsListed {
		return nil
	}
	return func(accountID int64) []string {
		directory := r.directory(accountID)
		jids := make([]string, 0, len(c.chats))
		for _, chat := range c.chats {
			jids = append(jids, whatsapp.ResolvePhone(ctx, directory, chat, types.JID{}).String())
		}
		return jids
	}
}

func (r *Registry) directory(accountID int64) whatsapp.Directory {
	client, ok := r.deps.Clients.Client(accountID)
	if !ok {
		return nil
	}
	return client
}

func (r *Registry) listedChatJIDs(ctx context.Context, w store.Webhook) ([]string, error) {
	chats, err := r.deps.Store.WebhookChats(ctx, w)
	jids := make([]string, 0, len(chats))
	for _, chat := range chats {
		jids = append(jids, chat.ChatJID)
	}
	return slices.Compact(jids), err
}

func (r *Registry) storedChats(ctx context.Context, w store.Webhook) ([]types.JID, error) {
	jids, err := r.listedChatJIDs(ctx, w)
	if err != nil {
		return nil, err
	}
	chats := make([]types.JID, 0, len(jids))
	for _, jid := range jids {
		chat, err := types.ParseJID(jid)
		if err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}
	return chats, nil
}
