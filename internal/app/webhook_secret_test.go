package app_test

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestWebhooksRotateSecretShowsTheNewSecretOnce(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	old := addWebhook(t, h, rx, "support-bot", "all")

	rotated := h.CLI(t, "webhooks", "rotate-secret", "support-bot")

	if rotated.Code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", rotated.Code, rotated.Stdout, rotated.Stderr)
	}
	secret := secretFrom(t, rotated.Stdout)
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if !strings.HasPrefix(secret, "whsec_") || err != nil || len(key) != 32 {
		t.Fatalf("secret %q is not whsec_ plus the base64 of 32 bytes", secret)
	}
	if secret == old {
		t.Fatal("rotate-secret kept the old secret")
	}
	want := "rotated the secret of the webhook support-bot; the old secret also signs until 2026-10-14T12:00:00Z\n" +
		"secret: " + secret + "\n" +
		"this is the only time the secret is shown; store it now\n"
	if rotated.Stdout != want {
		t.Fatalf("stdout %q, want %q", rotated.Stdout, want)
	}
	listed := h.CLI(t, "webhooks", "list")
	if strings.Contains(listed.Stdout, secret) || strings.Contains(listed.Stdout, "whsec_") {
		t.Fatalf("webhooks list shows a secret: %q", listed.Stdout)
	}
}

func TestWebhooksRotateSecretOfAnUnknownWebhook(t *testing.T) {
	h := apptest.Start(t)

	rotated := h.CLI(t, "webhooks", "rotate-secret", "ghost")

	if rotated.Code != 1 || rotated.Stderr != "zapgate: there is no webhook named \"ghost\"\n" || rotated.Stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", rotated.Code, rotated.Stdout, rotated.Stderr)
	}
}

func rotateSecret(t *testing.T, h *apptest.Harness, rx *apptest.Receiver, name string) string {
	t.Helper()
	rotated := h.CLI(t, "webhooks", "rotate-secret", name)
	if rotated.Code != 0 {
		t.Fatalf("rotate the secret of %s: exit %d, stderr %q", name, rotated.Code, rotated.Stderr)
	}
	secret := secretFrom(t, rotated.Stdout)
	rx.Trust(secret)
	return secret
}

func sendTest(t *testing.T, h *apptest.Harness, name string) {
	t.Helper()
	if tested := h.CLI(t, "webhooks", "test", name); tested.Code != 0 {
		t.Fatalf("test the webhook %s: exit %d, stderr %q", name, tested.Code, tested.Stderr)
	}
}

func requireSigners(t *testing.T, got []apptest.Received, secrets []string, want [][]string) {
	t.Helper()
	for i, signers := range want {
		if signedBy := got[i].SignedBy(secrets...); !slices.Equal(signedBy, signers) {
			t.Fatalf("POST %d is signed by %q, want %q", i+1, signedBy, signers)
		}
	}
}

func TestARotatedSecretSignsWithTheOldSecretForADay(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	old := addWebhook(t, h, rx, "support-bot", "all")
	fresh := rotateSecret(t, h, rx, "support-bot")

	sendTest(t, h, "support-bot")
	h.Clock.Advance(24*time.Hour - time.Second)
	sendTest(t, h, "support-bot")
	h.Clock.Advance(time.Second)
	sendTest(t, h, "support-bot")

	requireSigners(t, rx.Await(t, 3), []string{fresh, old}, [][]string{{fresh, old}, {fresh, old}, {fresh}})
}

func TestASecondRotationKeepsOnlyTheSecretBeforeIt(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	first := addWebhook(t, h, rx, "support-bot", "all")
	second := rotateSecret(t, h, rx, "support-bot")
	h.Clock.Advance(time.Hour)
	third := rotateSecret(t, h, rx, "support-bot")

	sendTest(t, h, "support-bot")
	h.Clock.Advance(23 * time.Hour)
	sendTest(t, h, "support-bot")
	h.Clock.Advance(time.Hour)
	sendTest(t, h, "support-bot")

	requireSigners(t, rx.Await(t, 3), []string{third, second, first}, [][]string{{third, second}, {third, second}, {third}})
}

func TestRotateSecretIsAuditedWithoutTheSecret(t *testing.T) {
	h := apptest.Start(t)
	rx := h.StartReceiver(t)
	old := addWebhook(t, h, rx, "support-bot", "all")
	fresh := rotateSecret(t, h, rx, "support-bot")

	actions := linesWithMessage(h.AuditLines(t), "admin")

	if len(actions) != 2 {
		t.Fatalf("audit has %d admin lines, want 2: %s", len(actions), h.AuditRaw())
	}
	assertFields(t, actions[1], map[string]string{"action": "webhook_rotate_secret", "webhook": "support-bot"})
	for _, secret := range []string{old, fresh, "whsec_"} {
		if strings.Contains(h.AuditRaw(), secret) {
			t.Fatalf("the audit log contains the secret %q", secret)
		}
	}
}
