package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const groupNameTimeout = 5 * time.Second

type Ingester struct {
	store *store.Store
	log   *slog.Logger

	mu     sync.Mutex
	merged map[int64]map[types.JID]bool
}

func New(st *store.Store, log *slog.Logger) *Ingester {
	return &Ingester{store: st, log: log, merged: map[int64]map[types.JID]bool{}}
}

type Source struct {
	AccountID int64
	Client    whatsapp.Client
}

func (i *Ingester) Handle(ctx context.Context, src Source, evt any) {
	acc := i.forAccount(ctx, src)
	switch e := evt.(type) {
	case *events.Message:
		acc.message(e)
	case *events.HistorySync:
		acc.history(e)
	case *events.Receipt:
		acc.receipt(e)
	case *events.GroupInfo:
		if e.Name != nil {
			acc.save(i.store.SetChatName(ctx, src.AccountID, e.JID.String(), e.Name.Name))
		}
	case *events.JoinedGroup:
		acc.save(i.store.SaveChat(ctx, src.AccountID, store.Chat{JID: e.JID.String(), Kind: store.ChatGroup, Name: e.Name}))
	case *events.Contact:
		acc.renameDirectChat(e.JID)
	case *events.PushName:
		acc.renameDirectChat(e.JID)
	}
}

type account struct {
	*Ingester
	ctx context.Context
	src Source
	ids identities
}

func (i *Ingester) forAccount(ctx context.Context, src Source) *account {
	acc := &account{Ingester: i, ctx: ctx, src: src}
	acc.ids = identities{ctx: ctx, directory: src.Client, merge: acc.merge}
	return acc
}

func (a *account) message(evt *events.Message) {
	a.ingest(evt, false)
}

func (a *account) ingest(evt *events.Message, fromHistory bool) {
	if skippedChat(evt.Info.Chat) {
		return
	}
	switch {
	case evt.Message.GetProtocolMessage() != nil:
		a.protocol(evt.Info, evt.Message.GetProtocolMessage())
	case evt.Message.GetReactionMessage() != nil:
		a.react(evt.Info, evt.Message.GetReactionMessage())
	default:
		a.keep(evt, fromHistory)
	}
}

func (a *account) keep(evt *events.Message, fromHistory bool) {
	content, ok := classify(evt.Message)
	if !ok {
		return
	}
	chat := a.chatOf(evt.Info)
	msg := store.Message{
		ChatJID:    chat.JID,
		ID:         evt.Info.ID,
		SenderJID:  a.senderOf(evt.Info).String(),
		SenderName: evt.Info.PushName,
		FromMe:     evt.Info.IsFromMe,
		SentAt:     evt.Info.Timestamp,
		Kind:       content.kind,
		Text:       content.text,
		QuotedID:   content.quotedID,
		Media:      content.media,
		Details:    content.details,
	}
	save := a.store.SaveMessage
	if fromHistory {
		save = a.store.SaveHistoryMessage
	}
	a.save(save(a.ctx, a.src.AccountID, chat, msg))
	if evt.IsEdit {
		a.save(a.store.EditMessage(a.ctx, a.src.AccountID, msg.ChatJID, msg.ID, msg.SenderJID, msg.Text, msg.SentAt))
	}
}

func (a *account) chatJID(info types.MessageInfo) types.JID {
	if info.Chat.Server == types.GroupServer {
		return info.Chat
	}
	alt := info.SenderAlt
	if info.IsFromMe {
		alt = info.RecipientAlt
	}
	return a.ids.phone(info.Chat, alt)
}

func (a *account) chatOf(info types.MessageInfo) store.Chat {
	if info.Chat.Server == types.GroupServer {
		return store.Chat{JID: info.Chat.String(), Kind: store.ChatGroup, Name: a.groupName(info.Chat)}
	}
	jid := a.chatJID(info)
	name := a.displayName(jid)
	if name == "" && !info.IsFromMe {
		name = info.PushName
	}
	return store.Chat{JID: jid.String(), Kind: store.ChatDirect, Name: name}
}

func (a *account) senderOf(info types.MessageInfo) types.JID {
	if info.IsFromMe {
		if self, err := types.ParseJID(a.src.Client.Device()); err == nil && !self.IsEmpty() {
			return self.ToNonAD()
		}
	}
	return a.ids.phone(info.Sender, info.SenderAlt)
}

func (a *account) groupName(group types.JID) string {
	chat, err := a.store.Chat(a.ctx, a.src.AccountID, group.String())
	if err == nil && chat.Name != "" {
		return chat.Name
	}
	ctx, cancel := context.WithTimeout(a.ctx, groupNameTimeout)
	defer cancel()
	info, err := a.src.Client.GroupInfo(ctx, group)
	if err != nil {
		a.log.Debug("cannot read the group name", "error", err)
		return ""
	}
	return info.Name
}

func (a *account) displayName(jid types.JID) string {
	contact, err := a.src.Client.Contact(a.ctx, jid)
	if err != nil {
		return ""
	}
	return whatsapp.DisplayName(contact)
}

func (a *account) receipt(evt *events.Receipt) {
	if evt.Type != types.ReceiptTypeReadSelf || skippedChat(evt.Chat) {
		return
	}
	chat := evt.Chat
	if chat.Server != types.GroupServer {
		chat = a.ids.phone(evt.Chat, evt.RecipientAlt)
	}
	a.save(a.store.MarkChatReadUntil(a.ctx, a.src.AccountID, chat.String(), evt.Timestamp))
}

func (a *account) renameDirectChat(jid types.JID) {
	pn := a.ids.phone(jid, types.JID{})
	a.save(a.store.SetChatName(a.ctx, a.src.AccountID, pn.String(), a.displayName(pn)))
}

func (a *account) merge(lid, pn types.JID) {
	if a.alreadyMerged(a.src.AccountID, lid) {
		return
	}
	err := a.store.MergeIdentity(a.ctx, a.src.AccountID, lid.String(), pn.String())
	a.save(err)
	if err == nil {
		a.rememberMerged(a.src.AccountID, lid)
	}
}

func (i *Ingester) alreadyMerged(accountID int64, lid types.JID) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.merged[accountID][lid]
}

func (i *Ingester) rememberMerged(accountID int64, lid types.JID) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.merged[accountID] == nil {
		i.merged[accountID] = map[types.JID]bool{}
	}
	i.merged[accountID][lid] = true
}

func (a *account) save(err error) {
	if err != nil && a.ctx.Err() == nil {
		a.log.Error("cannot save a WhatsApp event", "account_id", a.src.AccountID, "error", err)
	}
}

func skippedChat(chat types.JID) bool {
	switch chat.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
		return false
	}
	return true
}
