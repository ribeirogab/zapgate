package app_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/app"
	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/sqlite"
)

type deliveryBody struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Data      struct {
		Account  string `json:"account"`
		Messages []struct {
			ID    string          `json:"id"`
			Media json.RawMessage `json:"media"`
		} `json:"messages"`
		Fetch struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		} `json:"fetch"`
	} `json:"data"`
}

func (b deliveryBody) ids() string {
	var ids []string
	for _, msg := range b.Data.Messages {
		ids = append(ids, msg.ID)
	}
	return strings.Join(ids, ",")
}

func bodyOf(t *testing.T, request apptest.Received) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(request.Body, &body); err != nil {
		t.Fatalf("the body %s is not JSON: %v", request.Body, err)
	}
	return body
}

func deliveryWithID(t *testing.T, received []apptest.Received, id string) deliveryBody {
	t.Helper()
	for _, request := range received {
		if request.Header.Get("Webhook-Id") != id {
			continue
		}
		var body deliveryBody
		if err := json.Unmarshal(request.Body, &body); err != nil {
			t.Fatalf("the body %s is not JSON: %v", request.Body, err)
		}
		return body
	}
	t.Fatalf("the receiver got no delivery %s", id)
	return deliveryBody{}
}

func firstDelivery(t *testing.T, rx *apptest.Receiver) (string, deliveryBody) {
	t.Helper()
	got := rx.Await(t, 1)
	id := got[0].Header.Get("Webhook-Id")
	return id, deliveryWithID(t, got, id)
}

func TestAReceivedMessageReachesTheWebhookAsASignedDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	passQuietWindow(h)

	got := rx.Await(t, 1)
	if id := got[0].Header.Get("Webhook-Id"); id != "dlv_1" {
		t.Fatalf("webhook-id %q, want dlv_1", id)
	}
	assertJSON(t, bodyOf(t, got[0]), `{
		"type": "message.received",
		"timestamp": "2026-10-13T12:00:05Z",
		"data": {
			"account": "work",
			"account_phone": "+5511988887777",
			"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "phone": "+5531944443333"},
			"messages": [{
				"id": "3EB0A1",
				"kind": "text",
				"from_me": false,
				"sent_at": "2026-10-13T09:00:00Z",
				"sender": {"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333"}
			}],
			"fetch": {"tool": "get_messages", "arguments": {"account": "work", "chat": "5531944443333@s.whatsapp.net"}}
		}
	}`)
}

func TestTheMinimalBodyHasTheMediaButNoTextAndNoNames(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), image("a foto do cardápio")))
	passQuietWindow(h)
	rx.Await(t, 1)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A2", "Maria", morning.Add(time.Minute)), voiceNote()))
	passQuietWindow(h)

	got := rx.Await(t, 2)
	photo, voice := deliveryWithID(t, got, "dlv_1"), deliveryWithID(t, got, "dlv_2")
	if photo.ids() != "3EB0A1" || string(photo.Data.Messages[0].Media) != `{"mime":"image/jpeg","size":48213}` {
		t.Fatalf("the photo delivery has %s with media %s", photo.ids(), photo.Data.Messages[0].Media)
	}
	if voice.ids() != "3EB0A2" || string(voice.Data.Messages[0].Media) != `{"mime":"audio/ogg; codecs=opus","size":9120,"seconds":7,"voice_note":true}` {
		t.Fatalf("the voice delivery has %s with media %s", voice.ids(), voice.Data.Messages[0].Media)
	}
	if voice.Data.Fetch.Arguments["after"] != "3EB0A1" {
		t.Fatalf("the voice delivery fetches %v, want after 3EB0A1", voice.Data.Fetch.Arguments)
	}
	for _, request := range got {
		for _, private := range []string{"Maria", "Alex", "cardápio", "third_party", "name"} {
			if strings.Contains(string(request.Body), private) {
				t.Fatalf("the minimal body %s contains %q", request.Body, private)
			}
		}
	}
}

func TestTheFetchCallReadsTheMessagesOfTheDelivery(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	rx := h.StartReceiver(t)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("antes do webhook")))
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A2", "Maria", morning.Add(time.Minute)), text("depois do webhook")))
	passQuietWindow(h)

	_, body := firstDelivery(t, rx)
	want := map[string]any{"account": "work", "chat": mariaJID.String(), "after": "3EB0A1"}
	if body.Data.Fetch.Tool != "get_messages" || fmt.Sprint(body.Data.Fetch.Arguments) != fmt.Sprint(want) {
		t.Fatalf("fetch %s %v, want get_messages %v", body.Data.Fetch.Tool, body.Data.Fetch.Arguments, want)
	}
	if fetched := page(t, session, body.Data.Fetch.Arguments); fetched.ids() != body.ids() {
		t.Fatalf("the fetch call reads %s, the delivery has %s", fetched.ids(), body.ids())
	}
}

func TestAGroupDeliveryHasTheGroupAndTheSender(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, joaoJID, types.JID{}, "3EB0G1", "João", morning), text("oi, família")))
	passQuietWindow(h)

	got := rx.Await(t, 1)
	data := bodyOf(t, got[0])["data"].(map[string]any)
	assertJSON(t, data["chat"], `{"jid": "120363012345678901@g.us", "kind": "group"}`)
	assertJSON(t, data["messages"], `[{
		"id": "3EB0G1", "kind": "text", "from_me": false, "sent_at": "2026-10-13T09:00:00Z",
		"sender": {"jid": "5541933332222@s.whatsapp.net", "phone": "+5541933332222"}
	}]`)
}

func TestHistoryAndOwnMessagesMakeNoDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, pairingHistory())
	h.WhatsApp.Emit(workPhone, message(ownFromPhone(mariaLID, mariaJID, "3A0B2", morning.Add(time.Minute)), fromPhone(mariaLID, "tudo, e você?")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A3", "Maria", morning.Add(2*time.Minute)), text("e aí?")))
	passQuietWindow(h)

	id, body := firstDelivery(t, rx)
	if id != "dlv_1" || body.ids() != "3EB0A3" {
		t.Fatalf("the first delivery is %s with %s, want dlv_1 with 3EB0A3", id, body.ids())
	}
	if body.Data.Fetch.Arguments["after"] != "3A0B2" {
		t.Fatalf("fetch %v, want after the own message 3A0B2", body.Data.Fetch.Arguments)
	}
}

func TestADuplicateMessageMakesNoSecondDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0D1", "Maria", morning), text("uma vez")))
	passQuietWindow(h)
	rx.Await(t, 1)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0D1", "Maria", morning), text("uma vez")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0D2", "Maria", morning.Add(time.Minute)), text("outra")))
	passQuietWindow(h)

	got := rx.Await(t, 2)
	if first, second := deliveryWithID(t, got, "dlv_1"), deliveryWithID(t, got, "dlv_2"); first.ids() != "3EB0D1" || second.ids() != "3EB0D2" {
		t.Fatalf("the deliveries have %s and %s, want 3EB0D1 and 3EB0D2", first.ids(), second.ids())
	}
}

func TestANewWebhookGetsNoOlderEvent(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("antes do webhook")))
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "3EB0J1", "João", morning.Add(time.Minute)), text("depois do webhook")))
	passQuietWindow(h)

	if id, body := firstDelivery(t, rx); id != "dlv_1" || body.ids() != "3EB0J1" {
		t.Fatalf("the first delivery is %s with %s, want dlv_1 with 3EB0J1", id, body.ids())
	}
}

func TestAWebhookGetsNoEventOfAnAccountOutsideItsList(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(personalPhone, message(direct(mariaJID, "3EB0P1", "Maria", morning), text("no pessoal")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0W1", "Maria", morning), text("no trabalho")))
	passQuietWindow(h)

	if id, body := firstDelivery(t, rx); id != "dlv_1" || body.Data.Account != "work" || body.ids() != "3EB0W1" {
		t.Fatalf("the first delivery is %s for %s with %s, want dlv_1 for work with 3EB0W1", id, body.Data.Account, body.ids())
	}
}

func TestTheEventJournalIsWrittenWithoutAWebhook(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")

	h.WhatsApp.Emit(workPhone, pairingHistory())
	h.WhatsApp.Emit(workPhone, message(ownFromPhone(mariaLID, mariaJID, "3A0B2", morning.Add(time.Minute)), fromPhone(mariaLID, "tudo, e você?")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A3", "Maria", morning.Add(2*time.Minute)), text("e aí?")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A3", "Maria", morning.Add(2*time.Minute)), text("e aí?")))

	if got := journal(t, h); got != "message.received 5531944443333@s.whatsapp.net 3EB0A3" {
		t.Fatalf("the event journal has %q", got)
	}
}

func journal(t *testing.T, h *apptest.Harness) string {
	t.Helper()
	db, err := sql.Open(sqlite.Driver, sqlite.DSN(filepath.Join(h.DataDir, app.AppDBFile)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT kind, chat_jid, message_id FROM events WHERE kind = 'message.received' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var kind, chat, id string
		if err := rows.Scan(&kind, &chat, &id); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, kind+" "+chat+" "+id)
	}
	return strings.Join(lines, "\n")
}

func awaitJournalRead(t *testing.T, h *apptest.Harness) {
	t.Helper()
	db := appDB(t, h)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var behind int
		if err := db.QueryRow(`SELECT count(*) FROM webhooks WHERE cursor < (SELECT coalesce(max(seq), 0) FROM events)`).Scan(&behind); err != nil {
			t.Fatal(err)
		}
		if behind == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d webhooks did not read the whole event journal", behind)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEachDeliveryAttemptIsAudited(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	passQuietWindow(h)

	line := awaitAuditLine(t, h, "webhook_delivery")
	assertFields(t, line, map[string]string{"webhook": "support-bot", "account": "work", "delivery": "dlv_1", "result": "delivered"})
	if line["events"] != float64(1) || line["attempt"] != float64(1) || line["status"] != float64(http.StatusOK) {
		t.Fatalf("audit line %v, want 1 event, attempt 1 and status 200", line)
	}
	for _, private := range []string{"Maria", "oi, tudo bem?"} {
		if strings.Contains(h.AuditRaw(), private) {
			t.Fatalf("the audit log contains %q", private)
		}
	}
}

func awaitAuditLine(t *testing.T, h *apptest.Harness, msg string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if lines := linesWithMessage(h.AuditLines(t), msg); len(lines) > 0 {
			return lines[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s line in the audit log: %s", msg, h.AuditRaw())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAtMostFourPostsRunAtTheSameTime(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	release := make(chan struct{})
	rx := h.StartReceiver(t, apptest.Held(release, http.StatusOK))
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(open)
	addWebhook(t, h, rx, "support-bot", "work")

	for i := range 6 {
		peer := types.NewJID(fmt.Sprintf("55319000000%02d", i), types.DefaultUserServer)
		h.WhatsApp.Emit(workPhone, message(direct(peer, fmt.Sprintf("3EB0C%d", i), "", morning), text("oi")))
	}
	passQuietWindow(h)

	rx.Await(t, 4)
	time.Sleep(200 * time.Millisecond)
	if held := len(rx.Received()); held != 4 {
		t.Fatalf("%d posts run at the same time, want 4", held)
	}
	open()
	rx.Await(t, 6)
}

func TestAWebhookTestWaitsWhileFourPostsRun(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	release := make(chan struct{})
	rx := h.StartReceiver(t, apptest.Held(release, http.StatusOK))
	open := releaseOnce(release)
	t.Cleanup(open)
	addWebhook(t, h, rx, "support-bot", "work")
	for i := range 4 {
		peer := types.NewJID(fmt.Sprintf("55319000000%02d", i), types.DefaultUserServer)
		h.WhatsApp.Emit(workPhone, message(direct(peer, fmt.Sprintf("3EB0C%d", i), "", morning), text("oi")))
	}
	passQuietWindow(h)
	rx.Await(t, 4)

	tested := make(chan apptest.CLIResult, 1)
	go func() { tested <- h.CLI(t, "webhooks", "test", "support-bot") }()
	time.Sleep(200 * time.Millisecond)
	if held := len(rx.Received()); held != 4 {
		t.Fatalf("%d posts run at the same time, want 4", held)
	}
	open()

	if result := <-tested; result.Code != 0 || result.Stdout != "the receiver of the webhook support-bot answered 200 OK\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", result.Code, result.Stdout, result.Stderr)
	}
	if last := rx.Await(t, 5)[4]; !strings.HasPrefix(last.Header.Get("Webhook-Id"), "test_") {
		t.Fatalf("the last request has the webhook-id %q, want the test", last.Header.Get("Webhook-Id"))
	}
}

func TestADeliveryAfterARotationIsSignedWithBothSecrets(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	old := addWebhook(t, h, rx, "support-bot", "work")
	fresh := rotateSecret(t, h, rx, "support-bot")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	passQuietWindow(h)

	requireSigners(t, rx.Await(t, 1), []string{fresh, old}, [][]string{{fresh, old}})
}
