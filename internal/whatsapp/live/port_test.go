package live_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
	"github.com/ribeirogab/zapgate/internal/whatsapp/live"
)

func openPort(t *testing.T, sessionDB string) whatsapp.Port {
	t.Helper()
	port, err := live.Opener(slog.New(slog.NewJSONHandler(io.Discard, nil)))(context.Background(), sessionDB)
	if err != nil {
		t.Fatalf("open the session store: %v", err)
	}
	return port
}

func TestThePortKeepsItsSessionStoreInTheGivenFile(t *testing.T) {
	sessionDB := filepath.Join(t.TempDir(), "session.db")

	port := openPort(t, sessionDB)

	if err := port.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(sessionDB); err != nil {
		t.Fatalf("no session store at %s: %v", sessionDB, err)
	}
	if err := openPort(t, sessionDB).Close(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

func TestANewClientHasNoDeviceUntilItPairs(t *testing.T) {
	port := openPort(t, filepath.Join(t.TempDir(), "session.db"))
	t.Cleanup(func() { port.Close() })

	client, err := port.NewClient(context.Background())

	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if client.Device() != "" || client.ProfileName() != "" {
		t.Fatalf("new client has device %q and profile %q", client.Device(), client.ProfileName())
	}
	if err := client.Logout(context.Background()); err != nil {
		t.Fatalf("logout of an unpaired client: %v", err)
	}
}

func TestAnUnknownDeviceIsReportedAsMissing(t *testing.T) {
	port := openPort(t, filepath.Join(t.TempDir(), "session.db"))
	t.Cleanup(func() { port.Close() })

	_, err := port.Client(context.Background(), "5511988887777:7@s.whatsapp.net")

	if !errors.Is(err, whatsapp.ErrNoDevice) {
		t.Fatalf("error %v, want ErrNoDevice", err)
	}
}
