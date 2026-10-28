package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const resourceMetadata = `resource_metadata="https://wpp.example.test/.well-known/oauth-protected-resource"`

func postMCP(t *testing.T, h *apptest.Harness, authorization string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return h.Do(t, req)
}

func TestMCPWithoutTokenIsUnauthorized(t *testing.T) {
	h := apptest.Start(t)

	resp := postMCP(t, h, "")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, resourceMetadata) {
		t.Fatalf("WWW-Authenticate %q does not name the resource metadata", got)
	}
}

func tokenFrom(t *testing.T, stdout string) string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if token, ok := strings.CutPrefix(line, "token: "); ok {
			return token
		}
	}
	t.Fatalf("no token line in %q", stdout)
	return ""
}

func TestTokensCreateShowsTheTokenOnce(t *testing.T) {
	h := apptest.Start(t)

	created := h.CLI(t, "tokens", "create", "bot", "--accounts", "all")

	if created.Code != 0 {
		t.Fatalf("exit %d, stderr %q", created.Code, created.Stderr)
	}
	token := tokenFrom(t, created.Stdout)
	if !strings.HasPrefix(token, "wmcp_") || len(token) != len("wmcp_")+43 {
		t.Fatalf("token %q does not look like wmcp_ plus 32 random bytes", token)
	}
	wantFirst := "created the access bot (static token, accounts: none)\n" +
		"the access reaches no account yet; give it accounts with: zapgate access set bot --accounts <a,b|all>\n" +
		"token: " + token + "\n"
	if !strings.HasPrefix(created.Stdout, wantFirst) {
		t.Fatalf("stdout %q does not start with %q", created.Stdout, wantFirst)
	}
	if !strings.HasSuffix(created.Stdout, "this is the only time the token is shown; store it now\n") {
		t.Fatalf("stdout %q has no warning line", created.Stdout)
	}

	listed := h.CLI(t, "access", "list")

	if listed.Code != 0 {
		t.Fatalf("exit %d, stderr %q", listed.Code, listed.Stderr)
	}
	lines := strings.Split(strings.TrimSpace(listed.Stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("access list %q, want a header and one row", listed.Stdout)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "NAME KIND ACCOUNTS MODE APPROVAL CREATED" {
		t.Fatalf("header %q", lines[0])
	}
	if got := strings.Join(strings.Fields(lines[1]), " "); got != "bot static - read-write elicitation 2026-10-13T12:00:00Z" {
		t.Fatalf("row %q", lines[1])
	}
	if strings.Contains(listed.Stdout, token) {
		t.Fatal("access list shows the token")
	}
}

func TestDataDirKeepsOnlyTheTokenHash(t *testing.T) {
	h := apptest.Start(t)
	token := tokenFrom(t, h.CLI(t, "tokens", "create", "bot", "--accounts", "all").Stdout)
	sum := sha256.Sum256([]byte(token))

	stored := readDataFiles(t, h.DataDir, "app.db*")

	if strings.Contains(stored, token) {
		t.Fatal("app.db keeps the plaintext token")
	}
	if !strings.Contains(stored, hex.EncodeToString(sum[:])) {
		t.Fatal("app.db does not keep the SHA-256 hex of the token")
	}
}

func readDataFiles(t *testing.T, dir, pattern string) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no %s in %s: %v", pattern, dir, err)
	}
	var all strings.Builder
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(body)
	}
	return all.String()
}

func TestAccessListIsEmptyAtFirst(t *testing.T) {
	h := apptest.Start(t)

	listed := h.CLI(t, "access", "list")

	if listed.Code != 0 || listed.Stdout != "no access yet\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", listed.Code, listed.Stdout, listed.Stderr)
	}
}

func TestMCPWithUnknownTokenIsUnauthorized(t *testing.T) {
	h := apptest.Start(t)

	resp := postMCP(t, h, "Bearer wmcp_unknown")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, resourceMetadata) {
		t.Fatalf("WWW-Authenticate %q does not name the resource metadata", got)
	}
}
