// Package config loads smind daemon configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the root daemon configuration.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Orchestration OrchestrationConfig `yaml:"orchestration"`
	Worktrees     WorktreesConfig     `yaml:"worktrees"`
	Agents        AgentsConfig        `yaml:"agents"`
}

// WorktreesConfig holds daemon-wide knobs for task worktree setup/teardown
// scripts and per-task ports (ADR-0024).
type WorktreesConfig struct {
	Ports PortsConfig `yaml:"ports"`
	// SetupTimeoutSeconds bounds a setup script (default 600).
	SetupTimeoutSeconds int `yaml:"setupTimeoutSeconds"`
	// TeardownTimeoutSeconds bounds a teardown script (default 120).
	TeardownTimeoutSeconds int `yaml:"teardownTimeoutSeconds"`
}

// PortsConfig configures per-task port allocation.
type PortsConfig struct {
	// Range is "lo-hi" within 1024-65535; empty disables port allocation.
	Range string `yaml:"range"`
}

// PortRange parses Ports.Range. ok is false when no range is configured.
func (p PortsConfig) PortRange() (lo, hi int, ok bool, err error) {
	if p.Range == "" {
		return 0, 0, false, nil
	}
	if _, err := fmt.Sscanf(p.Range, "%d-%d", &lo, &hi); err != nil {
		return 0, 0, false, fmt.Errorf("worktrees.ports.range %q: want \"lo-hi\"", p.Range)
	}
	if lo < 1024 || hi > 65535 || lo > hi {
		return 0, 0, false, fmt.Errorf("worktrees.ports.range %q: need 1024 <= lo <= hi <= 65535", p.Range)
	}
	return lo, hi, true, nil
}

// AgentsConfig holds per-agent tuning.
type AgentsConfig struct {
	GLM GLMAgentConfig `yaml:"glm"`
}

// GLMAgentConfig tunes the GLM ACP agent.
type GLMAgentConfig struct {
	// MaxTurns, when set, is passed to the agent as --max-turns <n> so one
	// long prompt cannot exhaust its output budget. Unset (nil) keeps the
	// agent's own default; Load rejects values outside [1, 10000].
	MaxTurns *int `yaml:"maxTurns"`
}

// MaxGLMTurns is the upper bound accepted for agents.glm.maxTurns.
const MaxGLMTurns = 10000

// ServerConfig controls the HTTP server.
type ServerConfig struct {
	// Port defaults to 4648.
	Port int `yaml:"port"`
}

// OrchestrationConfig guards agent-driven task orchestration (a task
// spawning child tasks of its own, e.g. via the MCP task_new tool).
type OrchestrationConfig struct {
	// MaxDepth caps how many ancestors a task may have: a root task is at
	// depth 0, so the default (2) allows a root task's children and
	// grandchildren but rejects a great-grandchild. Enforced by
	// internal/workspace.Manager.CreateTask.
	MaxDepth int `yaml:"maxDepth"`
	// MaxConcurrentRunsPerTree caps how many runs may be StatusRunning at
	// once across one task tree (a root task and all its descendants).
	// Default 4; Load rejects values < 1.
	MaxConcurrentRunsPerTree int `yaml:"maxConcurrentRunsPerTree"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Server:        ServerConfig{Port: 4648},
		Orchestration: OrchestrationConfig{MaxDepth: 2, MaxConcurrentRunsPerTree: 4},
		Worktrees: WorktreesConfig{
			SetupTimeoutSeconds:    600,
			TeardownTimeoutSeconds: 120,
		},
	}
}

// Dir returns the smind home directory (~/.spacingmind).
func Dir() string {
	if v := os.Getenv("SMIND_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".spacingmind"
	}
	return filepath.Join(home, ".spacingmind")
}

// Path returns the config file location.
func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}

// Load reads the config file, falling back to defaults for missing keys.
// A missing file is not an error.
func Load() (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", Path(), err)
	}
	if _, _, _, err := cfg.Worktrees.Ports.PortRange(); err != nil {
		return cfg, fmt.Errorf("config %s: %w", Path(), err)
	}
	if n := cfg.Orchestration.MaxConcurrentRunsPerTree; n < 1 {
		return cfg, fmt.Errorf("config %s: orchestration.maxConcurrentRunsPerTree must be >= 1, got %d", Path(), n)
	}
	if mt := cfg.Agents.GLM.MaxTurns; mt != nil && (*mt < 1 || *mt > MaxGLMTurns) {
		return cfg, fmt.Errorf("config %s: agents.glm.maxTurns must be between 1 and %d, got %d", Path(), MaxGLMTurns, *mt)
	}
	return cfg, nil
}
