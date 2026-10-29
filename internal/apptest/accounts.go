package apptest

import (
	"context"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/whatsapp/fake"
)

const waitTimeout = 5 * time.Second

type CLIRun struct {
	done chan CLIResult
}

func (h *Harness) StartCLI(args ...string) *CLIRun {
	return h.StartCLIWithInput("", args...)
}

func (h *Harness) StartCLIWithInput(stdin string, args ...string) *CLIRun {
	run := &CLIRun{done: make(chan CLIResult, 1)}
	go func() { run.done <- h.runCLI(stdin, args) }()
	return run
}

func (r *CLIRun) Wait(t testing.TB) CLIResult {
	t.Helper()
	select {
	case result := <-r.done:
		return result
	case <-time.After(waitTimeout):
		t.Fatal("the command did not finish")
		return CLIResult{}
	}
}

func (r *CLIRun) WaitWhile(t testing.TB, tick func()) CLIResult {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case result := <-r.done:
			return result
		case <-deadline:
			t.Fatal("the command did not finish")
			return CLIResult{}
		case <-time.After(5 * time.Millisecond):
			tick()
		}
	}
}

func (h *Harness) AwaitPairing(t testing.TB, phone string) *fake.Pairing {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	pairing, err := h.WhatsApp.AwaitPairing(ctx, phone)
	if err != nil {
		t.Fatalf("await the pairing of %s: %v", phone, err)
	}
	return pairing
}

func (h *Harness) AddAccount(t testing.TB, name, phone, profile string) {
	t.Helper()
	run := h.StartCLI("accounts", "add", name, "--phone", "+"+phone)
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	pairing, err := h.WhatsApp.AwaitPairing(ctx, phone)
	if err != nil {
		result := run.Wait(t)
		t.Fatalf("add the account %s: %v; exit %d, stdout %q, stderr %q", name, err, result.Code, result.Stdout, result.Stderr)
	}
	pairing.Accept(profile)
	if result := run.Wait(t); result.Code != 0 {
		t.Fatalf("add the account %s: exit %d, stderr %q", name, result.Code, result.Stderr)
	}
}

func (h *Harness) AwaitAccountState(t testing.TB, name, state string) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		account, err := h.Owner.Account(context.Background(), name)
		if err == nil && account.State == state {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the account %s has state %q (error %v), want %q", name, account.State, err, state)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *Harness) awaitReconnection(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !h.WhatsApp.Reconnected() {
		if time.Now().After(deadline) {
			t.Fatal("the paired accounts did not reconnect after the start")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
