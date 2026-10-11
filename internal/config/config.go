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
	Agents        AgentsConfig        `yaml:"agents"`
}

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
}

// AgentsConfig holds per-agent settings for the ACP agents smind spawns.
type AgentsConfig struct {
	GLM GLMConfig `yaml:"glm"`
}

// GLMConfig configures the GLM ACP agent (glm-acp-agent).
type GLMConfig struct {
	// MaxTurns caps the model calls (turns) the agent makes per prompt,
	// passed as --max-turns. 0 or unset keeps the agent's own default
	// (100 in glm-acp-agent 1.14.0). Negative values are rejected by Load.
	MaxTurns int `yaml:"maxTurns"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Server:        ServerConfig{Port: 4648},
		Orchestration: OrchestrationConfig{MaxDepth: 2},
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
	if cfg.Agents.GLM.MaxTurns < 0 {
		return cfg, fmt.Errorf("config %s: agents.glm.maxTurns must be >= 0, got %d", Path(), cfg.Agents.GLM.MaxTurns)
	}
	return cfg, nil
}
