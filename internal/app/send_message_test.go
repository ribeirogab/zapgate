package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const ownMessage = `"from_me": true, "sender": {"jid": "5511988887777@s.whatsapp.net", "phone": "+5511988887777", "name": "Alex"}`

func writerSession(t *testing.T, h *apptest.Harness, opts ...apptest.ConnectOption) *mcp.ClientSession {
	t.Helper()
	h.AddAccount(t, "work", workPhone, "Alex")
	return h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false), opts...)
}

func TestAClientWithoutElicitationSendsAtOnceAndTheMessageAppearsInGetMessages(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session := writerSession(t, h, apptest.WithProtocol(path.protocol))

			result := callTool(t, session, "send_message", withWork(map[string]any{"to": "+55 31 94444-3333", "text": "oi, Maria"}))

			assertJSON(t, result.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "phone": "+5531944443333"},
				"message_id": "3EB0FAKE0001",
				"sent_at": "2026-10-13T12:00:00Z",
				"approval": "client_without_elicitation"
			}`)
			sent := h.WhatsApp.Sent(workPhone)
			if len(sent) != 1 || sent[0].To != mariaJID || sent[0].Message.GetConversation() != "oi, Maria" {
				t.Fatalf("WhatsApp got %+v", sent)
			}
			messages := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "phone": "+5531944443333"},
				"messages": [{"id": "3EB0FAKE0001", `+ownMessage+`, "sent_at": "2026-10-13T12:00:00Z", "kind": "text", "third_party": {"text": "oi, Maria"}}]
			}`)
		})
	}
}

func TestAReplyQuotesTheStoredMessageAndShowsItToTheOwner(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, o := approvalSession(t, h, path.protocol, "accept")

			result := callTool(t, session, "send_message", withWork(map[string]any{"to": "+5531944443333", "text": "às 15h", "reply_to": "3EB0M1"}))

			if result.IsError {
				t.Fatalf("send_message failed: %s", errorText(result))
			}
			o.assertAskedOnce(t, "Send this WhatsApp message?\n\nAccount: work (+5511988887777)\nTo: Maria (+5531944443333)\nIn reply to: \"você chega que horas?\", sent by them at 2026-10-13T09:00:00Z\n\nText:\nàs 15h")
			sent := h.WhatsApp.Sent(workPhone)
			if len(sent) != 1 {
				t.Fatalf("WhatsApp got %d messages", len(sent))
			}
			ext := sent[0].Message.GetExtendedTextMessage()
			quote := ext.GetContextInfo()
			if ext.GetText() != "às 15h" || quote.GetStanzaID() != "3EB0M1" || quote.GetParticipant() != mariaJID.String() || quote.GetQuotedMessage().GetConversation() != "você chega que horas?" {
				t.Fatalf("WhatsApp got %v", sent[0].Message)
			}
			messages := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String(), "after": "3EB0M1"}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"messages": [{"id": "3EB0FAKE0001", `+ownMessage+`, "sent_at": "2026-10-13T12:00:00Z", "kind": "text", "quoted_id": "3EB0M1", "third_party": {"text": "às 15h"}}],
				"older_before": "3EB0FAKE0001"
			}`)
		})
	}
}

func TestAReplyToAnUnknownMessageIsRefusedBeforeTheOwnerIsAsked(t *testing.T) {
	h := apptest.Start(t)
	session, o := approvalSession(t, h, apptest.LegacyProtocol, "accept")

	result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "oi", "reply_to": "NOPE"}))

	if !result.IsError || !strings.Contains(errorText(result), `no stored message "NOPE"`) || len(o.questions()) != 0 || len(h.WhatsApp.Sent(workPhone)) != 0 {
		t.Fatalf("error %v %q, asked %d", result.IsError, errorText(result), len(o.questions()))
	}
}

func TestAMessageToAGroupNamesTheGroupForTheOwner(t *testing.T) {
	h := apptest.Start(t)
	o := &owner{answer: "accept"}
	session := writerSession(t, h, o.elicitation())
	family(h)
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "F1", "Maria", morning), text("almoço domingo?")))

	result := callTool(t, session, "send_message", withWork(map[string]any{"to": familyJID.String(), "text": "vou sim"}))

	if result.IsError {
		t.Fatalf("send_message failed: %s", errorText(result))
	}
	o.assertAskedOnce(t, "Send this WhatsApp message?\n\nAccount: work (+5511988887777)\nTo: the group Família (120363012345678901@g.us)\n\nText:\nvou sim")
	if sent := h.WhatsApp.Sent(workPhone); len(sent) != 1 || sent[0].To != familyJID {
		t.Fatalf("WhatsApp got %+v", sent)
	}
	if got := page(t, session, map[string]any{"chat": familyJID.String()}); got.ids() != "F1,3EB0FAKE0001" {
		t.Fatalf("get_messages shows %s", got.ids())
	}
}

func TestAPhoneNumberDMIsStoredUnderThePhoneEvenWhenWhatsAppAnswersWithTheLID(t *testing.T) {
	h := apptest.Start(t)
	session := writerSession(t, h)
	h.WhatsApp.MapLID(mariaLID, mariaJID)

	callTool(t, session, "send_message", withWork(map[string]any{"to": mariaLID.String(), "text": "oi"}))

	if sent := h.WhatsApp.Sent(workPhone); len(sent) != 1 || sent[0].To != mariaJID {
		t.Fatalf("WhatsApp got %+v", sent)
	}
	if got := page(t, session, map[string]any{"chat": "+5531944443333"}); got.ids() != "3EB0FAKE0001" {
		t.Fatalf("get_messages shows %s", got.ids())
	}
}

func TestSendingFromADisconnectedAccountFailsClearly(t *testing.T) {
	h := apptest.Start(t)
	o := &owner{answer: "accept"}
	session := writerSession(t, h, o.elicitation())
	h.WhatsApp.DropConnection(workPhone)
	h.AwaitAccountState(t, "work", "disconnected")

	result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "oi"}))

	if !result.IsError || errorText(result) != "the account work is disconnected, so nothing was done; call list_accounts and try again when it is connected" || len(o.questions()) != 0 {
		t.Fatalf("error %v %q, asked %d", result.IsError, errorText(result), len(o.questions()))
	}
}

func TestWhenWhatsAppRefusesTheSendNothingIsStored(t *testing.T) {
	h := apptest.Start(t)
	session := writerSession(t, h)
	h.WhatsApp.FailSends(workPhone, errors.New("server returned error 479"))

	result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "oi"}))

	if !result.IsError || errorText(result) != "WhatsApp did not accept the message, so it was not sent: server returned error 479" {
		t.Fatalf("error %v %q", result.IsError, errorText(result))
	}
	if again := callTool(t, session, "get_messages", withWork(map[string]any{"chat": mariaJID.String()})); !again.IsError {
		t.Fatalf("a chat was stored: %s", structured(t, again))
	}
}
