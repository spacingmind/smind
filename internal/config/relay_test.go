package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadRelayYAML(t *testing.T, yaml string) (Config, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	return Load()
}

func TestConfig_RelayDefaultsAndValidation(t *testing.T) {
	d := Default().Relay
	if !d.LegacyDevices || d.MaxDevices != 16 {
		t.Fatalf("Default().Relay = %+v, want legacyDevices true, maxDevices 16", d)
	}

	cfg, err := loadRelayYAML(t, "")
	if err != nil || cfg.Relay != d {
		t.Fatalf("no file: %+v, %v", cfg.Relay, err)
	}

	// Setting one key keeps the other's default.
	cfg, err = loadRelayYAML(t, "relay:\n  legacyDevices: false\n")
	if err != nil || cfg.Relay.LegacyDevices || cfg.Relay.MaxDevices != 16 {
		t.Fatalf("legacyDevices:false: %+v, %v", cfg.Relay, err)
	}
	cfg, err = loadRelayYAML(t, "relay:\n  maxDevices: 3\n")
	if err != nil || !cfg.Relay.LegacyDevices || cfg.Relay.MaxDevices != 3 {
		t.Fatalf("maxDevices:3: %+v, %v", cfg.Relay, err)
	}

	for _, v := range []string{"0", "-2"} {
		_, err = loadRelayYAML(t, "relay:\n  maxDevices: "+v+"\n")
		if err == nil || !strings.Contains(err.Error(), "relay.maxDevices must be >= 1, got "+v) {
			t.Fatalf("maxDevices %s: err = %v", v, err)
		}
	}
}
