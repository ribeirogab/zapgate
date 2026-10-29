package app_test

import (
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestPasswordCommandPrintsTheOwnerPassword(t *testing.T) {
	h := apptest.Start(t)

	result := h.CLI(t, "password")

	if result.Code != 0 || result.Stdout != apptest.OwnerPassword+"\n" || result.Stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", result.Code, result.Stdout, result.Stderr)
	}
	actions := linesWithMessage(h.AuditLines(t), "admin")
	if len(actions) != 1 {
		t.Fatalf("audit has %d admin lines, want 1: %s", len(actions), h.AuditRaw())
	}
	assertFields(t, actions[0], map[string]string{"action": "password_show"})
	if strings.Contains(h.AuditRaw(), apptest.OwnerPassword) {
		t.Fatal("the audit log contains the password")
	}
}

func TestPasswordCommandTakesNoArguments(t *testing.T) {
	h := apptest.Start(t)

	result := h.CLI(t, "password", "extra")

	if result.Code != 2 || !strings.HasPrefix(result.Stderr, "usage:\n") || !strings.Contains(result.Stderr, "zapgate password") {
		t.Fatalf("exit %d, stderr %q", result.Code, result.Stderr)
	}
}
