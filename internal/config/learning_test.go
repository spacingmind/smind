package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestConfig_LearningDefaultsAndValidation covers the learning block
// (ADR-0026): defaults, partial override keeping the rest, the similarity
// and budget bounds, and the exact key set.
func TestConfig_LearningDefaultsAndValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	t.Setenv(EnvLearning, "")
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
	if cfg.Learning != DefaultLearning() {
		t.Fatalf("defaults = %+v, want %+v", cfg.Learning, DefaultLearning())
	}

	// A partial file leaves the other keys at their defaults.
	write("learning:\n  enabled: false\n")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() enabled:false error = %v", err)
	}
	if cfg.Learning.Enabled {
		t.Fatal("enabled override not applied")
	}
	if cfg.Learning.CadenceToolCalls != 50 || cfg.Learning.Capacity != 30 || cfg.Learning.Similarity.Duplicate != 0.85 {
		t.Fatalf("partial override = %+v, want cadence 50, capacity 30, duplicate 0.85", cfg.Learning)
	}

	for _, tc := range []struct {
		yaml     string
		wantFail bool
	}{
		{"learning:\n  similarity: {duplicate: 0.5, possible: 0.6}\n", true},
		{"learning:\n  similarity: {possible: 0}\n", true},
		{"learning:\n  similarity: {duplicate: 0.9, possible: 0.8}\n", false},
		{"learning:\n  capacity: 0\n", true},
		{"learning:\n  dailyBudgetUSD: 0\n", true},
		{"learning:\n  windowKB: 200\n", false},
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
}

// TestConfig_LearningEnvKillSwitch verifies SMIND_LEARNING=0 forces the
// kill switch off over the file, and that the key set matches the struct
// exactly, so a new knob must be added consciously.
func TestConfig_LearningEnvKillSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte("learning:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv(EnvLearning, "0")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Learning.Enabled {
		t.Fatal("SMIND_LEARNING=0 left learning enabled")
	}

	for _, v := range []string{"FALSE", "Off"} {
		t.Setenv(EnvLearning, v)
		cfg, err = Load()
		if err != nil || cfg.Learning.Enabled {
			t.Fatalf("SMIND_LEARNING=%s did not disable learning (err %v)", v, err)
		}
	}

	t.Setenv(EnvLearning, "")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Learning.Enabled {
		t.Fatal("SMIND_LEARNING unset left learning disabled")
	}

	// A missing config file must not drop the env kill switch.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvLearning, "0")
	cfg, err = Load()
	if err != nil || cfg.Learning.Enabled {
		t.Fatalf("SMIND_LEARNING=0 with no config file: enabled=%v err=%v", cfg.Learning.Enabled, err)
	}

	// The struct keys are a fixed list; a new knob must be added here too.
	want := map[string]bool{
		"enabled": false, "cadenceToolCalls": false, "tailFlushPending": false,
		"tailIdleMinutes": false, "cooldownMinutes": false, "windowKB": false,
		"recordKB": false, "inputKB": false, "resultKB": false,
		"failureResultKB": false, "dailyBudgetUSD": false, "dailyBudgetTokens": false,
		"probationRuns": false, "capacity": false, "curatorEveryLandings": false,
		"curatorMinSkills": false, "curatorMinIntervalDays": false,
		"prefaceBudgetBytes": false, "claudeIndexPreface": false, "similarity": false,
		"reflectorModel": false, "curatorModel": false,
	}
	got := reflect.TypeOf(LearningConfig{})
	if got.NumField() != len(want) {
		t.Fatalf("LearningConfig has %d fields, want exactly %d", got.NumField(), len(want))
	}
	for i := range got.NumField() {
		tag := got.Field(i).Tag.Get("yaml")
		if _, ok := want[tag]; !ok {
			t.Fatalf("LearningConfig has unexpected yaml key %q", tag)
		}
		want[tag] = true
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("LearningConfig is missing yaml key %q", k)
		}
	}
}

func TestConfig_LearningEnvKillSwitchAcceptedValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	for _, v := range []string{"0", "false", "off", "no", "n", "f", "disabled", " FALSE ", "No", "DISABLED", "Off"} {
		t.Setenv(EnvLearning, v)
		cfg, err := Load()
		if err != nil || cfg.Learning.Enabled {
			t.Errorf("SMIND_LEARNING=%q: enabled=%v err=%v, want disabled", v, cfg.Learning.Enabled, err)
		}
	}
	for _, v := range []string{"1", "true", "on", "yes"} {
		t.Setenv(EnvLearning, v)
		cfg, err := Load()
		if err != nil || !cfg.Learning.Enabled {
			t.Errorf("SMIND_LEARNING=%q: enabled=%v err=%v, want enabled", v, cfg.Learning.Enabled, err)
		}
	}
}

func TestConfig_LearningFailsClosedOnBrokenFile(t *testing.T) {
	t.Setenv(EnvLearning, "")
	dir := t.TempDir()

	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("learning:\n  enabled: false\nbad: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(bad)
	if err == nil {
		t.Fatal("malformed config: want error")
	}
	if cfg.Learning.Enabled {
		t.Fatal("malformed config left learning enabled")
	}

	asDir := filepath.Join(dir, "config.yaml")
	if err := os.Mkdir(asDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadFile(asDir)
	if err == nil {
		t.Fatal("unreadable config: want error")
	}
	if cfg.Learning.Enabled {
		t.Fatal("unreadable config left learning enabled")
	}
}

// TestConfig_LearningModelKnobs covers the reflector/curator model knobs:
// cheap-Claude defaults, override, "" = provider default, and rejection of
// values that could be read as argv flags.
func TestConfig_LearningModelKnobs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	t.Setenv(EnvLearning, "")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load()
	if err != nil || cfg.Learning.ReflectorModel != "claude-haiku-5-5" || cfg.Learning.CuratorModel != "claude-haiku-5-5" {
		t.Fatalf("defaults = %q/%q, err %v", cfg.Learning.ReflectorModel, cfg.Learning.CuratorModel, err)
	}
	write("learning:\n  reflectorModel: haiku\n  curatorModel: \"\"\n")
	cfg, err = Load()
	if err != nil || cfg.Learning.ReflectorModel != "haiku" || cfg.Learning.CuratorModel != "" {
		t.Fatalf("override = %q/%q, err %v", cfg.Learning.ReflectorModel, cfg.Learning.CuratorModel, err)
	}
	for _, bad := range []string{"--dangerously-skip-permissions", "a b", "x;y"} {
		write("learning:\n  reflectorModel: \"" + bad + "\"\n")
		if _, err := Load(); err == nil {
			t.Fatalf("reflectorModel %q accepted", bad)
		}
	}
}
