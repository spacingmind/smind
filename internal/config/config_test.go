package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefault_OrchestrationMaxDepth pins the O2 depth guard's default
// (docs/plans/active/orchestration-and-metering.md): maxDepth 2 means a
// root task's grandchildren are the deepest allowed level.
func TestDefault_OrchestrationMaxDepth(t *testing.T) {
	t.Parallel()
	if got := Default().Orchestration.MaxDepth; got != 2 {
		t.Fatalf("Default().Orchestration.MaxDepth = %d, want 2", got)
	}
}

// TestLoad_OrchestrationMaxDepthOverride proves orchestration.maxDepth is
// configured from config.yaml on top of the default, and that a missing
// file keeps the default.
func TestLoad_OrchestrationMaxDepthOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Orchestration.MaxDepth; got != 2 {
		t.Fatalf("missing file: MaxDepth = %d, want default 2", got)
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("orchestration:\n  maxDepth: 1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Orchestration.MaxDepth; got != 1 {
		t.Fatalf("override: MaxDepth = %d, want 1", got)
	}
}

func TestDefault_WorktreesConfig(t *testing.T) {
	t.Parallel()
	w := Default().Worktrees
	if w.SetupTimeoutSeconds != 600 || w.TeardownTimeoutSeconds != 120 {
		t.Fatalf("timeouts = %d/%d, want 600/120", w.SetupTimeoutSeconds, w.TeardownTimeoutSeconds)
	}
	if _, _, ok, err := w.Ports.PortRange(); ok || err != nil {
		t.Fatalf("default range ok=%v err=%v, want unset", ok, err)
	}
}

func TestLoad_WorktreesConfigOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	y := "worktrees:\n  ports:\n    range: \"31000-31999\"\n  setupTimeoutSeconds: 5\n  teardownTimeoutSeconds: 7\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(y), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	lo, hi, ok, err := cfg.Worktrees.Ports.PortRange()
	if err != nil || !ok || lo != 31000 || hi != 31999 {
		t.Fatalf("range = %d-%d ok=%v err=%v", lo, hi, ok, err)
	}
	if cfg.Worktrees.SetupTimeoutSeconds != 5 || cfg.Worktrees.TeardownTimeoutSeconds != 7 {
		t.Fatalf("timeouts = %+v", cfg.Worktrees)
	}
}

func TestLoad_WorktreesBadRangeRejected(t *testing.T) {
	for _, r := range []string{"500-600", "40000-31000", "abc", "1024-70000"} {
		home := t.TempDir()
		t.Setenv("SMIND_HOME", home)
		y := "worktrees:\n  ports:\n    range: \"" + r + "\"\n"
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(y), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(); err == nil {
			t.Fatalf("range %q accepted, want error", r)
		}
	}
}
