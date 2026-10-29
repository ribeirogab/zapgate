package live_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/ribeirogab/zapgate/internal/sqlite"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

var (
	peerPN  = types.NewJID("5531944443333", types.DefaultUserServer)
	peerLID = types.NewJID("187654321098765", types.HiddenUserServer)
)

func rememberLID(t *testing.T, sessionDB string, lid, pn types.JID) {
	t.Helper()
	ctx := context.Background()
	container, err := sqlstore.New(ctx, sqlite.Driver, sqlite.DSN(sessionDB), waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	defer container.Close()
	if err := container.LIDMap.PutLIDMapping(ctx, lid, pn); err != nil {
		t.Fatal(err)
	}
}

func TestAClientFindsThePhoneNumberOfALIDInTheSessionStore(t *testing.T) {
	sessionDB := filepath.Join(t.TempDir(), "session.db")
	rememberLID(t, sessionDB, peerLID, peerPN)
	port := openPort(t, sessionDB)
	t.Cleanup(func() { port.Close() })
	client, err := port.NewClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	known, err := client.PhoneForLID(context.Background(), types.JID{User: peerLID.User, Server: types.HiddenUserServer, Device: 4})
	if err != nil || known != peerPN {
		t.Fatalf("phone %v, error %v; want %v", known, err, peerPN)
	}
	unknown, err := client.PhoneForLID(context.Background(), types.NewJID("111111111111111", types.HiddenUserServer))
	if err != nil || !unknown.IsEmpty() {
		t.Fatalf("an unknown LID gave %v, error %v", unknown, err)
	}
	if resolved := whatsapp.ResolvePhone(context.Background(), client, peerLID, types.JID{}); resolved != peerPN {
		t.Fatalf("resolved %v", resolved)
	}
}

func TestAnUnpairedClientHasNoContactsNorGroups(t *testing.T) {
	port := openPort(t, filepath.Join(t.TempDir(), "session.db"))
	t.Cleanup(func() { port.Close() })
	client, err := port.NewClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	_, contactErr := client.Contact(context.Background(), peerPN)
	_, contactsErr := client.Contacts(context.Background())
	_, groupErr := client.GroupInfo(context.Background(), types.NewJID("120363012345678901", types.GroupServer))

	for name, err := range map[string]error{"Contact": contactErr, "Contacts": contactsErr, "GroupInfo": groupErr} {
		if !errors.Is(err, whatsapp.ErrNotPaired) {
			t.Errorf("%s: error %v, want ErrNotPaired", name, err)
		}
	}
}
