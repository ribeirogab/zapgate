package webhooks

import (
	"context"
	"slices"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
)

type matcher struct {
	webhook  store.Webhook
	accounts map[int64]bool
	chats    map[store.WebhookChat]bool
}

func (d *Dispatcher) matcherFor(ctx context.Context, w store.Webhook) (matcher, error) {
	accounts, err := d.deps.Store.WebhookAccounts(ctx, w)
	if err != nil {
		return matcher{}, err
	}
	chats, err := d.deps.Store.WebhookChats(ctx, w)
	m := matcher{webhook: w, accounts: map[int64]bool{}, chats: map[store.WebhookChat]bool{}}
	for _, account := range accounts {
		m.accounts[account.ID] = true
	}
	for _, chat := range chats {
		m.chats[chat] = true
	}
	return m, err
}

func (m matcher) keep(journal []store.Event) []store.Event {
	var kept []store.Event
	for _, event := range journal {
		if m.matches(event) {
			kept = append(kept, event)
		}
	}
	return kept
}

func (m matcher) matches(e store.Event) bool {
	return !m.webhook.Disabled() && m.accounts[e.AccountID] && slices.Contains(m.webhook.Events, e.Kind) && m.inChats(e)
}

func (m matcher) inChats(e store.Event) bool {
	if e.ChatJID == "" {
		return true
	}
	switch m.webhook.Chats {
	case ChatsDirect:
		return !isGroup(e.ChatJID)
	case ChatsGroups:
		return isGroup(e.ChatJID)
	case ChatsListed:
		return m.chats[store.WebhookChat{AccountID: e.AccountID, ChatJID: e.ChatJID}]
	}
	return true
}

func isGroup(chat string) bool {
	jid, err := types.ParseJID(chat)
	return err == nil && jid.Server == types.GroupServer
}
