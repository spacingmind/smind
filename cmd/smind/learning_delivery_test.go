package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestNewDeliveryService_NilWhenDisabled(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultLearning()
	cfg.Enabled = false
	if svc := newDeliveryService(cfg, nil, t.TempDir()); svc != nil {
		t.Fatal("service built with learning disabled")
	}
}

func TestNewDeliveryService_BuiltWhenEnabled(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	home := t.TempDir()
	svc := newDeliveryService(config.DefaultLearning(), db, home)
	if svc == nil {
		t.Fatal("no service with learning enabled")
	}
	defer svc.Close()
	ws, err := db.CreateWorkspace(store.Workspace{Path: "/w", Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	// Mode off (the default): nothing delivered.
	if d, _ := svc.Prepare(context.Background(), ws.ID, "r", taskrunner.ProviderClaudeNative, ""); !d.Empty() {
		t.Fatalf("delivery with learning_mode off: %+v", d)
	}
}
