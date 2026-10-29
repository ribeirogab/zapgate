package app_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/admin"
	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/config"
)

const retentionWait = 10 * time.Second

var sixMonthsBeforeTheNextPass = time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

func ownerStatus(t *testing.T, h *apptest.Harness) admin.StatusView {
	t.Helper()
	status, err := h.Owner.Status(context.Background())
	if err != nil {
		t.Fatalf("owner status: %v", err)
	}
	return status
}

func awaitRetentionPass(t *testing.T, h *apptest.Harness, startedAt time.Time) admin.RetentionView {
	t.Helper()
	want := startedAt.UTC().Format(time.RFC3339)
	deadline := time.Now().Add(retentionWait)
	for {
		pass := ownerStatus(t, h).Retention
		if pass.StartedAt == want && !pass.Running {
			return pass
		}
		if time.Now().After(deadline) {
			t.Fatalf("no finished retention pass started at %s; last pass %+v", want, pass)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runDailyRetention(t *testing.T, h *apptest.Harness) admin.RetentionView {
	t.Helper()
	awaitRetentionPass(t, h, h.Clock.Now())
	h.Clock.Advance(24 * time.Hour)
	return awaitRetentionPass(t, h, h.Clock.Now())
}

func TestTheDailyPassDeletesMessagesOlderThanSixMonthsAndKeepsTheChats(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	old := sixMonthsBeforeTheNextPass.Add(-time.Minute)
	recent := sixMonthsBeforeTheNextPass.Add(time.Minute)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "OLD1", "Maria", old), text("contrato antigo")))
	h.WhatsApp.Emit(workPhone, message(reactionInfo(direct(mariaJID, "R1", "Maria", old)), reaction(mariaJID, "OLD1", false, "👍", old)))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "OLD2", "Maria", old.Add(-time.Second)), pollMessage()))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "KEEP1", "Maria", recent), text("contrato novo")))
	h.WhatsApp.Emit(workPhone, message(direct(joaoJID, "J1", "João", old.AddDate(0, -1, 0)), text("contrato velho")))

	pass := runDailyRetention(t, h)

	if pass.DeletedMessages != 3 || pass.DeletedFiles != 0 {
		t.Fatalf("the pass %+v", pass)
	}
	if got := page(t, session, map[string]any{"chat": mariaJID.String()}).ids(); got != "KEEP1" {
		t.Fatalf("Maria's chat has %q", got)
	}
	if got := page(t, session, map[string]any{"chat": joaoJID.String()}).ids(); got != "" {
		t.Fatalf("João's chat has %q", got)
	}
	if got := searchMessages(t, session, map[string]any{"query": "contrato"}).ids(); got != "KEEP1" {
		t.Fatalf("search_messages found %q", got)
	}
	chats := structured(t, listChats(t, session, nil))
	if !strings.Contains(chats, `"jid":"5531944443333@s.whatsapp.net"`) || !strings.Contains(chats, `"jid":"5541933332222@s.whatsapp.net"`) {
		t.Fatalf("list_chats %s", chats)
	}
	if sent, receipts := h.WhatsApp.Sent(workPhone), h.WhatsApp.Receipts(workPhone); len(sent) != 0 || len(receipts) != 0 {
		t.Fatalf("WhatsApp got %d messages and %d receipts", len(sent), len(receipts))
	}
}

func TestTheDailyPassDeletesTheFilesOfOldMedia(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	oldPhoto := newCDNFile("old", []byte("foto antiga")).publish(h)
	newPhoto := newCDNFile("new", []byte("foto nova")).publish(h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "OLDP", "Maria", sixMonthsBeforeTheNextPass.Add(-time.Hour)), oldPhoto.image("velha")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "NEWP", "Maria", sixMonthsBeforeTheNextPass.Add(time.Hour)), newPhoto.image("nova")))
	awaitMediaFiles(t, h, 2)

	pass := runDailyRetention(t, h)

	if pass.DeletedMessages != 1 || pass.DeletedFiles != 1 {
		t.Fatalf("the pass %+v", pass)
	}
	files := mediaFiles(t, h)
	if len(files) != 1 {
		t.Fatalf("the media dir has %v", keysOf(files))
	}
	for name, content := range files {
		if string(content) != "foto nova" {
			t.Fatalf("the media dir kept %s with %q", name, content)
		}
	}
	if result := downloadMedia(t, session, "OLDP"); !result.IsError || !strings.Contains(errorText(result), `has no stored message "OLDP"`) {
		t.Fatalf("download_media of the deleted photo: %s", errorText(result))
	}
	if result := downloadMedia(t, session, "NEWP"); result.IsError {
		t.Fatalf("download_media of the kept photo: %s", errorText(result))
	}
	if sent := h.WhatsApp.Sent(workPhone); len(sent) != 0 {
		t.Fatalf("WhatsApp got %d messages", len(sent))
	}
}

func TestAnAccountKeepsItsOwnRetention(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "bot", []string{"work", "personal"}, true))
	fourMonthsOld := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "W1", "Maria", fourMonthsOld), text("pedido de junho")))
	h.WhatsApp.Emit(personalPhone, message(direct(mariaJID, "P1", "Maria", fourMonthsOld), text("foto de junho")))

	changed := h.CLI(t, "accounts", "retention", "work", "3")

	if changed.Code != 0 || changed.Stdout != "the account work keeps 3 months of messages; the next daily pass deletes the older ones\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", changed.Code, changed.Stdout, changed.Stderr)
	}
	if pass := runDailyRetention(t, h); pass.DeletedMessages != 1 {
		t.Fatalf("the pass %+v", pass)
	}
	if got := page(t, session, map[string]any{"chat": mariaJID.String()}).ids(); got != "" {
		t.Fatalf("work still has %q", got)
	}
	if got := page(t, session, map[string]any{"account": "personal", "chat": mariaJID.String()}).ids(); got != "P1" {
		t.Fatalf("personal has %q", got)
	}
	assertFields(t, linesWithMessage(h.AuditLines(t), "admin")[3], map[string]string{"action": "account_retention", "account": "work", "retention": "3"})
}

func TestTheDefaultRetentionComesBackForAnAccount(t *testing.T) {
	h := apptest.Start(t, apptest.WithConfig(func(cfg *config.Config) { cfg.RetentionMonths = 12 }))
	h.AddAccount(t, "work", workPhone, "Alex")
	h.CLI(t, "accounts", "retention", "work", "1")

	reset := h.CLI(t, "accounts", "retention", "work", "default")

	if reset.Code != 0 || reset.Stdout != "the account work keeps the default of 12 months of messages; the next daily pass deletes the older ones\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", reset.Code, reset.Stdout, reset.Stderr)
	}
	account, err := h.Owner.Account(context.Background(), "work")
	if err != nil || account.RetentionMonths != 12 || !account.RetentionDefault {
		t.Fatalf("account %+v, error %v", account, err)
	}
	assertFields(t, linesWithMessage(h.AuditLines(t), "admin")[2], map[string]string{"action": "account_retention", "account": "work", "retention": "default"})
}

func TestAccountsRetentionRefusesBadValues(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	cases := map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"unknown account": {[]string{"ghost", "3"}, 1, "zapgate: there is no account named \"ghost\"\n"},
		"too long":        {[]string{"work", "1201"}, 1, "zapgate: the retention must be from 1 to 1200 months, or the default\n"},
		"zero":            {[]string{"work", "0"}, 2, "usage:\n"},
		"not a number":    {[]string{"work", "six"}, 2, "usage:\n"},
		"no value":        {[]string{"work"}, 2, "usage:\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := h.CLI(t, append([]string{"accounts", "retention"}, tc.args...)...)

			if result.Code != tc.code || !strings.HasPrefix(result.Stderr, tc.stderr) {
				t.Fatalf("exit %d, stderr %q", result.Code, result.Stderr)
			}
		})
	}
	if account, _ := h.Owner.Account(context.Background(), "work"); account.RetentionMonths != 6 || !account.RetentionDefault {
		t.Fatalf("account %+v", account)
	}
}

func TestStatusShowsTheDiskUseAndTheRetentionOfEachAccount(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	h.CLI(t, "accounts", "retention", "personal", "1")
	photo := newCDNFile("scan", bytes.Repeat([]byte("x"), 2500)).publish(h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "OLD", "Maria", sixMonthsBeforeTheNextPass.Add(-time.Hour)), text("antiga")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "W1", "Maria", morning), photo.image("nota")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "W2", "Maria", morning), text("recebeu?")))
	awaitMediaFiles(t, h, 1)
	runDailyRetention(t, h)

	status := h.CLI(t, "status")

	want := []string{
		"NAME PHONE STATE MESSAGES MEDIA RETENTION DESCRIPTION",
		"personal +5521977776666 connected 0 0 B 1 month",
		"work +5511988887777 connected 2 2.5 kB 6 months (default)",
		"retention: the last pass ran at 2026-10-14T12:00:00Z and deleted 1 message and 0 media files; the next pass runs a day after it",
	}
	if got := tableRows(t, status.Stdout); status.Code != 0 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("exit %d, stdout:\n%s", status.Code, status.Stdout)
	}
}

func TestNewMessagesAreWrittenBetweenTheBatchesOfAPass(t *testing.T) {
	const old = 3000
	h := apptest.Start(t)
	session := workSession(t, h)
	for i := range old {
		at := sixMonthsBeforeTheNextPass.Add(-time.Duration(i+1) * time.Second)
		h.WhatsApp.Emit(workPhone, message(direct(mariaJID, fmt.Sprintf("OLD%04d", i), "Maria", at), text("antiga")))
	}
	awaitRetentionPass(t, h, h.Clock.Now())

	h.Clock.Advance(24 * time.Hour)
	written := writeDuringThePass(t, h, old)
	pass := awaitRetentionPass(t, h, h.Clock.Now())

	if pass.DeletedMessages != old {
		t.Fatalf("the pass %+v", pass)
	}
	if got := page(t, session, map[string]any{"chat": mariaJID.String()}).ids(); got != written {
		t.Fatalf("Maria's chat has %q, want %q", got, written)
	}
}

func writeDuringThePass(t *testing.T, h *apptest.Harness, total int) string {
	t.Helper()
	deadline := time.Now().Add(retentionWait)
	for i := 0; time.Now().Before(deadline); i++ {
		before := ownerStatus(t, h).Retention
		if !before.Running {
			if before.StartedAt == h.Clock.Now().UTC().Format(time.RFC3339) {
				t.Fatalf("the pass ended before a new message was written between two batches: %+v", before)
			}
			continue
		}
		if before.DeletedMessages == 0 || before.DeletedMessages == total {
			continue
		}
		id := fmt.Sprintf("NEW%d", i)
		h.WhatsApp.Emit(workPhone, message(direct(mariaJID, id, "Maria", morning.Add(24*time.Hour)), text("nova")))
		after := ownerStatus(t, h).Retention
		if after.Running {
			return id
		}
		t.Fatalf("the new message waited for the whole pass: before %+v, after %+v", before, after)
	}
	t.Fatal("the pass did not start")
	return ""
}
