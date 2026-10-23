package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_ProviderCooldowns(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE workspaces (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, title TEXT NOT NULL, routing_policy TEXT NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		var name string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'provider_cooldowns'`).Scan(&name); err != nil {
			t.Fatalf("Open #%d: provider_cooldowns missing: %v", i, err)
		}
		if err := s.SetProviderCooldown("glm", time.Now().Add(time.Hour), "r", time.Now()); err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
}

func TestStore_SetProviderCooldownKeepsLaterAndCaps(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	later, earlier := now.Add(2*time.Hour), now.Add(time.Hour)
	if err := s.SetProviderCooldown("glm", later, "r1", now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProviderCooldown("glm", earlier, "r2", now); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetProviderCooldown("glm", now)
	if err != nil || !ok || !got.Equal(later) {
		t.Fatalf("got %v ok=%v err=%v, want %v", got, ok, err, later)
	}
	if err := s.SetProviderCooldown("claude", now.Add(24*time.Hour), "r3", now); err != nil {
		t.Fatal(err)
	}
	got, ok, _ = s.GetProviderCooldown("claude", now)
	if !ok || !got.Equal(now.Add(MaxProviderCooldown)) {
		t.Fatalf("capped = %v ok=%v, want %v", got, ok, now.Add(MaxProviderCooldown))
	}
}

func TestStore_GetProviderCooldownOnlyWhileActive(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if _, ok, _ := s.GetProviderCooldown("glm", now); ok {
		t.Fatal("cooldown reported with no row")
	}
	reset := now.Add(time.Minute)
	if err := s.SetProviderCooldown("glm", reset, "r", now); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetProviderCooldown("glm", now); !ok {
		t.Fatal("active cooldown not reported")
	}
	if _, ok, _ := s.GetProviderCooldown("glm", reset); ok {
		t.Fatal("cooldown reported at its reset instant")
	}
	if _, ok, _ := s.GetProviderCooldown("glm", reset.Add(time.Second)); ok {
		t.Fatal("expired cooldown reported")
	}
}

func TestStore_ListProviderCooldowns(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.SetProviderCooldown("glm", now.Add(time.Hour), "a", now)
	_ = s.SetProviderCooldown("claude", now.Add(2*time.Hour), "b", now)
	_ = s.SetProviderCooldown("codex", now.Add(-time.Hour), "c", now.Add(-2*time.Hour))
	got, err := s.ListProviderCooldowns(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["glm"].Equal(now.Add(time.Hour)) || !got["claude"].Equal(now.Add(2*time.Hour)) {
		t.Fatalf("list = %v, want glm and claude only", got)
	}
}

func TestStore_PurgeExpiredProviderCooldowns(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.SetProviderCooldown("glm", now.Add(time.Hour), "a", now)
	_ = s.SetProviderCooldown("codex", now.Add(-time.Hour), "c", now.Add(-2*time.Hour))
	if err := s.PurgeExpiredProviderCooldowns(now); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM provider_cooldowns`); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
	if _, ok, _ := s.GetProviderCooldown("glm", now); !ok {
		t.Fatal("active cooldown purged")
	}
}
