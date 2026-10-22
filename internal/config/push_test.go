package config

import (
	"path/filepath"
	"testing"
)

func clearPushEnv(t *testing.T) {
	t.Helper()
	t.Setenv(EnvPush, "")
	t.Setenv(EnvExpoAccessToken, "")
}

func TestPushDefaultsEnabled(t *testing.T) {
	clearPushEnv(t)
	if d := Default().Push; !d.Enabled || d.ExpoAccessToken != "" {
		t.Fatalf("Default().Push = %+v", d)
	}
	cfg, err := loadRelayYAML(t, "")
	if err != nil || !cfg.Push.Enabled {
		t.Fatalf("no file: %+v, %v", cfg.Push, err)
	}
}

func TestPushYAMLOverrides(t *testing.T) {
	clearPushEnv(t)
	cfg, err := loadRelayYAML(t, "push:\n  enabled: false\n  expoAccessToken: x\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Push.Enabled || cfg.Push.ExpoAccessToken != "x" {
		t.Fatalf("Push = %+v", cfg.Push)
	}
}

func TestPushEnvOverrides(t *testing.T) {
	clearPushEnv(t)
	yaml := "push:\n  enabled: true\n  expoAccessToken: fromfile\n"
	for _, v := range []string{"0", "FALSE", "Off", "disabled"} {
		t.Setenv(EnvPush, v)
		cfg, err := loadRelayYAML(t, yaml)
		if err != nil || cfg.Push.Enabled {
			t.Fatalf("SMIND_PUSH=%s: %+v, %v", v, cfg.Push, err)
		}
	}
	t.Setenv(EnvPush, "1")
	t.Setenv(EnvExpoAccessToken, "  fromenv ")
	cfg, err := loadRelayYAML(t, yaml)
	if err != nil || !cfg.Push.Enabled || cfg.Push.ExpoAccessToken != "fromenv" {
		t.Fatalf("Push = %+v, %v", cfg.Push, err)
	}
}

func TestPushEnvOverridesApplyWithMissingFile(t *testing.T) {
	clearPushEnv(t)
	t.Setenv(EnvPush, "off")
	t.Setenv(EnvExpoAccessToken, "tok")
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Push.Enabled || cfg.Push.ExpoAccessToken != "tok" {
		t.Fatalf("Push = %+v", cfg.Push)
	}
}
