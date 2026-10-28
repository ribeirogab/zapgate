package tools

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/store"
)

type sendTarget struct {
	writeTarget
	to     types.JID
	chat   store.Chat
	quoted *store.Message
}

func (r *Registry) sendTarget(ctx context.Context, scope Scope, account, to, replyTo string) (sendTarget, error) {
	w, err := r.writeTarget(ctx, scope, account)
	if err != nil {
		return sendTarget{}, err
	}
	jid, err := parseChat(ctx, w.client, to)
	if err != nil {
		return sendTarget{}, err
	}
	s := sendTarget{writeTarget: w, to: jid, chat: r.destination(ctx, w, jid)}
	if replyTo == "" {
		return s, nil
	}
	msg, err := r.storedMessage(ctx, w, s.chat, replyTo)
	if err != nil {
		return sendTarget{}, err
	}
	s.quoted = &msg
	return s, nil
}

func (s sendTarget) question(title string) string {
	question := fmt.Sprintf("%s\n\nAccount: %s\nTo: %s\n", title, s.label(), chatLabel(s.chat))
	if s.quoted != nil {
		question += fmt.Sprintf("In reply to: %s\n", excerpt(*s.quoted))
	}
	return question
}

func (s sendTarget) quoteContext() *waE2E.ContextInfo {
	if s.quoted == nil {
		return nil
	}
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(s.quoted.ID),
		Participant:   proto.String(s.senderOf(*s.quoted).String()),
		QuotedMessage: &waE2E.Message{Conversation: proto.String(s.quoted.Text)},
	}
}

func (s sendTarget) sentMessage(resp whatsmeow.SendResponse, kind, text string) store.Message {
	msg := store.Message{
		ChatJID:    s.chat.JID,
		ID:         resp.ID,
		SenderJID:  s.self().String(),
		SenderName: s.account.ProfileName,
		FromMe:     true,
		SentAt:     resp.Timestamp,
		Kind:       kind,
		Text:       text,
	}
	if s.quoted != nil {
		msg.QuotedID = s.quoted.ID
	}
	return msg
}
