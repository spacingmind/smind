package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSteward_PolicyDisabledByDefaultAndReadFromYAML proves the policy
// engine's switch is off unless a human writes it on (ADR-0047 §2): with no
// steward block the permission path is unchanged, and -- like every other
// section, whose loader is not strict -- an unknown key under steward.policy
// is ignored rather than fatal.
func TestSteward_PolicyDisabledByDefaultAndReadFromYAML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Steward.Policy.Enabled {
		t.Fatal("no steward block: policy enabled, want off by default")
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("steward:\n  policy:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Steward.Policy.Enabled {
		t.Fatal("steward.policy.enabled: true not read")
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("steward:\n  policy:\n    enabled: true\n    bogus: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with an unknown key under steward.policy: %v", err)
	}
	if !cfg.Steward.Policy.Enabled {
		t.Fatal("an unknown key under steward.policy dropped the enabled value")
	}
}
