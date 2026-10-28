package app_test

import (
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

var anaLID = types.NewJID("154321098765432", types.HiddenUserServer)

func contacts(t *testing.T, h *apptest.Harness) *mcp.ClientSession {
	t.Helper()
	session := workSession(t, h)
	h.WhatsApp.SetContact(workPhone, mariaJID, types.ContactInfo{FirstName: "Maria", FullName: "Maria Souza", PushName: "Mari"})
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "J1", "João", morning), text("oi")))
	h.WhatsApp.SetContact(workPhone, pedroLID, types.ContactInfo{PushName: "Pedro"})
	h.WhatsApp.SetContact(workPhone, anaLID, types.ContactInfo{PushName: "Ana"})
	h.WhatsApp.MapLID(anaLID, types.NewJID("5551922221111", types.DefaultUserServer))
	return session
}

func TestSearchContactsFindsAContactByNameOrNumber(t *testing.T) {
	h := apptest.Start(t)
	session := contacts(t, h)
	cases := map[string]string{
		"souza":          `[{"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria Souza"}]`,
		"MARI":           `[{"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria Souza"}]`,
		"+55 31 94444":   `[{"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria Souza"}]`,
		"joao":           `[{"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222", "name": "João"}]`,
		"pedro":          `[{"jid": "165432109876543@lid", "name": "Pedro"}]`,
		"ana":            `[{"jid": "5551922221111@s.whatsapp.net", "phone": "+5551922221111", "name": "Ana"}]`,
		"5551922221111":  `[{"jid": "5551922221111@s.whatsapp.net", "phone": "+5551922221111", "name": "Ana"}]`,
		"ninguém":        `[]`,
		"+55 99 99999-9": `[]`,
	}

	for query, want := range cases {
		result := callTool(t, session, "search_contacts", withWork(map[string]any{"query": query}))

		if result.IsError {
			t.Fatalf("search_contacts %q failed: %s", query, errorText(result))
		}
		assertJSON(t, result.StructuredContent, `{`+envelope+`, "contacts": `+want+`}`)
	}
}

func TestSearchContactsPagesManyMatches(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	for i := range 150 {
		h.WhatsApp.SetContact(workPhone, types.NewJID(fmt.Sprintf("55219%08d", i), types.DefaultUserServer), types.ContactInfo{FullName: fmt.Sprintf("Cliente %03d", i)})
	}

	seen := map[string]bool{}
	args := withWork(map[string]any{"query": "cliente"})
	for pages := 0; ; pages++ {
		result := callTool(t, session, "search_contacts", args)
		var p struct {
			Contacts []struct {
				Name string `json:"name"`
			} `json:"contacts"`
			NextCursor string `json:"next_cursor"`
		}
		decode(t, result.StructuredContent, &p)
		for _, contact := range p.Contacts {
			seen[contact.Name] = true
		}
		if p.NextCursor == "" {
			break
		}
		if pages > 20 {
			t.Fatal("the pages do not end")
		}
		args["cursor"] = p.NextCursor
	}
	if len(seen) != 150 {
		t.Fatalf("saw %d contacts", len(seen))
	}
}

func TestSearchContactsNeedsAQuery(t *testing.T) {
	h := apptest.Start(t)
	session := contacts(t, h)

	result := callTool(t, session, "search_contacts", withWork(map[string]any{"query": "  "}))

	if !result.IsError {
		t.Fatalf("an empty query was accepted: %s", structured(t, result))
	}
}
