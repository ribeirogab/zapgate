package app_test

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestTheChatFilterKeepsOnlyTheChosenChats(t *testing.T) {
	group := message(inGroup(familyJID, joaoJID, types.JID{}, "3EB0G1", "João", morning), text("oi, família"))
	maria := message(direct(mariaJID, "3EB0M1", "Maria", morning), text("oi"))
	joao := message(direct(joaoJID, "3EB0J1", "João", morning), text("oi"))
	cases := []struct {
		chats   string
		dropped *events.Message
		kept    *events.Message
	}{
		{chats: "direct", dropped: group, kept: maria},
		{chats: "groups", dropped: maria, kept: group},
		{chats: "+55 (31) 94444-3333", dropped: joao, kept: maria},
		{chats: mariaJID.String(), dropped: joao, kept: maria},
		{chats: mariaLID.String(), dropped: joao, kept: maria},
		{chats: "+5531944443333, " + familyJID.String(), dropped: joao, kept: group},
	}
	for _, tc := range cases {
		t.Run(tc.chats, func(t *testing.T) {
			h := apptest.Start(t)
			h.WhatsApp.MapLID(mariaLID, mariaJID)
			h.AddAccount(t, "work", workPhone, "Alex")
			rx := h.StartReceiver(t)
			addWebhook(t, h, rx, "support-bot", "work", "--chats", tc.chats)

			h.WhatsApp.Emit(workPhone, tc.dropped)
			h.WhatsApp.Emit(workPhone, tc.kept)

			if id, body := firstDelivery(t, rx); id != "dlv_1" || body.ids() != tc.kept.Info.ID {
				t.Fatalf("the first delivery is %s with %s, want dlv_1 with %s", id, body.ids(), tc.kept.Info.ID)
			}
		})
	}
}

func TestAWebhookGetsOnlyTheEventsOfItsList(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	states := h.StartReceiver(t)
	rx := h.StartReceiver(t)
	addWebhook(t, h, states, "account-watch", "work", "--events", "account.state")
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("oi")))
	first, _ := firstDelivery(t, rx)
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "3EB0J1", "João", morning), text("oi")))
	second := rx.Await(t, 2)[1].Header.Get("Webhook-Id")

	if first != "dlv_1" || second != "dlv_2" {
		t.Fatalf("the support-bot deliveries are %s and %s, want dlv_1 and dlv_2: account-watch got a delivery", first, second)
	}
	if received := states.Received(); len(received) != 0 {
		t.Fatalf("account-watch got %d deliveries of message.received", len(received))
	}
}

func TestWebhooksAddRejectsBadFilters(t *testing.T) {
	cases := []struct {
		name    string
		options []string
		stderr  string
	}{
		{
			name:    "unknown event",
			options: []string{"--events", "message.received,message.sent"},
			stderr:  "zapgate: the event \"message.sent\" is not known: use message.received or account.state\n",
		},
		{
			name:    "empty event list",
			options: []string{"--events", ","},
			stderr:  "zapgate: the webhook needs at least one event: use message.received or account.state\n",
		},
		{
			name:    "kind inside a list",
			options: []string{"--chats", "direct,+5531944443333"},
			stderr:  "zapgate: the chat \"direct\" is not valid: use all, direct, groups, or a comma-separated list of phone numbers with the country code and JIDs\n",
		},
		{
			name:    "short phone",
			options: []string{"--chats", "+55 31"},
			stderr:  "zapgate: the chat \"+55 31\" is not valid: use all, direct, groups, or a comma-separated list of phone numbers with the country code and JIDs\n",
		},
		{
			name:    "broadcast",
			options: []string{"--chats", "status@broadcast"},
			stderr:  "zapgate: the chat \"status@broadcast\" is not a person or a group\n",
		},
		{
			name:    "empty chat list",
			options: []string{"--chats", ","},
			stderr:  "zapgate: the chat filter needs all, direct, groups, or a comma-separated list of phone numbers and JIDs\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")

			result := h.CLI(t, append([]string{"webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work"}, tc.options...)...)

			if result.Code != 1 || result.Stderr != tc.stderr || result.Stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q, want %q", result.Code, result.Stdout, result.Stderr, tc.stderr)
			}
			if listed := h.CLI(t, "webhooks", "list"); listed.Stdout != "no webhook yet\n" {
				t.Fatalf("a refused add created a webhook: %q", listed.Stdout)
			}
		})
	}
}
