package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFleet_DisabledByDefaultAndDefaults proves the fleet section is off and
// at its defaults with no config.yaml, and that switching it on alone does
// not disturb the other defaults (ADR-0048: off means today's daemon,
// byte for byte).
func TestFleet_DisabledByDefaultAndDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Fleet.Enabled {
		t.Fatal("no fleet block: fleet enabled, want off by default")
	}
	if cfg.Fleet != DefaultFleet() {
		t.Errorf("no fleet block: got %+v, want the defaults %+v", cfg.Fleet, DefaultFleet())
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("fleet:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := DefaultFleet()
	want.Enabled = true
	if cfg.Fleet != want {
		t.Errorf("fleet.enabled: true alone: got %+v, want the defaults with only enabled set %+v", cfg.Fleet, want)
	}
}

func TestFleet_EnabledReadFromYAML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("fleet:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Fleet.Enabled {
		t.Fatal("fleet.enabled: true not read")
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("fleet:\n  enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Fleet.Enabled {
		t.Fatal("fleet.enabled: false not read")
	}
}

// TestFleet_ValidationNamesField checks every bounded field's out-of-range
// rejection, including that validation runs while the section is disabled:
// the error must name the yaml path and the bad value so a typo is fixable
// from the message alone.
func TestFleet_ValidationNamesField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	path := filepath.Join(home, "config.yaml")

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"maxHosts zero", "fleet:\n  maxHosts: 0\n", "fleet.maxHosts must be between 1 and 256, got 0"},
		{"maxHosts too big", "fleet:\n  maxHosts: 257\n", "fleet.maxHosts must be between 1 and 256, got 257"},
		{"maxConnectionsPerPeer zero", "fleet:\n  maxConnectionsPerPeer: 0\n", "fleet.maxConnectionsPerPeer must be between 1 and 8, got 0"},
		{"maxConnectionsPerPeer too big", "fleet:\n  maxConnectionsPerPeer: 9\n", "fleet.maxConnectionsPerPeer must be between 1 and 8, got 9"},
		{"handshakeSeconds zero", "fleet:\n  handshakeSeconds: 0\n", "fleet.handshakeSeconds must be between 1 and 120, got 0"},
		{"handshakeSeconds too big", "fleet:\n  handshakeSeconds: 121\n", "fleet.handshakeSeconds must be between 1 and 120, got 121"},
		{"maxFrameKiB too small", "fleet:\n  maxFrameKiB: 15\n", "fleet.maxFrameKiB must be between 16 and 4096, got 15"},
		{"maxFrameKiB too big", "fleet:\n  maxFrameKiB: 4097\n", "fleet.maxFrameKiB must be between 16 and 4096, got 4097"},
		{"maxOutstandingPerLink zero", "fleet:\n  maxOutstandingPerLink: 0\n", "fleet.maxOutstandingPerLink must be between 1 and 256, got 0"},
		{"maxOutstandingPerLink too big", "fleet:\n  maxOutstandingPerLink: 257\n", "fleet.maxOutstandingPerLink must be between 1 and 256, got 257"},
		{"backoff min zero", "fleet:\n  observationBackoffMinSeconds: 0\n", "fleet.observationBackoffMinSeconds must be between 1 and 3600, got 0"},
		{"backoff max too big", "fleet:\n  observationBackoffMaxSeconds: 3601\n", "fleet.observationBackoffMaxSeconds must be between 1 and 3600, got 3601"},
		{"backoff max below min", "fleet:\n  observationBackoffMinSeconds: 5\n  observationBackoffMaxSeconds: 4\n", "fleet.observationBackoffMaxSeconds must be >= observationBackoffMinSeconds (5), got 4"},
		{"idempotencyHours zero", "fleet:\n  idempotencyHours: 0\n", "fleet.idempotencyHours must be between 1 and 168, got 0"},
		{"idempotencyHours too big", "fleet:\n  idempotencyHours: 169\n", "fleet.idempotencyHours must be between 1 and 168, got 169"},
		{"replayWindowMinutes zero", "fleet:\n  replayWindowMinutes: 0\n", "fleet.replayWindowMinutes must be between 1 and 60, got 0"},
		{"replayWindowMinutes too big", "fleet:\n  replayWindowMinutes: 61\n", "fleet.replayWindowMinutes must be between 1 and 60, got 61"},
		{"auditRetentionDays zero", "fleet:\n  auditRetentionDays: 0\n", "fleet.auditRetentionDays must be between 1 and 365, got 0"},
		{"auditRetentionDays too big", "fleet:\n  auditRetentionDays: 366\n", "fleet.auditRetentionDays must be between 1 and 365, got 366"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load()
			if err == nil {
				t.Fatalf("Load() = nil error, want %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load() error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestFleet_EnvCannotEnable pins that no environment variable can turn the
// fleet on: the switch is a human edit of config.yaml only, so a stray
// SMIND_FLEET in a container or CI environment changes nothing.
func TestFleet_EnvCannotEnable(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	t.Setenv("SMIND_FLEET", "1")
	t.Setenv("SMIND_FLEET_ENABLED", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Fleet.Enabled {
		t.Fatal("SMIND_FLEET/SMIND_FLEET_ENABLED enabled the fleet: there is deliberately no env override")
	}
}
