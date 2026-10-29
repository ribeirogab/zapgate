package fake

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

var (
	errClosedHandle   = errors.New("the WhatsApp port handle is closed")
	errAlreadyPaired  = errors.New("the client already has a device")
	errNotPaired      = errors.New("the client has no device")
	errDeviceDeleted  = errors.New("the device was deleted from the session store")
	errUnknownPairing = errors.New("no pairing is waiting for this phone")
	errOffline        = errors.New("websocket not connected")
	errNoGroup        = errors.New("that group does not exist")
)

type World struct {
	mu           sync.Mutex
	devices      map[string]*device
	clients      map[*Client]bool
	pairings     map[string]*Pairing
	lids         map[types.JID]types.JID
	arrived      chan struct{}
	deviceSeq    int
	pairAttempts int
	opens        int
	open         int
	sessionDB    string
	outboxes     map[string]*outbox
	clock        clock.Clock
	cdn          cdn
}

type device struct {
	phone    string
	jid      types.JID
	profile  string
	local    bool
	linked   bool
	online   bool
	contacts map[types.JID]types.ContactInfo
	groups   map[types.JID]types.GroupInfo

	historyRequests []HistoryRequest
}

func New() *World {
	return &World{
		devices:  map[string]*device{},
		clients:  map[*Client]bool{},
		pairings: map[string]*Pairing{},
		lids:     map[types.JID]types.JID{},
		arrived:  make(chan struct{}),
	}
}

func (w *World) Opener() whatsapp.Opener {
	return func(_ context.Context, sessionDB string) (whatsapp.Port, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.opens++
		w.open++
		w.sessionDB = sessionDB
		return &Port{world: w}, nil
	}
}

func (w *World) Opens() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.opens
}

func (w *World) OpenHandles() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.open
}

func (w *World) SessionDB() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sessionDB
}

func (w *World) PairAttempts() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pairAttempts
}

func (w *World) PendingPairings() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pairings)
}

func (w *World) Linked(phone string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	dev, ok := w.devices[phone]
	return ok && dev.linked
}

func (w *World) HasSession(phone string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	dev, ok := w.devices[phone]
	return ok && dev.local
}

func (w *World) AwaitPairing(ctx context.Context, phone string) (*Pairing, error) {
	for {
		w.mu.Lock()
		pairing, ok := w.pairings[phone]
		arrived := w.arrived
		w.mu.Unlock()
		if ok {
			return pairing, nil
		}
		select {
		case <-arrived:
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %s", errUnknownPairing, phone)
		}
	}
}

func (w *World) DropConnection(phone string) {
	w.update(phone, func(dev *device) any {
		dev.online = false
		return &events.Disconnected{}
	})
}

func (w *World) RestoreConnection(phone string) {
	w.update(phone, func(dev *device) any {
		dev.online = true
		return &events.Connected{}
	})
}

func (w *World) SetProfileName(phone, name string) {
	w.update(phone, func(dev *device) any {
		dev.profile = name
		return &events.PushNameSetting{Action: &waSyncAction.PushNameSetting{Name: &name}}
	})
}

func (w *World) SyncSettings(phone, name string) {
	w.update(phone, func(dev *device) any {
		dev.profile = name
		return &events.AppStateSyncComplete{Name: appstate.WAPatchCriticalBlock}
	})
}

func (w *World) UnlinkDevice(phone string) {
	w.update(phone, func(dev *device) any {
		dev.linked = false
		if len(w.clientsOf(dev.jid)) == 0 {
			return nil
		}
		dev.local = false
		return &events.LoggedOut{Reason: events.ConnectFailureLoggedOut}
	})
}

func (w *World) update(phone string, change func(dev *device) any) {
	w.mu.Lock()
	dev, ok := w.devices[phone]
	if !ok {
		w.mu.Unlock()
		return
	}
	evt := change(dev)
	clients := w.clientsOf(dev.jid)
	w.mu.Unlock()
	if evt == nil {
		return
	}
	for _, client := range clients {
		client.dispatch(evt)
	}
}

func (w *World) clientsOf(jid types.JID) []*Client {
	var found []*Client
	for client := range w.clients {
		if client.device() == jid.String() {
			found = append(found, client)
		}
	}
	return found
}

func (w *World) deviceByJID(jid string) *device {
	for _, dev := range w.devices {
		if dev.jid.String() == jid {
			return dev
		}
	}
	return nil
}

func (w *World) attach(c *Client) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.clients[c] = true
}

func (w *World) detach(c *Client) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.clients, c)
}

func (w *World) connect(c *Client) error {
	w.mu.Lock()
	dev := w.deviceByJID(c.device())
	if dev == nil || !dev.local {
		w.mu.Unlock()
		return errDeviceDeleted
	}
	w.clients[c] = true
	var evt any
	switch {
	case !dev.linked:
		dev.local = false
		evt = &events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureLoggedOut}
	case dev.online:
		evt = &events.Connected{}
	default:
		evt = &events.Disconnected{}
	}
	w.mu.Unlock()
	go c.dispatchConnection(evt)
	return nil
}

func (w *World) Reconnected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, dev := range w.devices {
		if dev.local && !w.hasSettledClient(dev.jid) {
			return false
		}
	}
	return true
}

func (w *World) hasSettledClient(jid types.JID) bool {
	for _, client := range w.clientsOf(jid) {
		if client.settled {
			return true
		}
	}
	return false
}

func (w *World) logout(c *Client) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.clients, c)
	dev := w.deviceByJID(c.device())
	if dev == nil {
		return nil
	}
	dev.local = false
	if !dev.online {
		return whatsapp.ErrUnlinkedLocallyOnly
	}
	dev.linked = false
	return nil
}

func (w *World) startPairing(ctx context.Context, c *Client, req whatsapp.PairRequest) *Pairing {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pairAttempts++
	p := &Pairing{world: w, client: c, Phone: req.Phone, updates: make(chan whatsapp.PairUpdate, 4)}
	if req.QR {
		p.QR = fmt.Sprintf("2@fake-qr-%d", w.pairAttempts)
		p.updates <- whatsapp.PairUpdate{QR: p.QR}
	} else {
		p.Code = fmt.Sprintf("FAKE-%04d", w.pairAttempts)
		p.updates <- whatsapp.PairUpdate{Code: p.Code}
	}
	w.pairings[req.Phone] = p
	w.clients[c] = true
	close(w.arrived)
	w.arrived = make(chan struct{})
	go p.endWith(ctx)
	return p
}

func (w *World) finishPairing(p *Pairing) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p.finished {
		return false
	}
	p.finished = true
	if w.pairings[p.Phone] == p {
		delete(w.pairings, p.Phone)
	}
	return true
}

func (w *World) linkDevice(p *Pairing) types.JID {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deviceSeq++
	jid := types.NewADJID(p.Phone, 0, uint8(w.deviceSeq))
	w.devices[p.Phone] = &device{
		phone:    p.Phone,
		jid:      jid,
		local:    true,
		linked:   true,
		online:   true,
		contacts: map[types.JID]types.ContactInfo{},
		groups:   map[types.JID]types.GroupInfo{},
	}
	return jid
}

type Pairing struct {
	world    *World
	client   *Client
	updates  chan whatsapp.PairUpdate
	finished bool
	Phone    string
	Code     string
	QR       string
}

func (p *Pairing) Accept(profile string) {
	if p.Confirm() {
		p.world.SyncSettings(p.Phone, profile)
	}
}

func (p *Pairing) Confirm() bool {
	if !p.world.finishPairing(p) {
		return false
	}
	jid := p.world.linkDevice(p)
	p.client.setDevice(jid.String())
	p.client.dispatch(&events.PairSuccess{ID: jid})
	close(p.updates)
	p.client.dispatch(&events.Connected{})
	return true
}

func (p *Pairing) Expire() {
	if !p.world.finishPairing(p) {
		return
	}
	p.updates <- whatsapp.PairUpdate{Err: whatsapp.ErrPairingExpired}
	close(p.updates)
	p.world.detach(p.client)
}

func (p *Pairing) endWith(ctx context.Context) {
	<-ctx.Done()
	if p.world.finishPairing(p) {
		close(p.updates)
		p.world.detach(p.client)
	}
}

type Port struct {
	world  *World
	mu     sync.Mutex
	closed bool
}

func (p *Port) NewClient(context.Context) (whatsapp.Client, error) {
	if err := p.ensureOpen(); err != nil {
		return nil, err
	}
	return &Client{world: p.world, port: p}, nil
}

func (p *Port) Client(_ context.Context, jid string) (whatsapp.Client, error) {
	if err := p.ensureOpen(); err != nil {
		return nil, err
	}
	p.world.mu.Lock()
	dev := p.world.deviceByJID(jid)
	p.world.mu.Unlock()
	if dev == nil || !dev.local {
		return nil, fmt.Errorf("%w: %s", whatsapp.ErrNoDevice, jid)
	}
	return &Client{world: p.world, port: p, jid: jid}, nil
}

func (p *Port) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.world.mu.Lock()
	defer p.world.mu.Unlock()
	for client := range p.world.clients {
		if client.port == p {
			delete(p.world.clients, client)
		}
	}
	p.world.open--
	return nil
}

func (p *Port) ensureOpen() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errClosedHandle
	}
	return nil
}

type Client struct {
	world    *World
	port     *Port
	mu       sync.Mutex
	jid      string
	handlers []whatsapp.EventHandler
	settled  bool
}

func (c *Client) Device() string { return c.device() }

func (c *Client) device() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.jid
}

func (c *Client) setDevice(jid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.jid = jid
}

func (c *Client) ProfileName() string {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	if dev := c.world.deviceByJID(c.device()); dev != nil {
		return dev.profile
	}
	return ""
}

func (c *Client) AddEventHandler(handler whatsapp.EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers = append(c.handlers, handler)
}

func (c *Client) Connect() error {
	if c.device() == "" {
		return errNotPaired
	}
	return c.world.connect(c)
}

func (c *Client) Pair(ctx context.Context, req whatsapp.PairRequest) (<-chan whatsapp.PairUpdate, error) {
	if c.device() != "" {
		return nil, errAlreadyPaired
	}
	return c.world.startPairing(ctx, c, req).updates, nil
}

func (c *Client) Logout(context.Context) error {
	if c.device() == "" {
		c.Disconnect()
		return nil
	}
	return c.world.logout(c)
}

func (c *Client) Disconnect() {
	c.world.detach(c)
}

func (c *Client) dispatchConnection(evt any) {
	c.dispatch(evt)
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	c.settled = true
}

func (c *Client) dispatch(evt any) {
	c.mu.Lock()
	handlers := append([]whatsapp.EventHandler(nil), c.handlers...)
	c.mu.Unlock()
	for _, handler := range handlers {
		handler(evt)
	}
}
