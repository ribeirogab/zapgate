package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestAReceivedTextAppearsInGetMessages(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			session := h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false), apptest.WithProtocol(path.protocol))

			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))

			result := callTool(t, session, "get_messages", map[string]any{"account": "work", "chat": mariaJID.String()})
			if result.IsError {
				t.Fatalf("get_messages failed: %+v", result.Content)
			}
			assertJSON(t, result.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"messages": [{
					"id": "3EB0A1",
					"from_me": false,
					"sender": {"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria"},
					"sent_at": "2026-10-13T09:00:00Z",
					"kind": "text",
					"third_party": {"text": "oi, tudo bem?"}
				}]
			}`)
		})
	}
}

func workSession(t *testing.T, h *apptest.Harness) *mcp.ClientSession {
	t.Helper()
	h.AddAccount(t, "work", workPhone, "Alex")
	return h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false))
}

func getMessages(t *testing.T, session *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result := callTool(t, session, "get_messages", args)
	if result.IsError {
		t.Fatalf("get_messages %v failed: %s", args, errorText(result))
	}
	return result
}

func errorText(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestAMessageTheOwnerSendsFromThePhoneAppearsUnderThePhoneNumber(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))

	h.WhatsApp.Emit(workPhone, message(ownFromPhone(mariaLID, mariaJID, "3A0B2", morning.Add(time.Minute)), fromPhone(mariaLID, "tudo, e você?")))
	h.WhatsApp.Emit(workPhone, message(ownFromPhone(mariaLID, mariaJID, "3A0B3", morning.Add(2*time.Minute)), reply("respondendo", "3EB0A1", mariaJID)))

	result := getMessages(t, session, map[string]any{"account": "work", "chat": "+55 31 94444-3333"})
	assertJSON(t, result.StructuredContent, `{
		"account": "work",
		"account_phone": "+5511988887777",
		"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
		"messages": [
			{"id": "3EB0A1", "from_me": false, "sender": {"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "oi, tudo bem?"}},
			{"id": "3A0B2", "from_me": true, "sender": {"jid": "5511988887777@s.whatsapp.net", "phone": "+5511988887777", "name": "Alex"}, "sent_at": "2026-10-13T09:01:00Z", "kind": "text", "third_party": {"text": "tudo, e você?"}},
			{"id": "3A0B3", "from_me": true, "sender": {"jid": "5511988887777@s.whatsapp.net", "phone": "+5511988887777", "name": "Alex"}, "sent_at": "2026-10-13T09:02:00Z", "kind": "text", "quoted_id": "3EB0A1", "third_party": {"text": "respondendo"}}
		]
	}`)
}

func TestAMessageDeliveredTwiceIsStoredOnce(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	first := message(direct(mariaJID, "3EB0D1", "Maria", morning), text("uma vez"))

	h.WhatsApp.Emit(workPhone, first)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0D1", "Maria", morning), text("uma vez")))

	if got := page(t, session, map[string]any{"chat": mariaJID.String()}); got.ids() != "3EB0D1" {
		t.Fatalf("messages %s", got.ids())
	}
	if got := structured(t, listChats(t, session, nil)); !strings.Contains(got, `"unread_count":1`) {
		t.Fatalf("list_chats %s", got)
	}
}
