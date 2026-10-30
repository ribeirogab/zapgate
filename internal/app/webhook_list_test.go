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

func TestWebhooksListShowsThePendingAndFailedDeliveriesAndTheLastSuccess(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusOK), apptest.Reply(http.StatusServiceUnavailable),
		apptest.ReplyWithBody(http.StatusBadRequest, "wrong tenant acme-42"))
	addWebhook(t, h, rx, "support-bot", "work")
	h.Clock.Advance(time.Minute)

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi")))
	awaitAttempts(t, h, 1)
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "3EB0J1", "João", morning), text("bom dia")))
	awaitAttempts(t, h, 2)
	peer := types.NewJID("5531900000001", types.DefaultUserServer)
	h.WhatsApp.Emit(workPhone, message(direct(peer, "3EB0C1", "", morning), text("olá")))
	awaitAttempts(t, h, 3)

	row := listedWebhook(t, h, "support-bot")
	if row["LAST SUCCESS"] != "2026-10-13T12:01:00Z" || row["PENDING"] != "1" || row["FAILED"] != "1" || row["LAST ERROR"] != "400 Bad Request" {
		t.Fatalf("webhooks list %v", row)
	}
	if strings.Contains(h.CLI(t, "webhooks", "list").Stdout, "acme-42") {
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
	h.CLI(t, "webhooks", "add", "support-bot", "--url", downURL, "--accounts", "work")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi")))
	awaitAttempts(t, h, 1)
	row := listedWebhook(t, h, "support-bot")
	h.Clock.Advance(5 * time.Second)
	lines := awaitAttempts(t, h, 2)

	requireResults(t, lines, "retrying", "retrying")
	if lines[0]["status"] != float64(0) {
		t.Fatalf("attempt line %v, want no status", lines[0])
	}
	if row["LAST SUCCESS"] != "-" || row["PENDING"] != "1" || row["FAILED"] != "0" || !strings.Contains(row["LAST ERROR"], "connection refused") {
		t.Fatalf("webhooks list %v, want 1 pending delivery and the connection error", row)
	}
}

func TestABlockedAttemptIsAuditedAndShownInTheList(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	h.Hosts.Point("bot", "127.0.0.1")
	webhookURL := strings.Replace(rx.URL, "127.0.0.1", "bot", 1)
	added := h.CLI(t, "webhooks", "add", "support-bot", "--url", webhookURL, "--accounts", "work")
	rx.Trust(secretFrom(t, added.Stdout))
	h.Hosts.Point("bot", "203.0.113.10")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi")))
	awaitAttempts(t, h, 1)
	row := listedWebhook(t, h, "support-bot")
	h.Hosts.Point("bot", "127.0.0.1")
	h.Clock.Advance(5 * time.Second)
	got := rx.Await(t, 1)

	want := "bot (203.0.113.10) is not a loopback, private or CGNAT address, so the URL must use https"
	if row["PENDING"] != "1" || row["FAILED"] != "0" || row["LAST ERROR"] != want {
		t.Fatalf("webhooks list %v, want 1 pending delivery and the error %q", row, want)
	}
	requireAttempts(t, got, "dlv_1", 5*time.Second)
	requireResults(t, awaitAttempts(t, h, 2), "blocked", "delivered")
}
