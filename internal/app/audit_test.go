package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/config"
)

func linesWithMessage(lines []map[string]any, msg string) []map[string]any {
	var found []map[string]any
	for _, line := range lines {
		if line["msg"] == msg {
			found = append(found, line)
		}
	}
	return found
}

func assertFields(t *testing.T, line map[string]any, want map[string]string) {
	t.Helper()
	for key, value := range want {
		if line[key] != value {
			t.Fatalf("audit line %v: %s = %v, want %q", line, key, line[key], value)
		}
	}
}

func TestToolCallsAreAudited(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			token := h.StaticToken(t, "bot", nil, false)
			session := h.Connect(t, token, apptest.WithProtocol(path.protocol), apptest.WithIP("198.51.100.20"))

			callTool(t, session, "list_accounts", nil)

			calls := linesWithMessage(h.AuditLines(t), "tool_call")
			if len(calls) != 1 {
				t.Fatalf("audit has %d tool_call lines, want 1: %s", len(calls), h.AuditRaw())
			}
			assertFields(t, calls[0], map[string]string{
				"grant":   "bot",
				"tool":    "list_accounts",
				"account": "",
				"result":  "ok",
				"ip":      "198.51.100.20",
				"time":    "2026-10-13T12:00:00Z",
				"level":   "INFO",
			})
			if id, ok := calls[0]["grant_id"].(float64); !ok || id < 1 {
				t.Fatalf("audit line %v has no numeric grant_id", calls[0])
			}
			if strings.Contains(h.AuditRaw(), token) {
				t.Fatal("the audit log contains the token")
			}
		})
	}
}

func TestFailedToolCallIsAuditedAsError(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "bot", nil, false)
	session := h.Connect(t, token)

	result := callTool(t, session, "list_accounts", map[string]any{"account": "work"})

	if !result.IsError {
		t.Fatal("list_accounts accepted an unknown argument")
	}
	calls := linesWithMessage(h.AuditLines(t), "tool_call")
	if len(calls) != 1 {
		t.Fatalf("audit has %d tool_call lines, want 1: %s", len(calls), h.AuditRaw())
	}
	assertFields(t, calls[0], map[string]string{"tool": "list_accounts", "account": "work", "result": "error"})
}

func TestAuditCutsTheAccountArgument(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "bot", nil, false)
	session := h.Connect(t, token)

	result := callTool(t, session, "list_accounts", map[string]any{"account": strings.Repeat("a", 200)})

	if !result.IsError {
		t.Fatal("list_accounts accepted an unknown argument")
	}
	calls := linesWithMessage(h.AuditLines(t), "tool_call")
	if len(calls) != 1 {
		t.Fatalf("audit has %d tool_call lines, want 1: %s", len(calls), h.AuditRaw())
	}
	assertFields(t, calls[0], map[string]string{"account": strings.Repeat("a", 64), "result": "error"})
}

func TestAuditIgnoresAnAccountThatIsNotAString(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "bot", nil, false)
	session := h.Connect(t, token)

	callTool(t, session, "list_accounts", map[string]any{"account": 42, "text": "secret message"})

	calls := linesWithMessage(h.AuditLines(t), "tool_call")
	if len(calls) != 1 {
		t.Fatalf("audit has %d tool_call lines, want 1: %s", len(calls), h.AuditRaw())
	}
	assertFields(t, calls[0], map[string]string{"account": "", "result": "error"})
	if strings.Contains(h.AuditRaw(), "secret message") {
		t.Fatal("the audit log contains a tool argument")
	}
}

func TestToolCallAuditIgnoresAClientIPThatIsNotAnIP(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "bot", nil, false)
	session := h.Connect(t, token, apptest.WithIP("not-an-ip"))

	callTool(t, session, "list_accounts", nil)

	calls := linesWithMessage(h.AuditLines(t), "tool_call")
	if len(calls) != 1 {
		t.Fatalf("audit has %d tool_call lines, want 1: %s", len(calls), h.AuditRaw())
	}
	assertFields(t, calls[0], map[string]string{"ip": "127.0.0.1"})
}

func TestAuthAuditIgnoresAClientIPThatIsNotAnIP(t *testing.T) {
	h := apptest.Start(t)
	req, err := http.NewRequest(http.MethodPost, h.URL+"/mcp", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("CF-Connecting-IP", "203.0.113.99, 10.0.0.1")

	h.Do(t, req)

	denied := linesWithMessage(h.AuditLines(t), "auth")
	if len(denied) != 1 {
		t.Fatalf("audit has %d auth lines, want 1", len(denied))
	}
	assertFields(t, denied[0], map[string]string{"ip": "127.0.0.1"})
}

func TestUnauthorizedRequestsAreAudited(t *testing.T) {
	h := apptest.Start(t)

	postMCP(t, h, "")
	postMCP(t, h, "Bearer wmcp_not-a-real-token")

	denied := linesWithMessage(h.AuditLines(t), "auth")
	if len(denied) != 2 {
		t.Fatalf("audit has %d auth lines, want 2: %s", len(denied), h.AuditRaw())
	}
	assertFields(t, denied[0], map[string]string{"result": "denied", "reason": "missing", "path": "/mcp", "ip": "127.0.0.1"})
	assertFields(t, denied[1], map[string]string{"result": "denied", "reason": "invalid", "path": "/mcp", "ip": "127.0.0.1"})
	if strings.Contains(h.AuditRaw(), "wmcp_not-a-real-token") {
		t.Fatal("the audit log contains the presented token")
	}
}

func TestUnauthorizedRequestUsesTheTunnelClientIP(t *testing.T) {
	h := apptest.Start(t)
	req, err := http.NewRequest(http.MethodPost, h.URL+"/mcp", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("CF-Connecting-IP", "203.0.113.99")

	h.Do(t, req)

	denied := linesWithMessage(h.AuditLines(t), "auth")
	if len(denied) != 1 {
		t.Fatalf("audit has %d auth lines, want 1", len(denied))
	}
	assertFields(t, denied[0], map[string]string{"ip": "203.0.113.99"})
}

func TestUnauthorizedRequestIgnoresClientIPHeadersWhenNoneIsTrusted(t *testing.T) {
	h := apptest.Start(t, apptest.WithConfig(func(cfg *config.Config) { cfg.ClientIPHeader = "" }))
	req, err := http.NewRequest(http.MethodPost, h.URL+"/mcp", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("CF-Connecting-IP", "203.0.113.99")
	req.Header.Set("X-Zapgate-Client-Ip", "203.0.113.98")

	h.Do(t, req)

	denied := linesWithMessage(h.AuditLines(t), "auth")
	if len(denied) != 1 {
		t.Fatalf("audit has %d auth lines, want 1", len(denied))
	}
	assertFields(t, denied[0], map[string]string{"ip": "127.0.0.1"})
}

func TestUnauthorizedRequestCannotSetTheResolvedClientIP(t *testing.T) {
	h := apptest.Start(t)
	req, err := http.NewRequest(http.MethodPost, h.URL+"/mcp", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Zapgate-Client-Ip", "203.0.113.98")

	h.Do(t, req)

	denied := linesWithMessage(h.AuditLines(t), "auth")
	if len(denied) != 1 {
		t.Fatalf("audit has %d auth lines, want 1", len(denied))
	}
	assertFields(t, denied[0], map[string]string{"ip": "127.0.0.1"})
}

func TestHealthzAnswersOkWithoutAudit(t *testing.T) {
	h := apptest.Start(t)
	req, err := http.NewRequest(http.MethodGet, h.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp := h.Do(t, req)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if raw := h.AuditRaw(); raw != "" {
		t.Fatalf("healthz wrote audit lines: %s", raw)
	}
}

func TestHealthCommandChecksTheListener(t *testing.T) {
	h := apptest.Start(t)

	result := h.CLI(t, "health")

	if result.Code != 0 || result.Stdout != "ok\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", result.Code, result.Stdout, result.Stderr)
	}
}

func TestOwnerActionsAreAudited(t *testing.T) {
	h := apptest.Start(t)
	created := h.CLI(t, "tokens", "create", "bot", "--accounts", "all")
	token := tokenFrom(t, created.Stdout)

	h.CLI(t, "access", "revoke", "bot")
	h.CLI(t, "access", "revoke", "ghost")

	actions := linesWithMessage(h.AuditLines(t), "admin")
	if len(actions) != 2 {
		t.Fatalf("audit has %d admin lines, want 2: %s", len(actions), h.AuditRaw())
	}
	assertFields(t, actions[0], map[string]string{"action": "token_create", "grant": "bot", "kind": "static", "time": "2026-10-13T12:00:00Z"})
	assertFields(t, actions[1], map[string]string{"action": "access_revoke", "grant": "bot", "kind": "static"})
	for _, line := range actions {
		if id, ok := line["grant_id"].(float64); !ok || id < 1 {
			t.Fatalf("audit line %v has no numeric grant_id", line)
		}
	}
	if strings.Contains(h.AuditRaw(), token) {
		t.Fatal("the audit log contains the token")
	}
}
