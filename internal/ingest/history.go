package ingest

import (
	"slices"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/store"
)

type lidMap map[types.JID]types.JID

func (a *account) history(evt *events.HistorySync) {
	lids := chunkLIDs(evt.Data)
	snapshot := carriesUnreadCounts(evt.Data.GetSyncType())
	for _, conv := range evt.Data.GetConversations() {
		a.conversation(conv, lids, snapshot)
	}
}

func chunkLIDs(data *waHistorySync.HistorySync) lidMap {
	lids := lidMap{}
	for _, mapping := range data.GetPhoneNumberToLidMappings() {
		lid, lidErr := types.ParseJID(mapping.GetLidJID())
		pn, pnErr := types.ParseJID(mapping.GetPnJID())
		if lidErr == nil && pnErr == nil && lid.Server == types.HiddenUserServer && pn.Server == types.DefaultUserServer {
			lids[lid.ToNonAD()] = pn.ToNonAD()
		}
	}
	return lids
}

func (lids lidMap) alt(jid types.JID) types.JID {
	if jid.Server != types.HiddenUserServer {
		return types.JID{}
	}
	return lids[jid.ToNonAD()]
}

func carriesUnreadCounts(kind waHistorySync.HistorySync_HistorySyncType) bool {
	switch kind {
	case waHistorySync.HistorySync_INITIAL_BOOTSTRAP, waHistorySync.HistorySync_RECENT, waHistorySync.HistorySync_FULL:
		return true
	}
	return false
}

func (a *account) conversation(conv *waHistorySync.Conversation, lids lidMap, snapshot bool) {
	chat, err := types.ParseJID(conv.GetID())
	if err != nil || skippedChat(chat) {
		return
	}
	if pn, err := types.ParseJID(conv.GetPnJID()); err == nil && pn.Server == types.DefaultUserServer && chat.Server == types.HiddenUserServer {
		lids[chat.ToNonAD()] = pn.ToNonAD()
	}
	messages := a.parseConversation(chat, conv, lids)
	if len(messages) == 0 {
		return
	}
	if chat.Server == types.GroupServer && conv.GetName() != "" {
		a.save(a.store.SaveChat(a.ctx, a.src.AccountID, store.Chat{JID: chat.String(), Kind: store.ChatGroup, Name: conv.GetName()}))
	}
	for _, msg := range messages {
		a.ingest(msg, true)
		a.historyReactions(msg, lids)
	}
	if snapshot && conv.UnreadCount != nil {
		a.save(a.store.SetUnreadCount(a.ctx, a.src.AccountID, a.chatJID(messages[0].Info).String(), int(conv.GetUnreadCount())))
	}
}

func (a *account) parseConversation(chat types.JID, conv *waHistorySync.Conversation, lids lidMap) []*events.Message {
	var messages []*events.Message
	for _, item := range conv.GetMessages() {
		web := item.GetMessage()
		if web.GetMessage() == nil {
			continue
		}
		msg, err := a.src.Client.ParseWebMessage(chat, web)
		if err != nil {
			a.log.Debug("cannot parse a history message", "account_id", a.src.AccountID, "error", err)
			continue
		}
		withAlternates(&msg.Info.MessageSource, lids)
		messages = append(messages, msg)
	}
	slices.SortStableFunc(messages, func(x, y *events.Message) int { return x.Info.Timestamp.Compare(y.Info.Timestamp) })
	return messages
}

func withAlternates(source *types.MessageSource, lids lidMap) {
	if source.SenderAlt.IsEmpty() {
		source.SenderAlt = lids.alt(source.Sender)
	}
	if source.Chat.Server != types.HiddenUserServer {
		return
	}
	if source.IsFromMe && source.RecipientAlt.IsEmpty() {
		source.RecipientAlt = lids.alt(source.Chat)
	}
}

func (a *account) historyReactions(msg *events.Message, lids lidMap) {
	for _, r := range msg.SourceWebMsg.GetReactions() {
		info, ok := reactorOf(msg.Info, r.GetKey(), lids)
		if !ok {
			continue
		}
		a.react(info, &waE2E.ReactionMessage{
			Key:               &waCommon.MessageKey{RemoteJID: proto.String(msg.Info.Chat.String()), ID: proto.String(msg.Info.ID)},
			Text:              proto.String(r.GetText()),
			SenderTimestampMS: proto.Int64(r.GetSenderTimestampMS()),
		})
	}
}

func reactorOf(target types.MessageInfo, key *waCommon.MessageKey, lids lidMap) (types.MessageInfo, bool) {
	info := types.MessageInfo{Timestamp: target.Timestamp}
	info.Chat = target.Chat
	info.IsGroup = target.IsGroup
	info.IsFromMe = key.GetFromMe()
	switch {
	case info.IsFromMe:
		info.RecipientAlt = lids.alt(target.Chat)
	case key.GetParticipant() != "":
		sender, err := types.ParseJID(key.GetParticipant())
		if err != nil {
			return info, false
		}
		info.Sender = sender
	case target.Chat.Server != types.GroupServer:
		info.Sender = target.Chat
	default:
		return info, false
	}
	info.SenderAlt = lids.alt(info.Sender)
	return info, true
}
