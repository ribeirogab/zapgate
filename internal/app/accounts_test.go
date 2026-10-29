package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/admin"
	"github.com/ribeirogab/zapgate/internal/apptest"
)

const (
	workPhone     = "5511988887777"
	personalPhone = "5521977776666"
)

func fields(line string) string {
	return strings.Join(strings.Fields(line), " ")
}

func tableRows(t *testing.T, stdout string) []string {
	t.Helper()
	var rows []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		rows = append(rows, fields(line))
	}
	return rows
}

func TestAccountsAddShowsThePhoneCodeAndWaitsForTheConnection(t *testing.T) {
	h := apptest.Start(t)

	run := h.StartCLI("accounts", "add", "work", "--phone", "+55 (11) 98888-7777", "--description", "Conta do trabalho")
	pairing := h.AwaitPairing(t, workPhone)
	pairing.Accept("Alex Work")
	added := run.Wait(t)

	if added.Code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", added.Code, added.Stdout, added.Stderr)
	}
	if pairing.Code == "" || !strings.Contains(added.Stdout, "\n    "+pairing.Code+"\n") {
		t.Fatalf("stdout %q does not show the pairing code %q", added.Stdout, pairing.Code)
	}
	if !strings.HasSuffix(added.Stdout, "the account work is connected (+5511988887777, profile Alex Work)\n") {
		t.Fatalf("stdout %q does not end with the connection line", added.Stdout)
	}
	listed := h.CLI(t, "accounts", "list")
	rows := tableRows(t, listed.Stdout)
	if len(rows) != 2 || rows[1] != "work +5511988887777 Alex Work connected Conta do trabalho" {
		t.Fatalf("accounts list %q", listed.Stdout)
	}
}

func TestAccountsAddRefusesBadRequestsBeforeContactingWhatsApp(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		stderr string
	}{
		{
			name:   "name in use",
			args:   []string{"accounts", "add", "work", "--phone", "+5531966665555"},
			stderr: "zapgate: the account name \"work\" is already in use\n",
		},
		{
			name:   "uppercase name",
			args:   []string{"accounts", "add", "Work", "--phone", "+5531966665555"},
			stderr: "zapgate: the account name \"Work\" is not valid: use lowercase letters, digits and single hyphens, at most 32 characters\n",
		},
		{
			name:   "underscore",
			args:   []string{"accounts", "add", "my_work", "--phone", "+5531966665555"},
			stderr: "is not valid",
		},
		{
			name:   "name too long",
			args:   []string{"accounts", "add", strings.Repeat("a", 33), "--phone", "+5531966665555"},
			stderr: "is not valid",
		},
		{
			name:   "phone without country code",
			args:   []string{"accounts", "add", "sales", "--phone", "0119888"},
			stderr: "zapgate: the phone number \"0119888\" is not valid: use the international number with the country code, for example +5511988887777\n",
		},
		{
			name:   "phone in use",
			args:   []string{"accounts", "add", "sales", "--phone", "+55 11 98888-7777"},
			stderr: "zapgate: the phone number +5511988887777 already belongs to the account \"work\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			attempts := h.WhatsApp.PairAttempts()

			result := h.CLI(t, tc.args...)

			if result.Code != 1 || !strings.Contains(result.Stderr, tc.stderr) {
				t.Fatalf("exit %d, stderr %q, want %q", result.Code, result.Stderr, tc.stderr)
			}
			if got := h.WhatsApp.PairAttempts(); got != attempts {
				t.Fatalf("the server contacted WhatsApp %d more times", got-attempts)
			}
			if rows := tableRows(t, h.CLI(t, "accounts", "list").Stdout); len(rows) != 2 {
				t.Fatalf("accounts list %q, want only work", rows)
			}
		})
	}
}

func TestAccountsAddWithQRShowsTheQRCode(t *testing.T) {
	h := apptest.Start(t)

	run := h.StartCLI("accounts", "add", "personal", "--phone", "+"+personalPhone, "--qr")
	pairing := h.AwaitPairing(t, personalPhone)
	pairing.Accept("Alex")
	added := run.Wait(t)

	if added.Code != 0 {
		t.Fatalf("exit %d, stderr %q", added.Code, added.Stderr)
	}
	if pairing.QR == "" || pairing.Code != "" {
		t.Fatalf("the server asked for a phone code instead of a QR code: %+v", pairing)
	}
	if !strings.Contains(added.Stdout, "scan this QR code:\n") || !strings.Contains(added.Stdout, "▀") {
		t.Fatalf("stdout %q shows no QR code", added.Stdout)
	}
	if !strings.HasSuffix(added.Stdout, "the account personal is connected (+5521977776666, profile Alex)\n") {
		t.Fatalf("stdout %q", added.Stdout)
	}
}

func TestAnExpiredPairingLeavesNoAccount(t *testing.T) {
	h := apptest.Start(t)

	run := h.StartCLI("accounts", "add", "work", "--phone", "+"+workPhone)
	h.AwaitPairing(t, workPhone).Expire()
	result := run.Wait(t)

	if result.Code != 1 || result.Stderr != "zapgate: the pairing expired before the phone confirmed; run the command again\n" {
		t.Fatalf("exit %d, stderr %q", result.Code, result.Stderr)
	}
	if listed := h.CLI(t, "accounts", "list"); !strings.HasPrefix(listed.Stdout, "no accounts yet") {
		t.Fatalf("accounts list %q", listed.Stdout)
	}
	h.AddAccount(t, "work", workPhone, "Alex")
}

func TestACanceledPairingLeavesNoAccount(t *testing.T) {
	h := apptest.Start(t)
	ctx, cancel := context.WithCancel(context.Background())
	failed := make(chan error, 1)
	go func() {
		_, err := h.Owner.AddAccount(ctx, admin.AddAccountRequest{Name: "work", Phone: workPhone}, func(admin.PairingEvent) {})
		failed <- err
	}()
	h.AwaitPairing(t, workPhone)

	cancel()

	if err := <-failed; err == nil {
		t.Fatal("the canceled pairing reported success")
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.WhatsApp.PendingPairings() > 0 || len(tableRows(t, h.CLI(t, "accounts", "list").Stdout)) > 1 {
		if time.Now().After(deadline) {
			t.Fatal("the canceled pairing is still pending")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
