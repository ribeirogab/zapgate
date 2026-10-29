package app_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/app"
	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/config"
	"github.com/ribeirogab/zapgate/internal/whatsapp/fake"
)

func newApp(t *testing.T, world *fake.World) *app.App {
	t.Helper()
	a, err := app.New(context.Background(), config.Config{DataDir: t.TempDir(), PublicURL: apptest.PublicURL}, app.Deps{
		OpenWhatsApp: world.Opener(),
		Clock:        clock.NewManual(apptest.StartTime),
		Audit:        io.Discard,
		Log:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func listeners(t *testing.T) (net.Listener, net.Listener) {
	t.Helper()
	public, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := net.Listen("unix", filepath.Join(shortTempDir(t), "admin.sock"))
	if err != nil {
		t.Fatal(err)
	}
	return public, owner
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wa")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func healthy(url string) bool {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func waitForHealth(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !healthy(url) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never answered", url)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCloseWithoutServeReleasesTheWhatsAppPort(t *testing.T) {
	world := fake.New()
	a := newApp(t, world)

	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if got := world.OpenHandles(); got != 0 {
		t.Fatalf("open handles %d after close, want 0", got)
	}
}

func TestCloseStopsARunningServe(t *testing.T) {
	world := fake.New()
	a := newApp(t, world)
	public, owner := listeners(t)
	served := make(chan error, 1)
	go func() { served <- a.Serve(context.Background(), public, owner) }()
	waitForHealth(t, "http://"+public.Addr().String()+"/healthz")

	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after close")
	}
	if got := world.OpenHandles(); got != 0 {
		t.Fatalf("open handles %d after close, want 0", got)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestServeReturnsWhenItsContextEnds(t *testing.T) {
	world := fake.New()
	a := newApp(t, world)
	t.Cleanup(func() { a.Close() })
	public, owner := listeners(t)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- a.Serve(ctx, public, owner) }()
	url := "http://" + public.Addr().String() + "/healthz"
	waitForHealth(t, url)

	cancel()

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context ended")
	}
	if healthy(url) {
		t.Fatal("the public listener still answers after serve returned")
	}
	if got := world.OpenHandles(); got != 1 {
		t.Fatalf("open handles %d before close, want 1", got)
	}
}
