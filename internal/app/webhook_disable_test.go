package app_test

import (
	"net/http"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func requireColumns(t *testing.T, h *apptest.Harness, name string, want map[string]string) {
	t.Helper()
	row := listedWebhook(t, h, name)
	for column, value := range want {
		if row[column] != value {
			t.Fatalf("webhooks list shows %s %q for %s, want %q: %v", column, row[column], name, value, row)
		}
	}
}

func receiveAndWait(h *apptest.Harness, chat types.JID, id string) {
	h.WhatsApp.Emit(workPhone, message(direct(chat, id, "", h.Clock.Now()), text("oi")))
	h.Clock.Advance(time.Minute)
}

func TestAGoneReceiverFailsTheDeliveryAndDisablesTheWebhook(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusGone), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "work")

	receiveAndWait(h, mariaJID, "3EB0A1")
	lines := awaitAttempts(t, h, 1)
	h.Clock.Advance(24 * time.Hour)
	requireNoNewRequest(t, rx, 1)

	requireResults(t, lines, "failed")
	if lines[0]["status"] != float64(http.StatusGone) {
		t.Fatalf("attempt line %v, want the status 410", lines[0])
	}
	requireColumns(t, h, "support-bot", map[string]string{"STATE": "disabled", "PENDING": "0", "FAILED": "1", "LAST ERROR": "410 Gone"})
}

func TestADisabledWebhookDropsItsEventsAndEnableLetsNewEventsReachIt(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusGone), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "work")
	receiveAndWait(h, mariaJID, "3EB0A1")
	awaitAttempts(t, h, 1)

	receiveAndWait(h, mariaJID, "3EB0A2")
	requireNoNewRequest(t, rx, 1)
	enabled := h.CLI(t, "webhooks", "enable", "support-bot")
	receiveAndWait(h, mariaJID, "3EB0A3")

	if enabled.Code != 0 || enabled.Stdout != "enabled the webhook support-bot\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", enabled.Code, enabled.Stdout, enabled.Stderr)
	}
	got := rx.Await(t, 2)
	if id := got[1].Header.Get("Webhook-Id"); id != "dlv_2" || deliveryWithID(t, got, id).ids() != "3EB0A3" {
		t.Fatalf("the second delivery is %s with %s, want dlv_2 with 3EB0A3", id, deliveryWithID(t, got, id).ids())
	}
	awaitAttempts(t, h, 2)
	requireColumns(t, h, "support-bot", map[string]string{"STATE": "enabled", "PENDING": "0", "FAILED": "1"})
}

func TestADisabledWebhookHoldsItsPendingDeliveriesUntilItIsEnabled(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.RetryAfter(http.StatusServiceUnavailable, "3600"), apptest.Reply(http.StatusGone), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "work")
	receiveAndWait(h, mariaJID, "3EB0A1")
	awaitAttempts(t, h, 1)
	receiveAndWait(h, joaoJID, "3EB0J1")
	awaitAttempts(t, h, 2)

	h.Clock.Advance(2 * time.Hour)
	requireNoNewRequest(t, rx, 2)
	requireColumns(t, h, "support-bot", map[string]string{"STATE": "disabled", "PENDING": "1", "FAILED": "1"})
	h.CLI(t, "webhooks", "enable", "support-bot")

	got := rx.Await(t, 3)
	if order := webhookIDs(got); order != "dlv_1 dlv_2 dlv_1" {
		t.Fatalf("the receiver got %s, want dlv_1 dlv_2 dlv_1", order)
	}
	assertFields(t, awaitAttempts(t, h, 3)[2], map[string]string{"delivery": "dlv_1", "result": "delivered"})
}

func failTwoDeliveries(t *testing.T, gap time.Duration) (*apptest.Harness, *apptest.Receiver) {
	t.Helper()
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusBadRequest), apptest.Reply(http.StatusBadRequest), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "work")
	receiveAndWait(h, mariaJID, "3EB0A1")
	awaitAttempts(t, h, 1)
	h.Clock.Advance(gap)
	receiveAndWait(h, joaoJID, "3EB0J1")
	awaitAttempts(t, h, 2)
	return h, rx
}

func redeliver(t *testing.T, h *apptest.Harness, args ...string) string {
	t.Helper()
	result := h.CLI(t, append([]string{"webhooks", "redeliver", "support-bot"}, args...)...)
	if result.Code != 0 {
		t.Fatalf("redeliver: exit %d, stderr %q", result.Code, result.Stderr)
	}
	return result.Stdout
}

func TestRedeliverSendsTheFailedDeliveriesAgainWithTheirIDs(t *testing.T) {
	h, rx := failTwoDeliveries(t, 0)

	stdout := redeliver(t, h)

	if stdout != "opened again 2 failed deliveries of the webhook support-bot\n" {
		t.Fatalf("stdout %q", stdout)
	}
	got := rx.Await(t, 4)
	if again := webhookIDs(got[2:]); again != "dlv_1 dlv_2" && again != "dlv_2 dlv_1" {
		t.Fatalf("the receiver got %s again, want dlv_1 and dlv_2", again)
	}
	for _, line := range awaitAttempts(t, h, 4)[2:] {
		assertFields(t, line, map[string]string{"result": "delivered"})
		if line["attempt"] != float64(1) {
			t.Fatalf("attempt line %v, want attempt 1 after the redeliver", line)
		}
	}
	requireColumns(t, h, "support-bot", map[string]string{"PENDING": "0", "FAILED": "0"})
}

func TestRedeliverOpensOnlyTheDeliveriesThatFailedInThePeriod(t *testing.T) {
	h, rx := failTwoDeliveries(t, 30*time.Hour)

	recent := redeliver(t, h)
	rx.Await(t, 3)
	awaitAttempts(t, h, 3)
	older := redeliver(t, h, "--since", "48h")
	got := rx.Await(t, 4)

	if recent != "opened again 1 failed delivery of the webhook support-bot\n" || older != recent {
		t.Fatalf("redeliver printed %q, then %q", recent, older)
	}
	if again := webhookIDs(got[2:]); again != "dlv_2 dlv_1" {
		t.Fatalf("the receiver got %s again, want dlv_2 then dlv_1", again)
	}
}

func TestRedeliverWithoutAFailedDelivery(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "all")

	if stdout := redeliver(t, h, "--since=1h"); stdout != "opened again 0 failed deliveries of the webhook support-bot\n" {
		t.Fatalf("stdout %q", stdout)
	}
}

func TestEnableAndRedeliverRejectBadRequests(t *testing.T) {
	cases := map[string]struct {
		args   []string
		stderr string
	}{
		"enable an unknown webhook":    {args: []string{"enable", "ghost"}, stderr: "zapgate: there is no webhook named \"ghost\"\n"},
		"redeliver an unknown webhook": {args: []string{"redeliver", "ghost"}, stderr: "zapgate: there is no webhook named \"ghost\"\n"},
		"a period that is not a duration": {
			args:   []string{"redeliver", "support-bot", "--since", "yesterday"},
			stderr: "zapgate: the period \"yesterday\" is not a duration: use a value such as 24h\n",
		},
		"a period that is not positive": {
			args:   []string{"redeliver", "support-bot", "--since", "-1h"},
			stderr: "zapgate: the period -1h0m0s is not valid: use a duration greater than 0s\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := apptest.Start(t)
			h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "all")

			result := h.CLI(t, append([]string{"webhooks"}, tc.args...)...)

			if result.Code != 1 || result.Stderr != tc.stderr || result.Stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q, want %q", result.Code, result.Stdout, result.Stderr, tc.stderr)
			}
		})
	}
}

func TestEnableAndRedeliverAreAudited(t *testing.T) {
	h, _ := failTwoDeliveries(t, 0)
	h.CLI(t, "webhooks", "enable", "support-bot")
	redeliver(t, h)

	actions := linesWithMessage(h.AuditLines(t), "admin")
	want := []map[string]string{
		{"action": "account_add", "account": "work"},
		{"action": "webhook_add", "webhook": "support-bot"},
		{"action": "webhook_enable", "webhook": "support-bot"},
		{"action": "webhook_redeliver", "webhook": "support-bot"},
	}
	if len(actions) != len(want) {
		t.Fatalf("audit has %d admin lines, want %d: %s", len(actions), len(want), h.AuditRaw())
	}
	for i, fields := range want {
		assertFields(t, actions[i], fields)
	}
	if actions[3]["deliveries"] != float64(2) {
		t.Fatalf("audit line %v, want 2 deliveries", actions[3])
	}
}
