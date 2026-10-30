package app_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestALogoutReachesTheWebhookAsAnAccountStateDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "account-watch", "work", "--events", "account.state")

	h.WhatsApp.UnlinkDevice(workPhone)

	got := rx.Await(t, 1)
	if id := got[0].Header.Get("Webhook-Id"); id != "dlv_1" {
		t.Fatalf("webhook-id %q, want dlv_1", id)
	}
	assertJSON(t, bodyOf(t, got[0]), `{
		"type": "account.state",
		"timestamp": "2026-10-13T12:00:00Z",
		"data": {"account": "work", "account_phone": "+5511988887777", "state": "logged_out"}
	}`)
}

func TestARestartShowsTheReceiverTheDisconnectAndTheReconnect(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "account-watch", "work", "--events", "account.state")

	h.Restart(t)
	rx.Await(t, 2)
	h.WhatsApp.DropConnection(workPhone)

	if got := deliveredStates(t, rx.Await(t, 3)); got != "dlv_1 disconnected, dlv_2 connected, dlv_3 disconnected" {
		t.Fatalf("the receiver got %s", got)
	}
}

func TestARestartWhileOfflineSendsNoSecondDisconnect(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "account-watch", "work", "--events", "account.state")
	h.WhatsApp.DropConnection(workPhone)
	rx.Await(t, 1)

	h.Restart(t)
	h.WhatsApp.RestoreConnection(workPhone)

	if got := deliveredStates(t, rx.Await(t, 2)); got != "dlv_1 disconnected, dlv_2 connected" {
		t.Fatalf("the receiver got %s", got)
	}
}

func TestAnAccountStateDeliveryFollowsTheRetrySchedule(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "account-watch", "work", "--events", "account.state")

	h.WhatsApp.DropConnection(workPhone)
	rx.Await(t, 1)
	h.WhatsApp.RestoreConnection(workPhone)
	advanceToNextAttempt(t, h, rx, 5*time.Second)

	got := rx.Await(t, 3)
	if states := deliveredStates(t, got); states != "dlv_1 disconnected, dlv_1 disconnected, dlv_2 connected" {
		t.Fatalf("the receiver got %s", states)
	}
	requireAttempts(t, got[:2], "dlv_1", 0, 5*time.Second)
	lines := awaitAttempts(t, h, 3)
	requireResults(t, lines[:2], "retrying", "delivered")
	assertFields(t, lines[2], map[string]string{"webhook": "account-watch", "account": "work", "delivery": "dlv_2", "result": "delivered"})
	if lines[2]["events"] != float64(1) {
		t.Fatalf("audit line %v, want 1 event", lines[2])
	}
}

func TestAWebhookWithoutAccountStateGetsNoAccountStateDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	states := h.StartReceiver(t)
	rx := h.StartReceiver(t)
	addWebhook(t, h, states, "account-watch", "work", "--events", "account.state")
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.DropConnection(workPhone)
	h.WhatsApp.RestoreConnection(workPhone)
	states.Await(t, 2)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("oi")))
	h.Clock.Advance(time.Minute)

	if id, body := firstDelivery(t, rx); id != "dlv_3" || body.ids() != "3EB0M1" {
		t.Fatalf("the first support-bot delivery is %s with %s, want dlv_3 with 3EB0M1", id, body.ids())
	}
}

func deliveredStates(t *testing.T, received []apptest.Received) string {
	t.Helper()
	var states []string
	for _, request := range received {
		data, _ := bodyOf(t, request)["data"].(map[string]any)
		states = append(states, fmt.Sprintf("%s %v", request.Header.Get("Webhook-Id"), data["state"]))
	}
	return strings.Join(states, ", ")
}

func TestTheEventJournalHasEachRealChangeOfTheAccountState(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")

	h.WhatsApp.DropConnection(workPhone)
	h.WhatsApp.DropConnection(workPhone)
	h.WhatsApp.RestoreConnection(workPhone)
	h.WhatsApp.RestoreConnection(workPhone)
	h.WhatsApp.UnlinkDevice(workPhone)

	if got := stateJournal(t, h); got != "connected,disconnected,connected,logged_out" {
		t.Fatalf("the event journal has the account states %q", got)
	}
}

func stateJournal(t *testing.T, h *apptest.Harness) string {
	t.Helper()
	rows, err := appDB(t, h).Query(`SELECT state FROM events WHERE kind = 'account.state' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var states []string
	for rows.Next() {
		var state string
		if err := rows.Scan(&state); err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	return strings.Join(states, ",")
}
