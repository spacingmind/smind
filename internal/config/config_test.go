package config

import (
	"os"
	"path/filepath"
	"strings"
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

// TestConfig_GLMMaxTurns proves agents.glm.maxTurns parses, defaults to
// zero (the agent's own cap), and a missing file is fine.
func TestConfig_GLMMaxTurns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Agents.GLM.MaxTurns; got != 0 {
		t.Fatalf("missing file: MaxTurns = %d, want 0", got)
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("agents:\n  glm:\n    maxTurns: 150\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Agents.GLM.MaxTurns; got != 150 {
		t.Fatalf("MaxTurns = %d, want 150", got)
	}
}

func TestConfig_GLMMaxTurnsNegativeRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("agents:\n  glm:\n    maxTurns: -1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "agents.glm.maxTurns") {
		t.Fatalf("Load() error = %v, want one naming agents.glm.maxTurns", err)
	}
}
