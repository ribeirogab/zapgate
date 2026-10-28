package app_test

import (
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func sentByTheAccount(t *testing.T, h *apptest.Harness, path string, answer string) (*mcp.ClientSession, *owner) {
	t.Helper()
	session, o := approvalSession(t, h, path, answer)
	direct := h.Connect(t, h.StaticToken(t, "direct", []string{"work"}, false))
	if sent := callTool(t, direct, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "Chego às 15h"})); sent.IsError {
		t.Fatalf("send_message failed: %s", errorText(sent))
	}
	return session, o
}

const sentLine = `"Chego às 15h", sent by you at 2026-10-13T12:00:00Z`

func TestTheOwnerApprovesAnEditWithinFifteenMinutes(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, o := sentByTheAccount(t, h, path.protocol, "accept")
			h.Clock.Advance(14 * time.Minute)

			result := callTool(t, session, "edit_message", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0FAKE0001", "text": "Chego às 16h"}))

			if result.IsError {
				t.Fatalf("edit_message failed: %s", errorText(result))
			}
			o.assertAskedOnce(t, "Edit this WhatsApp message?\n\nAccount: work (+5511988887777)\nChat: Maria (+5531944443333)\nMessage: "+sentLine+"\n\nNew text:\nChego às 16h")
			sent := h.WhatsApp.Sent(workPhone)
			edit := sent[1].Message.GetEditedMessage().GetMessage().GetProtocolMessage()
			if len(sent) != 2 || sent[1].To != mariaJID || edit.GetKey().GetID() != "3EB0FAKE0001" || edit.GetEditedMessage().GetConversation() != "Chego às 16h" {
				t.Fatalf("WhatsApp got %+v", sent)
			}
			messages := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String(), "after": "3EB0M1"}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"messages": [{"id": "3EB0FAKE0001", `+ownMessage+`, "sent_at": "2026-10-13T12:00:00Z", "edited_at": "2026-10-13T12:14:00Z", "kind": "text", "third_party": {"text": "Chego às 16h"}}],
				"older_before": "3EB0FAKE0001"
			}`)
		})
	}
}

func TestTheOwnerApprovesADeleteForEveryoneAndTheTextStays(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, o := sentByTheAccount(t, h, path.protocol, "accept")
			h.Clock.Advance(47 * time.Hour)

			result := callTool(t, session, "delete_message", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0FAKE0001"}))

			if result.IsError {
				t.Fatalf("delete_message failed: %s", errorText(result))
			}
			o.assertAskedOnce(t, "Delete this WhatsApp message for everyone?\n\nAccount: work (+5511988887777)\nChat: Maria (+5531944443333)\nMessage: "+sentLine)
			sent := h.WhatsApp.Sent(workPhone)
			revoke := sent[1].Message.GetProtocolMessage()
			if len(sent) != 2 || revoke.GetType().String() != "REVOKE" || revoke.GetKey().GetID() != "3EB0FAKE0001" || !revoke.GetKey().GetFromMe() {
				t.Fatalf("WhatsApp got %+v", sent)
			}
			messages := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String(), "after": "3EB0M1"}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"messages": [{"id": "3EB0FAKE0001", `+ownMessage+`, "sent_at": "2026-10-13T12:00:00Z", "revoked_at": "2026-10-15T11:00:00Z", "kind": "text", "third_party": {"text": "Chego às 15h"}}],
				"older_before": "3EB0FAKE0001"
			}`)
		})
	}
}

func TestEditAndDeleteAreRefusedClearlyBeforeTheOwnerIsAsked(t *testing.T) {
	cases := []struct {
		name    string
		advance time.Duration
		tool    string
		args    map[string]any
		want    string
	}{
		{
			name: "edit after 15 minutes", advance: 15*time.Minute + time.Second, tool: "edit_message",
			args: map[string]any{"message_id": "3EB0FAKE0001", "text": "Chego às 16h"},
			want: "the message 3EB0FAKE0001 was sent at 2026-10-13T12:00:00Z; WhatsApp only allows editing a message within 15 minutes of sending it, so it can no longer be edited",
		},
		{
			name: "delete after 2 days", advance: 48*time.Hour + time.Second, tool: "delete_message",
			args: map[string]any{"message_id": "3EB0FAKE0001"},
			want: "the message 3EB0FAKE0001 was sent at 2026-10-13T12:00:00Z; WhatsApp only allows deleting a message for everyone within about 2 days of sending it, so it can no longer be deleted for everyone",
		},
		{
			name: "edit a received message", tool: "edit_message",
			args: map[string]any{"message_id": "3EB0M1", "text": "outra coisa"},
			want: "the message 3EB0M1 was not sent by the account work; only its own messages can be edited",
		},
		{
			name: "delete a received message", tool: "delete_message",
			args: map[string]any{"message_id": "3EB0M1"},
			want: "the message 3EB0M1 was not sent by the account work; only its own messages can be deleted for everyone",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, o := sentByTheAccount(t, h, apptest.LegacyProtocol, "accept")
			h.Clock.Advance(tc.advance)
			args := withWork(map[string]any{"chat": mariaJID.String()})
			for key, value := range tc.args {
				args[key] = value
			}

			result := callTool(t, session, tc.tool, args)

			if !result.IsError || errorText(result) != tc.want {
				t.Fatalf("error %v %q\nwant %q", result.IsError, errorText(result), tc.want)
			}
			if len(o.questions()) != 0 || len(h.WhatsApp.Sent(workPhone)) != 1 {
				t.Fatalf("asked %d, sent %d", len(o.questions()), len(h.WhatsApp.Sent(workPhone)))
			}
		})
	}
}

func TestADeletedMessageCanNoLongerBeEditedOrDeleted(t *testing.T) {
	h := apptest.Start(t)
	session, _ := sentByTheAccount(t, h, apptest.LegacyProtocol, "accept")
	if deleted := callTool(t, session, "delete_message", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0FAKE0001"})); deleted.IsError {
		t.Fatalf("delete_message failed: %s", errorText(deleted))
	}

	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"delete_message", map[string]any{}},
		{"edit_message", map[string]any{"text": "x"}},
		{"react", map[string]any{"emoji": "👍"}},
	} {
		args := withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0FAKE0001"})
		for key, value := range call.args {
			args[key] = value
		}
		result := callTool(t, session, call.tool, args)
		if want := "the message 3EB0FAKE0001 was deleted for everyone at 2026-10-13T12:00:00Z, so nothing was done"; !result.IsError || errorText(result) != want {
			t.Fatalf("%s: error %v %q", call.tool, result.IsError, errorText(result))
		}
	}
}

func TestDecliningAnEditOrADeleteChangesNothing(t *testing.T) {
	h := apptest.Start(t)
	session, _ := sentByTheAccount(t, h, apptest.StatelessProtocol, "decline")

	edit := callTool(t, session, "edit_message", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0FAKE0001", "text": "Chego às 16h"}))
	del := callTool(t, session, "delete_message", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0FAKE0001"}))

	if edit.IsError != true || errorText(edit) != "the owner did not approve (decline): the message was not edited" {
		t.Fatalf("edit: %q", errorText(edit))
	}
	if del.IsError != true || errorText(del) != "the owner did not approve (decline): the message was not deleted" {
		t.Fatalf("delete: %q", errorText(del))
	}
	if got := page(t, session, map[string]any{"chat": mariaJID.String()}); len(h.WhatsApp.Sent(workPhone)) != 1 || got.Messages[1].ThirdParty.Text != "Chego às 15h" {
		t.Fatalf("sent %d, messages %+v", len(h.WhatsApp.Sent(workPhone)), got)
	}
}
