package app_test

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const webhookListHeader = "NAME URL ACCOUNTS CONTENT STATE LAST SUCCESS PENDING FAILED LAST ERROR"

func webhookRows(t *testing.T, h *apptest.Harness) []string {
	t.Helper()
	listed := h.CLI(t, "webhooks", "list")
	if listed.Code != 0 {
		t.Fatalf("webhooks list: exit %d, stderr %q", listed.Code, listed.Stderr)
	}
	rows := tableRows(t, listed.Stdout)
	if len(rows) == 0 || rows[0] != webhookListHeader {
		t.Fatalf("webhooks list %q, want the header %q", listed.Stdout, webhookListHeader)
	}
	return rows
}

func TestWebhooksListShowsThePendingAndFailedDeliveriesAndTheLastSuccess(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusOK), apptest.Reply(http.StatusServiceUnavailable),
		apptest.ReplyWithBody(http.StatusBadRequest, "wrong tenant acme-42"))
	addWebhookSendingAtOnce(t, h, rx, "support-bot")
	h.Clock.Advance(time.Minute)

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi")))
	awaitAttempts(t, h, 1)
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "3EB0J1", "João", morning), text("bom dia")))
	awaitAttempts(t, h, 2)
	peer := types.NewJID("5531900000001", types.DefaultUserServer)
	h.WhatsApp.Emit(workPhone, message(direct(peer, "3EB0C1", "", morning), text("olá")))
	awaitAttempts(t, h, 3)

	rows := webhookRows(t, h)
	want := "support-bot " + rx.URL + " work minimal enabled 2026-10-13T12:01:00Z 1 1 400 Bad Request"
	if len(rows) != 2 || rows[1] != want {
		t.Fatalf("webhooks list %q, want %q", rows, want)
	}
	if strings.Contains(strings.Join(rows, "\n"), "acme-42") {
		t.Fatal("webhooks list shows the response body")
	}
}

func TestAConnectionFailureIsRetriedAndShownInTheList(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	downURL := "http://" + closed.Addr().String() + "/hooks/whatsapp"
	closed.Close()
	h.CLI(t, "webhooks", "add", "support-bot", "--url", downURL, "--accounts", "work", "--quiet", "0s")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi")))
	awaitAttempts(t, h, 1)
	rows := webhookRows(t, h)
	h.Clock.Advance(5 * time.Second)
	lines := awaitAttempts(t, h, 2)

	requireResults(t, lines, "retrying", "retrying")
	if lines[0]["status"] != float64(0) {
		t.Fatalf("attempt line %v, want no status", lines[0])
	}
	if !strings.HasPrefix(rows[1], "support-bot "+downURL+" work minimal enabled - 1 0 ") || !strings.Contains(rows[1], "connection refused") {
		t.Fatalf("webhooks list %q, want 1 pending delivery and the connection error", rows)
	}
}

func TestABlockedAttemptIsAuditedAndShownInTheList(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	h.Hosts.Point("bot", "127.0.0.1")
	webhookURL := strings.Replace(rx.URL, "127.0.0.1", "bot", 1)
	added := h.CLI(t, "webhooks", "add", "support-bot", "--url", webhookURL, "--accounts", "work", "--quiet", "0s")
	rx.Trust(secretFrom(t, added.Stdout))
	h.Hosts.Point("bot", "203.0.113.10")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi")))
	awaitAttempts(t, h, 1)
	rows := webhookRows(t, h)
	h.Hosts.Point("bot", "127.0.0.1")
	h.Clock.Advance(5 * time.Second)
	got := rx.Await(t, 1)

	want := "support-bot " + webhookURL + " work minimal enabled - 1 0 bot (203.0.113.10) is not a loopback, private or CGNAT address, so the URL must use https"
	if rows[1] != want {
		t.Fatalf("webhooks list %q, want %q", rows, want)
	}
	requireAttempts(t, got, "dlv_1", 5*time.Second)
	requireResults(t, awaitAttempts(t, h, 2), "blocked", "delivered")
}
