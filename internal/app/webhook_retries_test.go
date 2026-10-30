package app_test

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const quietPeriod = 100 * time.Millisecond

func awaitAttempts(t *testing.T, h *apptest.Harness, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if lines := linesWithMessage(h.AuditLines(t), "webhook_delivery"); len(lines) >= n {
			return lines
		}
		if time.Now().After(deadline) {
			t.Fatalf("the audit log has fewer than %d delivery attempts: %s", n, h.AuditRaw())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func requireNoNewRequest(t *testing.T, rx *apptest.Receiver, n int) {
	t.Helper()
	time.Sleep(quietPeriod)
	if got := len(rx.Received()); got != n {
		t.Fatalf("the receiver got %d requests, want %d", got, n)
	}
}

func advanceToNextAttempt(t *testing.T, h *apptest.Harness, rx *apptest.Receiver, delay time.Duration) []apptest.Received {
	t.Helper()
	n := len(rx.Received())
	awaitAttempts(t, h, n)
	h.Clock.Advance(delay - time.Millisecond)
	requireNoNewRequest(t, rx, n)
	h.Clock.Advance(time.Millisecond)
	return rx.Await(t, n+1)
}

func requireAttempts(t *testing.T, received []apptest.Received, id string, after ...time.Duration) {
	t.Helper()
	if len(received) != len(after) {
		t.Fatalf("the receiver got %d requests, want %d", len(received), len(after))
	}
	for i, request := range received {
		want := strconv.FormatInt(apptest.StartTime.Add(after[i]).Unix(), 10)
		if got := request.Header.Get("Webhook-Id"); got != id {
			t.Fatalf("attempt %d has the webhook-id %q, want %q", i+1, got, id)
		}
		if got := request.Header.Get("Webhook-Timestamp"); got != want {
			t.Fatalf("attempt %d has the webhook-timestamp %s, want %s", i+1, got, want)
		}
	}
}

func requireResults(t *testing.T, lines []map[string]any, results ...string) {
	t.Helper()
	if len(lines) != len(results) {
		t.Fatalf("the audit log has %d delivery attempts, want %d: %v", len(lines), len(results), lines)
	}
	for i, result := range results {
		if lines[i]["result"] != result || lines[i]["attempt"] != float64(i+1) {
			t.Fatalf("attempt line %d is %v, want attempt %d with the result %s", i+1, lines[i], i+1, result)
		}
	}
}

func TestAFailedDeliveryLeavesAgainWithTheSameIDOnTheSchedule(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusOK))
	addWebhookSendingAtOnce(t, h, rx, "support-bot")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	rx.Await(t, 1)
	advanceToNextAttempt(t, h, rx, 5*time.Second)
	got := advanceToNextAttempt(t, h, rx, 30*time.Second)

	requireAttempts(t, got, "dlv_1", 0, 5*time.Second, 35*time.Second)
	requireResults(t, awaitAttempts(t, h, 3), "retrying", "retrying", "delivered")
}

func TestADeliveryFailsAfterTheLastRetryOfTheSchedule(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusInternalServerError))
	addWebhookSendingAtOnce(t, h, rx, "support-bot")
	schedule := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute,
		time.Hour, 2 * time.Hour, 4 * time.Hour, 6 * time.Hour, 8 * time.Hour}

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	rx.Await(t, 1)
	sent := []time.Duration{0}
	for _, delay := range schedule {
		advanceToNextAttempt(t, h, rx, delay)
		sent = append(sent, sent[len(sent)-1]+delay)
	}
	awaitAttempts(t, h, len(sent))
	h.Clock.Advance(24 * time.Hour)
	requireNoNewRequest(t, rx, len(sent))

	requireAttempts(t, rx.Received(), "dlv_1", sent...)
	results := []string{"retrying", "retrying", "retrying", "retrying", "retrying", "retrying", "retrying", "retrying", "retrying", "retrying", "failed"}
	requireResults(t, awaitAttempts(t, h, len(sent)), results...)
}

func startOneDelivery(t *testing.T, answers ...apptest.Answer) (*apptest.Harness, *apptest.Receiver) {
	t.Helper()
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, answers...)
	addWebhookSendingAtOnce(t, h, rx, "support-bot")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	rx.Await(t, 1)
	return h, rx
}

func TestAnAnswerThatAsksForARetryGetsTheNextAttempt(t *testing.T) {
	answers := map[string]apptest.Answer{
		"408":             apptest.Reply(http.StatusRequestTimeout),
		"429":             apptest.Reply(http.StatusTooManyRequests),
		"500":             apptest.Reply(http.StatusInternalServerError),
		"502":             apptest.Reply(http.StatusBadGateway),
		"503":             apptest.Reply(http.StatusServiceUnavailable),
		"504":             apptest.Reply(http.StatusGatewayTimeout),
		"301":             apptest.Redirect(http.StatusMovedPermanently, "/elsewhere"),
		"302":             apptest.Redirect(http.StatusFound, "/elsewhere"),
		"307":             apptest.Redirect(http.StatusTemporaryRedirect, "/elsewhere"),
		"308":             apptest.Redirect(http.StatusPermanentRedirect, "/elsewhere"),
		"500 with a body": apptest.ReplyWithBody(http.StatusInternalServerError, "try again later"),
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			h, rx := startOneDelivery(t, answer, apptest.Reply(http.StatusOK))

			got := advanceToNextAttempt(t, h, rx, 5*time.Second)

			requireAttempts(t, got, "dlv_1", 0, 5*time.Second)
			lines := awaitAttempts(t, h, 2)
			requireResults(t, lines, "retrying", "delivered")
			if lines[0]["status"] != float64(answer.Status) {
				t.Fatalf("attempt line %v, want the status %d", lines[0], answer.Status)
			}
		})
	}
}

func TestAnotherClientErrorFailsTheDeliveryAtOnce(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			h, rx := startOneDelivery(t, apptest.Reply(status), apptest.Reply(http.StatusOK))

			lines := awaitAttempts(t, h, 1)
			h.Clock.Advance(24 * time.Hour)
			requireNoNewRequest(t, rx, 1)

			requireResults(t, lines, "failed")
			if lines[0]["status"] != float64(status) {
				t.Fatalf("attempt line %v, want the status %d", lines[0], status)
			}
		})
	}
}

func TestASuccessDeliversTheDeliveryAndIgnoresTheBody(t *testing.T) {
	answers := map[string]apptest.Answer{
		"200":             apptest.Reply(http.StatusOK),
		"202 with a body": apptest.ReplyWithBody(http.StatusAccepted, `{"error":"this body is ignored"}`),
		"204":             apptest.Reply(http.StatusNoContent),
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			h, rx := startOneDelivery(t, answer, apptest.Reply(http.StatusInternalServerError))

			lines := awaitAttempts(t, h, 1)
			h.Clock.Advance(24 * time.Hour)
			requireNoNewRequest(t, rx, 1)

			requireResults(t, lines, "delivered")
		})
	}
}

func TestTheNextAttemptWaitsTheRetryAfterTimeForAtMostAnHour(t *testing.T) {
	inNinetySeconds := apptest.StartTime.Add(90 * time.Second).Format(http.TimeFormat)
	cases := map[string]struct {
		answer apptest.Answer
		wait   time.Duration
	}{
		"429 in seconds":       {answer: apptest.RetryAfter(http.StatusTooManyRequests, "120"), wait: 2 * time.Minute},
		"503 in seconds":       {answer: apptest.RetryAfter(http.StatusServiceUnavailable, "60"), wait: time.Minute},
		"429 as a date":        {answer: apptest.RetryAfter(http.StatusTooManyRequests, inNinetySeconds), wait: 90 * time.Second},
		"429 over an hour":     {answer: apptest.RetryAfter(http.StatusTooManyRequests, "7200"), wait: time.Hour},
		"429 shorter":          {answer: apptest.RetryAfter(http.StatusTooManyRequests, "2"), wait: 2 * time.Second},
		"503 with a bad value": {answer: apptest.RetryAfter(http.StatusServiceUnavailable, "soon"), wait: 6 * time.Second},
		"500 with Retry-After": {answer: apptest.RetryAfter(http.StatusInternalServerError, "120"), wait: 6 * time.Second},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := apptest.Start(t)
			h.Random.Set(1)
			h.AddAccount(t, "work", workPhone, "Alex")
			rx := h.StartReceiver(t, tc.answer, apptest.Reply(http.StatusOK))
			addWebhookSendingAtOnce(t, h, rx, "support-bot")

			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
			rx.Await(t, 1)
			got := advanceToNextAttempt(t, h, rx, tc.wait)

			requireAttempts(t, got, "dlv_1", 0, tc.wait)
		})
	}
}

func TestEachRetryHasAJitterOfTwentyPercent(t *testing.T) {
	cases := map[string]struct {
		random float64
		first  time.Duration
		second time.Duration
	}{
		"earliest": {random: 0, first: 4 * time.Second, second: 24 * time.Second},
		"latest":   {random: 1, first: 6 * time.Second, second: 36 * time.Second},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := apptest.Start(t)
			h.Random.Set(tc.random)
			h.AddAccount(t, "work", workPhone, "Alex")
			rx := h.StartReceiver(t, apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusOK))
			addWebhookSendingAtOnce(t, h, rx, "support-bot")

			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
			rx.Await(t, 1)
			advanceToNextAttempt(t, h, rx, tc.first)
			got := advanceToNextAttempt(t, h, rx, tc.second)

			requireAttempts(t, got, "dlv_1", 0, tc.first, tc.first+tc.second)
		})
	}
}

func TestADeliveryThatWaitsForARetrySurvivesARestart(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusOK))
	addWebhookSendingAtOnce(t, h, rx, "support-bot")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	rx.Await(t, 1)
	awaitAttempts(t, h, 1)
	h.Restart(t)
	got := advanceToNextAttempt(t, h, rx, 5*time.Second)

	requireAttempts(t, got, "dlv_1", 0, 5*time.Second)
	requireResults(t, awaitAttempts(t, h, 2), "retrying", "delivered")
}

func TestAPostCutByARestartLeavesAgainAfterTheAttemptLease(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	release := make(chan struct{})
	rx := h.StartReceiver(t, apptest.Held(release, http.StatusOK), apptest.Reply(http.StatusOK))
	t.Cleanup(releaseOnce(release))
	addWebhookSendingAtOnce(t, h, rx, "support-bot")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	rx.Await(t, 1)
	h.Restart(t)
	h.Clock.Advance(30*time.Second - time.Millisecond)
	requireNoNewRequest(t, rx, 1)
	h.Clock.Advance(time.Millisecond)
	got := rx.Await(t, 2)

	requireAttempts(t, got, "dlv_1", 0, 30*time.Second)
	lines := awaitAttempts(t, h, 1)
	if len(lines) != 1 || lines[0]["attempt"] != float64(2) || lines[0]["result"] != "delivered" {
		t.Fatalf("the attempt lines are %v, want only attempt 2 delivered", lines)
	}
}

func TestANewerDeliveryOfAChatWaitsWhileAnOlderOneRetries(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusOK))
	addWebhookSendingAtOnce(t, h, rx, "support-bot")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi")))
	rx.Await(t, 1)
	awaitAttempts(t, h, 1)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A2", "Maria", morning.Add(time.Minute)), text("tem alguém aí?")))
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "3EB0J1", "João", morning.Add(time.Minute)), text("bom dia")))
	rx.Await(t, 2)
	awaitAttempts(t, h, 2)
	requireNoNewRequest(t, rx, 2)
	h.Clock.Advance(5 * time.Second)
	got := rx.Await(t, 4)

	if order := webhookIDs(got); order != "dlv_1 dlv_3 dlv_1 dlv_2" {
		t.Fatalf("the receiver got %s, want dlv_1 dlv_3 dlv_1 dlv_2", order)
	}
	if body := deliveryWithID(t, got, "dlv_2"); body.ids() != "3EB0A2" {
		t.Fatalf("dlv_2 has %s, want 3EB0A2", body.ids())
	}
}

func TestAFailedDeliveryLetsTheNextDeliveryOfTheChatGo(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	release := make(chan struct{})
	rx := h.StartReceiver(t, apptest.Held(release, http.StatusBadRequest), apptest.Reply(http.StatusOK))
	answer := releaseOnce(release)
	t.Cleanup(answer)
	addWebhookSendingAtOnce(t, h, rx, "support-bot")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi")))
	rx.Await(t, 1)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A2", "Maria", morning.Add(time.Minute)), text("tem alguém aí?")))
	requireNoNewRequest(t, rx, 1)
	answer()
	got := rx.Await(t, 2)

	if order := webhookIDs(got); order != "dlv_1 dlv_2" {
		t.Fatalf("the receiver got %s, want dlv_1 dlv_2", order)
	}
	lines := awaitAttempts(t, h, 2)
	assertFields(t, lines[0], map[string]string{"delivery": "dlv_1", "result": "failed"})
	assertFields(t, lines[1], map[string]string{"delivery": "dlv_2", "result": "delivered"})
}

func releaseOnce(release chan struct{}) func() {
	var once sync.Once
	return func() { once.Do(func() { close(release) }) }
}

func webhookIDs(received []apptest.Received) string {
	ids := make([]string, 0, len(received))
	for _, request := range received {
		ids = append(ids, request.Header.Get("Webhook-Id"))
	}
	return strings.Join(ids, " ")
}

func corruptTheEventOf(t *testing.T, h *apptest.Harness, messageID string) {
	t.Helper()
	if _, err := appDB(t, h).Exec(`UPDATE events SET created_at = 'not a time' WHERE message_id = ?`, messageID); err != nil {
		t.Fatal(err)
	}
}

func TestADeliveryWithABodyThatCannotBeBuiltFailsAtOnce(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")
	receive(h, mariaJID, "3EB0A1")
	awaitJournalRead(t, h)
	corruptTheEventOf(t, h, "3EB0A1")

	passQuietWindow(h)
	failed := awaitAttempts(t, h, 1)[0]
	receiveAndWait(h, mariaJID, "3EB0A2")
	got := rx.Await(t, 1)
	awaitAttempts(t, h, 2)
	h.Clock.Advance(24 * time.Hour)
	requireNoNewRequest(t, rx, 1)

	assertFields(t, failed, map[string]string{"webhook": "support-bot", "account": "work", "delivery": "dlv_1", "result": "failed"})
	if failed["events"] != float64(1) || failed["attempt"] != float64(1) || failed["status"] != float64(0) {
		t.Fatalf("attempt line %v, want 1 event, attempt 1 and no status", failed)
	}
	if body := deliveryWithID(t, got, "dlv_2"); body.ids() != "3EB0A2" {
		t.Fatalf("dlv_2 has %s, want 3EB0A2", body.ids())
	}
	if lines := awaitAttempts(t, h, 2); len(lines) != 2 {
		t.Fatalf("the audit log has %d delivery attempts, want 2: %v", len(lines), lines)
	}
	row := listedWebhook(t, h, "support-bot")
	if row["PENDING"] != "0" || row["FAILED"] != "1" || !strings.HasPrefix(row["LAST ERROR"], "zapgate cannot build the delivery: ") {
		t.Fatalf("webhooks list %v, want 1 failed delivery that zapgate cannot build", row)
	}
}
