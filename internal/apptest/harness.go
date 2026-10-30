package apptest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/admin"
	"github.com/ribeirogab/zapgate/internal/app"
	"github.com/ribeirogab/zapgate/internal/cli"
	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/config"
	"github.com/ribeirogab/zapgate/internal/media"
	"github.com/ribeirogab/zapgate/internal/whatsapp/fake"
)

const (
	PublicURL     = "https://wpp.example.test"
	OwnerPassword = "owner-password-for-tests"
	DefaultIP     = "203.0.113.7"
	signingKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
)

var StartTime = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

type Harness struct {
	URL         string
	AdminSocket string
	DataDir     string
	Owner       *admin.Client
	WhatsApp    *fake.World
	Clock       *clock.Manual
	Hosts       *Hosts
	Random      *Random

	cfg            config.Config
	listenerURL    bool
	webhookTimeout time.Duration
	transcoder     media.Transcoder
	audit          *lockedBuffer
	address        string
	running        *running
	web            *http.Transport
	browser        *http.Client
}

type running struct {
	app    *app.App
	cancel context.CancelFunc
	done   chan error
}

type options struct {
	configure      []func(*config.Config)
	listenerIsURL  bool
	realFFmpeg     bool
	webhookTimeout time.Duration
}

type Option func(*options)

func WithConfig(fn func(*config.Config)) Option {
	return func(o *options) { o.configure = append(o.configure, fn) }
}

func WithRealFFmpeg() Option {
	return func(o *options) { o.realFFmpeg = true }
}

func WithWebhookTimeout(timeout time.Duration) Option {
	return func(o *options) { o.webhookTimeout = timeout }
}

func WithPublicURLOfTheListener() Option {
	return func(o *options) { o.listenerIsURL = true }
}

func Start(t testing.TB, opts ...Option) *Harness {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	socketDir, err := os.MkdirTemp("", "wa")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	h := &Harness{
		DataDir:        t.TempDir(),
		AdminSocket:    filepath.Join(socketDir, "admin.sock"),
		WhatsApp:       fake.New(),
		Clock:          clock.NewManual(StartTime),
		Hosts:          newHosts(),
		Random:         newRandom(),
		audit:          &lockedBuffer{},
		address:        "127.0.0.1:0",
		listenerURL:    o.listenerIsURL,
		webhookTimeout: o.webhookTimeout,
		transcoder:     fakeTranscoder{},
		web:            http.DefaultTransport.(*http.Transport).Clone(),
	}
	h.browser = &http.Client{
		Transport:     h.web,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if o.realFFmpeg {
		h.transcoder = media.FFmpeg{}
	}
	h.WhatsApp.SetClock(h.Clock)
	h.cfg = h.defaultConfig()
	for _, fn := range o.configure {
		fn(&h.cfg)
	}
	h.DataDir = h.cfg.DataDir
	h.AdminSocket = h.cfg.AdminSocket
	h.Owner = admin.Dial(h.AdminSocket)
	h.start(t)
	t.Cleanup(func() { h.stop(t) })
	return h
}

func (h *Harness) defaultConfig() config.Config {
	cfg, err := config.FromEnv(func(key string) string {
		return map[string]string{
			"ZAPGATE_DATA_DIR":       h.DataDir,
			"ZAPGATE_LISTEN":         "127.0.0.1:0",
			"ZAPGATE_ADMIN_SOCKET":   h.AdminSocket,
			"ZAPGATE_PUBLIC_URL":     PublicURL,
			"ZAPGATE_OWNER_PASSWORD": OwnerPassword,
			"ZAPGATE_SIGNING_KEY":    signingKeyHex,

			"ZAPGATE_CLIENT_IP_HEADER": "CF-Connecting-IP",
		}[key]
	})
	if err != nil {
		panic(err)
	}
	return cfg
}

func (h *Harness) Restart(t testing.TB) {
	t.Helper()
	h.stop(t)
	h.start(t)
}

func (h *Harness) Stop(t testing.TB) {
	t.Helper()
	h.stop(t)
}

func (h *Harness) Resume(t testing.TB) {
	t.Helper()
	h.start(t)
}

func (h *Harness) start(t testing.TB) {
	t.Helper()
	public, owner := h.listen(t)
	h.address = public.Addr().String()
	h.URL = "http://" + h.address
	if h.listenerURL {
		h.cfg.PublicURL = h.URL
	}
	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.New(ctx, h.cfg, app.Deps{
		OpenWhatsApp:   h.WhatsApp.Opener(),
		Clock:          h.Clock,
		Audit:          h.audit,
		Log:            slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Transcoder:     h.transcoder,
		Resolver:       h.Hosts,
		Random:         h.Random.Float64,
		WebhookTimeout: h.webhookTimeout,
	})
	if err != nil {
		cancel()
		public.Close()
		owner.Close()
		t.Fatalf("start the app: %v", err)
	}
	r := &running{app: a, cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- a.Serve(ctx, public, owner) }()
	h.running = r
	h.awaitReconnection(t)
}

func (h *Harness) listen(t testing.TB) (net.Listener, net.Listener) {
	t.Helper()
	public, err := net.Listen("tcp", h.address)
	if err != nil {
		t.Fatalf("listen on %s: %v", h.address, err)
	}
	owner, err := net.Listen("unix", h.AdminSocket)
	if err != nil {
		public.Close()
		t.Fatalf("listen on %s: %v", h.AdminSocket, err)
	}
	return public, owner
}

func (h *Harness) stop(t testing.TB) {
	t.Helper()
	r := h.running
	if r == nil {
		return
	}
	h.running = nil
	h.web.CloseIdleConnections()
	h.Owner.CloseIdleConnections()
	r.cancel()
	if err := <-r.done; err != nil {
		t.Errorf("serve: %v", err)
	}
	if err := r.app.Close(); err != nil {
		t.Errorf("close the app: %v", err)
	}
}

type CLIResult struct {
	Stdout string
	Stderr string
	Code   int
}

func (h *Harness) CLI(t testing.TB, args ...string) CLIResult {
	t.Helper()
	return h.runCLI("", args)
}

func (h *Harness) CLIWithInput(t testing.TB, stdin string, args ...string) CLIResult {
	t.Helper()
	return h.runCLI(stdin, args)
}

func (h *Harness) runCLI(stdin string, args []string) CLIResult {
	var stdout, stderr bytes.Buffer
	env := map[string]string{"ZAPGATE_ADMIN_SOCKET": h.AdminSocket, "ZAPGATE_LISTEN": h.address}
	code := cli.Run(context.Background(), args, cli.IO{
		Stdin:  strings.NewReader(stdin),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(key string) string { return env[key] },
	})
	return CLIResult{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

func (h *Harness) StaticToken(t testing.TB, name string, accounts []string, readOnly bool) string {
	t.Helper()
	req := admin.CreateTokenRequest{Name: name, Accounts: accounts, ReadOnly: readOnly}
	if len(accounts) == 0 || (len(accounts) == 1 && accounts[0] == "all") {
		req.Accounts = nil
		req.AllExistingAccounts = true
	}
	created, err := h.Owner.CreateToken(context.Background(), req)
	if err != nil {
		t.Fatalf("create the token %s: %v", name, err)
	}
	return created.Token
}

func (h *Harness) Do(t testing.TB, req *http.Request) *http.Response {
	t.Helper()
	resp, err := (&http.Client{Transport: h.web}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (h *Harness) AuditRaw() string {
	return h.audit.String()
}

func (h *Harness) AuditLines(t testing.TB) []map[string]any {
	t.Helper()
	var lines []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(h.AuditRaw()))
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("audit line %q is not JSON: %v", scanner.Text(), err)
		}
		lines = append(lines, line)
	}
	return lines
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
