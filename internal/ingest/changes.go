package ingest

import (
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
)

func (a *account) protocol(info types.MessageInfo, msg *waE2E.ProtocolMessage) {
	switch msg.GetType() {
	case waE2E.ProtocolMessage_MESSAGE_EDIT:
		a.edit(info, msg)
	case waE2E.ProtocolMessage_REVOKE:
		a.revoke(info, msg)
	}
}

func (a *account) edit(info types.MessageInfo, msg *waE2E.ProtocolMessage) {
	edited, ok := classify(msg.GetEditedMessage())
	if !ok {
		return
	}
	at := millisOr(msg.GetTimestampMS(), info.Timestamp)
	a.save(a.store.EditMessage(a.ctx, a.src.AccountID, a.chatJID(info).String(), msg.GetKey().GetID(), a.senderOf(info).String(), edited.text, at))
}

func (a *account) revoke(info types.MessageInfo, msg *waE2E.ProtocolMessage) {
	byAdmin := info.Edit == types.EditAttributeAdminRevoke && info.Chat.Server == types.GroupServer
	a.save(a.store.RevokeMessage(a.ctx, a.src.AccountID, a.chatJID(info).String(), msg.GetKey().GetID(), a.senderOf(info).String(), byAdmin, info.Timestamp))
}

func (a *account) react(info types.MessageInfo, msg *waE2E.ReactionMessage) {
	r := store.Reaction{
		ChatJID:   a.chatJID(info).String(),
		MessageID: msg.GetKey().GetID(),
		SenderJID: a.senderOf(info).String(),
		FromMe:    info.IsFromMe,
		Emoji:     msg.GetText(),
		ReactedAt: millisOr(msg.GetSenderTimestampMS(), info.Timestamp),
	}
	if !info.IsFromMe {
		r.SenderName = info.PushName
	}
	a.save(a.store.React(a.ctx, a.src.AccountID, r))
}

func millisOr(ms int64, fallback time.Time) time.Time {
	if ms <= 0 {
		return fallback
	}
	return time.UnixMilli(ms)
}
