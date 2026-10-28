package whatsapp

import (
	"context"
	"errors"

	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type Port interface {
	NewClient(life context.Context) (Client, error)
	Client(life context.Context, device string) (Client, error)
	Close() error
}

type EventHandler func(evt any)

type Client interface {
	Device() string
	ProfileName() string
	AddEventHandler(handler EventHandler)
	Connect() error
	Pair(ctx context.Context, req PairRequest) (<-chan PairUpdate, error)
	Logout(ctx context.Context) error
	Disconnect()
	Directory
	History
	Writer
	Media
}

type History interface {
	ParseWebMessage(chat types.JID, msg *waWeb.WebMessageInfo) (*events.Message, error)
	RequestOlderHistory(ctx context.Context, oldest types.MessageInfo, count int) error
}

type Directory interface {
	Contact(ctx context.Context, jid types.JID) (types.ContactInfo, error)
	Contacts(ctx context.Context) (map[types.JID]types.ContactInfo, error)
	GroupInfo(ctx context.Context, group types.JID) (*types.GroupInfo, error)
	PhoneForLID(ctx context.Context, lid types.JID) (types.JID, error)
}

type PairRequest struct {
	Phone string
	QR    bool
}

type PairUpdate struct {
	Code string
	QR   string
	Err  error
}

var (
	ErrNoDevice            = errors.New("the session store has no such device")
	ErrNotPaired           = errors.New("the account has no paired device")
	ErrPairingExpired      = errors.New("the pairing window closed before the phone confirmed")
	ErrUnlinkedLocallyOnly = errors.New("WhatsApp did not confirm the logout; only the local session was deleted")
)

type Opener func(ctx context.Context, sessionDB string) (Port, error)
