package app_test

import (
	"regexp"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

var tableGap = regexp.MustCompile(`\s{2,}`)

func listedWebhook(t *testing.T, h *apptest.Harness, name string) map[string]string {
	t.Helper()
	listed := h.CLI(t, "webhooks", "list")
	if listed.Code != 0 {
		t.Fatalf("webhooks list: exit %d, stderr %q", listed.Code, listed.Stderr)
	}
	lines := strings.Split(strings.TrimSpace(listed.Stdout), "\n")
	header := tableGap.Split(lines[0], -1)
	for _, line := range lines[1:] {
		cells := tableGap.Split(line, -1)
		if cells[0] != name {
			continue
		}
		row := map[string]string{}
		for i, column := range header {
			row[column] = cells[i]
		}
		return row
	}
	t.Fatalf("webhooks list has no webhook %s: %q", name, listed.Stdout)
	return nil
}

func TestWebhooksSetChangesTheChatsForTheNextDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, joaoJID, types.JID{}, "3EB0G1", "João", morning), text("oi, família")))
	rx.Await(t, 1)

	set := h.CLI(t, "webhooks", "set", "support-bot", "--chats", "direct")
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, joaoJID, types.JID{}, "3EB0G2", "João", morning), text("de novo")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("oi")))

	if set.Code != 0 || set.Stdout != "changed the webhook support-bot (accounts: work, chats: direct, events: message.received, content: minimal)\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
	}
	got := rx.Await(t, 2)
	if id := got[1].Header.Get("Webhook-Id"); id != "dlv_2" || deliveryWithID(t, got, id).ids() != "3EB0M1" {
		t.Fatalf("the second delivery is %s with %s, want dlv_2 with 3EB0M1", id, deliveryWithID(t, got, id).ids())
	}
}

func TestWebhooksSetKeepsTheOptionsItDoesNotGet(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	old := h.StartReceiver(t)
	rx := h.StartReceiver(t)
	secret := addWebhook(t, h, old, "support-bot", "work", "--chats", "groups", "--events", "account.state,message.received")
	rx.Trust(secret)

	set := h.CLI(t, "webhooks", "set", "support-bot", "--url", rx.URL)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("oi")))
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, joaoJID, types.JID{}, "3EB0G1", "João", morning), text("oi, família")))

	if set.Code != 0 || set.Stdout != "changed the webhook support-bot (accounts: work, chats: groups, events: message.received,account.state, content: minimal)\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
	}
	if id, body := firstDelivery(t, rx); id != "dlv_1" || body.ids() != "3EB0G1" {
		t.Fatalf("the first delivery is %s with %s, want dlv_1 with 3EB0G1", id, body.ids())
	}
	if received := old.Received(); len(received) != 0 {
		t.Fatalf("the old URL got %d deliveries", len(received))
	}
	row := listedWebhook(t, h, "support-bot")
	if row["URL"] != rx.URL || row["ACCOUNTS"] != "work" || row["CHATS"] != "groups" || row["EVENTS"] != "message.received,account.state" {
		t.Fatalf("webhooks list %v", row)
	}
}

func TestWebhooksSetChangesTheContentLevelForTheNextDelivery(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	set := h.CLI(t, "webhooks", "set", "support-bot", "--content", "full")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("oi, tudo bem?")))

	if set.Code != 0 || set.Stdout != "changed the webhook support-bot (accounts: work, chats: all, events: message.received, content: full)\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
	}
	if body := string(rx.Await(t, 1)[0].Body); !strings.Contains(body, "oi, tudo bem?") {
		t.Fatalf("the delivery after set --content full has no text: %s", body)
	}
	if row := listedWebhook(t, h, "support-bot"); row["CONTENT"] != "full" {
		t.Fatalf("webhooks list %v", row)
	}
}

func TestWebhooksSetMovesTheListedChatsToTheNewAccounts(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work", "--chats", "+"+mariaPhone)

	set := h.CLI(t, "webhooks", "set", "support-bot", "--accounts", "personal")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0W1", "Maria", morning), text("no trabalho")))
	h.WhatsApp.Emit(personalPhone, message(direct(joaoJID, "3EB0J1", "João", morning), text("outro chat")))
	h.WhatsApp.Emit(personalPhone, message(direct(mariaJID, "3EB0P1", "Maria", morning), text("no pessoal")))

	if set.Code != 0 || set.Stdout != "changed the webhook support-bot (accounts: personal, chats: "+mariaJID.String()+", events: message.received, content: minimal)\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
	}
	if id, body := firstDelivery(t, rx); id != "dlv_1" || body.Data.Account != "personal" || body.ids() != "3EB0P1" {
		t.Fatalf("the first delivery is %s for %s with %s, want dlv_1 for personal with 3EB0P1", id, body.Data.Account, body.ids())
	}
}

func TestWebhooksListShowsTheChatsAndTheEvents(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.CLI(t, "webhooks", "add", "all-bot", "--url", botURL, "--accounts", "work")
	h.CLI(t, "webhooks", "add", "list-bot", "--url", botURL, "--accounts", "work", "--chats", familyJID.String()+",+"+mariaPhone, "--events", "account.state")

	all, listed := listedWebhook(t, h, "all-bot"), listedWebhook(t, h, "list-bot")

	if all["CHATS"] != "all" || all["EVENTS"] != "message.received" {
		t.Fatalf("all-bot %v", all)
	}
	if listed["CHATS"] != familyJID.String()+","+mariaJID.String() || listed["EVENTS"] != "account.state" {
		t.Fatalf("list-bot %v", listed)
	}
}

func TestWebhooksSetRejectsBadRequests(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		stderr string
	}{
		{name: "unknown webhook", args: []string{"webhooks", "set", "ghost", "--chats", "direct"}, stderr: "zapgate: there is no webhook named \"ghost\"\n"},
		{name: "unknown account", args: []string{"webhooks", "set", "support-bot", "--accounts", "work,sales"}, stderr: "zapgate: there is no account named \"sales\"\n"},
		{name: "empty account list", args: []string{"webhooks", "set", "support-bot", "--accounts", ","}, stderr: "zapgate: the webhook needs a list of accounts, or all existing accounts\n"},
		{name: "unknown event", args: []string{"webhooks", "set", "support-bot", "--events", "message.sent"}, stderr: "zapgate: the event \"message.sent\" is not known: use message.received or account.state\n"},
		{name: "unknown content level", args: []string{"webhooks", "set", "support-bot", "--content", "everything"}, stderr: "zapgate: the content level \"everything\" is not valid: use minimal or full\n"},
		{name: "bad chat", args: []string{"webhooks", "set", "support-bot", "--chats", "nobody"}, stderr: "zapgate: the chat \"nobody\" is not valid: use all, direct, groups, or a comma-separated list of phone numbers with the country code and JIDs\n"},
		{name: "bad url", args: []string{"webhooks", "set", "support-bot", "--url", "ftp://bot.example.test/hook"}, stderr: "zapgate: the webhook URL \"ftp://bot.example.test/hook\" is not valid: use an http or https URL with a host\n"},
		{name: "public http url", args: []string{"webhooks", "set", "support-bot", "--url", "http://bot.example.test/hook"}, stderr: "zapgate: the webhook URL \"http://bot.example.test/hook\" is not valid: the host bot.example.test does not resolve\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work", "--chats", "groups")
			before := h.CLI(t, "webhooks", "list").Stdout

			result := h.CLI(t, tc.args...)

			if result.Code != 1 || result.Stderr != tc.stderr || result.Stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q, want %q", result.Code, result.Stdout, result.Stderr, tc.stderr)
			}
			if after := h.CLI(t, "webhooks", "list").Stdout; after != before {
				t.Fatalf("a refused set changed the webhook: %q, want %q", after, before)
			}
		})
	}
}

func TestWebhooksSetUsageErrors(t *testing.T) {
	cases := [][]string{
		{"webhooks", "set"},
		{"webhooks", "set", "support-bot"},
		{"webhooks", "set", "--chats", "direct"},
		{"webhooks", "set", "support-bot", "sales-bot", "--chats", "direct"},
		{"webhooks", "set", "support-bot", "--chats"},
		{"webhooks", "set", "support-bot", "--force"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := apptest.Start(t)

			result := h.CLI(t, args...)

			if result.Code != 2 || !strings.HasPrefix(result.Stderr, "usage:\n") {
				t.Fatalf("exit %d, stderr %q", result.Code, result.Stderr)
			}
		})
	}
}

func TestWebhooksSetIsAudited(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work")

	h.CLI(t, "webhooks", "set", "support-bot", "--chats", "+"+mariaPhone)

	actions := linesWithMessage(h.AuditLines(t), "admin")
	assertFields(t, actions[len(actions)-1], map[string]string{"action": "webhook_set", "webhook": "support-bot"})
}
