package app_test

import (
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const (
	mariaChat  = `{"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333", "unread_count": 2, "last_message_at": "2026-10-13T09:05:00Z"}`
	familyChat = `{"jid": "120363012345678901@g.us", "kind": "group", "name": "Família", "unread_count": 1, "last_message_at": "2026-10-13T09:03:00Z"}`
	joaoChat   = `{"jid": "5541933332222@s.whatsapp.net", "kind": "direct", "name": "João", "phone": "+5541933332222", "unread_count": 0, "last_message_at": "2026-10-13T09:02:00Z"}`
	envelope   = `"account": "work", "account_phone": "+5511988887777"`
)

func threeChats(t *testing.T, h *apptest.Harness) *mcp.ClientSession {
	t.Helper()
	session := workSession(t, h)
	family(h)
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "J1", "João", morning.Add(time.Minute)), text("oi")))
	h.WhatsApp.Emit(workPhone, message(ownFromPhone(joaoLID, joaoJID, "J2", morning.Add(2*time.Minute)), fromPhone(joaoLID, "oi, João")))
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "F1", "Maria", morning.Add(3*time.Minute)), text("almoço domingo?")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning.Add(4*time.Minute)), text("oi")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M2", "Maria", morning.Add(5*time.Minute)), text("me liga")))
	return session
}

func listChats(t *testing.T, session *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result := callTool(t, session, "list_chats", withWork(args))
	if result.IsError {
		t.Fatalf("list_chats %v failed: %s", args, errorText(result))
	}
	return result
}

func withWork(args map[string]any) map[string]any {
	out := map[string]any{"account": "work"}
	for key, value := range args {
		out[key] = value
	}
	return out
}

func TestListChatsShowsTheNewestChatsFirstWithUnreadCounts(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)

	result := listChats(t, session, nil)

	assertJSON(t, result.StructuredContent, `{`+envelope+`, "chats": [`+mariaChat+`, `+familyChat+`, `+joaoChat+`]}`)
}

func TestListChatsSearchesByNameOrNumber(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)
	cases := map[string]string{
		"mar":         mariaChat,
		"FAMILIA":     familyChat,
		"joão":        joaoChat,
		"joao":        joaoChat,
		"+55 41 9333": joaoChat,
	}

	for search, want := range cases {
		result := listChats(t, session, map[string]any{"search": search})

		assertJSON(t, result.StructuredContent, `{`+envelope+`, "chats": [`+want+`]}`)
	}
	none := listChats(t, session, map[string]any{"search": "pedro"})
	assertJSON(t, none.StructuredContent, `{`+envelope+`, "chats": []}`)
}

func TestListChatsFiltersTheUnreadChats(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)

	unread := listChats(t, session, map[string]any{"unread_only": true})

	assertJSON(t, unread.StructuredContent, `{`+envelope+`, "chats": [`+mariaChat+`, `+familyChat+`]}`)

	h.WhatsApp.Emit(workPhone, &events.Receipt{
		MessageSource: types.MessageSource{Chat: mariaLID, Sender: workLID, IsFromMe: true, RecipientAlt: mariaJID},
		MessageIDs:    []types.MessageID{"M2", "M1"},
		Timestamp:     morning.Add(6 * time.Minute),
		Type:          types.ReceiptTypeReadSelf,
	})

	afterRead := listChats(t, session, map[string]any{"unread_only": true})
	assertJSON(t, afterRead.StructuredContent, `{`+envelope+`, "chats": [`+familyChat+`]}`)
}

func TestListChatsPagesWithACursor(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)

	first := listChats(t, session, map[string]any{"limit": 2})

	var page struct {
		NextCursor string `json:"next_cursor"`
	}
	decode(t, first.StructuredContent, &page)
	if page.NextCursor == "" {
		t.Fatalf("the first page has no next_cursor: %s", structured(t, first))
	}
	assertJSON(t, first.StructuredContent, `{`+envelope+`, "chats": [`+mariaChat+`, `+familyChat+`], "next_cursor": "`+page.NextCursor+`"}`)
	second := listChats(t, session, map[string]any{"limit": 2, "cursor": page.NextCursor})
	assertJSON(t, second.StructuredContent, `{`+envelope+`, "chats": [`+joaoChat+`]}`)
	bad := callTool(t, session, "list_chats", withWork(map[string]any{"cursor": "not-a-cursor"}))
	if !bad.IsError {
		t.Fatalf("a bad cursor was accepted: %s", structured(t, bad))
	}
}
