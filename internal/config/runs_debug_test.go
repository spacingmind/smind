package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_RunsAndDebug(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	t.Setenv(EnvPprof, "")
	t.Setenv(EnvRunHistoryBudgetMB, "")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runs.HistoryBudgetMB != 64 || cfg.Debug.Pprof {
		t.Fatalf("defaults = %+v / %+v, want 64 MB and pprof off", cfg.Runs, cfg.Debug)
	}

	write("runs:\n  historyBudgetMB: 128\ndebug:\n  pprof: true\n")
	if cfg, err = Load(); err != nil || cfg.Runs.HistoryBudgetMB != 128 || !cfg.Debug.Pprof {
		t.Fatalf("file values = %+v %+v, %v", cfg.Runs, cfg.Debug, err)
	}

	for _, bad := range []string{"-5", "0"} {
		write("runs:\n  historyBudgetMB: " + bad + "\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "runs.historyBudgetMB") {
			t.Fatalf("historyBudgetMB %s: err = %v, want a rejection", bad, err)
		}
	}

	write("runs:\n  historyBudgetMB: 32\n")
	t.Setenv(EnvRunHistoryBudgetMB, "256")
	t.Setenv(EnvPprof, "1")
	if cfg, err = Load(); err != nil || cfg.Runs.HistoryBudgetMB != 256 || !cfg.Debug.Pprof {
		t.Fatalf("env override = %+v %+v, %v", cfg.Runs, cfg.Debug, err)
	}
	t.Setenv(EnvRunHistoryBudgetMB, "junk")
	if cfg, _ = Load(); cfg.Runs.HistoryBudgetMB != 32 {
		t.Fatalf("a bad env value must leave the file value, got %d", cfg.Runs.HistoryBudgetMB)
	}
}
