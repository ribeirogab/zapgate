package app_test

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/admin"
	"github.com/ribeirogab/zapgate/internal/apptest"
)

const botURL = "https://bot.example.test/hooks/whatsapp"

func secretFrom(t *testing.T, stdout string) string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if secret, ok := strings.CutPrefix(line, "secret: "); ok {
			return secret
		}
	}
	t.Fatalf("no secret line in %q", stdout)
	return ""
}

func TestWebhooksAddShowsTheSecretOnce(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")

	added := h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work")

	if added.Code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", added.Code, added.Stdout, added.Stderr)
	}
	secret := secretFrom(t, added.Stdout)
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if !strings.HasPrefix(secret, "whsec_") || err != nil || len(key) != 32 {
		t.Fatalf("secret %q is not whsec_ plus the base64 of 32 bytes", secret)
	}
	want := "created the webhook support-bot (accounts: work, events: message.received, content: minimal)\n" +
		"secret: " + secret + "\n" +
		"this is the only time the secret is shown; store it now\n"
	if added.Stdout != want {
		t.Fatalf("stdout %q, want %q", added.Stdout, want)
	}

	listed := h.CLI(t, "webhooks", "list")

	if listed.Code != 0 {
		t.Fatalf("exit %d, stderr %q", listed.Code, listed.Stderr)
	}
	rows := tableRows(t, listed.Stdout)
	if len(rows) != 2 || rows[0] != webhookListHeader || rows[1] != "support-bot "+botURL+" work enabled - 0 0 -" {
		t.Fatalf("webhooks list %q", listed.Stdout)
	}
	if strings.Contains(listed.Stdout, secret) || strings.Contains(listed.Stdout, "whsec_") {
		t.Fatal("webhooks list shows the secret")
	}
}

func TestWebhooksListWithoutWebhooks(t *testing.T) {
	h := apptest.Start(t)

	listed := h.CLI(t, "webhooks", "list")

	if listed.Code != 0 || listed.Stdout != "no webhook yet\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", listed.Code, listed.Stdout, listed.Stderr)
	}
}

func addWebhook(t *testing.T, h *apptest.Harness, rx *apptest.Receiver, name string, accounts string) string {
	t.Helper()
	added := h.CLI(t, "webhooks", "add", name, "--url", rx.URL, "--accounts", accounts)
	if added.Code != 0 {
		t.Fatalf("add the webhook %s: exit %d, stderr %q", name, added.Code, added.Stderr)
	}
	secret := secretFrom(t, added.Stdout)
	rx.Trust(secret)
	return secret
}

func TestWebhooksTestSendsOneSignedPost(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work")

	tested := h.CLI(t, "webhooks", "test", "support-bot")

	if tested.Code != 0 || tested.Stdout != "the receiver of the webhook support-bot answered 200 OK\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", tested.Code, tested.Stdout, tested.Stderr)
	}
	got := rx.Await(t, 1)
	if len(got) != 1 {
		t.Fatalf("the receiver got %d requests, want 1", len(got))
	}
	post := got[0]
	if post.Path != apptest.ReceiverPath {
		t.Fatalf("path %q", post.Path)
	}
	wantHeaders := map[string]string{
		"Content-Type":      "application/json",
		"User-Agent":        "zapgate",
		"Webhook-Timestamp": strconv.FormatInt(apptest.StartTime.Unix(), 10),
	}
	for name, value := range wantHeaders {
		if post.Header.Get(name) != value {
			t.Fatalf("header %s = %q, want %q", name, post.Header.Get(name), value)
		}
	}
	if post.Header.Get("Webhook-Id") == "" || !strings.HasPrefix(post.Header.Get("Webhook-Signature"), "v1,") {
		t.Fatalf("headers %v have no webhook-id or no v1 signature", post.Header)
	}
	if body := string(post.Body); body != `{"type":"webhook.test","timestamp":"2026-10-13T12:00:00Z","data":{"webhook":"support-bot"}}` {
		t.Fatalf("body %s", body)
	}
}

func TestWebhooksTestSendsADifferentIDEachTime(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "all")

	h.CLI(t, "webhooks", "test", "support-bot")
	h.CLI(t, "webhooks", "test", "support-bot")

	got := rx.Await(t, 2)
	if got[0].Header.Get("Webhook-Id") == got[1].Header.Get("Webhook-Id") {
		t.Fatalf("two tests share the webhook-id %q", got[0].Header.Get("Webhook-Id"))
	}
}

func TestWebhooksTestPrintsAFailedStatusWithoutARetry(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t, apptest.Reply(http.StatusServiceUnavailable), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "all")

	tested := h.CLI(t, "webhooks", "test", "support-bot")
	h.Clock.Advance(24 * time.Hour)

	if tested.Code != 1 || tested.Stderr != "zapgate: the receiver of the webhook support-bot answered 503 Service Unavailable\n" || tested.Stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", tested.Code, tested.Stdout, tested.Stderr)
	}
	if got := rx.Await(t, 1); len(got) != 1 {
		t.Fatalf("the receiver got %d requests, want 1", len(got))
	}
}

func TestWebhooksTestDoesNotFollowARedirect(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t, apptest.Redirect(http.StatusFound, "/elsewhere"), apptest.Reply(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "all")

	tested := h.CLI(t, "webhooks", "test", "support-bot")

	if tested.Code != 1 || tested.Stderr != "zapgate: the receiver of the webhook support-bot answered 302 Found\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", tested.Code, tested.Stdout, tested.Stderr)
	}
	if got := rx.Received(); len(got) != 1 || got[0].Path != apptest.ReceiverPath {
		t.Fatalf("the receiver got %d requests, want only the first one", len(got))
	}
}

func TestWebhooksTestStopsReadingALongAnswer(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t, apptest.EndlessBody(http.StatusOK))
	addWebhook(t, h, rx, "support-bot", "all")

	started := time.Now()
	tested := h.CLI(t, "webhooks", "test", "support-bot")

	if tested.Code != 0 || tested.Stdout != "the receiver of the webhook support-bot answered 200 OK\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", tested.Code, tested.Stdout, tested.Stderr)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the test took %s on an endless answer", elapsed)
	}
}

func TestWebhooksTestPrintsTheErrorWhenTheReceiverIsDown(t *testing.T) {
	h := apptest.Start(t)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	downURL := "http://" + closed.Addr().String() + "/hooks/whatsapp"
	closed.Close()
	h.CLI(t, "webhooks", "add", "support-bot", "--url", downURL, "--accounts", "all")

	tested := h.CLI(t, "webhooks", "test", "support-bot")

	if tested.Code != 1 || !strings.HasPrefix(tested.Stderr, "zapgate: the webhook support-bot did not reach its receiver: ") || !strings.Contains(tested.Stderr, "connection refused") {
		t.Fatalf("exit %d, stdout %q, stderr %q", tested.Code, tested.Stdout, tested.Stderr)
	}
}

func TestWebhooksTestOfAnUnknownWebhook(t *testing.T) {
	h := apptest.Start(t)

	tested := h.CLI(t, "webhooks", "test", "ghost")

	if tested.Code != 1 || tested.Stderr != "zapgate: there is no webhook named \"ghost\"\n" {
		t.Fatalf("exit %d, stderr %q", tested.Code, tested.Stderr)
	}
}

func ownerGet(t *testing.T, h *apptest.Harness, path string) string {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", h.AdminSocket)
		},
	}}
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://owner" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return apptest.ReadBody(t, resp)
}

func TestOnlyTheCreateRouteReturnsTheSecret(t *testing.T) {
	h := apptest.Start(t)

	created, err := h.Owner.AddWebhook(context.Background(), admin.AddWebhookRequest{Name: "support-bot", URL: botURL, AllExistingAccounts: true})

	if err != nil || !strings.HasPrefix(created.Secret, "whsec_") || created.Webhook.Name != "support-bot" {
		t.Fatalf("created %+v, error %v", created, err)
	}
	listed := ownerGet(t, h, "/webhooks")
	if !strings.Contains(listed, `"name":"support-bot"`) || strings.Contains(listed, created.Secret) || strings.Contains(listed, "whsec_") {
		t.Fatalf("GET /webhooks %s", listed)
	}
}

func TestWebhookCommandsAreAudited(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	secret := addWebhook(t, h, rx, "support-bot", "all")
	h.CLI(t, "webhooks", "test", "support-bot")
	h.CLI(t, "webhooks", "remove", "support-bot")

	actions := linesWithMessage(h.AuditLines(t), "admin")
	want := []map[string]string{
		{"action": "webhook_add", "webhook": "support-bot"},
		{"action": "webhook_test", "webhook": "support-bot", "result": "ok"},
		{"action": "webhook_remove", "webhook": "support-bot"},
	}
	if len(actions) != len(want) {
		t.Fatalf("audit has %d admin lines, want %d: %s", len(actions), len(want), h.AuditRaw())
	}
	for i, fields := range want {
		assertFields(t, actions[i], fields)
	}
	if actions[1]["status"] != float64(http.StatusOK) {
		t.Fatalf("audit line %v has no status 200", actions[1])
	}
	if strings.Contains(h.AuditRaw(), secret) || strings.Contains(h.AuditRaw(), "whsec_") {
		t.Fatal("the audit log contains the secret")
	}
}

func TestWebhooksUsageErrors(t *testing.T) {
	cases := [][]string{
		{"webhooks"},
		{"webhooks", "unknown"},
		{"webhooks", "add", "support-bot"},
		{"webhooks", "add", "support-bot", "--url", botURL},
		{"webhooks", "add", "support-bot", "--accounts", "all"},
		{"webhooks", "add", "--url", botURL, "--accounts", "all"},
		{"webhooks", "add", "support-bot", "--url", "--accounts", "all"},
		{"webhooks", "add", "support-bot", "other", "--url", botURL, "--accounts", "all"},
		{"webhooks", "add", "support-bot", "--url", botURL, "--accounts", "all", "--force"},
		{"webhooks", "list", "support-bot"},
		{"webhooks", "remove"},
		{"webhooks", "remove", "support-bot", "sales-bot"},
		{"webhooks", "test"},
		{"webhooks", "test", "support-bot", "sales-bot"},
		{"webhooks", "rotate-secret"},
		{"webhooks", "rotate-secret", "support-bot", "sales-bot"},
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

func TestWebhooksAddRejectsBadRequests(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		stderr string
	}{
		{
			name:   "name in use",
			args:   []string{"webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work"},
			stderr: "zapgate: the webhook name \"support-bot\" is already in use\n",
		},
		{
			name:   "invalid name",
			args:   []string{"webhooks", "add", "Support_Bot", "--url", botURL, "--accounts", "work"},
			stderr: "zapgate: the webhook name \"Support_Bot\" is not valid: use lowercase letters, digits and single hyphens, at most 64 characters\n",
		},
		{
			name:   "name too long",
			args:   []string{"webhooks", "add", strings.Repeat("a", 65), "--url", botURL, "--accounts", "work"},
			stderr: "is not valid",
		},
		{
			name:   "unknown account",
			args:   []string{"webhooks", "add", "sales-bot", "--url", botURL, "--accounts", "work,sales"},
			stderr: "zapgate: there is no account named \"sales\"\n",
		},
		{
			name:   "empty account list",
			args:   []string{"webhooks", "add", "sales-bot", "--url", botURL, "--accounts", ","},
			stderr: "zapgate: the webhook needs a list of accounts, or all existing accounts\n",
		},
		{
			name:   "url without http or https",
			args:   []string{"webhooks", "add", "sales-bot", "--url", "ftp://bot.example.test/hook", "--accounts", "work"},
			stderr: "zapgate: the webhook URL \"ftp://bot.example.test/hook\" is not valid: use an http or https URL with a host\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work")

			result := h.CLI(t, tc.args...)

			if result.Code != 1 || !strings.Contains(result.Stderr, tc.stderr) {
				t.Fatalf("exit %d, stderr %q, want %q", result.Code, result.Stderr, tc.stderr)
			}
			if result.Stdout != "" {
				t.Fatalf("stdout %q, want nothing", result.Stdout)
			}
			if rows := tableRows(t, h.CLI(t, "webhooks", "list").Stdout); len(rows) != 2 {
				t.Fatalf("a refused add changed the webhooks: %q", rows)
			}
		})
	}
}

func TestWebhooksAllAccountsIsASnapshotOfTheExistingAccounts(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")

	added := h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "all")
	h.AddAccount(t, "sales", "5531966665555", "Loja")

	if !strings.HasPrefix(added.Stdout, "created the webhook support-bot (accounts: personal,work, events: message.received, content: minimal)\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", added.Code, added.Stdout, added.Stderr)
	}
	if rows := tableRows(t, h.CLI(t, "webhooks", "list").Stdout); rows[1] != "support-bot "+botURL+" personal,work enabled - 0 0 -" {
		t.Fatalf("webhooks list %q", rows)
	}
}

func TestWebhooksAllAccountsWithoutAccounts(t *testing.T) {
	h := apptest.Start(t)

	added := h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "all")

	if !strings.HasPrefix(added.Stdout, "created the webhook support-bot (accounts: none, events: message.received, content: minimal)\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", added.Code, added.Stdout, added.Stderr)
	}
	if rows := tableRows(t, h.CLI(t, "webhooks", "list").Stdout); rows[1] != "support-bot "+botURL+" - enabled - 0 0 -" {
		t.Fatalf("webhooks list %q", rows)
	}
}

func TestWebhooksRemoveDeletesTheWebhook(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work")
	h.CLI(t, "webhooks", "add", "sales-bot", "--url", botURL, "--accounts", "work")

	removed := h.CLI(t, "webhooks", "remove", "support-bot")

	if removed.Code != 0 || removed.Stdout != "removed the webhook support-bot\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", removed.Code, removed.Stdout, removed.Stderr)
	}
	if rows := tableRows(t, h.CLI(t, "webhooks", "list").Stdout); len(rows) != 2 || !strings.HasPrefix(rows[1], "sales-bot ") {
		t.Fatalf("webhooks list %q", rows)
	}
	again := h.CLI(t, "webhooks", "remove", "support-bot")
	if again.Code != 1 || again.Stderr != "zapgate: there is no webhook named \"support-bot\"\n" {
		t.Fatalf("exit %d, stderr %q", again.Code, again.Stderr)
	}
}

func TestRemovingAnAccountRemovesItFromEveryWebhook(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work,personal")
	h.CLI(t, "webhooks", "add", "sales-bot", "--url", botURL, "--accounts", "work")

	if result := h.CLI(t, "accounts", "remove", "work", "--yes"); result.Code != 0 {
		t.Fatalf("remove the account: exit %d, stderr %q", result.Code, result.Stderr)
	}

	rows := tableRows(t, h.CLI(t, "webhooks", "list").Stdout)
	want := []string{webhookListHeader, "sales-bot " + botURL + " - enabled - 0 0 -", "support-bot " + botURL + " personal enabled - 0 0 -"}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Fatalf("webhooks list %q, want %q", rows, want)
	}
}
