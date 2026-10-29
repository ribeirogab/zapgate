package app_test

import (
	"fmt"
	"testing"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestGetChatShowsTheParticipantsOfAGroup(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)
	h.WhatsApp.SetContact(workPhone, pedroLID, types.ContactInfo{PushName: "Pedro"})

	result := callTool(t, session, "get_chat", withWork(map[string]any{"chat": familyJID.String()}))

	if result.IsError {
		t.Fatalf("get_chat failed: %s", errorText(result))
	}
	assertJSON(t, result.StructuredContent, `{`+envelope+`,
		"chat": `+familyChat+`,
		"participant_count": 3,
		"participants": [
			{"jid": "5511988887777@s.whatsapp.net", "phone": "+5511988887777", "role": "super_admin", "is_account": true},
			{"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria", "role": "admin"},
			{"jid": "165432109876543@lid", "name": "Pedro", "role": "member"}
		]
	}`)
}

func TestGetChatOfADirectChatShowsTheContact(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)
	h.WhatsApp.SetContact(workPhone, mariaJID, types.ContactInfo{FullName: "Maria Souza", PushName: "Maria"})

	result := callTool(t, session, "get_chat", withWork(map[string]any{"chat": "+" + mariaPhone}))

	assertJSON(t, result.StructuredContent, `{`+envelope+`,
		"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria Souza", "phone": "+5531944443333", "unread_count": 2, "last_message_at": "2026-10-13T09:05:00Z"}
	}`)
}

func TestGetChatPagesTheParticipantsOfALargeGroup(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	members := []types.GroupParticipant{}
	for i := range 1024 {
		pn := types.NewJID(fmt.Sprintf("55119%08d", i), types.DefaultUserServer)
		members = append(members, types.GroupParticipant{JID: pn, PhoneNumber: pn})
		h.WhatsApp.SetContact(workPhone, pn, types.ContactInfo{PushName: fmt.Sprintf("Pessoa com um nome comprido %04d", i)})
	}
	h.WhatsApp.SetGroup(workPhone, types.GroupInfo{JID: familyJID, GroupName: types.GroupName{Name: "Grande"}, Participants: members})

	seen := map[string]bool{}
	args := withWork(map[string]any{"chat": familyJID.String()})
	for pages := 0; ; pages++ {
		result := callTool(t, session, "get_chat", args)
		if result.IsError {
			t.Fatalf("get_chat failed: %s", errorText(result))
		}
		if size := len(structured(t, result)); size > 40_000 {
			t.Fatalf("a page has %d bytes", size)
		}
		var p struct {
			Count        int    `json:"participant_count"`
			NextCursor   string `json:"next_cursor"`
			Participants []struct {
				JID string `json:"jid"`
			} `json:"participants"`
		}
		decode(t, result.StructuredContent, &p)
		if p.Count != 1024 {
			t.Fatalf("participant_count %d", p.Count)
		}
		for _, member := range p.Participants {
			seen[member.JID] = true
		}
		if p.NextCursor == "" {
			break
		}
		if pages > 50 {
			t.Fatal("the pages do not end")
		}
		args["cursor"] = p.NextCursor
	}
	if len(seen) != 1024 {
		t.Fatalf("saw %d participants", len(seen))
	}
}

func TestGetChatOfAGroupNeedsTheConnection(t *testing.T) {
	h := apptest.Start(t)
	session := threeChats(t, h)
	h.WhatsApp.DropConnection(workPhone)

	result := callTool(t, session, "get_chat", withWork(map[string]any{"chat": familyJID.String()}))

	if !result.IsError {
		t.Fatalf("get_chat answered offline: %s", structured(t, result))
	}
}
