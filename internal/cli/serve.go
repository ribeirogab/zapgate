package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ribeirogab/zapgate/internal/app"
	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/config"
	"github.com/ribeirogab/zapgate/internal/whatsapp/live"
)

const (
	healthTimeout = 3 * time.Second
	healthHost    = "127.0.0.1"
	socketMode    = 0o600
)

func serve(ctx context.Context, args []string, std IO) error {
	if len(args) > 0 {
		return errUsage
	}
	cfg, err := config.FromEnv(std.Getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(std.Stderr, nil))
	a, err := app.New(ctx, cfg, app.Deps{
		OpenWhatsApp: live.Opener(log),
		Clock:        clock.System(),
		Audit:        std.Stdout,
		Log:          log,
	})
	if err != nil {
		return err
	}
	public, owner, err := listen(cfg)
	if err != nil {
		return errors.Join(err, a.Close())
	}
	log.Info("serving", "listen", public.Addr().String(), "admin_socket", cfg.AdminSocket)
	err = a.Serve(ctx, public, owner)
	log.Info("stopped")
	return errors.Join(err, a.Close())
}

func listen(cfg config.Config) (net.Listener, net.Listener, error) {
	public, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	owner, err := listenOwner(cfg.AdminSocket)
	if err != nil {
		public.Close()
		return nil, nil, err
	}
	return public, owner, nil
}

func listenOwner(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return nil, fmt.Errorf("create the owner socket dir: %w", err)
	}
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove the stale owner socket: %w", err)
	}
	owner, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socket, err)
	}
	if err := os.Chmod(socket, socketMode); err != nil {
		owner.Close()
		return nil, fmt.Errorf("protect the owner socket: %w", err)
	}
	return owner, nil
}

func health(ctx context.Context, args []string, std IO) error {
	if len(args) > 0 {
		return errUsage
	}
	_, port, err := net.SplitHostPort(config.ListenFromEnv(std.Getenv))
	if err != nil {
		return fmt.Errorf("ZAPGATE_LISTEN is not host:port: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	url := "http://" + net.JoinHostPort(healthHost, port) + app.HealthPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: %s", resp.Status)
	}
	fmt.Fprintln(std.Stdout, "ok")
	return nil
}
