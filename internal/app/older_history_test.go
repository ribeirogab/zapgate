package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/proto/waHistorySync"

	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/whatsapp/fake"
)

func readOnlySession(t *testing.T, h *apptest.Harness) *mcp.ClientSession {
	t.Helper()
	h.AddAccount(t, "work", workPhone, "Alex")
	return h.Connect(t, h.StaticToken(t, "reader", []string{"work"}, true))
}

func requestOlder(t *testing.T, session *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	result := callTool(t, session, "request_older_history", withWork(args))
	if result.IsError {
		return "error: " + errorText(result)
	}
	return structured(t, result)
}

func TestRequestOlderHistoryAsksThePhoneAndTheAnswerAppearsInGetMessages(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning), text("oi")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M2", "Maria", morning.Add(time.Minute)), text("tudo bem?")))

	result := callTool(t, session, "request_older_history", withWork(map[string]any{"chat": "+" + mariaPhone, "count": 30}))

	if result.IsError {
		t.Fatalf("request_older_history failed: %s", errorText(result))
	}
	assertJSON(t, result.StructuredContent, `{`+envelope+`, "chat": `+mariaChatRef+`, "requested": 30, "before_message_id": "M1"}`)
	want := []fake.HistoryRequest{{Chat: mariaJID, OldestID: "M1", OldestAt: morning, Count: 30}}
	if got := h.WhatsApp.HistoryRequests(workPhone); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("requests %+v, want %+v", got, want)
	}

	h.WhatsApp.Emit(workPhone, historySync(waHistorySync.HistorySync_ON_DEMAND, nil, conversation(mariaJID, "", 0,
		webMessage{chat: mariaJID, id: "O1", pushName: "Maria", at: morning.Add(-48 * time.Hour), content: text("mensagem antiga")}.info(),
	)))

	if got := page(t, session, map[string]any{"chat": mariaJID.String()}); got.ids() != "O1,M1,M2" {
		t.Fatalf("messages %s", got.ids())
	}
	assertJSON(t, listChats(t, session, nil).StructuredContent, `{`+envelope+`, "chats": [{"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333", "unread_count": 2, "last_message_at": "2026-10-13T09:01:00Z"}]}`)
}

func TestRequestOlderHistoryRefusesWhatThePhoneCannotAnswer(t *testing.T) {
	h := apptest.Start(t)
	session := readOnlySession(t, h)
	family(h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning), text("oi")))

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "chat without messages", args: map[string]any{"chat": familyJID.String()}, want: "has no stored message"},
		{name: "count too large", args: map[string]any{"chat": mariaJID.String(), "count": 51}, want: "error: "},
		{name: "chat not valid", args: map[string]any{"chat": "maria"}, want: "is not valid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := requestOlder(t, session, tc.args); !strings.HasPrefix(got, "error: ") || !strings.Contains(got, tc.want) {
				t.Fatalf("got %s, want an error with %q", got, tc.want)
			}
		})
	}
	if got := h.WhatsApp.HistoryRequests(workPhone); len(got) != 0 {
		t.Fatalf("the phone got requests %+v", got)
	}
}

func TestRequestOlderHistoryNeedsTheConnectionAndWaitsBetweenRequests(t *testing.T) {
	h := apptest.Start(t)
	session := readOnlySession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning), text("oi")))
	args := map[string]any{"chat": mariaJID.String()}

	h.WhatsApp.DropConnection(workPhone)
	offline := requestOlder(t, session, args)
	h.WhatsApp.RestoreConnection(workPhone)
	first := requestOlder(t, session, args)
	tooSoon := requestOlder(t, session, args)
	h.Clock.Advance(10 * time.Second)
	later := requestOlder(t, session, args)

	if !strings.HasPrefix(offline, "error: ") || strings.HasPrefix(first, "error: ") || !strings.Contains(tooSoon, "wait") || strings.HasPrefix(later, "error: ") {
		t.Fatalf("offline %s\nfirst %s\ntoo soon %s\nlater %s", offline, first, tooSoon, later)
	}
	if got := h.WhatsApp.HistoryRequests(workPhone); len(got) != 2 || got[0].Count != 50 {
		t.Fatalf("requests %+v, want two with the default count 50", got)
	}
}
