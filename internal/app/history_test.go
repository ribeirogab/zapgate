package app_test

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

type webMessage struct {
	chat        types.JID
	id          string
	fromMe      bool
	participant types.JID
	pushName    string
	at          time.Time
	content     *waE2E.Message
}

func (m webMessage) info() *waWeb.WebMessageInfo {
	key := &waCommon.MessageKey{RemoteJID: proto.String(m.chat.String()), FromMe: proto.Bool(m.fromMe), ID: proto.String(m.id)}
	if !m.participant.IsEmpty() {
		key.Participant = proto.String(m.participant.String())
	}
	return &waWeb.WebMessageInfo{
		Key:              key,
		Message:          m.content,
		MessageTimestamp: proto.Uint64(uint64(m.at.Unix())),
		PushName:         proto.String(m.pushName),
	}
}

func conversation(chat types.JID, name string, unread uint32, messages ...*waWeb.WebMessageInfo) *waHistorySync.Conversation {
	conv := &waHistorySync.Conversation{ID: proto.String(chat.String()), UnreadCount: proto.Uint32(unread)}
	if name != "" {
		conv.Name = proto.String(name)
	}
	for i := len(messages) - 1; i >= 0; i-- {
		conv.Messages = append(conv.Messages, &waHistorySync.HistorySyncMsg{MsgOrderID: proto.Uint64(uint64(len(messages) - i)), Message: messages[i]})
	}
	return conv
}

func historySync(kind waHistorySync.HistorySync_HistorySyncType, mappings map[types.JID]types.JID, conversations ...*waHistorySync.Conversation) *events.HistorySync {
	data := &waHistorySync.HistorySync{SyncType: kind.Enum(), ChunkOrder: proto.Uint32(1), Progress: proto.Uint32(100), Conversations: conversations}
	for lid, pn := range mappings {
		data.PhoneNumberToLidMappings = append(data.PhoneNumberToLidMappings, &waHistorySync.PhoneNumberToLIDMapping{PnJID: proto.String(pn.String()), LidJID: proto.String(lid.String())})
	}
	return &events.HistorySync{Data: data}
}

func pairingHistory() *events.HistorySync {
	yesterday := morning.Add(-24 * time.Hour)
	withReaction := webMessage{chat: mariaLID, id: "H1", pushName: "Maria", at: yesterday, content: text("ontem eu disse oi")}.info()
	withReaction.Reactions = []*waWeb.Reaction{{
		Key:               &waCommon.MessageKey{RemoteJID: proto.String(mariaLID.String()), FromMe: proto.Bool(true), ID: proto.String("HR1")},
		Text:              proto.String("👋"),
		SenderTimestampMS: proto.Int64(yesterday.Add(time.Minute).UnixMilli()),
	}}
	stub := webMessage{chat: mariaLID, id: "STUB", at: yesterday}.info()
	stub.MessageStubType = waWeb.WebMessageInfo_E2E_ENCRYPTED.Enum()
	return historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP, map[types.JID]types.JID{mariaLID: mariaJID, joaoLID: joaoJID},
		conversation(mariaLID, "", 2,
			stub,
			withReaction,
			webMessage{chat: mariaLID, id: "H2", fromMe: true, at: yesterday.Add(2 * time.Minute), content: text("oi, Maria")}.info(),
			webMessage{chat: mariaLID, id: "H3", pushName: "Maria", at: yesterday.Add(3 * time.Minute), content: image("a foto")}.info(),
			webMessage{chat: mariaLID, id: "M1", pushName: "Maria", at: morning, content: text("bom dia")}.info(),
		),
		conversation(familyJID, "Família", 0,
			webMessage{chat: familyJID, id: "HG1", participant: joaoLID, pushName: "João", at: yesterday, content: text("oi, família")}.info(),
		),
	)
}

func TestThePairingHistoryIsStoredWithoutDuplicates(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.MapLID(workLID, workJID)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning), text("bom dia")))

	h.WhatsApp.Emit(workPhone, pairingHistory())
	h.WhatsApp.Emit(workPhone, pairingHistory())

	maria := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
	assertJSON(t, maria.StructuredContent, mariaMessages(
		fromMaria("H1", "2026-10-12T09:00:00Z", `"kind": "text", "third_party": {"text": "ontem eu disse oi"},
			"reactions": [{"emoji": "👋", "from_me": true, "sender": `+ownerSender+`, "reacted_at": "2026-10-12T09:01:00Z"}]`)+`,
		{"id": "H2", "from_me": true, "sender": `+ownerSender+`, "sent_at": "2026-10-12T09:02:00Z", "kind": "text", "third_party": {"text": "oi, Maria"}}, `+
			fromMaria("H3", "2026-10-12T09:03:00Z", `"kind": "image", "media": {"mime": "image/jpeg", "size": 48213}, "third_party": {"text": "a foto"}`)+`, `+
			fromMaria("M1", "2026-10-13T09:00:00Z", `"kind": "text", "third_party": {"text": "bom dia"}`)))
	group := getMessages(t, session, withWork(map[string]any{"chat": familyJID.String()}))
	assertJSON(t, group.StructuredContent, `{`+envelope+`, "chat": {"jid": "120363012345678901@g.us", "kind": "group", "name": "Família"},
		"messages": [{"id": "HG1", "from_me": false, "sender": {"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222", "name": "João"}, "sent_at": "2026-10-12T09:00:00Z", "kind": "text", "third_party": {"text": "oi, família"}}]}`)
	chats := listChats(t, session, nil)
	assertJSON(t, chats.StructuredContent, `{`+envelope+`, "chats": [
		{"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333", "unread_count": 2, "last_message_at": "2026-10-13T09:00:00Z"},
		{"jid": "120363012345678901@g.us", "kind": "group", "name": "Família", "unread_count": 0, "last_message_at": "2026-10-12T09:00:00Z"}]}`)
}
