package app_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/admin"
	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestTokensCreateRejectsBadRequests(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		stderr string
	}{
		{
			name:   "name in use",
			args:   []string{"tokens", "create", "bot", "--accounts", "all"},
			stderr: "zapgate: the access name \"bot\" is already in use\n",
		},
		{
			name:   "unknown account",
			args:   []string{"tokens", "create", "other", "--accounts", "work,personal"},
			stderr: "zapgate: there is no account named \"work\"\n",
		},
		{
			name:   "invalid name",
			args:   []string{"tokens", "create", "Bad_Name", "--accounts", "all"},
			stderr: "zapgate: the access name \"Bad_Name\" is not valid: use lowercase letters, digits and single hyphens, at most 64 characters\n",
		},
		{
			name:   "name too long",
			args:   []string{"tokens", "create", strings.Repeat("a", 65), "--accounts", "all"},
			stderr: "is not valid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.StaticToken(t, "bot", nil, false)

			result := h.CLI(t, tc.args...)

			if result.Code != 1 || !strings.Contains(result.Stderr, tc.stderr) {
				t.Fatalf("exit %d, stderr %q, want %q", result.Code, result.Stderr, tc.stderr)
			}
			if result.Stdout != "" {
				t.Fatalf("stdout %q, want nothing", result.Stdout)
			}
		})
	}
}

func TestOwnerAPIAnswersConflictForANameInUse(t *testing.T) {
	h := apptest.Start(t)
	h.StaticToken(t, "bot", nil, false)

	_, err := h.Owner.CreateToken(context.Background(), admin.CreateTokenRequest{Name: "bot", AllExistingAccounts: true})

	var apiErr *admin.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("error %v, want a 409 from the owner API", err)
	}
}

func TestOwnerAPIRejectsAnUnclearAccountChoice(t *testing.T) {
	cases := map[string]admin.CreateTokenRequest{
		"no choice":          {Name: "bot"},
		"empty list":         {Name: "bot", Accounts: []string{}},
		"list and all":       {Name: "bot", Accounts: []string{"work"}, AllExistingAccounts: true},
		"read-only, no list": {Name: "bot", ReadOnly: true},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			h := apptest.Start(t)

			_, err := h.Owner.CreateToken(context.Background(), req)

			var apiErr *admin.Error
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
				t.Fatalf("error %v, want a 400 from the owner API", err)
			}
		})
	}
}

func TestOwnerAPIAcceptsAllExistingAccountsWhenThereIsNone(t *testing.T) {
	h := apptest.Start(t)

	created, err := h.Owner.CreateToken(context.Background(), admin.CreateTokenRequest{Name: "bot", AllExistingAccounts: true})

	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Access.Accounts) != 0 || created.Access.Name != "bot" || created.Token == "" {
		t.Fatalf("created %+v", created)
	}
}

func TestRevokedNameCanBeUsedAgain(t *testing.T) {
	h := apptest.Start(t)
	h.StaticToken(t, "bot", nil, false)
	h.CLI(t, "access", "revoke", "bot")

	created := h.CLI(t, "tokens", "create", "bot", "--accounts", "all", "--read-only")

	if created.Code != 0 || !strings.HasPrefix(created.Stdout, "created the access bot (static token, accounts: none, read-only)\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", created.Code, created.Stdout, created.Stderr)
	}
	listed := h.CLI(t, "access", "list")
	if !strings.Contains(listed.Stdout, "read-only") {
		t.Fatalf("access list %q does not show read-only", listed.Stdout)
	}
}

func TestCLIUsageErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"unknown"},
		{"tokens"},
		{"tokens", "create", "bot"},
		{"tokens", "create", "--accounts", "all"},
		{"access", "revoke"},
		{"accounts", "remove"},
		{"accounts", "remove", "work", "--force"},
		{"accounts", "add", "work"},
		{"accounts", "add", "--phone", "+5511988887777"},
		{"accounts", "describe", "work"},
		{"access", "set", "bot"},
		{"access", "set", "bot", "--accounts", "all", "--read-only"},
		{"access", "set", "bot", "--approval"},
		{"access", "set", "bot", "--approval", "auto"},
		{"access", "set", "bot", "--approval="},
		{"tokens", "create", "bot", "--accounts", "all", "--approval", "auto"},
		{"tokens", "create", "bot", "--approval", "app"},
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

func TestStatusAndAccountsListWithoutAccounts(t *testing.T) {
	h := apptest.Start(t)

	status := h.CLI(t, "status")
	accounts := h.CLI(t, "accounts", "list")

	if status.Code != 0 || status.Stdout != "accounts: none\n" {
		t.Fatalf("status: exit %d, stdout %q, stderr %q", status.Code, status.Stdout, status.Stderr)
	}
	want := "no accounts yet; connect one with: zapgate accounts add <name> --phone <number>\n"
	if accounts.Code != 0 || accounts.Stdout != want {
		t.Fatalf("accounts list: exit %d, stdout %q, stderr %q", accounts.Code, accounts.Stdout, accounts.Stderr)
	}
}

func TestOwnerCommandFailsWhenTheServerIsDown(t *testing.T) {
	h := apptest.Start(t)
	socket := h.AdminSocket
	h.AdminSocket = socket + ".missing"

	result := h.CLI(t, "status")

	if result.Code != 1 || !strings.HasPrefix(result.Stderr, "zapgate: cannot reach the server on its owner socket") {
		t.Fatalf("exit %d, stderr %q", result.Code, result.Stderr)
	}
}
