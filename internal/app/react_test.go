package app_test

import (
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const mariaAsks = `"id": "3EB0M1", "from_me": false, "sender": {"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria"}, "sent_at": "2026-10-13T09:00:00Z", "kind": "text", "third_party": {"text": "você chega que horas?"}`

func TestTheOwnerApprovesAReactionAndAnEmptyEmojiRemovesIt(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, o := approvalSession(t, h, path.protocol, "accept")

			added := callTool(t, session, "react", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0M1", "emoji": "👍"}))

			if added.IsError {
				t.Fatalf("react failed: %s", errorText(added))
			}
			o.assertAskedOnce(t, "React to this WhatsApp message?\n\nAccount: work (+5511988887777)\nChat: Maria (+5531944443333)\nMessage: \"você chega que horas?\", sent by them at 2026-10-13T09:00:00Z\n\nReaction: 👍")
			assertJSON(t, added.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"message_id": "3EB0M1",
				"emoji": "👍",
				"approval": "owner_approved"
			}`)
			sent := h.WhatsApp.Sent(workPhone)
			reaction := sent[0].Message.GetReactionMessage()
			if len(sent) != 1 || sent[0].To != mariaJID || reaction.GetText() != "👍" || reaction.GetKey().GetID() != "3EB0M1" || reaction.GetKey().GetFromMe() {
				t.Fatalf("WhatsApp got %+v", sent)
			}
			messages := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"messages": [{`+mariaAsks+`, "reactions": [{"emoji": "👍", `+ownMessage+`, "reacted_at": "2026-10-13T12:00:00Z"}]}]
			}`)

			h.Clock.Advance(time.Minute)
			removed := callTool(t, session, "react", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0M1", "emoji": ""}))

			if removed.IsError {
				t.Fatalf("react failed: %s", errorText(removed))
			}
			if asked := o.questions(); len(asked) != 2 || asked[1] != "Remove your reaction from this WhatsApp message?\n\nAccount: work (+5511988887777)\nChat: Maria (+5531944443333)\nMessage: \"você chega que horas?\", sent by them at 2026-10-13T09:00:00Z" {
				t.Fatalf("the owner was asked %q", asked)
			}
			if sent := h.WhatsApp.Sent(workPhone); len(sent) != 2 || sent[1].Message.GetReactionMessage().GetText() != "" {
				t.Fatalf("WhatsApp got %+v", sent)
			}
			messages = getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"messages": [{`+mariaAsks+`}]
			}`)
		})
	}
}

func TestADeclinedReactionChangesNothing(t *testing.T) {
	h := apptest.Start(t)
	session, _ := approvalSession(t, h, apptest.LegacyProtocol, "decline")

	result := callTool(t, session, "react", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0M1", "emoji": "❤️"}))

	if !result.IsError || errorText(result) != "the owner did not approve (decline): the reaction was not changed" || len(h.WhatsApp.Sent(workPhone)) != 0 {
		t.Fatalf("error %v %q", result.IsError, errorText(result))
	}
}
