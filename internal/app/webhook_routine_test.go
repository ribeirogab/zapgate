package app_test

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const routineToken = "sk-ant-oat01-routine-token"

func addRoutineWebhook(t *testing.T, h *apptest.Harness, rx *apptest.Receiver, name, accounts string, options ...string) {
	t.Helper()
	addWebhook(t, h, rx, name, accounts, append([]string{"--format", "claude-routine", "--header", "Authorization: Bearer " + routineToken}, options...)...)
}

func routineText(t *testing.T, request apptest.Received) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(request.Body, &body); err != nil {
		t.Fatalf("the routine body %s is not a JSON object of strings: %v", request.Body, err)
	}
	if len(body) != 1 {
		t.Fatalf("the routine body %s has other fields than text", request.Body)
	}
	return body["text"]
}

func requireRoutineHeaders(t *testing.T, request apptest.Received) {
	t.Helper()
	want := map[string]string{
		"Authorization":     "Bearer " + routineToken,
		"Anthropic-Beta":    "experimental-cc-routine-2026-04-01",
		"Anthropic-Version": "2026-10-21",
		"Content-Type":      "application/json",
		"User-Agent":        "zapgate",
	}
	for name, value := range want {
		if got := request.Header.Get(name); got != value {
			t.Fatalf("header %s = %q, want %q", name, got, value)
		}
	}
}

func TestAClaudeRoutineWebhookSendsTheStandardBodyAsText(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	standard := h.StartReceiver(t)
	routine := h.StartReceiver(t)
	addWebhook(t, h, standard, "support-bot", "work")
	addRoutineWebhook(t, h, routine, "triage", "work")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	passQuietWindow(h)
	want := standard.Await(t, 1)[0]
	got := routine.Await(t, 1)[0]

	if text := routineText(t, got); text != string(want.Body) {
		t.Fatalf("the routine text is %s, want the standard body %s", text, want.Body)
	}
	requireRoutineHeaders(t, got)
	if id := got.Header.Get("Webhook-Id"); !strings.HasPrefix(id, "dlv_") {
		t.Fatalf("the routine delivery has the webhook-id %q, want a dlv_ id", id)
	}
}

const shortWebhookTimeout = 200 * time.Millisecond

func startTimingOutReceiver(t *testing.T, h *apptest.Harness) *apptest.Receiver {
	t.Helper()
	release := make(chan struct{})
	rx := h.StartReceiver(t, apptest.Held(release, http.StatusOK), apptest.Reply(http.StatusOK))
	t.Cleanup(releaseOnce(release))
	return rx
}

func TestAClaudeRoutineRequestThatTimedOutAfterTheSendFailsWithoutARetry(t *testing.T) {
	h := apptest.Start(t, apptest.WithWebhookTimeout(shortWebhookTimeout))
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := startTimingOutReceiver(t, h)
	addRoutineWebhook(t, h, rx, "triage", "work", "--quiet", "0s")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	lines := awaitAttempts(t, h, 1)
	h.Clock.Advance(24 * time.Hour)
	requireNoNewRequest(t, rx, 1)

	requireResults(t, lines, "failed")
	row := listedWebhook(t, h, "triage")
	if row["PENDING"] != "0" || row["FAILED"] != "1" || row["LAST ERROR"] != "the receiver did not answer within 200ms" {
		t.Fatalf("webhooks list %v, want 1 failed delivery and the timeout", row)
	}
}

func TestAClaudeRoutineRequestThatLostItsConnectionAfterTheSendFailsWithoutARetry(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.HangUp(), apptest.Reply(http.StatusOK))
	addRoutineWebhook(t, h, rx, "triage", "work", "--quiet", "0s")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	lines := awaitAttempts(t, h, 1)
	h.Clock.Advance(24 * time.Hour)
	requireNoNewRequest(t, rx, 1)

	requireResults(t, lines, "failed")
}

func TestAStandardRequestThatTimedOutGetsTheNextAttempt(t *testing.T) {
	h := apptest.Start(t, apptest.WithWebhookTimeout(shortWebhookTimeout))
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := startTimingOutReceiver(t, h)
	addWebhook(t, h, rx, "support-bot", "work", "--quiet", "0s")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	rx.Await(t, 1)
	got := advanceToNextAttempt(t, h, rx, 5*time.Second)

	requireAttempts(t, got, "dlv_1", 0, 5*time.Second)
	requireResults(t, awaitAttempts(t, h, 2), "retrying", "delivered")
}

func TestAClaudeRoutineDeliveryGetsTheNextAttemptAfterA429OrA5xx(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			rx := h.StartReceiver(t, apptest.Reply(status), apptest.Reply(http.StatusOK))
			addRoutineWebhook(t, h, rx, "triage", "work", "--quiet", "0s")

			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
			rx.Await(t, 1)
			got := advanceToNextAttempt(t, h, rx, 5*time.Second)

			requireAttempts(t, got, "dlv_1", 0, 5*time.Second)
			requireResults(t, awaitAttempts(t, h, 2), "retrying", "delivered")
		})
	}
}

func TestAClaudeRoutineDeliveryGetsTheNextAttemptWhenTheConnectionFails(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	downURL := "http://" + closed.Addr().String() + "/hooks/whatsapp"
	closed.Close()
	h.CLI(t, "webhooks", "add", "triage", "--url", downURL, "--accounts", "work", "--format", "claude-routine", "--quiet", "0s")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	awaitAttempts(t, h, 1)
	h.Clock.Advance(5 * time.Second)

	requireResults(t, awaitAttempts(t, h, 2), "retrying", "retrying")
}

func TestTheTextOfAClaudeRoutineBodyHasAtMost65536Characters(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addRoutineWebhook(t, h, rx, "triage", "work", "--content", "full")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A2", "Maria", morning), crowdedContactCards()))
	passQuietWindow(h)
	text := routineText(t, rx.Await(t, 1)[0])

	if chars := utf8.RuneCountInString(text); chars > 65_536 {
		t.Fatalf("the routine text has %d characters, want at most 65536", chars)
	}
	var standard map[string]any
	if err := json.Unmarshal([]byte(text), &standard); err != nil {
		t.Fatalf("the routine text is not the standard body: %v", err)
	}
	if data := standard["data"].(map[string]any); data["truncated"] != true {
		t.Fatalf("the standard body in the text is not truncated: %s", text)
	}
}

func TestWebhooksTestSendsTheClaudeRoutineFormat(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	addRoutineWebhook(t, h, rx, "triage", "all")

	tested := h.CLI(t, "webhooks", "test", "triage")

	if tested.Code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", tested.Code, tested.Stdout, tested.Stderr)
	}
	got := rx.Await(t, 1)[0]
	if text := routineText(t, got); text != `{"type":"webhook.test","timestamp":"2026-10-13T12:00:00Z","data":{"webhook":"triage"}}` {
		t.Fatalf("the routine text is %s", text)
	}
	requireRoutineHeaders(t, got)
}
