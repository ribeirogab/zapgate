package app_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestMarkReadSendsReceiptsWithoutAskingTheOwner(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			o := &owner{answer: "decline"}
			session := writerSession(t, h, apptest.WithProtocol(path.protocol), o.elicitation())
			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning), text("oi")))
			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M2", "Maria", morning.Add(time.Minute)), text("me liga")))
			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M3", "Maria", morning.Add(2*time.Minute)), text("urgente")))

			result := callTool(t, session, "mark_read", withWork(map[string]any{"chat": "+5531944443333", "message_ids": []string{"M1", "M2"}}))

			if result.IsError {
				t.Fatalf("mark_read failed: %s", errorText(result))
			}
			assertJSON(t, result.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"message_ids": ["M1", "M2"],
				"unread_count": 1
			}`)
			if len(o.questions()) != 0 {
				t.Fatalf("the owner was asked %q", o.questions())
			}
			receipts := h.WhatsApp.Receipts(workPhone)
			if len(receipts) != 1 || receipts[0].Chat != mariaJID || !receipts[0].Sender.IsEmpty() || !slices.Equal(receipts[0].IDs, []types.MessageID{"M1", "M2"}) || !receipts[0].At.Equal(apptest.StartTime) {
				t.Fatalf("WhatsApp got %+v", receipts)
			}
			if got := structured(t, listChats(t, session, nil)); !strings.Contains(got, `"unread_count":1`) {
				t.Fatalf("list_chats %s", got)
			}
		})
	}
}

func TestMarkReadInAGroupSendsOneReceiptPerSender(t *testing.T) {
	h := apptest.Start(t)
	session := writerSession(t, h)
	family(h)
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "F1", "Maria", morning), text("almoço?")))
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, joaoJID, types.JID{}, "F2", "João", morning.Add(time.Minute)), text("eu vou")))
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "F3", "Maria", morning.Add(2*time.Minute)), text("ótimo")))

	result := callTool(t, session, "mark_read", withWork(map[string]any{"chat": familyJID.String(), "message_ids": []string{"F1", "F2", "F3"}}))

	if result.IsError || !strings.Contains(structured(t, result), `"unread_count":0`) {
		t.Fatalf("mark_read: %s %s", errorText(result), structured(t, result))
	}
	receipts := h.WhatsApp.Receipts(workPhone)
	if len(receipts) != 2 || receipts[0].Sender != mariaJID || !slices.Equal(receipts[0].IDs, []types.MessageID{"F1", "F3"}) || receipts[1].Sender != joaoJID || !slices.Equal(receipts[1].IDs, []types.MessageID{"F2"}) {
		t.Fatalf("WhatsApp got %+v", receipts)
	}
}

func TestMarkReadRefusesUnknownOrOwnMessages(t *testing.T) {
	h := apptest.Start(t)
	session := writerSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning), text("oi")))
	callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "oi, Maria"}))
	cases := map[string]struct {
		ids  []string
		want string
	}{
		"unknown": {ids: []string{"M1", "NOPE"}, want: `the chat 5531944443333@s.whatsapp.net of the account work has no stored message "NOPE"; call get_messages to find the id`},
		"own":     {ids: []string{"3EB0FAKE0001"}, want: "the message 3EB0FAKE0001 was sent by the account work; mark_read only marks received messages"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := callTool(t, session, "mark_read", withWork(map[string]any{"chat": mariaJID.String(), "message_ids": tc.ids}))

			if !result.IsError || errorText(result) != tc.want {
				t.Fatalf("error %v %q", result.IsError, errorText(result))
			}
			if receipts := h.WhatsApp.Receipts(workPhone); len(receipts) != 0 {
				t.Fatalf("WhatsApp got %+v", receipts)
			}
		})
	}
}
