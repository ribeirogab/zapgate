package live

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/sqlite"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	deviceName     = "zapgate"
	sessionLogName = "whatsmeow"
)

var deviceVersion = [3]uint32{1, 0, 0}

var describeDevice = sync.OnceFunc(func() {
	store.SetOSInfo(deviceName, deviceVersion)
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_CHROME.Enum()
})

type Port struct {
	container *sqlstore.Container
	log       *slog.Logger
}

func Opener(log *slog.Logger) whatsapp.Opener {
	return func(ctx context.Context, sessionDB string) (whatsapp.Port, error) {
		describeDevice()
		container, err := sqlstore.New(ctx, sqlite.Driver, sqlite.DSN(sessionDB), newLogger(log, sessionLogName+"/store"))
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", sessionDB, err)
		}
		return &Port{container: container, log: log}, nil
	}
}

func (p *Port) NewClient(life context.Context) (whatsapp.Client, error) {
	return p.newClient(life, p.container.NewDevice()), nil
}

func (p *Port) Client(life context.Context, device string) (whatsapp.Client, error) {
	jid, err := types.ParseJID(device)
	if err != nil {
		return nil, fmt.Errorf("parse the device %q: %w", device, err)
	}
	stored, err := p.container.GetDevice(life, jid)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("%w: %s", whatsapp.ErrNoDevice, device)
	}
	return p.newClient(life, stored), nil
}

func (p *Port) newClient(life context.Context, device *store.Device) *client {
	cli := whatsmeow.NewClient(device, newLogger(p.log, sessionLogName))
	cli.BackgroundEventCtx = life
	cli.InitialAutoReconnect = true
	return &client{cli: cli, life: life, lids: p.container.LIDMap}
}

func (p *Port) Close() error {
	return p.container.Close()
}
