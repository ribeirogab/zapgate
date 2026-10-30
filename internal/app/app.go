package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ribeirogab/zapgate/internal/accounts"
	"github.com/ribeirogab/zapgate/internal/admin"
	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/config"
	"github.com/ribeirogab/zapgate/internal/ingest"
	"github.com/ribeirogab/zapgate/internal/mcpserver"
	"github.com/ribeirogab/zapgate/internal/media"
	"github.com/ribeirogab/zapgate/internal/oauth"
	"github.com/ribeirogab/zapgate/internal/retention"
	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/tools"
	"github.com/ribeirogab/zapgate/internal/webhooks"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	AppDBFile         = "app.db"
	SessionDBFile     = "session.db"
	MediaDir          = "media"
	HealthPath        = "/healthz"
	shutdownTimeout   = 10 * time.Second
	readHeaderTimeout = 10 * time.Second
	dataDirMode       = 0o700
)

type Config = config.Config

type Deps struct {
	OpenWhatsApp whatsapp.Opener
	Clock        clock.Clock
	Audit        io.Writer
	Log          *slog.Logger
	Transcoder   media.Transcoder
	Resolver     webhooks.Resolver
	Random       func() float64
}

var (
	errAlreadyServing = errors.New("the app is already serving")
	errClosed         = errors.New("the app is closed")
)

type worker func(ctx context.Context) error

type App struct {
	cfg      Config
	log      *slog.Logger
	store    *store.Store
	whatsapp whatsapp.Port
	media    *media.Files
	endpoint *mcpserver.Endpoint
	public   http.Handler
	owner    http.Handler
	workers  []worker

	mu          sync.Mutex
	closed      bool
	stopServing context.CancelFunc
	served      chan struct{}

	closeOnce sync.Once
	closeErr  error
}

func New(ctx context.Context, cfg Config, deps Deps) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, dataDirMode); err != nil {
		return nil, fmt.Errorf("create the data dir: %w", err)
	}
	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, AppDBFile))
	if err != nil {
		return nil, err
	}
	files, err := media.OpenFiles(filepath.Join(cfg.DataDir, MediaDir))
	if err != nil {
		st.Close()
		return nil, err
	}
	port, err := deps.OpenWhatsApp(ctx, filepath.Join(cfg.DataDir, SessionDBFile))
	if err != nil {
		files.Close()
		st.Close()
		return nil, fmt.Errorf("open the WhatsApp session store: %w", err)
	}
	outbox, err := media.NewOutbox(media.OutboxDeps{
		DataDir:    cfg.DataDir,
		Clock:      deps.Clock,
		Transcoder: transcoder(deps),
		Fetch:      media.FetchConfig{Timeout: cfg.MediaFetchTimeout, Allowed: cfg.MediaFetchAllowed},
		Log:        deps.Log,
	})
	if err != nil {
		return nil, errors.Join(err, port.Close(), files.Close(), st.Close())
	}
	a := &App{cfg: cfg, log: deps.Log, store: st, whatsapp: port, media: files}
	a.wire(deps, outbox)
	return a, nil
}

func (a *App) wire(deps Deps, outbox *media.Outbox) {
	auditLog := audit.New(deps.Audit, deps.Clock)
	hosts := resolver(deps)
	ingester := ingest.New(a.store, deps.Clock, deps.Log)
	sender := webhooks.NewSender(hosts)
	dispatcher := webhooks.NewDispatcher(webhooks.DispatcherDeps{Store: a.store, Sender: sender, Clock: deps.Clock, Random: random(deps), Audit: auditLog, Log: deps.Log})
	var downloads *media.Downloader
	manager := accounts.New(accounts.Deps{
		Store:      a.store,
		WhatsApp:   a.whatsapp,
		Clock:      deps.Clock,
		Log:        deps.Log,
		Changed:    func(ctx context.Context) { a.endpoint.AccountsChanged(ctx) },
		StateSaved: dispatcher.Wake,
		Ingest: func(ctx context.Context, accountID int64, client whatsapp.Client, evt any) {
			ingester.Handle(ctx, ingest.Source{AccountID: accountID, Client: client}, evt)
			downloads.Notice(accountID, evt)
			dispatcher.Notice(evt)
		},
		Removed: func(_ context.Context, account store.Account) error { return a.media.RemoveAccount(account.ID) },
	})
	downloads = media.NewDownloader(media.DownloaderDeps{Store: a.store, Files: a.media, Clients: manager, Clock: deps.Clock, Log: deps.Log})
	links := media.NewLinks(a.cfg.SigningKey, deps.Clock, a.cfg.PublicURL)
	registry := tools.NewRegistry(tools.Deps{
		Store:       a.store,
		Clients:     manager,
		Outbox:      outbox,
		Clock:       deps.Clock,
		Log:         deps.Log,
		SigningKey:  a.cfg.SigningKey,
		ApprovalTTL: a.cfg.ApprovalTimeout,
		Media:       tools.MediaDeps{Downloads: downloads, Files: a.media, Links: links},
	})
	a.endpoint = mcpserver.New(mcpserver.Deps{
		Store:     a.store,
		Tools:     registry,
		Audit:     auditLog,
		Clock:     deps.Clock,
		PublicURL: a.cfg.PublicURL,
		Log:       deps.Log,

		ApprovalTimeout: a.cfg.ApprovalTimeout,
		ApprovalPing:    a.cfg.ApprovalPing,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, health)
	mux.Handle(mcpserver.Path, a.endpoint.Handler())
	mux.Handle(media.LinkRoute, media.Handler(media.HandlerDeps{Store: a.store, Files: a.media, Links: links, Log: deps.Log}))
	mux.Handle("POST "+media.UploadsPath, a.endpoint.RequireBearer(media.UploadHandler(media.UploadHandlerDeps{Outbox: outbox, Store: a.store, Audit: auditLog})))
	oauth.NewServer(oauth.Deps{
		Store:         a.store,
		Clock:         deps.Clock,
		Audit:         auditLog,
		Grants:        a.endpoint,
		PublicURL:     a.cfg.PublicURL,
		OwnerPassword: a.cfg.OwnerPassword,
		Log:           deps.Log,
	}).Register(mux)
	a.public = audit.WithClientIP(a.cfg.ClientIPHeader, mux)
	expiry := retention.New(retention.Deps{Store: a.store, Files: a.media, Clock: deps.Clock, Log: deps.Log, DefaultMonths: a.cfg.RetentionMonths})
	a.workers = append(a.workers, manager.Run, downloads.Run, outbox.Run, expiry.Run, dispatcher.Run)
	a.owner = admin.Handler(admin.Deps{
		Store:         a.store,
		Accounts:      manager,
		Clock:         deps.Clock,
		Grants:        a.endpoint,
		Audit:         auditLog,
		OwnerPassword: a.cfg.OwnerPassword,
		Media:         a.media,
		Retention:     expiry,
		Webhooks:      webhooks.NewRegistry(webhooks.RegistryDeps{Store: a.store, Clock: deps.Clock, Sender: sender, Resolver: hosts, Clients: manager}),

		DefaultRetentionMonths: a.cfg.RetentionMonths,
	})
}

func transcoder(deps Deps) media.Transcoder {
	if deps.Transcoder == nil {
		return media.FFmpeg{}
	}
	return deps.Transcoder
}

func resolver(deps Deps) webhooks.Resolver {
	if deps.Resolver == nil {
		return net.DefaultResolver
	}
	return deps.Resolver
}

func random(deps Deps) func() float64 {
	if deps.Random == nil {
		return rand.Float64
	}
	return deps.Random
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok")
}

func (a *App) Handler() http.Handler { return a.public }

func (a *App) AdminHandler() http.Handler { return a.owner }

func (a *App) Serve(ctx context.Context, public, owner net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	served, err := a.beginServing(cancel)
	if err != nil {
		public.Close()
		owner.Close()
		return err
	}
	defer close(served)
	servers := []*http.Server{a.newServer(a.public, context.Background()), a.newServer(a.owner, ctx)}
	failed := make(chan error, len(servers)+len(a.workers))
	for i, listener := range []net.Listener{public, owner} {
		go func() { failed <- servers[i].Serve(listener) }()
	}
	stopWorkers := a.startWorkers(ctx, failed)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-failed:
	}
	cancel()
	shutdownErr := a.shutdown(servers)
	stopWorkers()
	return errors.Join(serveErr, shutdownErr)
}

func (a *App) beginServing(cancel context.CancelFunc) (chan struct{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errClosed
	}
	if a.served != nil {
		return nil, errAlreadyServing
	}
	a.stopServing = cancel
	a.served = make(chan struct{})
	return a.served, nil
}

func (a *App) startWorkers(ctx context.Context, failed chan<- error) func() {
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	var wg sync.WaitGroup
	for _, run := range a.workers {
		wg.Go(func() {
			if err := run(ctx); err != nil && ctx.Err() == nil {
				failed <- err
			}
		})
	}
	return func() {
		cancel()
		wg.Wait()
	}
}

func (a *App) newServer(h http.Handler, base context.Context) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(a.log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return base },
	}
}

func (a *App) shutdown(servers []*http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	a.endpoint.Close()
	var errs []error
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			errs = append(errs, err, srv.Close())
		}
	}
	return errors.Join(errs...)
}

func (a *App) Close() error {
	a.closeOnce.Do(func() { a.closeErr = a.close() })
	return a.closeErr
}

func (a *App) close() error {
	a.mu.Lock()
	a.closed = true
	stop, served := a.stopServing, a.served
	a.mu.Unlock()
	if stop != nil {
		stop()
		<-served
	}
	return errors.Join(a.whatsapp.Close(), a.media.Close(), a.store.Close())
}
