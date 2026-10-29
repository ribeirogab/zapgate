package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func listAccounts(t *testing.T, session *mcp.ClientSession) string {
	t.Helper()
	result := callTool(t, session, "list_accounts", nil)
	if result.IsError {
		t.Fatalf("list_accounts failed: %+v", result.Content)
	}
	return structured(t, result)
}

func TestListAccountsShowsEveryFieldOfTheAccounts(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex Work")
			h.AddAccount(t, "personal", personalPhone, "")
			h.CLI(t, "accounts", "describe", "work", "Conta", "do", "trabalho")
			session := h.Connect(t, h.StaticToken(t, "bot", nil, false), apptest.WithProtocol(path.protocol))

			got := listAccounts(t, session)

			want := `{"accounts":[` +
				`{"description":"","name":"personal","phone":"+5521977776666","profile_name":"","state":"connected"},` +
				`{"description":"Conta do trabalho","name":"work","phone":"+5511988887777","profile_name":"Alex Work","state":"connected"}]}`
			if got != want {
				t.Fatalf("list_accounts returned\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestAccountsDescribeChangesTheDescription(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")

	described := h.CLI(t, "accounts", "describe", "work", "Clientes da loja")
	cleared := h.CLI(t, "accounts", "describe", "work", "")
	h.CLI(t, "accounts", "describe", "work", "Fornecedores")
	missing := h.CLI(t, "accounts", "describe", "ghost", "x")

	if described.Code != 0 || described.Stdout != "the account work has the description: Clientes da loja\n" {
		t.Fatalf("describe: exit %d, stdout %q, stderr %q", described.Code, described.Stdout, described.Stderr)
	}
	if cleared.Code != 0 || cleared.Stdout != "the account work has no description now\n" {
		t.Fatalf("clear: exit %d, stdout %q, stderr %q", cleared.Code, cleared.Stdout, cleared.Stderr)
	}
	if missing.Code != 1 || missing.Stderr != "zapgate: there is no account named \"ghost\"\n" {
		t.Fatalf("missing: exit %d, stderr %q", missing.Code, missing.Stderr)
	}
	if rows := tableRows(t, h.CLI(t, "accounts", "list").Stdout); rows[1] != "work +5511988887777 Alex connected Fornecedores" {
		t.Fatalf("accounts list %q", rows)
	}
}

func TestAccountsReconnectAfterARestart(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")

	h.Restart(t)

	h.AwaitAccountState(t, "work", "connected")
	h.AwaitAccountState(t, "personal", "connected")
	h.WhatsApp.DropConnection(workPhone)
	h.AwaitAccountState(t, "work", "disconnected")
	status := h.CLI(t, "status")
	rows := tableRows(t, status.Stdout)
	if status.Code != 0 || len(rows) != 4 || rows[1] != "personal +5521977776666 connected 0 0 B 6 months (default)" || rows[2] != "work +5511988887777 disconnected 0 0 B 6 months (default)" {
		t.Fatalf("status: exit %d, stdout %q", status.Code, status.Stdout)
	}
}

func TestAnAccountThatLosesTheConnectionComesBack(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")

	h.WhatsApp.DropConnection(workPhone)
	h.AwaitAccountState(t, "work", "disconnected")
	h.WhatsApp.RestoreConnection(workPhone)

	h.AwaitAccountState(t, "work", "connected")
}

func TestARestartWhileOfflineShowsDisconnectedUntilTheNetworkReturns(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.WhatsApp.DropConnection(workPhone)

	h.Restart(t)

	h.AwaitAccountState(t, "work", "disconnected")
	h.WhatsApp.RestoreConnection(workPhone)
	h.AwaitAccountState(t, "work", "connected")
}

func TestADeviceRemovedOnThePhoneShowsLoggedOut(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "bot", nil, false))

	h.WhatsApp.UnlinkDevice(workPhone)

	h.AwaitAccountState(t, "work", "logged_out")
	if got := listAccounts(t, session); !strings.Contains(got, `"name":"work","phone":"+5511988887777","profile_name":"Alex","state":"logged_out"`) {
		t.Fatalf("list_accounts returned %s", got)
	}
	if rows := tableRows(t, h.CLI(t, "status").Stdout); rows[1] != "work +5511988887777 logged_out 0 0 B 6 months (default)" {
		t.Fatalf("status %q", rows)
	}
	h.Restart(t)
	h.AwaitAccountState(t, "work", "logged_out")
}

func TestADeviceRemovedWhileTheServerWasDownShowsLoggedOut(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.Stop(t)
	h.WhatsApp.UnlinkDevice(workPhone)

	h.Resume(t)

	h.AwaitAccountState(t, "work", "logged_out")
}

func TestTheProfileNameFollowsThePhone(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "")

	h.WhatsApp.SetProfileName(workPhone, "Loja do Alex")

	account, err := h.Owner.Account(context.Background(), "work")
	if err != nil || account.ProfileName != "Loja do Alex" {
		t.Fatalf("account %+v, error %v", account, err)
	}
}

func TestTheProfileNameFromTheFirstSettingsSyncReachesTheAccount(t *testing.T) {
	h := apptest.Start(t)
	run := h.StartCLI("accounts", "add", "work", "--phone", "+"+workPhone)
	pairing := h.AwaitPairing(t, workPhone)

	pairing.Confirm()
	h.WhatsApp.SyncSettings(workPhone, "Alex Work")
	added := run.Wait(t)

	if added.Code != 0 || !strings.HasSuffix(added.Stdout, "the account work is connected (+5511988887777, profile Alex Work)\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", added.Code, added.Stdout, added.Stderr)
	}
	rows := tableRows(t, h.CLI(t, "accounts", "list").Stdout)
	if len(rows) != 2 || rows[1] != "work +5511988887777 Alex Work connected" {
		t.Fatalf("accounts list %q", rows)
	}
}

func TestAccountsAddDoesNotWaitLongForAPhoneThatSendsNoSettings(t *testing.T) {
	h := apptest.Start(t)
	run := h.StartCLI("accounts", "add", "work", "--phone", "+"+workPhone)
	pairing := h.AwaitPairing(t, workPhone)

	pairing.Confirm()
	added := run.WaitWhile(t, func() { h.Clock.Advance(time.Minute) })

	if added.Code != 0 || !strings.HasSuffix(added.Stdout, "the account work is connected (+5511988887777)\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", added.Code, added.Stdout, added.Stderr)
	}
	h.WhatsApp.SyncSettings(workPhone, "Alex Work")
	session := h.Connect(t, h.StaticToken(t, "bot", nil, false))
	if got := listAccounts(t, session); !strings.Contains(got, `"name":"work","phone":"+5511988887777","profile_name":"Alex Work"`) {
		t.Fatalf("list_accounts returned %s", got)
	}
}

func TestTheProfileNameSurvivesARestart(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex Work")

	h.Restart(t)

	h.AwaitAccountState(t, "work", "connected")
	if rows := tableRows(t, h.CLI(t, "accounts", "list").Stdout); rows[1] != "work +5511988887777 Alex Work connected" {
		t.Fatalf("accounts list %q", rows)
	}
}
