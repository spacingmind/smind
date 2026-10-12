package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestConfig_AccountUsageDefaultsAndValidation covers ADR-0028's
// accountUsage block: defaults, partial override keeping the rest, ttl
// floor, exhaust threshold bounds, and the exact four-field shape (budget
// constants must not become config keys).
func TestConfig_AccountUsageDefaultsAndValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() missing file error = %v", err)
	}
	if want := (AccountUsageConfig{Enabled: true, TTLSeconds: 300, Routing: true, ExhaustThresholdPct: 100}); cfg.AccountUsage != want {
		t.Fatalf("defaults = %+v, want %+v", cfg.AccountUsage, want)
	}

	// A partial file leaves the other keys at their defaults.
	write("accountUsage:\n  enabled: false\n")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() enabled:false error = %v", err)
	}
	if cfg.AccountUsage.Enabled {
		t.Fatal("enabled override not applied")
	}
	if cfg.AccountUsage.TTLSeconds != 300 || !cfg.AccountUsage.Routing || cfg.AccountUsage.ExhaustThresholdPct != 100 {
		t.Fatalf("partial override = %+v, want ttl 300, routing true, threshold 100", cfg.AccountUsage)
	}

	for _, tc := range []struct {
		yaml     string
		wantFail bool
	}{
		{"accountUsage:\n  ttlSeconds: 299\n", true},
		{"accountUsage:\n  ttlSeconds: 600\n", false},
		{"accountUsage:\n  exhaustThresholdPct: 0\n", true},
		{"accountUsage:\n  exhaustThresholdPct: 101\n", true},
		{"accountUsage:\n  exhaustThresholdPct: 1\n", false},
		{"accountUsage:\n  exhaustThresholdPct: 100\n", false},
	} {
		write(tc.yaml)
		_, err := Load()
		if tc.wantFail && err == nil {
			t.Fatalf("%q accepted, want rejection", tc.yaml)
		}
		if !tc.wantFail && err != nil {
			t.Fatalf("%q rejected: %v", tc.yaml, err)
		}
	}

	// Exactly four fields, so no budget constant can sneak in as a key.
	got := reflect.TypeOf(AccountUsageConfig{})
	if got.NumField() != 4 {
		t.Fatalf("AccountUsageConfig has %d fields, want exactly 4", got.NumField())
	}
	want := map[string]bool{"Enabled": false, "TTLSeconds": false, "Routing": false, "ExhaustThresholdPct": false}
	for i := range got.NumField() {
		name := got.Field(i).Name
		if _, ok := want[name]; !ok {
			t.Fatalf("AccountUsageConfig has unexpected field %q", name)
		}
		want[name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("AccountUsageConfig is missing field %q", name)
		}
	}
}
