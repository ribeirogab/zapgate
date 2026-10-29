package app_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestWebhooksAddRefusesHTTPToAPublicAddress(t *testing.T) {
	cases := map[string]string{
		"http://203.0.113.10/hook":          "zapgate: the webhook URL \"http://203.0.113.10/hook\" is not valid: 203.0.113.10 is not a loopback, private or CGNAT address, so the URL must use https\n",
		"http://[2001:db8::1]:8080/hook":    "is not valid: 2001:db8::1 is not a loopback, private or CGNAT address",
		"http://169.254.169.254/latest":     "is not valid: 169.254.169.254 is not a loopback, private or CGNAT address",
		"http://100.128.0.1/hook":           "is not valid: 100.128.0.1 is not a loopback, private or CGNAT address",
		"http://0.0.0.0:8080/hook":          "is not valid: 0.0.0.0 is not a loopback, private or CGNAT address",
		"http://[::ffff:203.0.113.10]/hook": "is not valid: ::ffff:203.0.113.10 (203.0.113.10) is not a loopback, private or CGNAT address",
		"http://:8080/hook":                 "zapgate: the webhook URL \"http://:8080/hook\" is not valid: use an http or https URL with a host\n",
		"bot:8080/hook":                     "is not valid: use an http or https URL with a host",
	}
	for webhookURL, stderr := range cases {
		t.Run(webhookURL, func(t *testing.T) {
			h := apptest.Start(t)

			added := h.CLI(t, "webhooks", "add", "support-bot", "--url", webhookURL, "--accounts", "all")

			if added.Code != 1 || !strings.Contains(added.Stderr, stderr) || added.Stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q, want %q", added.Code, added.Stdout, added.Stderr, stderr)
			}
			if listed := h.CLI(t, "webhooks", "list"); listed.Stdout != "no webhook yet\n" {
				t.Fatalf("a refused add created a webhook: %q", listed.Stdout)
			}
		})
	}
}

func TestWebhooksAddTakesHTTPOnlyInsideAPrivateNetworkAndHTTPSAnywhere(t *testing.T) {
	urls := []string{
		"http://127.0.0.1:8080/hook",
		"http://[::1]:8080/hook",
		"http://10.1.2.3/hook",
		"http://172.16.0.9/hook",
		"http://192.168.1.20:5678/webhook/whatsapp",
		"http://[fd7a:115c:a1e0::1]/hook",
		"http://100.64.0.1/hook",
		"http://100.101.102.103/hook",
		"https://203.0.113.10/hook",
		"https://10.1.2.3/hook",
		"https://bot.example.test/hook",
	}
	h := apptest.Start(t)

	for i, webhookURL := range urls {
		added := h.CLI(t, "webhooks", "add", "bot-"+strconv.Itoa(i), "--url", webhookURL, "--accounts", "all")

		if added.Code != 0 {
			t.Errorf("%s: exit %d, stderr %q", webhookURL, added.Code, added.Stderr)
		}
	}
}

func TestWebhooksAddResolvesTheHostOfAnHTTPURL(t *testing.T) {
	cases := []struct {
		host   string
		addrs  []string
		code   int
		stderr string
	}{
		{host: "bot", addrs: []string{"172.18.0.4"}},
		{host: "n8n.tail1e81ae.ts.net", addrs: []string{"100.88.1.2", "fd7a:115c:a1e0::2"}},
		{host: "bot", addrs: []string{"172.18.0.4", "203.0.113.10"}, code: 1, stderr: "zapgate: the webhook URL \"http://bot:8080/hook\" is not valid: bot (203.0.113.10) is not a loopback, private or CGNAT address, so the URL must use https\n"},
		{host: "bot", code: 1, stderr: "zapgate: the webhook URL \"http://bot:8080/hook\" is not valid: the host bot does not resolve\n"},
	}
	for _, tc := range cases {
		t.Run(tc.host+" "+strings.Join(tc.addrs, ","), func(t *testing.T) {
			h := apptest.Start(t)
			h.Hosts.Point(tc.host, tc.addrs...)
			webhookURL := "http://" + tc.host + ":8080/hook"

			added := h.CLI(t, "webhooks", "add", "support-bot", "--url", webhookURL, "--accounts", "all")

			if added.Code != tc.code || added.Code != 0 && added.Stderr != tc.stderr {
				t.Fatalf("exit %d, stdout %q, stderr %q, want exit %d and %q", added.Code, added.Stdout, added.Stderr, tc.code, tc.stderr)
			}
		})
	}
}

func TestWebhooksTestRefusesAnHTTPHostThatNowResolvesToAPublicAddress(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	h.Hosts.Point("bot", "127.0.0.1")
	webhookURL := strings.Replace(rx.URL, "127.0.0.1", "bot", 1)
	added := h.CLI(t, "webhooks", "add", "support-bot", "--url", webhookURL, "--accounts", "all")
	rx.Trust(secretFrom(t, added.Stdout))
	first := h.CLI(t, "webhooks", "test", "support-bot")
	h.Hosts.Point("bot", "203.0.113.10")

	second := h.CLI(t, "webhooks", "test", "support-bot")

	if first.Code != 0 || first.Stdout != "the receiver of the webhook support-bot answered 200 OK\n" {
		t.Fatalf("first test: exit %d, stdout %q, stderr %q", first.Code, first.Stdout, first.Stderr)
	}
	want := "zapgate: the webhook support-bot did not reach its receiver: bot (203.0.113.10) is not a loopback, private or CGNAT address, so the URL must use https\n"
	if second.Code != 1 || second.Stderr != want || second.Stdout != "" {
		t.Fatalf("second test: exit %d, stdout %q, stderr %q, want %q", second.Code, second.Stdout, second.Stderr, want)
	}
	if got := rx.Await(t, 1); len(got) != 1 {
		t.Fatalf("the receiver got %d requests, want only the first one", len(got))
	}
	tests := linesWithMessage(h.AuditLines(t), "admin")
	if len(tests) != 3 {
		t.Fatalf("audit has %d admin lines, want 3: %s", len(tests), h.AuditRaw())
	}
	assertFields(t, tests[1], map[string]string{"action": "webhook_test", "webhook": "support-bot", "result": "ok"})
	assertFields(t, tests[2], map[string]string{"action": "webhook_test", "webhook": "support-bot", "result": "blocked"})
}
