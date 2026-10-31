package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfig_SharedCachesDefaultAndValidate pins the agents.sharedCaches
// knob: absent means true, explicit booleans win, and a non-bool value is a
// load error.
func TestConfig_SharedCachesDefaultAndValidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("orchestration:\n  maxDepth: 2\n")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("no agents block: %v", err)
	}
	if !cfg.Agents.SharedCachesEnabled() || cfg.Agents.SharedCaches != nil {
		t.Fatalf("no agents block: SharedCaches = %v, want nil (enabled)", cfg.Agents.SharedCaches)
	}

	write("agents:\n  sharedCaches: false\n")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("sharedCaches: false: %v", err)
	}
	if cfg.Agents.SharedCachesEnabled() {
		t.Fatalf("sharedCaches: false -> SharedCachesEnabled() = true, want false")
	}

	write("agents:\n  sharedCaches: true\n")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("sharedCaches: true: %v", err)
	}
	if !cfg.Agents.SharedCachesEnabled() {
		t.Fatalf("sharedCaches: true -> SharedCachesEnabled() = false, want true")
	}

	write("agents:\n  sharedCaches: maybe\n")
	if _, err := Load(); err == nil {
		t.Fatal("sharedCaches: maybe accepted, want a load error")
	}
}
