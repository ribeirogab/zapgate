package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const excerptRunes = 300

type writeTarget struct {
	scope   Scope
	account store.Account
	client  whatsapp.Client
}

type writeResult struct {
	accountFields
	Chat     chatRef `json:"chat"`
	Approval string  `json:"approval" jsonschema:"owner_approved when the owner accepted the approval request, client_without_elicitation when the client cannot ask the owner and asked for permission itself, access_trusts_app when the owner set this access to let the app ask for permission"`
}

func (r *Registry) writeTarget(ctx context.Context, scope Scope, name string) (writeTarget, error) {
	account, err := scope.account(name)
	if err != nil {
		return writeTarget{}, err
	}
	client, ok := r.deps.Clients.Client(account.ID)
	if !ok || account.State != store.AccountConnected {
		return writeTarget{}, fmt.Errorf("the account %s is %s, so nothing was done; call list_accounts and try again when it is connected", account.Name, account.State)
	}
	return writeTarget{scope: scope, account: account, client: client}, nil
}

func (w writeTarget) action(tool string, question, undone string, fields ...string) action {
	return action{
		tool:      tool,
		grantID:   w.scope.Grant.ID,
		trustsApp: w.scope.Grant.TrustsApp(),
		fields:    append([]string{strconv.FormatInt(w.account.ID, 10)}, fields...),
		question:  question,
		undone:    undone,
	}
}

func (w writeTarget) label() string {
	return fmt.Sprintf("%s (%s)", w.account.Name, w.account.Phone)
}

func (w writeTarget) self() types.JID {
	jid, err := types.ParseJID(w.client.Device())
	if err != nil {
		return types.JID{}
	}
	return jid.ToNonAD()
}

func (w writeTarget) result(chat store.Chat, verdict approvalVerdict) writeResult {
	return writeResult{accountFields: fieldsOf(w.account), Chat: chatRefOf(chat), Approval: string(verdict)}
}

func (r *Registry) destination(ctx context.Context, w writeTarget, jid types.JID) store.Chat {
	chat, err := r.deps.Store.Chat(ctx, w.account.ID, jid.String())
	if err == nil {
		return chat
	}
	chat = store.Chat{JID: jid.String(), Kind: store.ChatDirect}
	if jid.Server == types.GroupServer {
		chat.Kind = store.ChatGroup
		if info, err := w.client.GroupInfo(ctx, jid); err == nil {
			chat.Name = info.Name
		}
		return chat
	}
	if contact, err := w.client.Contact(ctx, jid); err == nil {
		chat.Name = whatsapp.DisplayName(contact)
	}
	return chat
}

func chatLabel(chat store.Chat) string {
	ref := chatRefOf(chat)
	address := ref.Phone
	if address == "" {
		address = ref.JID
	}
	switch {
	case chat.Kind == store.ChatGroup && chat.Name != "":
		return fmt.Sprintf("the group %s (%s)", chat.Name, address)
	case chat.Kind == store.ChatGroup:
		return fmt.Sprintf("the group %s", address)
	case chat.Name != "":
		return fmt.Sprintf("%s (%s)", chat.Name, address)
	}
	return address
}

func (r *Registry) knownChat(ctx context.Context, w writeTarget, value string) (store.Chat, error) {
	jid, err := parseChat(ctx, w.client, value)
	if err != nil {
		return store.Chat{}, err
	}
	chat, err := r.deps.Store.Chat(ctx, w.account.ID, jid.String())
	if errors.Is(err, store.ErrNotFound) {
		return store.Chat{}, fmt.Errorf("the account %s has no stored chat %s; call list_chats to find the chat", w.account.Name, jid)
	}
	return chat, err
}

func (r *Registry) storedMessage(ctx context.Context, w writeTarget, chat store.Chat, id string) (store.Message, error) {
	msg, err := r.deps.Store.Message(ctx, w.account.ID, chat.JID, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Message{}, fmt.Errorf("the chat %s of the account %s has no stored message %q; call get_messages to find the id", chat.JID, w.account.Name, id)
	}
	return msg, err
}

func (w writeTarget) senderOf(msg store.Message) types.JID {
	if msg.FromMe {
		return w.self()
	}
	sender, err := types.ParseJID(msg.SenderJID)
	if err != nil {
		return types.JID{}
	}
	return sender
}

func excerpt(msg store.Message) string {
	text, truncated := cut(msg.Text, excerptRunes)
	if text == "" {
		text = "(" + msg.Kind + " without text)"
	}
	if truncated {
		text += "…"
	}
	who := "them"
	if msg.FromMe {
		who = "you"
	}
	return fmt.Sprintf("%q, sent by %s at %s", text, who, timestamp(msg.SentAt))
}

func (r *Registry) logUnsaved(w writeTarget, tool string, err error) {
	if err != nil {
		r.deps.Log.Error("WhatsApp accepted a write but the server could not record it", "tool", tool, "account", w.account.Name, "error", err)
	}
}
