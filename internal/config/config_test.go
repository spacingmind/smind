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

func TestLoad_GLMMaxTurns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := Load()
	if err != nil || cfg.Agents.GLM.MaxTurns != nil {
		t.Fatalf("missing file: MaxTurns = %v, err %v, want unset", cfg.Agents.GLM.MaxTurns, err)
	}
	write("agents:\n  glm:\n    maxTurns: 40\n")
	cfg, err = Load()
	if err != nil || cfg.Agents.GLM.MaxTurns == nil || *cfg.Agents.GLM.MaxTurns != 40 {
		t.Fatalf("MaxTurns = %v, err %v, want 40", cfg.Agents.GLM.MaxTurns, err)
	}
	for _, v := range []string{"0", "-3", "10001"} {
		write("agents:\n  glm:\n    maxTurns: " + v + "\n")
		if _, err := Load(); err == nil {
			t.Fatalf("maxTurns %s accepted, want a load error", v)
		}
	}
	write("agents:\n  glm:\n    maxTurns: 10000\n")
	if _, err := Load(); err != nil {
		t.Fatalf("maxTurns 10000: %v", err)
	}
}

// TestDefault_OrchestrationMaxConcurrentRunsPerTree pins the per-tree
// concurrency guard's default (PR-G).
func TestDefault_OrchestrationMaxConcurrentRunsPerTree(t *testing.T) {
	t.Parallel()
	if got := Default().Orchestration.MaxConcurrentRunsPerTree; got != 4 {
		t.Fatalf("Default().Orchestration.MaxConcurrentRunsPerTree = %d, want 4", got)
	}
}

// TestLoad_OrchestrationMaxConcurrentRunsPerTreeOverride proves the knob
// is configured from config.yaml; a missing file keeps the default.
func TestLoad_OrchestrationMaxConcurrentRunsPerTreeOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Orchestration.MaxConcurrentRunsPerTree; got != 4 {
		t.Fatalf("missing file: MaxConcurrentRunsPerTree = %d, want default 4", got)
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("orchestration:\n  maxConcurrentRunsPerTree: 2\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Orchestration.MaxConcurrentRunsPerTree; got != 2 {
		t.Fatalf("override: MaxConcurrentRunsPerTree = %d, want 2", got)
	}
}

// TestLoad_OrchestrationMaxConcurrentRunsPerTreeRejectsZero proves Load
// rejects a limit below 1.
func TestLoad_OrchestrationMaxConcurrentRunsPerTreeRejectsZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("orchestration:\n  maxConcurrentRunsPerTree: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load() = nil error, want rejection of maxConcurrentRunsPerTree 0")
	}
}

func TestDefault_LogRotation(t *testing.T) {
	t.Parallel()
	got := Default().Log
	if got.MaxSizeMB != 10 || got.MaxFiles != 5 {
		t.Fatalf("Default().Log = %+v, want 10 MiB x 5 files", got)
	}
}

func TestLoad_LogRotationOverrideAndValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("log:\n  maxSizeMB: 2\n  maxFiles: 3\n")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Log.MaxSizeMB != 2 || cfg.Log.MaxFiles != 3 {
		t.Fatalf("Log = %+v, want 2 x 3", cfg.Log)
	}

	// A key left out keeps its default.
	write("log:\n  maxFiles: 7\n")
	if cfg, err = Load(); err != nil || cfg.Log.MaxSizeMB != 10 || cfg.Log.MaxFiles != 7 {
		t.Fatalf("partial override: %+v, %v", cfg.Log, err)
	}

	for _, bad := range []string{"log:\n  maxSizeMB: 0\n", "log:\n  maxFiles: 0\n", "log:\n  maxFiles: -1\n"} {
		write(bad)
		if _, err := Load(); err == nil {
			t.Errorf("Load() accepted %q", bad)
		}
	}
}

func TestLoadFile_ReadsAnotherUsersConfigWithoutMigrating(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 5555\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil || cfg.Server.Port != 5555 {
		t.Fatalf("LoadFile = %+v, %v", cfg.Server, err)
	}
	if cfg, err := LoadFile(filepath.Join(dir, "missing.yaml")); err != nil || cfg.Server.Port != 4648 {
		t.Fatalf("missing file must give defaults: %+v, %v", cfg.Server, err)
	}
}

func TestDirFor_IsDotSmind(t *testing.T) {
	home := t.TempDir()
	if got := DirFor(home); got != filepath.Join(home, ".smind") {
		t.Errorf("DirFor = %s", got)
	}
}

func TestConfig_OrchestrationNotifyDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	load := func(yaml string) (Config, error) {
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		return Load()
	}

	if d := Default().Orchestration; !d.NotifyParent || d.NotifyDebounceSeconds != 20 {
		t.Fatalf("Default() = %+v, want notifyParent true, debounce 20", d)
	}
	cfg, err := load("orchestration:\n  maxDepth: 2\n")
	if err != nil || !cfg.Orchestration.NotifyParent || cfg.Orchestration.NotifyDebounceSeconds != 20 {
		t.Fatalf("absent keys: %+v, %v, want true/20", cfg.Orchestration, err)
	}
	cfg, err = load("orchestration:\n  notifyParent: false\n  notifyDebounceSeconds: 0\n")
	if err != nil || cfg.Orchestration.NotifyParent || cfg.Orchestration.NotifyDebounceSeconds != 0 {
		t.Fatalf("explicit false/0: %+v, %v", cfg.Orchestration, err)
	}
	cfg, err = load("orchestration:\n  notifyParent: true\n  notifyDebounceSeconds: 45\n")
	if err != nil || !cfg.Orchestration.NotifyParent || cfg.Orchestration.NotifyDebounceSeconds != 45 {
		t.Fatalf("explicit true/45: %+v, %v", cfg.Orchestration, err)
	}
	if _, err := load("orchestration:\n  notifyDebounceSeconds: -1\n"); err == nil {
		t.Fatal("Load() accepted a negative notifyDebounceSeconds")
	}
}
