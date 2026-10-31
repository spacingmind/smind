package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsclient"
	"net/http/httptest"
)

type noQuotaFetcher struct{}

func (noQuotaFetcher) Fetch(context.Context, accounts.Account) (quota.Report, error) {
	panic("quota fetch in a fleet wiring test")
}

// fleetTestServer builds a daemon server over a temp store with its own
// token "tok".
func fleetTestServer(t *testing.T, cfg config.Config) (*server.Server, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	reg := accounts.New(db)
	poller := quota.New(db, reg, noQuotaFetcher{}, quota.WithEnabled(false))
	t.Cleanup(poller.Close)
	wm := workspace.New(db)
	srv, err := server.New(cfg, reg, routing.New(db, reg, poller), wm, taskrunner.New(wm), db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, db
}

func dialFleetTest(t *testing.T, srv *server.Server) *wsclient.Client {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl, err := wsclient.Dial(ctx, ts.Listener.Addr().String(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	return cl
}

func TestNewFleetOffTouchesNothing(t *testing.T) {
	home := t.TempDir()
	cfg := config.Default() // fleet.enabled is false
	srv, db := fleetTestServer(t, cfg)
	stop := newFleet(srv, cfg, home, "test", db, nil)
	stop()
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("fleet off wrote into home: %v", entries)
	}
	cl := dialFleetTest(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out map[string]any
	if err := cl.Call(ctx, "fleet.host.list", nil, &out); err == nil {
		t.Fatalf("fleet.host.list answered with the fleet off: %v", out)
	}
}

func TestNewFleetOnRegistersMethods(t *testing.T) {
	home := t.TempDir()
	cfg := config.Default()
	cfg.Fleet.Enabled = true
	srv, db := fleetTestServer(t, cfg)
	stop := newFleet(srv, cfg, home, "test", db, nil)
	t.Cleanup(stop)
	fi, err := os.Stat(filepath.Join(home, "fleet", "host-key"))
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("host key mode = %v, want 0600", fi.Mode().Perm())
	}
	cl := dialFleetTest(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out struct {
		Hosts []any `json:"hosts"`
	}
	if err := cl.Call(ctx, "fleet.host.list", nil, &out); err != nil {
		t.Fatalf("fleet.host.list: %v", err)
	}
	if len(out.Hosts) != 0 {
		t.Fatalf("hosts = %v", out.Hosts)
	}
	// An unregistered method answers "method not found"; remote.write must
	// fail on its params or caller instead.
	if err := cl.Call(ctx, "remote.write", nil, nil); err == nil || strings.Contains(err.Error(), "method not found") {
		t.Fatalf("remote.write not registered: %v", err)
	}
}
