package app_test

import (
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const tenantToken = "tenant-token-acme-42"

func requireHeaders(t *testing.T, request apptest.Received, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got := request.Header.Get(name); got != value {
			t.Fatalf("header %s = %q, want %q", name, got, value)
		}
	}
}

func TestExtraHeadersGoOutWithEveryPost(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work", "--header", "Authorization: Bearer "+tenantToken, "--header=x-tenant:acme")

	h.CLI(t, "webhooks", "test", "support-bot")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))
	got := rx.Await(t, 2)

	for _, request := range got {
		requireHeaders(t, request, map[string]string{"Authorization": "Bearer " + tenantToken, "X-Tenant": "acme", "Anthropic-Version": ""})
	}
}

func TestWebhooksListShowsTheHeaderNamesButNeverTheValues(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work", "--header", "Authorization: Bearer "+tenantToken, "--header", "X-Tenant: acme-tenant")
	h.CLI(t, "webhooks", "add", "plain-bot", "--url", botURL, "--accounts", "work")

	listed := h.CLI(t, "webhooks", "list")

	if row := listedWebhook(t, h, "support-bot"); row["HEADERS"] != "Authorization,X-Tenant" {
		t.Fatalf("webhooks list %v, want the header names", row)
	}
	if row := listedWebhook(t, h, "plain-bot"); row["HEADERS"] != "-" {
		t.Fatalf("webhooks list %v, want no header", row)
	}
	for place, text := range map[string]string{"webhooks list": listed.Stdout, "GET /webhooks": ownerGet(t, h, "/webhooks"), "the audit log": h.AuditRaw()} {
		if strings.Contains(text, tenantToken) || strings.Contains(text, "acme-tenant") {
			t.Fatalf("%s shows a header value: %s", place, text)
		}
	}
}

func TestWebhooksSetReplacesAllExtraHeaders(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work", "--header", "Authorization: Bearer "+tenantToken, "--header", "X-Tenant: acme")

	set := h.CLI(t, "webhooks", "set", "support-bot", "--header", "X-Region: sa-east")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))

	if set.Code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
	}
	requireHeaders(t, rx.Await(t, 1)[0], map[string]string{"X-Region": "sa-east", "Authorization": "", "X-Tenant": ""})
	if row := listedWebhook(t, h, "support-bot"); row["HEADERS"] != "X-Region" {
		t.Fatalf("webhooks list %v", row)
	}
}

func TestWebhooksSetKeepsTheHeadersWhenItGetsNoHeader(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "support-bot", "work", "--header", "X-Tenant: acme")

	h.CLI(t, "webhooks", "set", "support-bot", "--content", "full")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))

	requireHeaders(t, rx.Await(t, 1)[0], map[string]string{"X-Tenant": "acme"})
}

func TestWebhooksRefuseABadHeader(t *testing.T) {
	cases := map[string]struct {
		header string
		stderr string
	}{
		"Content-Type":            {header: "Content-Type: text/plain", stderr: "the header Content-Type is set by zapgate and cannot be replaced"},
		"content-length":          {header: "content-length: 10", stderr: "the header Content-Length is set by zapgate and cannot be replaced"},
		"Host":                    {header: "Host: elsewhere.example.test", stderr: "the header Host is set by zapgate and cannot be replaced"},
		"User-Agent":              {header: "User-Agent: curl", stderr: "the header User-Agent is set by zapgate and cannot be replaced"},
		"webhook-id":              {header: "webhook-id: msg_1", stderr: "the header Webhook-Id is set by zapgate and cannot be replaced"},
		"Webhook-Signature":       {header: "Webhook-Signature: v1,abc", stderr: "the header Webhook-Signature is set by zapgate and cannot be replaced"},
		"WEBHOOK-TIMESTAMP":       {header: "WEBHOOK-TIMESTAMP: 1", stderr: "the header Webhook-Timestamp is set by zapgate and cannot be replaced"},
		"another webhook- header": {header: "Webhook-Origin: zapgate", stderr: "the header Webhook-Origin is set by zapgate and cannot be replaced"},
		"no colon":                {header: "Bearer " + tenantToken, stderr: `a header is not valid: use "<name>: <value>"`},
		"no name":                 {header: ": acme", stderr: `a header is not valid: use "<name>: <value>"`},
		"no value":                {header: "X-Tenant:", stderr: `a header is not valid: use "<name>: <value>"`},
		"bad name":                {header: "X Tenant: acme", stderr: `the header name "X Tenant" is not valid`},
	}
	for name, tc := range cases {
		for _, command := range []string{"add", "set"} {
			t.Run(command+" "+name, func(t *testing.T) {
				h := apptest.Start(t)
				h.AddAccount(t, "work", workPhone, "Alex")
				h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work", "--header", "X-Tenant: acme")
				before := h.CLI(t, "webhooks", "list").Stdout
				args := []string{"webhooks", "add", "sales-bot", "--url", botURL, "--accounts", "work", "--header", tc.header}
				if command == "set" {
					args = []string{"webhooks", "set", "support-bot", "--header", tc.header}
				}

				result := h.CLI(t, args...)

				if want := "zapgate: " + tc.stderr + "\n"; result.Code != 1 || result.Stderr != want || result.Stdout != "" {
					t.Fatalf("exit %d, stdout %q, stderr %q, want %q", result.Code, result.Stdout, result.Stderr, want)
				}
				if strings.Contains(result.Stderr, tenantToken) {
					t.Fatalf("the error shows the header value: %q", result.Stderr)
				}
				if after := h.CLI(t, "webhooks", "list").Stdout; after != before {
					t.Fatalf("a refused %s changed the webhooks: %q, want %q", command, after, before)
				}
			})
		}
	}
}

func TestWebhooksRefuseAHeaderGivenTwice(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")

	result := h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work", "--header", "X-Tenant: acme", "--header", "x-tenant: other")

	if result.Code != 1 || result.Stderr != "zapgate: the header X-Tenant is given twice\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", result.Code, result.Stdout, result.Stderr)
	}
}

func TestWebhooksAddAndSetTakeTheFormat(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addWebhook(t, h, rx, "triage", "work", "--format", "standard")
	h.CLI(t, "webhooks", "add", "routine-bot", "--url", botURL, "--accounts", "work", "--format", "claude-routine")
	h.CLI(t, "webhooks", "add", "plain-bot", "--url", botURL, "--accounts", "work")

	set := h.CLI(t, "webhooks", "set", "triage", "--format", "claude-routine", "--header", "Authorization: Bearer "+routineToken)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0A1", "Maria", morning), text("oi, tudo bem?")))

	if set.Code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
	}
	got := rx.Await(t, 1)[0]
	requireRoutineHeaders(t, got)
	if text := routineText(t, got); !strings.HasPrefix(text, `{"type":"message.received",`) {
		t.Fatalf("the routine text is %s", text)
	}
	for name, format := range map[string]string{"triage": "claude-routine", "routine-bot": "claude-routine", "plain-bot": "standard"} {
		if row := listedWebhook(t, h, name); row["FORMAT"] != format {
			t.Fatalf("webhooks list %v, want the format %s", row, format)
		}
	}
}

func TestWebhooksRefuseAnUnknownFormat(t *testing.T) {
	for _, command := range []string{"add", "set"} {
		t.Run(command, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.CLI(t, "webhooks", "add", "support-bot", "--url", botURL, "--accounts", "work")
			args := []string{"webhooks", "add", "sales-bot", "--url", botURL, "--accounts", "work", "--format", "xml"}
			if command == "set" {
				args = []string{"webhooks", "set", "support-bot", "--format", "xml"}
			}

			result := h.CLI(t, args...)

			if result.Code != 1 || result.Stderr != "zapgate: the format \"xml\" is not valid: use standard or claude-routine\n" {
				t.Fatalf("exit %d, stdout %q, stderr %q", result.Code, result.Stdout, result.Stderr)
			}
		})
	}
}

func TestAnExtraHeaderReplacesAHeaderOfTheClaudeRoutineFormat(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	rx := h.StartReceiver(t)
	addRoutineWebhook(t, h, rx, "triage", "work", "--header", "anthropic-version: 2023-06-01")

	h.CLI(t, "webhooks", "test", "triage")

	requireHeaders(t, rx.Await(t, 1)[0], map[string]string{"Anthropic-Version": "2023-06-01", "Anthropic-Beta": "experimental-cc-routine-2026-04-01"})
}

func TestWebhooksHeaderAndFormatUsageErrors(t *testing.T) {
	cases := [][]string{
		{"webhooks", "add", "support-bot", "--url", botURL, "--accounts", "all", "--header"},
		{"webhooks", "add", "support-bot", "--url", botURL, "--accounts", "all", "--header="},
		{"webhooks", "add", "support-bot", "--url", botURL, "--accounts", "all", "--format"},
		{"webhooks", "set", "support-bot", "--header"},
		{"webhooks", "set", "support-bot", "--format"},
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
