package app_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/app"
	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/sqlite"
)

const week = 7 * 24 * time.Hour

func TestTheDailyPassDeletesTheEventsAndTheSettledDeliveriesOlderThanAWeek(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusOK), apptest.Reply(http.StatusBadRequest), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "work")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("da semana passada")))
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "3EB0J1", "João", morning), text("também")))
	awaitAttempts(t, h, 2)
	awaitRetentionPass(t, h, h.Clock.Now())
	h.Clock.Advance(week)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A2", "Maria", morning.Add(week)), text("de hoje")))
	awaitAttempts(t, h, 3)

	runDailyRetention(t, h)

	if got := journal(t, h); got != "message.received 5531944443333@s.whatsapp.net 3EB0A2" {
		t.Fatalf("the event journal has %q", got)
	}
	if got := deliveries(t, h); got != "dlv_3 delivered" {
		t.Fatalf("the deliveries are %q", got)
	}
}

func TestAPendingDeliveryKeepsItsEventsThroughAWeekOff(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t, apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "work")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	awaitAttempts(t, h, 1)
	awaitRetentionPass(t, h, h.Clock.Now())
	h.Stop(t)
	h.Clock.Advance(week + 24*time.Hour)

	h.Resume(t)

	awaitAttempts(t, h, 2)
	awaitRetentionPass(t, h, h.Clock.Now())
	if got := journal(t, h); got != "message.received 5531944443333@s.whatsapp.net 3EB0A1" {
		t.Fatalf("the event journal has %q", got)
	}
	got := advanceToNextAttempt(t, h, rx, 30*time.Second)
	if last := deliveryWithID(t, got[len(got)-1:], "dlv_1"); last.ids() != "3EB0A1" {
		t.Fatalf("the last attempt of dlv_1 has %q, want 3EB0A1", last.ids())
	}
	requireResults(t, awaitAttempts(t, h, 3), "retrying", "retrying", "delivered")
}

func TestTheEventsOfADeliveryInFlightStay(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	release := make(chan struct{})
	rx := h.StartReceiver(t, apptest.Reply(http.StatusOK), apptest.Held(release, http.StatusOK))
	t.Cleanup(releaseOnce(release))
	addWebhook(t, h, rx, "support-bot", "work")
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "3EB0J1", "João", morning), text("entregue")))
	awaitAttempts(t, h, 1)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("ainda no envio")))
	rx.Await(t, 2)
	awaitRetentionPass(t, h, h.Clock.Now())
	h.Clock.Advance(week)

	runDailyRetention(t, h)

	if got := journal(t, h); got != "message.received 5531944443333@s.whatsapp.net 3EB0A1" {
		t.Fatalf("the event journal has %q", got)
	}
	if got := deliveries(t, h); got != "dlv_2 open" {
		t.Fatalf("the deliveries are %q", got)
	}
}

func TestAnEventAfterTheCleanupGetsANewSequenceNumberAndANewDeliveryID(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("da semana passada")))
	awaitAttempts(t, h, 1)
	awaitRetentionPass(t, h, h.Clock.Now())
	h.Clock.Advance(week)
	runDailyRetention(t, h)
	if events, sent := journal(t, h), deliveries(t, h); events != "" || sent != "" {
		t.Fatalf("after the cleanup the journal has %q and the deliveries are %q", events, sent)
	}

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A2", "Maria", morning.Add(week+24*time.Hour)), text("de hoje")))

	if body := deliveryWithID(t, rx.Await(t, 2), "dlv_2"); body.ids() != "3EB0A2" {
		t.Fatalf("dlv_2 has %s, want 3EB0A2", body.ids())
	}
}

func TestNewMessagesAreWrittenBetweenTheBatchesOfTheEventCleanup(t *testing.T) {
	const old = 3000
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	for i := range old {
		h.WhatsApp.Emit(workPhone, message(direct(mariaJID, fmt.Sprintf("OLD%04d", i), "Maria", morning.Add(time.Duration(i)*time.Second)), text("antiga")))
	}
	awaitRetentionPass(t, h, h.Clock.Now())

	h.Clock.Advance(week + 24*time.Hour)
	written := writeDuringTheEventCleanup(t, h, old)
	awaitRetentionPass(t, h, h.Clock.Now())

	if got, want := journal(t, h), "message.received 5531944443333@s.whatsapp.net "+written; got != want {
		t.Fatalf("the event journal has %q, want %q", got, want)
	}
}

func writeDuringTheEventCleanup(t *testing.T, h *apptest.Harness, total int) string {
	t.Helper()
	db := appDB(t, h)
	deadline := time.Now().Add(retentionWait)
	for i := 0; time.Now().Before(deadline); i++ {
		before := ownerStatus(t, h).Retention
		if !before.Running {
			if before.StartedAt == h.Clock.Now().UTC().Format(time.RFC3339) {
				t.Fatalf("the pass ended before a new message was written between two batches: %+v", before)
			}
			continue
		}
		if left := eventCount(t, db); left == 0 || left == total {
			continue
		}
		id := fmt.Sprintf("NEW%d", i)
		h.WhatsApp.Emit(workPhone, message(direct(mariaJID, id, "Maria", morning.Add(week+24*time.Hour)), text("nova")))
		if ownerStatus(t, h).Retention.Running {
			return id
		}
		t.Fatalf("the new message waited for the whole event cleanup: before %+v", before)
	}
	t.Fatal("the event cleanup did not start")
	return ""
}

func eventCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func appDB(t *testing.T, h *apptest.Harness) *sql.DB {
	t.Helper()
	db, err := sql.Open(sqlite.Driver, sqlite.DSN(filepath.Join(h.DataDir, app.AppDBFile)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func deliveries(t *testing.T, h *apptest.Harness) string {
	t.Helper()
	rows, err := appDB(t, h).Query(`SELECT id, state FROM webhook_deliveries ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var (
			id    int64
			state string
		)
		if err := rows.Scan(&id, &state); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("dlv_%d %s", id, state))
	}
	return strings.Join(lines, "\n")
}
