package app_test

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func receiveFromAnUnknownLID(t *testing.T, h *apptest.Harness) {
	t.Helper()
	h.WhatsApp.Emit(workPhone, message(directFromLID(joaoLID, types.JID{}, "3EB0U1", "João", morning), text("antes do mapa")))
	awaitJournalRead(t, h)
	h.Clock.Advance(2 * time.Second)
}

func receiveWithThePhoneOfTheLID(h *apptest.Harness) {
	h.WhatsApp.Emit(workPhone, message(directFromLID(joaoLID, joaoJID, "3EB0U3", "João", morning.Add(time.Minute)), text("depois do mapa")))
}

func TestADeliveryOfALIDChatLeavesWithThePhoneChatAfterTheIdentityMerges(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")
	receiveFromAnUnknownLID(t, h)

	receiveWithThePhoneOfTheLID(h)
	passQuietWindow(h)

	got := rx.Await(t, 1)
	requireNoNewRequest(t, rx, 1)
	if id := got[0].Header.Get("Webhook-Id"); id != "dlv_1" {
		t.Fatalf("the receiver got %s, want dlv_1", id)
	}
	data := bodyOf(t, got[0])["data"].(map[string]any)
	assertJSON(t, data["chat"], `{"jid": "5541933332222@s.whatsapp.net", "kind": "direct", "phone": "+5541933332222"}`)
	assertJSON(t, data["messages"], `[
		{"id": "3EB0U1", "kind": "text", "from_me": false, "sent_at": "2026-10-13T09:00:00Z", "sender": {"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222"}},
		{"id": "3EB0U3", "kind": "text", "from_me": false, "sent_at": "2026-10-13T09:01:00Z", "sender": {"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222"}}
	]`)
	assertJSON(t, data["fetch"], `{"tool": "get_messages", "arguments": {"account": "work", "chat": "5541933332222@s.whatsapp.net"}}`)
	body := deliveryWithID(t, got, "dlv_1")
	if fetched := page(t, session, body.Data.Fetch.Arguments); fetched.ids() != body.ids() {
		t.Fatalf("the fetch call reads %s, the delivery has %s", fetched.ids(), body.ids())
	}
	line := awaitAttempts(t, h, 1)[0]
	assertFields(t, line, map[string]string{"delivery": "dlv_1", "result": "delivered"})
	if line["events"] != float64(2) {
		t.Fatalf("attempt line %v, want 2 events", line)
	}
}

func TestAMergedIdentityMovesTheListedChatOfAWebhook(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	lidOnly := h.StartReceiver(t)
	both := h.StartReceiver(t)
	addWebhook(t, h, lidOnly, "lid-bot", "work", "--chats", joaoLID.String())
	addWebhook(t, h, both, "both-bot", "work", "--chats", joaoLID.String()+",+"+joaoPhone)
	receiveFromAnUnknownLID(t, h)

	receiveWithThePhoneOfTheLID(h)
	passQuietWindow(h)

	for name, rx := range map[string]*apptest.Receiver{"lid-bot": lidOnly, "both-bot": both} {
		if _, body := firstDelivery(t, rx); body.ids() != "3EB0U1,3EB0U3" {
			t.Fatalf("%s got %s, want 3EB0U1,3EB0U3 in one delivery", name, body.ids())
		}
		requireNoNewRequest(t, rx, 1)
		requireColumns(t, h, name, map[string]string{"CHATS": joaoJID.String()})
	}
}
