package app_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestRestartKeepsTheAccess(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "bot", nil, false)
	open := h.Connect(t, token, apptest.WithProtocol(apptest.LegacyProtocol))
	callTool(t, open, "list_accounts", nil)

	started := time.Now()
	h.Restart(t)
	h.Restart(t)

	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("two restarts with an open session took %s", elapsed)
	}
	for _, path := range protocolPaths {
		session := h.Connect(t, token, apptest.WithProtocol(path.protocol))
		if result := callTool(t, session, "list_accounts", nil); result.IsError {
			t.Fatalf("%s: list_accounts failed after restart", path.name)
		}
	}
	if listed := h.CLI(t, "access", "list"); listed.Code != 0 {
		t.Fatalf("access list after restart: %q", listed.Stderr)
	}
}

func TestAppOpensTheWhatsAppPortInTheDataDir(t *testing.T) {
	h := apptest.Start(t)

	if got, want := h.WhatsApp.SessionDB(), filepath.Join(h.DataDir, "session.db"); got != want {
		t.Fatalf("session db %q, want %q", got, want)
	}
	if got := h.WhatsApp.OpenHandles(); got != 1 {
		t.Fatalf("open handles %d while the app runs, want 1", got)
	}
}
