package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

var protocolPaths = []struct {
	name       string
	protocol   string
	negotiated string
	stateful   bool
}{
	{name: "stateful", protocol: apptest.LegacyProtocol, negotiated: apptest.LegacyProtocol, stateful: true},
	{name: "stateless", protocol: "", negotiated: apptest.StatelessProtocol, stateful: false},
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

func structured(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestRevokedAccessGetsUnauthorized(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			token := h.StaticToken(t, "bot", nil, false)
			session := h.Connect(t, token, apptest.WithProtocol(path.protocol))
			callTool(t, session, "list_accounts", nil)

			revoked := h.CLI(t, "access", "revoke", "bot")

			if revoked.Code != 0 || revoked.Stdout != "revoked the access bot; its next call gets 401\n" {
				t.Fatalf("exit %d, stdout %q, stderr %q", revoked.Code, revoked.Stdout, revoked.Stderr)
			}
			if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_accounts"}); err == nil {
				t.Fatal("the open session still answers after the revoke")
			}
			if _, err := h.TryConnect(token, apptest.WithProtocol(path.protocol)); err == nil {
				t.Fatal("a new connection with the revoked token succeeded")
			}
			if resp := postMCP(t, h, "Bearer "+token); resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", resp.StatusCode)
			}
			if listed := h.CLI(t, "access", "list"); listed.Stdout != "no access yet\n" {
				t.Fatalf("access list after revoke: %q", listed.Stdout)
			}
		})
	}
}

func TestRevokeUnknownAccessFails(t *testing.T) {
	h := apptest.Start(t)

	revoked := h.CLI(t, "access", "revoke", "ghost")

	if revoked.Code != 1 || revoked.Stderr != "zapgate: there is no access named \"ghost\"\n" {
		t.Fatalf("exit %d, stderr %q", revoked.Code, revoked.Stderr)
	}
}

func TestListAccountsOnBothProtocolPaths(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			token := h.StaticToken(t, "bot", nil, false)

			session := h.Connect(t, token, apptest.WithProtocol(path.protocol))

			if got := session.InitializeResult().ProtocolVersion; got != path.negotiated {
				t.Fatalf("negotiated %s, want %s", got, path.negotiated)
			}
			if got := session.ID() != ""; got != path.stateful {
				t.Fatalf("session id %q, stateful %v", session.ID(), path.stateful)
			}
			tools, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(tools.Tools) != 1 || tools.Tools[0].Name != "list_accounts" {
				t.Fatalf("tools %+v, want only list_accounts", tools.Tools)
			}
			if !tools.Tools[0].Annotations.ReadOnlyHint {
				t.Fatal("list_accounts has no readOnlyHint")
			}
			result := callTool(t, session, "list_accounts", nil)
			if result.IsError {
				t.Fatalf("list_accounts failed: %+v", result.Content)
			}
			if got := structured(t, result); got != `{"accounts":[]}` {
				t.Fatalf("list_accounts returned %s", got)
			}
		})
	}
}
