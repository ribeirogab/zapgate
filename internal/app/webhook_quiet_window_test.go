package app_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/webhooks"
)

const settle = 100 * time.Millisecond

func passQuietWindow(h *apptest.Harness) {
	h.Clock.Advance(webhooks.DefaultQuiet)
}

func receive(h *apptest.Harness, chat types.JID, id string) {
	h.WhatsApp.Emit(workPhone, message(direct(chat, id, "", h.Clock.Now()), text("oi")))
}

func requireNothingSent(t *testing.T, rx *apptest.Receiver) {
	t.Helper()
	time.Sleep(settle)
	if got := rx.Received(); len(got) != 0 {
		t.Fatalf("the receiver got %d requests, want none yet", len(got))
	}
}

func sentAt(seconds int) string {
	return apptest.StartTime.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339)
}

func TestThreeMessagesInFourSecondsLeaveAsOneDeliveryAfterTheQuietWindow(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	receive(h, mariaJID, "3EB0B1")
	h.Clock.Advance(2 * time.Second)
	receive(h, mariaJID, "3EB0B2")
	h.Clock.Advance(2 * time.Second)
	receive(h, mariaJID, "3EB0B3")
	h.Clock.Advance(4 * time.Second)
	requireNothingSent(t, rx)
	h.Clock.Advance(time.Second)

	got := rx.Await(t, 1)
	body := deliveryWithID(t, got, "dlv_1")
	if len(got) != 1 || body.ids() != "3EB0B1,3EB0B2,3EB0B3" || body.Timestamp != sentAt(9) {
		t.Fatalf("the receiver got %d requests; dlv_1 has %s at %s, want 3EB0B1,3EB0B2,3EB0B3 at %s", len(got), body.ids(), body.Timestamp, sentAt(9))
	}
}

func TestMessagesThatNeverStopLeaveAtTheMaxWait(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	var ids []string
	for i := range 8 {
		if i > 0 {
			h.Clock.Advance(4 * time.Second)
		}
		ids = append(ids, fmt.Sprintf("3EB0M%d", i))
		receive(h, mariaJID, ids[i])
	}
	h.Clock.Advance(2 * time.Second)

	body := deliveryWithID(t, rx.Await(t, 1), "dlv_1")
	if want := strings.Join(ids, ","); body.ids() != want || body.Timestamp != sentAt(30) {
		t.Fatalf("dlv_1 has %s at %s, want %s at %s", body.ids(), body.Timestamp, want, sentAt(30))
	}
}

func TestTheHundredAndFirstEventOfAChatOpensASecondDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	var ids []string
	for i := range 101 {
		ids = append(ids, fmt.Sprintf("3EB0C%03d", i))
		receive(h, mariaJID, ids[i])
	}
	h.Clock.Advance(5 * time.Second)

	got := rx.Await(t, 2)
	first, second := deliveryWithID(t, got, "dlv_1"), deliveryWithID(t, got, "dlv_2")
	if want := strings.Join(ids[:100], ","); first.ids() != want || second.ids() != ids[100] {
		t.Fatalf("dlv_1 has %d messages and dlv_2 has %s, want the first 100 and then %s", len(first.Data.Messages), second.ids(), ids[100])
	}
}

func TestAnEventAfterTheFirstAttemptOpensANewDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	release := make(chan struct{})
	rx := h.StartReceiver(t, apptest.Held(release, http.StatusOK))
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(open)
	addWebhook(t, h, rx, "support-bot", "work")

	receive(h, mariaJID, "3EB0F1")
	h.Clock.Advance(5 * time.Second)
	rx.Await(t, 1)
	receive(h, mariaJID, "3EB0F2")
	open()
	h.Clock.Advance(5 * time.Second)

	got := rx.Await(t, 2)
	if first, second := deliveryWithID(t, got, "dlv_1"), deliveryWithID(t, got, "dlv_2"); first.ids() != "3EB0F1" || second.ids() != "3EB0F2" {
		t.Fatalf("the deliveries have %s and %s, want 3EB0F1 and 3EB0F2", first.ids(), second.ids())
	}
}

func TestTwoChatsLeaveAsTwoDeliveries(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	receive(h, mariaJID, "3EB0A1")
	receive(h, joaoJID, "3EB0J1")
	receive(h, mariaJID, "3EB0A2")
	h.Clock.Advance(5 * time.Second)

	got := rx.Await(t, 2)
	if maria, joao := deliveryWithID(t, got, "dlv_1"), deliveryWithID(t, got, "dlv_2"); maria.ids() != "3EB0A1,3EB0A2" || joao.ids() != "3EB0J1" {
		t.Fatalf("the deliveries have %s and %s, want 3EB0A1,3EB0A2 and 3EB0J1", maria.ids(), joao.ids())
	}
}

func TestARestartKeepsTheQuietWindowOfAnOpenDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	receive(h, mariaJID, "3EB0R1")
	h.Clock.Advance(3 * time.Second)
	h.Restart(t)
	receive(h, mariaJID, "3EB0R2")
	h.Clock.Advance(4 * time.Second)
	requireNothingSent(t, rx)
	h.Clock.Advance(time.Second)

	body := deliveryWithID(t, rx.Await(t, 1), "dlv_1")
	if body.ids() != "3EB0R1,3EB0R2" || body.Timestamp != sentAt(8) {
		t.Fatalf("dlv_1 has %s at %s, want 3EB0R1,3EB0R2 at %s", body.ids(), body.Timestamp, sentAt(8))
	}
}

func TestABurstOverTheSizeCapKeepsTheNewestMessagesAndFetchesFromTheFirst(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addFullWebhook(t, h, rx, "support-bot", "work")
	receive(h, mariaJID, "3EB0T0")
	passQuietWindow(h)
	bodyWithID(t, rx, "dlv_1")

	long := strings.Repeat("🙂", 4000)
	for i := 1; i <= 5; i++ {
		h.Clock.Advance(time.Second)
		h.WhatsApp.Emit(workPhone, message(direct(mariaJID, fmt.Sprintf("3EB0T%d", i), "Maria", h.Clock.Now()), text(long)))
	}
	passQuietWindow(h)

	var raw []byte
	body := awaitBody(t, rx, func(request apptest.Received, _ map[string]any) bool {
		raw = request.Body
		return request.Header.Get("Webhook-Id") == "dlv_2"
	})
	var kept []string
	for _, msg := range messagesOf(body) {
		kept = append(kept, msg.(map[string]any)["id"].(string))
	}
	data := body["data"].(map[string]any)
	if len(raw) > 60_000 || data["truncated"] != true || strings.Join(kept, ",") != "3EB0T3,3EB0T4,3EB0T5" {
		t.Fatalf("dlv_2 has %d bytes, truncated %v and %v, want at most 60000 bytes, truncated and 3EB0T3,3EB0T4,3EB0T5", len(raw), data["truncated"], kept)
	}
	assertJSON(t, data["fetch"], `{"tool": "get_messages", "arguments": {"account": "work", "chat": "5531944443333@s.whatsapp.net", "after": "3EB0T0"}}`)
}

func addWebhookWithWindow(t *testing.T, h *apptest.Harness, rx *apptest.Receiver, name string, window ...string) {
	t.Helper()
	added := h.CLI(t, append([]string{"webhooks", "add", name, "--url", rx.URL, "--accounts", "work"}, window...)...)
	if added.Code != 0 {
		t.Fatalf("add the webhook %s: exit %d, stderr %q", name, added.Code, added.Stderr)
	}
	rx.Trust(secretFrom(t, added.Stdout))
}

func TestTheOwnerSetsTheQuietWindowAndTheMaxWait(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhookWithWindow(t, h, rx, "support-bot", "--quiet", "2s", "--max-wait=3s")

	receive(h, mariaJID, "3EB0Q1")
	h.Clock.Advance(time.Second)
	receive(h, mariaJID, "3EB0Q2")
	h.Clock.Advance(time.Second)
	receive(h, mariaJID, "3EB0Q3")
	h.Clock.Advance(time.Second)

	body := deliveryWithID(t, rx.Await(t, 1), "dlv_1")
	if body.ids() != "3EB0Q1,3EB0Q2,3EB0Q3" || body.Timestamp != sentAt(3) {
		t.Fatalf("dlv_1 has %s at %s, want 3EB0Q1,3EB0Q2,3EB0Q3 at %s", body.ids(), body.Timestamp, sentAt(3))
	}
}

func TestAQuietWindowOfZeroSendsAtOnce(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhookWithWindow(t, h, rx, "support-bot", "--quiet", "0s", "--max-wait", "0s")

	receive(h, mariaJID, "3EB0Z1")

	if body := deliveryWithID(t, rx.Await(t, 1), "dlv_1"); body.ids() != "3EB0Z1" || body.Timestamp != sentAt(0) {
		t.Fatalf("dlv_1 has %s at %s, want 3EB0Z1 at %s", body.ids(), body.Timestamp, sentAt(0))
	}
}

func TestWebhooksAddTakesAQuietWindowUpToAMinuteAndAMaxWaitUpToTenMinutes(t *testing.T) {
	cases := [][]string{
		{"--quiet", "0s"},
		{"--quiet", "60s", "--max-wait", "60s"},
		{"--quiet", "20s"},
		{"--max-wait", "5s"},
		{"--max-wait", "10m"},
	}
	for _, window := range cases {
		t.Run(strings.Join(window, " "), func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")

			added := h.CLI(t, append([]string{"webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work"}, window...)...)

			if added.Code != 0 || !strings.HasPrefix(added.Stdout, "created the webhook support-bot ") {
				t.Fatalf("exit %d, stdout %q, stderr %q", added.Code, added.Stdout, added.Stderr)
			}
		})
	}
}

func TestWebhooksAddRefusesAQuietWindowOrAMaxWaitOutOfRange(t *testing.T) {
	cases := []struct {
		window []string
		stderr string
	}{
		{[]string{"--quiet", "61s"}, "the quiet window 1m1s is not valid: use a duration from 0s to 60s"},
		{[]string{"--quiet", "-1s"}, "the quiet window -1s is not valid: use a duration from 0s to 60s"},
		{[]string{"--quiet", "soon"}, `the quiet window "soon" is not a duration: use a value such as 5s`},
		{[]string{"--quiet", "5"}, `the quiet window "5" is not a duration: use a value such as 5s`},
		{[]string{"--max-wait", "4s"}, "the max wait 4s is not valid: use a duration from the quiet window (5s) to 10m"},
		{[]string{"--quiet", "40s"}, "the max wait 30s is not valid: use a duration from the quiet window (40s) to 10m"},
		{[]string{"--max-wait", "10m1s"}, "the max wait 10m1s is not valid: use a duration from the quiet window (5s) to 10m"},
		{[]string{"--max-wait", "later"}, `the max wait "later" is not a duration: use a value such as 30s`},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.window, " "), func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")

			added := h.CLI(t, append([]string{"webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work"}, tc.window...)...)

			if added.Code != 1 || added.Stderr != "zapgate: "+tc.stderr+"\n" || added.Stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q, want %q", added.Code, added.Stdout, added.Stderr, tc.stderr)
			}
			if listed := h.CLI(t, "webhooks", "list"); listed.Stdout != "no webhook yet\n" {
				t.Fatalf("a refused add changed the webhooks: %q", listed.Stdout)
			}
		})
	}
}
