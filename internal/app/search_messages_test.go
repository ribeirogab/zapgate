package app_test

import (
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

type searchPage struct {
	Results []struct {
		Chat struct {
			JID string `json:"jid"`
		} `json:"chat"`
		Message struct {
			ID         string `json:"id"`
			ThirdParty struct {
				Text string `json:"text"`
			} `json:"third_party"`
		} `json:"message"`
	} `json:"results"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

func (p searchPage) ids() string {
	var ids []string
	for _, result := range p.Results {
		ids = append(ids, result.Message.ID)
	}
	return strings.Join(ids, ",")
}

func searchMessages(t *testing.T, session *mcp.ClientSession, args map[string]any) searchPage {
	t.Helper()
	result := callTool(t, session, "search_messages", withWork(args))
	if result.IsError {
		t.Fatalf("search_messages %v failed: %s", args, errorText(result))
	}
	var p searchPage
	decode(t, result.StructuredContent, &p)
	return p
}

func messagesToSearch(t *testing.T, h *apptest.Harness) *mcp.ClientSession {
	t.Helper()
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "bot", []string{"work", "personal"}, true))
	family(h)
	at := func(minutes int) time.Time { return morning.Add(time.Duration(minutes) * time.Minute) }
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", at(0)), text("Reunião amanhã: 10h, sala 3")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M2", "Maria", at(1)), image("nota da REUNIÃO")))
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "G1", "Maria", at(2)), text("reuniao de família no domingo")))
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "J1", "João", at(3)), text("nada a ver")))
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "J2", "João", at(4)), text("a senha do wifi é 1234")))
	h.WhatsApp.Emit(workPhone, message(editInfo(direct(joaoJID, "X1", "João", at(5)), types.EditAttributeMessageEdit), edit(joaoJID, "J2", text("não lembro mais"), at(5))))
	h.WhatsApp.Emit(workPhone, message(editInfo(direct(mariaJID, "X2", "Maria", at(6)), types.EditAttributeSenderRevoke), revoke(mariaJID, "M1")))
	h.WhatsApp.Emit(personalPhone, message(direct(mariaJID, "P1", "Maria", at(7)), text("reunião pessoal")))
	return session
}

func TestSearchMessagesFindsTextInOneAccount(t *testing.T) {
	h := apptest.Start(t)
	session := messagesToSearch(t, h)
	cases := []struct {
		name string
		args map[string]any
		ids  string
		more bool
	}{
		{name: "accents and case do not matter", args: map[string]any{"query": "reuniao"}, ids: "G1,M2,M1"},
		{name: "every word must match", args: map[string]any{"query": "Reunião sala"}, ids: "M1"},
		{name: "search syntax is plain text", args: map[string]any{"query": `amanhã: "10h" -sala OR NOT`}, ids: ""},
		{name: "punctuation is plain text", args: map[string]any{"query": `amanhã: 10h`}, ids: "M1"},
		{name: "one chat", args: map[string]any{"query": "reunião", "chat": "+" + mariaPhone}, ids: "M2,M1"},
		{name: "since is inclusive", args: map[string]any{"query": "reunião", "since": "2026-10-13T09:01:00Z"}, ids: "G1,M2"},
		{name: "until is exclusive", args: map[string]any{"query": "reunião", "until": "2026-10-13T09:01:00Z"}, ids: "M1"},
		{name: "limit", args: map[string]any{"query": "reunião", "limit": 2}, ids: "G1,M2", more: true},
		{name: "an edit replaces the old text", args: map[string]any{"query": "wifi"}, ids: ""},
		{name: "the new text of an edit", args: map[string]any{"query": "lembro"}, ids: "J2"},
		{name: "the other account", args: map[string]any{"query": "reunião", "account": "personal"}, ids: "P1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := searchMessages(t, session, tc.args)

			if got.ids() != tc.ids || got.HasMore != tc.more {
				t.Fatalf("ids %q more %v, want %q %v", got.ids(), got.HasMore, tc.ids, tc.more)
			}
		})
	}
}

func TestSearchMessagesPagesThroughMessagesOfTheSameSecond(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	for _, m := range []struct {
		chat types.JID
		id   string
	}{{mariaJID, "A1"}, {joaoJID, "A2"}, {mariaJID, "A3"}, {joaoJID, "A4"}, {mariaJID, "A5"}} {
		h.WhatsApp.Emit(workPhone, message(direct(m.chat, m.id, "Maria", morning), text("boleto do condomínio")))
	}
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "B1", "Maria", morning.Add(-time.Second)), text("boleto antigo")))

	var seen []string
	args := map[string]any{"query": "boleto", "limit": 2}
	for page := 0; page < 10; page++ {
		got := searchMessages(t, session, args)
		if got.ids() != "" {
			seen = append(seen, got.ids())
		}
		if !got.HasMore {
			break
		}
		if got.NextCursor == "" {
			t.Fatalf("page %d has more results but no next_cursor", page)
		}
		args["cursor"] = got.NextCursor
	}

	if strings.Join(seen, ",") != "A5,A4,A3,A2,A1,B1" {
		t.Fatalf("pages %q, want every message once, newest first", seen)
	}
}

func TestSearchMessagesShowsTheChatAndTheMessage(t *testing.T) {
	h := apptest.Start(t)
	session := messagesToSearch(t, h)

	result := callTool(t, session, "search_messages", withWork(map[string]any{"query": "domingo"}))

	assertJSON(t, result.StructuredContent, `{`+envelope+`, "query": "domingo", "results": [{
		"chat": {"jid": "120363012345678901@g.us", "kind": "group", "name": "Família"},
		"message": {"id": "G1", "from_me": false, "sender": `+mariaSender+`, "sent_at": "2026-10-13T09:02:00Z", "kind": "text", "third_party": {"text": "reuniao de família no domingo"}}
	}]}`)
}

func TestSearchMessagesRefusesBadArguments(t *testing.T) {
	h := apptest.Start(t)
	session := messagesToSearch(t, h)
	cases := map[string]map[string]any{
		"empty query":      {"query": "  "},
		"only symbols":     {"query": `" - :`},
		"since not valid":  {"query": "reunião", "since": "ontem"},
		"until not valid":  {"query": "reunião", "until": "2026-13-01"},
		"chat not valid":   {"query": "reunião", "chat": "maria"},
		"limit too large":  {"query": "reunião", "limit": 201},
		"cursor not valid": {"query": "reunião", "cursor": "not-a-cursor"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if result := callTool(t, session, "search_messages", withWork(args)); !result.IsError {
				t.Fatalf("accepted: %s", structured(t, result))
			}
		})
	}
}

func TestRemovingAnAccountDeletesItsMessagesFromTheSearch(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning), image("reunião")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M2", "Maria", morning.Add(time.Minute)), pollMessage()))
	h.WhatsApp.Emit(workPhone, message(reactionInfo(direct(mariaJID, "R1", "Maria", morning)), reaction(mariaJID, "M1", false, "👍", morning.Add(2*time.Minute))))
	if got := searchMessages(t, session, map[string]any{"query": "reunião"}); got.ids() != "M1" {
		t.Fatalf("before the removal: %q", got.ids())
	}

	if result := h.CLI(t, "accounts", "remove", "--yes", "work"); result.Code != 0 {
		t.Fatalf("remove: exit %d, stderr %q", result.Code, result.Stderr)
	}
	h.AddAccount(t, "work", workPhone, "Alex")
	again := h.Connect(t, h.StaticToken(t, "again", []string{"work"}, true))

	if got := searchMessages(t, again, map[string]any{"query": "reunião"}); got.ids() != "" {
		t.Fatalf("the search still finds %q", got.ids())
	}
	if result := callTool(t, again, "get_messages", withWork(map[string]any{"chat": mariaJID.String()})); !result.IsError {
		t.Fatalf("the old chat is still stored: %s", structured(t, result))
	}
}
