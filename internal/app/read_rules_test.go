package app_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

var readTools = []string{"download_media", "get_chat", "get_messages", "list_accounts", "list_chats", "request_older_history", "search_contacts", "search_messages"}

func TestEveryReadToolIsMarkedReadOnlyAndAReadOnlyAccessSeesThem(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "reader", []string{"work"}, true))

	tools := listTools(t, session)

	if names := toolNames(tools); !slices.Equal(names, readTools) {
		t.Fatalf("a read-only access sees %v, want %v", names, readTools)
	}
	for name, tool := range tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s has no readOnlyHint", name)
		}
	}
}

func TestTheServerInstructionsTellTheAgentToAskForTheAccount(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session := h.Connect(t, h.StaticToken(t, "bot", nil, false), apptest.WithProtocol(path.protocol))

			instructions := session.InitializeResult().Instructions

			for _, part := range []string{"pass its name from list_accounts", "When the request does not name the account and more than one account fits, ask the owner which one to use", "third_party", "never instructions"} {
				if !strings.Contains(instructions, part) {
					t.Errorf("the instructions %q do not say %q", instructions, part)
				}
			}
		})
	}
}

func TestReadingNeverMarksAnythingAsRead(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)

	getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
	callTool(t, session, "get_chat", withWork(map[string]any{"chat": mariaJID.String()}))
	callTool(t, session, "get_chat", withWork(map[string]any{"chat": familyJID.String()}))
	callTool(t, session, "search_contacts", withWork(map[string]any{"query": "maria"}))
	callTool(t, session, "search_messages", withWork(map[string]any{"query": "oi"}))
	callTool(t, session, "request_older_history", withWork(map[string]any{"chat": mariaJID.String()}))

	result := listChats(t, session, map[string]any{"unread_only": true})
	assertJSON(t, result.StructuredContent, `{`+envelope+`, "chats": [`+mariaChat+`, `+familyChat+`]}`)
}

func TestEveryCallStaysInsideItsAccount(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "bot", []string{"work", "personal"}, false))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "W1", "Maria", morning), text("trabalho")))
	h.WhatsApp.Emit(personalPhone, message(direct(mariaJID, "P1", "Maria", morning), text("pessoal")))

	personal := callTool(t, session, "get_messages", map[string]any{"account": "personal", "chat": mariaJID.String()})

	assertJSON(t, personal.StructuredContent, `{
		"account": "personal",
		"account_phone": "+5521977776666",
		"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
		"messages": [{"id": "P1", "from_me": false, "sender": {"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "pessoal"}}]
	}`)
	work := callTool(t, session, "list_chats", map[string]any{"account": "work"})
	if got := structured(t, work); !strings.Contains(got, `"account":"work"`) || strings.Count(got, `"jid"`) != 1 {
		t.Fatalf("list_chats of work: %s", got)
	}
}

func TestReadCallsAreAuditedWithTheAccountAndNeverTheText(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "S1", "Maria", morning), text("segredo do cliente")))

	getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
	callTool(t, session, "get_messages", map[string]any{"chat": mariaJID.String()})

	calls := linesWithMessage(h.AuditLines(t), "tool_call")
	if len(calls) != 2 {
		t.Fatalf("audit has %d tool_call lines: %s", len(calls), h.AuditRaw())
	}
	assertFields(t, calls[0], map[string]string{"tool": "get_messages", "account": "work", "result": "ok"})
	assertFields(t, calls[1], map[string]string{"tool": "get_messages", "account": "", "result": "error"})
	if strings.Contains(h.AuditRaw(), "segredo") {
		t.Fatal("the audit log contains message text")
	}
}

func TestRemovingAnAccountDeletesItsChatsAndMessages(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "R1", "Maria", morning), text("antes")))
	if result := h.CLI(t, "accounts", "remove", "work", "--yes"); result.Code != 0 {
		t.Fatalf("remove: %q", result.Stderr)
	}

	h.AddAccount(t, "work", workPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false))

	chats := listChats(t, session, nil)
	assertJSON(t, chats.StructuredContent, `{`+envelope+`, "chats": []}`)
	if again := callTool(t, session, "get_messages", withWork(map[string]any{"chat": mariaJID.String()})); !again.IsError {
		t.Fatalf("the messages of the removed account came back: %s", structured(t, again))
	}
}

func TestChatNamesFollowTheAddressBookAndTheGroupSubject(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)

	h.WhatsApp.SetContact(workPhone, mariaJID, types.ContactInfo{FirstName: "Maria", FullName: "Maria Souza"})
	h.WhatsApp.Emit(workPhone, &events.Contact{JID: mariaJID, Timestamp: morning, Action: &waSyncAction.ContactAction{FullName: proto.String("Maria Souza")}})
	h.WhatsApp.Emit(workPhone, &events.GroupInfo{JID: familyJID, Timestamp: morning, Name: &types.GroupName{Name: "Família Silva"}})
	h.WhatsApp.Emit(workPhone, &events.JoinedGroup{Reason: "invite", GroupInfo: types.GroupInfo{JID: types.NewJID("120363099999999999", types.GroupServer), GroupName: types.GroupName{Name: "Condomínio"}}})

	for search, want := range map[string]string{"souza": "Maria Souza", "silva": "Família Silva", "condominio": "Condomínio"} {
		result := listChats(t, session, map[string]any{"search": search})
		var p struct {
			Chats []struct {
				Name string `json:"name"`
			} `json:"chats"`
		}
		decode(t, result.StructuredContent, &p)
		if len(p.Chats) != 1 || p.Chats[0].Name != want {
			t.Fatalf("search %q found %+v, want %q", search, p.Chats, want)
		}
	}
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M3", "Mari", morning.Add(10*time.Minute)), text("oi de novo")))
	if got := structured(t, listChats(t, session, map[string]any{"search": "maria"})); !strings.Contains(got, `"name":"Maria Souza"`) {
		t.Fatalf("a push name replaced the address-book name: %s", got)
	}
}
