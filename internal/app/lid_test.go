package app_test

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func family(h *apptest.Harness) {
	h.WhatsApp.SetGroup(workPhone, types.GroupInfo{
		JID:       familyJID,
		GroupName: types.GroupName{Name: "Família"},
		Participants: []types.GroupParticipant{
			{JID: workLID, LID: workLID, PhoneNumber: workJID, IsSuperAdmin: true},
			{JID: mariaLID, LID: mariaLID, PhoneNumber: mariaJID, IsAdmin: true},
			{JID: pedroLID, LID: pedroLID},
		},
	})
}

func TestAGroupSenderWithALIDAppearsWithThePhoneNumber(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	family(h)

	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, types.JID{User: mariaLID.User, Server: types.HiddenUserServer, Device: 3}, types.JID{User: mariaPhone, Server: types.DefaultUserServer, Device: 3}, "3EB0G1", "Maria", morning), text("bom dia")))

	result := getMessages(t, session, map[string]any{"account": "work", "chat": familyJID.String()})
	assertJSON(t, result.StructuredContent, `{
		"account": "work",
		"account_phone": "+5511988887777",
		"chat": {"jid": "120363012345678901@g.us", "kind": "group", "name": "Família"},
		"messages": [{"id": "3EB0G1", "from_me": false, "sender": {"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "bom dia"}}]
	}`)
}

func TestADirectChatWithALIDIsStoredUnderThePhoneNumber(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.MapLID(joaoLID, joaoJID)

	h.WhatsApp.Emit(workPhone, message(directFromLID(mariaLID, mariaJID, "3EB0L1", "Maria", morning), text("com sender_pn")))
	h.WhatsApp.Emit(workPhone, message(directFromLID(joaoLID, types.JID{}, "3EB0L2", "João", morning), text("só com o mapa")))

	maria := getMessages(t, session, map[string]any{"account": "work", "chat": mariaLID.String()})
	assertJSON(t, maria.StructuredContent, `{
		"account": "work",
		"account_phone": "+5511988887777",
		"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
		"messages": [{"id": "3EB0L1", "from_me": false, "sender": {"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "com sender_pn"}}]
	}`)
	joao := getMessages(t, session, map[string]any{"account": "work", "chat": "+" + joaoPhone})
	assertJSON(t, joao.StructuredContent, `{
		"account": "work",
		"account_phone": "+5511988887777",
		"chat": {"jid": "5541933332222@s.whatsapp.net", "kind": "direct", "name": "João", "phone": "+5541933332222"},
		"messages": [{"id": "3EB0L2", "from_me": false, "sender": {"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222", "name": "João"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "só com o mapa"}}]
	}`)
}

func TestMessagesFromAnUnknownLIDMoveToThePhoneNumberOnceItIsKnown(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	family(h)
	h.WhatsApp.Emit(workPhone, message(directFromLID(joaoLID, types.JID{}, "3EB0U1", "João", morning), text("antes do mapa")))
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, joaoLID, types.JID{}, "3EB0U2", "João", morning), text("no grupo")))
	hidden := getMessages(t, session, map[string]any{"account": "work", "chat": joaoLID.String()})
	assertJSON(t, hidden.StructuredContent, `{
		"account": "work",
		"account_phone": "+5511988887777",
		"chat": {"jid": "176543210987654@lid", "kind": "direct", "name": "João"},
		"messages": [{"id": "3EB0U1", "from_me": false, "sender": {"jid": "176543210987654@lid", "name": "João"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "antes do mapa"}}]
	}`)

	h.WhatsApp.Emit(workPhone, message(directFromLID(joaoLID, joaoJID, "3EB0U3", "João", morning.Add(time.Minute)), text("depois do mapa")))

	direct := getMessages(t, session, map[string]any{"account": "work", "chat": joaoLID.String()})
	assertJSON(t, direct.StructuredContent, `{
		"account": "work",
		"account_phone": "+5511988887777",
		"chat": {"jid": "5541933332222@s.whatsapp.net", "kind": "direct", "name": "João", "phone": "+5541933332222"},
		"messages": [
			{"id": "3EB0U1", "from_me": false, "sender": {"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222", "name": "João"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "antes do mapa"}},
			{"id": "3EB0U3", "from_me": false, "sender": {"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222", "name": "João"}, "sent_at": "2026-10-13T09:01:00Z", "kind": "text", "third_party": {"text": "depois do mapa"}}
		]
	}`)
	group := getMessages(t, session, map[string]any{"account": "work", "chat": familyJID.String()})
	assertJSON(t, group.StructuredContent, `{
		"account": "work",
		"account_phone": "+5511988887777",
		"chat": {"jid": "120363012345678901@g.us", "kind": "group", "name": "Família"},
		"messages": [{"id": "3EB0U2", "from_me": false, "sender": {"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222", "name": "João"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "no grupo"}}]
	}`)
}

func TestAnUnknownLIDChatJoinsAnExistingPhoneChat(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "P1", "João Pedro", morning), text("pelo número")))
	h.WhatsApp.Emit(workPhone, message(directFromLID(joaoLID, types.JID{}, "P2", "Jota", morning.Add(time.Minute)), text("pelo lid")))

	h.WhatsApp.Emit(workPhone, message(directFromLID(joaoLID, joaoJID, "P3", "João Pedro", morning.Add(2*time.Minute)), text("os dois")))

	if got := page(t, session, map[string]any{"chat": joaoJID.String()}); got.ids() != "P1,P2,P3" {
		t.Fatalf("messages %s", got.ids())
	}
	chats := listChats(t, session, nil)
	assertJSON(t, chats.StructuredContent, `{`+envelope+`, "chats": [{"jid": "5541933332222@s.whatsapp.net", "kind": "direct", "name": "João Pedro", "phone": "+5541933332222", "unread_count": 3, "last_message_at": "2026-10-13T09:02:00Z"}]}`)
}
