package accounts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const LogoutTimeout = 20 * time.Second

type Deps struct {
	Store    *store.Store
	WhatsApp whatsapp.Port
	Clock    clock.Clock
	Log      *slog.Logger
	Changed  func(ctx context.Context)
	Ingest   func(ctx context.Context, accountID int64, client whatsapp.Client, evt any)
	Removed  func(ctx context.Context, account store.Account) error
}

type Manager struct {
	deps  Deps
	life  context.Context
	stop  context.CancelFunc
	ready chan struct{}

	mu      sync.Mutex
	live    map[int64]*liveAccount
	stopped bool
}

func New(deps Deps) *Manager {
	life, stop := context.WithCancel(context.Background())
	return &Manager{deps: deps, life: life, stop: stop, ready: make(chan struct{}), live: map[int64]*liveAccount{}}
}

func (m *Manager) Run(ctx context.Context) error {
	m.reconnectAll(ctx)
	close(m.ready)
	<-ctx.Done()
	m.stopAll()
	return nil
}

func (m *Manager) List(ctx context.Context) ([]store.Account, error) {
	return m.deps.Store.ListAccounts(ctx)
}

func (m *Manager) Get(ctx context.Context, name string) (store.Account, error) {
	account, err := m.deps.Store.AccountByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return store.Account{}, notFound(name)
	}
	return account, err
}

func (m *Manager) Describe(ctx context.Context, name, description string) (store.Account, error) {
	if err := validateDescription(description); err != nil {
		return store.Account{}, err
	}
	account, err := m.deps.Store.SetAccountDescription(ctx, name, description)
	if errors.Is(err, store.ErrNotFound) {
		return store.Account{}, notFound(name)
	}
	return account, err
}

func (m *Manager) SetRetention(ctx context.Context, name string, months int) (store.Account, error) {
	account, err := m.deps.Store.SetAccountRetention(ctx, name, months)
	if errors.Is(err, store.ErrNotFound) {
		return store.Account{}, notFound(name)
	}
	return account, err
}

type Removal struct {
	Account store.Account
	Warning string
}

func (m *Manager) Remove(ctx context.Context, name string) (Removal, error) {
	account, err := m.Get(ctx, name)
	if err != nil {
		return Removal{}, err
	}
	ctx = context.WithoutCancel(ctx)
	removal := Removal{Account: account}
	if err := m.unlinkWithin(ctx, account); err != nil {
		removal.Warning = fmt.Sprintf("the phone may still list this device; remove it in WhatsApp > Linked devices (%v)", err)
	}
	if err := m.deps.Store.DeleteAccount(ctx, account.ID); err != nil {
		return Removal{}, err
	}
	if err := m.deps.Removed(ctx, account); err != nil {
		m.deps.Log.Error("cannot delete the local files of a removed account", "account", account.Name, "error", err)
	}
	m.deps.Changed(ctx)
	return removal, nil
}

func (m *Manager) unlinkWithin(ctx context.Context, account store.Account) error {
	ctx, cancel := context.WithTimeout(ctx, LogoutTimeout)
	defer cancel()
	return m.unlink(ctx, account)
}

func (m *Manager) unlink(ctx context.Context, account store.Account) error {
	if acc := m.detach(account.ID); acc != nil {
		return logout(ctx, acc.client)
	}
	if account.JID == "" {
		return nil
	}
	client, err := m.deps.WhatsApp.Client(ctx, account.JID)
	if errors.Is(err, whatsapp.ErrNoDevice) {
		return nil
	}
	if err != nil {
		return err
	}
	return logout(ctx, client)
}

func logout(ctx context.Context, client whatsapp.Client) error {
	err := client.Logout(ctx)
	if err != nil {
		client.Disconnect()
	}
	return err
}

func (m *Manager) reconnectAll(ctx context.Context) {
	if err := m.deps.Store.DeleteUnfinishedPairings(ctx); err != nil {
		m.deps.Log.Warn("cannot delete unfinished pairings", "error", err)
	}
	accounts, err := m.deps.Store.ListAccounts(ctx)
	if err != nil {
		m.deps.Log.Error("cannot list the accounts to reconnect", "error", err)
		return
	}
	for _, account := range accounts {
		if account.JID != "" {
			m.reconnect(ctx, account)
		}
	}
}

func (m *Manager) reconnect(ctx context.Context, account store.Account) {
	life, cancel := context.WithCancel(m.life)
	client, err := m.deps.WhatsApp.Client(life, account.JID)
	if errors.Is(err, whatsapp.ErrNoDevice) {
		cancel()
		m.deps.Log.Warn("the WhatsApp session of an account is gone", "account", account.Name)
		m.save(m.deps.Store.SetAccountLoggedOut(ctx, account.ID))
		return
	}
	if err != nil {
		cancel()
		m.deps.Log.Error("cannot open the WhatsApp session of an account", "account", account.Name, "error", err)
		return
	}
	m.save(m.deps.Store.SetAccountState(ctx, account.ID, store.AccountDisconnected))
	acc := m.attach(account, client, life, cancel)
	acc.mark(func(s *pairState) { s.paired = true })
	if err := client.Connect(); err != nil {
		m.deps.Log.Warn("cannot connect an account", "account", account.Name, "error", err)
	}
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	m.stopped = true
	live := m.live
	m.live = map[int64]*liveAccount{}
	m.mu.Unlock()
	m.stop()
	for _, acc := range live {
		acc.client.Disconnect()
	}
}

func (m *Manager) attach(account store.Account, client whatsapp.Client, life context.Context, cancel context.CancelFunc) *liveAccount {
	acc := &liveAccount{id: account.ID, name: account.Name, client: client, life: life, cancel: cancel, wake: make(chan struct{}, 1)}
	client.AddEventHandler(func(evt any) { m.handle(acc, evt) })
	m.mu.Lock()
	defer m.mu.Unlock()
	m.live[account.ID] = acc
	return acc
}

func (m *Manager) detach(id int64) *liveAccount {
	m.mu.Lock()
	acc := m.live[id]
	delete(m.live, id)
	m.mu.Unlock()
	if acc != nil {
		acc.cancel()
	}
	return acc
}

func (m *Manager) isLive(acc *liveAccount) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live[acc.id] == acc
}

func (m *Manager) handle(acc *liveAccount, evt any) {
	if !m.isLive(acc) {
		return
	}
	ctx := context.Background()
	switch e := evt.(type) {
	case *events.PairSuccess:
		m.save(m.deps.Store.SetAccountPaired(ctx, acc.id, e.ID.String(), "+"+e.ID.User))
		acc.mark(func(s *pairState) { s.paired = true })
	case *events.Connected:
		m.save(m.deps.Store.SetAccountState(ctx, acc.id, store.AccountConnected))
		m.saveProfileName(ctx, acc, acc.client.ProfileName())
		acc.mark(func(s *pairState) { s.connected = true })
	case *events.Disconnected, *events.StreamReplaced, *events.TemporaryBan:
		if acc.state().paired {
			m.save(m.deps.Store.SetAccountState(ctx, acc.id, store.AccountDisconnected))
		}
	case *events.LoggedOut:
		m.deps.Log.Warn("WhatsApp logged out an account", "account", acc.name, "reason", e.Reason.String())
		m.save(m.deps.Store.SetAccountLoggedOut(ctx, acc.id))
		m.detach(acc.id)
		acc.mark(func(s *pairState) { s.loggedOut = true })
	case *events.PushNameSetting:
		m.saveProfileName(ctx, acc, e.Action.GetName())
		acc.mark(func(s *pairState) { s.settingsSynced = true })
	case *events.AppStateSyncComplete:
		if e.Name == appstate.WAPatchCriticalBlock {
			m.saveProfileName(ctx, acc, acc.client.ProfileName())
			acc.mark(func(s *pairState) { s.settingsSynced = true })
		}
	}
	if m.deps.Ingest != nil {
		m.deps.Ingest(acc.life, acc.id, acc.client, evt)
	}
}

func (m *Manager) Client(accountID int64) (whatsapp.Client, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.live[accountID]
	if !ok || acc.client.Device() == "" {
		return nil, false
	}
	return acc.client, true
}

func (m *Manager) saveProfileName(ctx context.Context, acc *liveAccount, name string) {
	if name != "" {
		m.save(m.deps.Store.SetAccountProfileName(ctx, acc.id, name))
	}
}

func (m *Manager) save(err error) {
	if err != nil {
		m.deps.Log.Error("cannot save the state of an account", "error", err)
	}
}

func (m *Manager) isStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

type pairState struct {
	paired         bool
	connected      bool
	settingsSynced bool
	loggedOut      bool
}

type liveAccount struct {
	id     int64
	name   string
	client whatsapp.Client
	life   context.Context
	cancel context.CancelFunc
	wake   chan struct{}

	mu    sync.Mutex
	flags pairState
}

func (a *liveAccount) mark(change func(s *pairState)) {
	a.mu.Lock()
	change(&a.flags)
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *liveAccount) state() pairState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.flags
}
