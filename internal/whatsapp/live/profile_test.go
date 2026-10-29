package live_test

import (
	"context"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/ribeirogab/zapgate/internal/sqlite"
)

const ownDevice = "5511988887777:12@s.whatsapp.net"

type storedNames struct {
	push     string
	business string
}

func rememberDevice(t *testing.T, sessionDB string, names storedNames) {
	t.Helper()
	ctx := context.Background()
	container, err := sqlstore.New(ctx, sqlite.Driver, sqlite.DSN(sessionDB), waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	defer container.Close()
	device := container.NewDevice()
	jid := types.NewADJID("5511988887777", 0, 12)
	device.ID = &jid
	device.PushName = names.push
	device.BusinessName = names.business
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{1},
		AccountSignature:    make([]byte, 64),
		AccountSignatureKey: make([]byte, 32),
		DeviceSignature:     make([]byte, 64),
	}
	if err := container.PutDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
}

func TestAPairedClientReportsTheOwnProfileName(t *testing.T) {
	cases := []struct {
		name   string
		stored storedNames
		want   string
	}{
		{name: "push name", stored: storedNames{push: "Alex", business: "Loja do Alex"}, want: "Alex"},
		{name: "business name before the settings sync", stored: storedNames{business: "Loja do Alex"}, want: "Loja do Alex"},
		{name: "no name yet", stored: storedNames{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionDB := filepath.Join(t.TempDir(), "session.db")
			rememberDevice(t, sessionDB, tc.stored)
			port := openPort(t, sessionDB)
			t.Cleanup(func() { port.Close() })

			client, err := port.Client(context.Background(), ownDevice)

			if err != nil {
				t.Fatalf("client: %v", err)
			}
			if got := client.ProfileName(); got != tc.want {
				t.Fatalf("profile name %q, want %q", got, tc.want)
			}
		})
	}
}
