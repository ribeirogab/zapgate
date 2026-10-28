package app_test

import (
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestAccountsRemoveAsksForConfirmation(t *testing.T) {
	for _, answer := range []string{"", "n\n", "no\n", "work\n"} {
		t.Run(answer, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")

			result := h.CLIWithInput(t, answer, "accounts", "remove", "work")

			if !strings.HasPrefix(result.Stdout, "remove the account work (+5511988887777)? This logs out its device on WhatsApp, deletes its local data, and takes it out of every access. [y/N] ") {
				t.Fatalf("stdout %q has no question", result.Stdout)
			}
			if result.Code != 1 || result.Stderr != "zapgate: the account stays; answer y to remove it, or pass --yes\n" {
				t.Fatalf("exit %d, stderr %q", result.Code, result.Stderr)
			}
			if !h.WhatsApp.Linked(workPhone) {
				t.Fatal("the device was logged out without confirmation")
			}
			h.AwaitAccountState(t, "work", "connected")
		})
	}
}

func TestAccountsRemoveLogsOutDeletesAndLeavesEveryAccess(t *testing.T) {
	cases := map[string]func(h *apptest.Harness) apptest.CLIResult{
		"answer y": func(h *apptest.Harness) apptest.CLIResult {
			return h.CLIWithInput(t, "y\n", "accounts", "remove", "work")
		},
		"flag --yes": func(h *apptest.Harness) apptest.CLIResult {
			return h.CLI(t, "accounts", "remove", "--yes", "work")
		},
	}
	for name, remove := range cases {
		t.Run(name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.AddAccount(t, "personal", personalPhone, "Alex")
			session := h.Connect(t, h.StaticToken(t, "bot", []string{"work", "personal"}, false))
			h.StaticToken(t, "only-work", []string{"work"}, true)

			result := remove(h)

			if result.Code != 0 || !strings.HasSuffix(result.Stdout, "removed the account work\n") || result.Stderr != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q", result.Code, result.Stdout, result.Stderr)
			}
			if h.WhatsApp.Linked(workPhone) || h.WhatsApp.HasSession(workPhone) {
				t.Fatal("the device is still linked on WhatsApp or kept in the session store")
			}
			if !h.WhatsApp.Linked(personalPhone) {
				t.Fatal("the other account lost its device")
			}
			if rows := tableRows(t, h.CLI(t, "accounts", "list").Stdout); len(rows) != 2 || !strings.HasPrefix(rows[1], "personal ") {
				t.Fatalf("accounts list %q", rows)
			}
			if got := listAccounts(t, session); strings.Contains(got, `"work"`) {
				t.Fatalf("list_accounts still shows the removed account: %s", got)
			}
			access := tableRows(t, h.CLI(t, "access", "list").Stdout)
			if access[1] != "bot static personal read-write elicitation 2026-10-13T12:00:00Z" || access[2] != "only-work static - read-only elicitation 2026-10-13T12:00:00Z" {
				t.Fatalf("access list %q", access)
			}
			h.AddAccount(t, "work", workPhone, "Alex")
		})
	}
}

func TestAccountsRemoveWhileOfflineWarnsAboutThePhone(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.WhatsApp.DropConnection(workPhone)

	result := h.CLI(t, "accounts", "remove", "work", "--yes")

	if result.Code != 0 || result.Stdout != "removed the account work\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", result.Code, result.Stdout, result.Stderr)
	}
	if !strings.HasPrefix(result.Stderr, "warning: the phone may still list this device; remove it in WhatsApp > Linked devices") {
		t.Fatalf("stderr %q has no warning", result.Stderr)
	}
	if h.WhatsApp.HasSession(workPhone) {
		t.Fatal("the local session of the removed account is still there")
	}
}

func TestAccountsRemoveALoggedOutAccount(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.WhatsApp.UnlinkDevice(workPhone)
	h.AwaitAccountState(t, "work", "logged_out")

	result := h.CLI(t, "accounts", "remove", "work", "--yes")

	if result.Code != 0 || result.Stdout != "removed the account work\n" || result.Stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", result.Code, result.Stdout, result.Stderr)
	}
}

func TestAccountsRemoveAnUnknownAccount(t *testing.T) {
	h := apptest.Start(t)

	result := h.CLI(t, "accounts", "remove", "ghost", "--yes")

	if result.Code != 1 || result.Stderr != "zapgate: there is no account named \"ghost\"\n" {
		t.Fatalf("exit %d, stderr %q", result.Code, result.Stderr)
	}
}
