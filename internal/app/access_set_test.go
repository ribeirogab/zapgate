package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func accountNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	var names []string
	for _, part := range strings.Split(listAccounts(t, session), `"name":"`)[1:] {
		names = append(names, part[:strings.Index(part, `"`)])
	}
	return names
}

func TestAccessSetChangesTheAccountsOnTheNextCall(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.AddAccount(t, "personal", personalPhone, "Alex")
			changed := make(chan struct{}, 8)
			session := h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false),
				apptest.WithProtocol(path.protocol),
				apptest.WithToolListChanged(func(context.Context, *mcp.ToolListChangedRequest) { changed <- struct{}{} }))
			if got := accountNames(t, session); strings.Join(got, ",") != "work" {
				t.Fatalf("accounts before %v", got)
			}

			set := h.CLI(t, "access", "set", "bot", "--accounts", "personal")

			if set.Code != 0 || set.Stdout != "the access bot now reaches the accounts: personal\n" {
				t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
			}
			if got := accountNames(t, session); strings.Join(got, ",") != "personal" {
				t.Fatalf("accounts after access set %v", got)
			}
			select {
			case <-changed:
			case <-time.After(5 * time.Second):
				t.Fatal("the open session got no tools/list_changed")
			}

			all := h.CLI(t, "access", "set", "bot", "--accounts", "all")

			if all.Stdout != "the access bot now reaches the accounts: personal,work\n" {
				t.Fatalf("stdout %q", all.Stdout)
			}
			if got := accountNames(t, session); strings.Join(got, ",") != "personal,work" {
				t.Fatalf("accounts after access set all %v", got)
			}
		})
	}
}

func TestAccessSetRejectsBadRequests(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		stderr string
	}{
		{name: "unknown access", args: []string{"access", "set", "ghost", "--accounts", "work"}, stderr: "zapgate: there is no access named \"ghost\"\n"},
		{name: "unknown account", args: []string{"access", "set", "bot", "--accounts", "work,sales"}, stderr: "zapgate: there is no account named \"sales\"\n"},
		{name: "empty list", args: []string{"access", "set", "bot", "--accounts", ","}, stderr: "zapgate: the access needs a list of accounts, or all existing accounts\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			session := h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false))

			result := h.CLI(t, tc.args...)

			if result.Code != 1 || result.Stderr != tc.stderr {
				t.Fatalf("exit %d, stderr %q, want %q", result.Code, result.Stderr, tc.stderr)
			}
			if got := accountNames(t, session); strings.Join(got, ",") != "work" {
				t.Fatalf("a refused access set changed the accounts: %v", got)
			}
		})
	}
}

func TestAllAccountsIsASnapshotOfTheExistingAccounts(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")

	created := h.CLI(t, "tokens", "create", "bot", "--accounts", "all")
	h.AddAccount(t, "sales", "5531966665555", "Loja")

	if !strings.HasPrefix(created.Stdout, "created the access bot (static token, accounts: personal,work)\n") {
		t.Fatalf("stdout %q", created.Stdout)
	}
	session := h.Connect(t, tokenFrom(t, created.Stdout))
	if got := accountNames(t, session); strings.Join(got, ",") != "personal,work" {
		t.Fatalf("the access reaches %v, want the snapshot personal,work", got)
	}
	if rows := tableRows(t, h.CLI(t, "access", "list").Stdout); rows[1] != "bot static personal,work read-write elicitation 2026-10-13T12:00:00Z" {
		t.Fatalf("access list %q", rows)
	}
}

func TestAccountActionsAreAudited(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.StaticToken(t, "bot", []string{"work"}, false)
	h.CLI(t, "accounts", "describe", "work", "Clientes")
	h.CLI(t, "access", "set", "bot", "--accounts", "all")
	h.CLI(t, "accounts", "remove", "work", "--yes")

	actions := linesWithMessage(h.AuditLines(t), "admin")
	want := []map[string]string{
		{"action": "account_add", "account": "work"},
		{"action": "token_create", "grant": "bot"},
		{"action": "account_describe", "account": "work"},
		{"action": "access_set", "grant": "bot", "kind": "static"},
		{"action": "account_remove", "account": "work"},
	}
	if len(actions) != len(want) {
		t.Fatalf("audit has %d admin lines, want %d: %s", len(actions), len(want), h.AuditRaw())
	}
	for i, fields := range want {
		assertFields(t, actions[i], fields)
	}
	if strings.Contains(h.AuditRaw(), "Clientes") {
		t.Fatal("the audit log contains the description")
	}
}
