package accounts

import (
	"context"
	"errors"
	"time"

	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	PairingTimeout = 3 * time.Minute
	SettingsWait   = 15 * time.Second
)

type NewAccount struct {
	Name        string
	Phone       string
	Description string
	QR          bool
}

type Progress struct {
	Code string
	QR   string
}

type Pairing struct {
	m       *Manager
	account store.Account
	request whatsapp.PairRequest
}

func (m *Manager) BeginPairing(ctx context.Context, n NewAccount) (*Pairing, error) {
	if err := ValidateName(n.Name); err != nil {
		return nil, err
	}
	digits, err := phoneDigits(n.Phone)
	if err != nil {
		return nil, err
	}
	if err := validateDescription(n.Description); err != nil {
		return nil, err
	}
	if err := m.awaitReady(ctx); err != nil {
		return nil, err
	}
	account, err := m.deps.Store.CreateAccount(ctx, store.NewAccount{Name: n.Name, Phone: "+" + digits, Description: n.Description, CreatedAt: m.deps.Clock.Now()})
	var phoneInUse *store.PhoneInUseError
	switch {
	case errors.Is(err, store.ErrNameInUse):
		return nil, failure(Conflict, "the account name %q is already in use", n.Name)
	case errors.As(err, &phoneInUse):
		return nil, failure(Conflict, "the phone number %s already belongs to the account %q", phoneInUse.Phone, phoneInUse.Account)
	case err != nil:
		return nil, err
	}
	return &Pairing{m: m, account: account, request: whatsapp.PairRequest{Phone: digits, QR: n.QR}}, nil
}

func (m *Manager) awaitReady(ctx context.Context) error {
	select {
	case <-m.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	if m.isStopped() {
		return failure(Unavailable, "the server is stopping; try again when it runs")
	}
	return nil
}

func (p *Pairing) Run(ctx context.Context, report func(Progress)) (store.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, PairingTimeout)
	defer cancel()
	acc, updates, err := p.start(ctx)
	if err != nil {
		p.abandon(ctx, acc)
		return store.Account{}, err
	}
	if err := p.wait(ctx, acc, updates, report); err != nil {
		if !acc.state().paired {
			p.abandon(ctx, acc)
			return store.Account{}, err
		}
		return store.Account{}, failure(Unavailable, "the phone confirmed, but the account %s is not connected yet; check it with: zapgate status", p.account.Name)
	}
	p.m.deps.Changed(context.WithoutCancel(ctx))
	return p.m.deps.Store.Account(context.WithoutCancel(ctx), p.account.ID)
}

func (p *Pairing) start(ctx context.Context) (*liveAccount, <-chan whatsapp.PairUpdate, error) {
	life, cancel := context.WithCancel(p.m.life)
	client, err := p.m.deps.WhatsApp.NewClient(life)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	acc := p.m.attach(p.account, client, life, cancel)
	updates, err := client.Pair(ctx, p.request)
	return acc, updates, err
}

func (p *Pairing) wait(ctx context.Context, acc *liveAccount, updates <-chan whatsapp.PairUpdate, report func(Progress)) error {
	var settingsLate <-chan time.Time
	for {
		select {
		case update, ok := <-updates:
			switch {
			case !ok:
				updates = nil
				if !acc.state().paired {
					return pairingError(whatsapp.ErrPairingExpired)
				}
			case update.Err != nil:
				return pairingError(update.Err)
			default:
				report(Progress{Code: update.Code, QR: update.QR})
			}
		case <-acc.wake:
			state := acc.state()
			switch {
			case state.connected && state.settingsSynced:
				return nil
			case state.connected && settingsLate == nil:
				settingsLate = p.m.deps.Clock.After(SettingsWait)
			case state.loggedOut:
				return failure(Unavailable, "WhatsApp logged out the new device during the pairing; run the command again")
			}
		case <-settingsLate:
			return nil
		case <-acc.life.Done():
			return failure(Unavailable, "the pairing stopped: the account was removed or the server is stopping")
		case <-ctx.Done():
			return pairingError(ctx.Err())
		}
	}
}

func pairingError(err error) error {
	switch {
	case errors.Is(err, whatsapp.ErrPairingExpired):
		return failure(Unavailable, "the pairing expired before the phone confirmed; run the command again")
	case errors.Is(err, context.DeadlineExceeded):
		return failure(Unavailable, "the phone did not confirm within %s; run the command again", PairingTimeout)
	case errors.Is(err, context.Canceled):
		return failure(Unavailable, "the pairing was canceled")
	}
	return failure(Unavailable, "the pairing failed: %v", err)
}

func (p *Pairing) abandon(ctx context.Context, acc *liveAccount) {
	if acc != nil && p.m.detach(acc.id) == acc {
		acc.client.Disconnect()
	}
	if err := p.m.deps.Store.DeleteAccount(context.WithoutCancel(ctx), p.account.ID); err != nil {
		p.m.deps.Log.Error("cannot delete an unfinished pairing", "account", p.account.Name, "error", err)
	}
}
