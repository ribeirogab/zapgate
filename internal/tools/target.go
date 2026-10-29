package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
)

type target struct {
	writeTarget
	chat    store.Chat
	message store.Message
}

func (r *Registry) target(ctx context.Context, req *mcp.CallToolRequest, accountName, chat, messageID string) (target, error) {
	scope, err := r.scopeFor(ctx, req)
	if err != nil {
		return target{}, err
	}
	w, err := r.writeTarget(ctx, scope, accountName)
	if err != nil {
		return target{}, err
	}
	stored, err := r.knownChat(ctx, w, chat)
	if err != nil {
		return target{}, err
	}
	msg, err := r.storedMessage(ctx, w, stored, messageID)
	if err != nil {
		return target{}, err
	}
	if !msg.RevokedAt.IsZero() {
		return target{}, fmt.Errorf("the message %s was deleted for everyone at %s, so nothing was done", msg.ID, timestamp(msg.RevokedAt))
	}
	return target{writeTarget: w, chat: stored, message: msg}, nil
}

func (t target) chatJID() types.JID {
	jid, _ := types.ParseJID(t.chat.JID)
	return jid
}

func (t target) describe(question string) string {
	return fmt.Sprintf("%s\n\nAccount: %s\nChat: %s\nMessage: %s", question, t.label(), chatLabel(t.chat), excerpt(t.message))
}

func (t target) ownMessage(verb string) error {
	if t.message.FromMe {
		return nil
	}
	return fmt.Errorf("the message %s was not sent by the account %s; only its own messages can be %s", t.message.ID, t.account.Name, verb)
}
