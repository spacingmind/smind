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
	Server ServerConfig `yaml:"server"`
}

// ServerConfig controls the HTTP server.
type ServerConfig struct {
	// Port defaults to 4648.
	Port int `yaml:"port"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Server: ServerConfig{Port: 4648},
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
	return cfg, nil
}
