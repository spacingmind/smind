// Package bridge wires the daemon itself into a relay as a client: it
// persists the `smind relay connect` triple (address, workspace ID,
// secret) plus the relay's pinned TLS fingerprint, and runs the background
// loop that dials the relay, admits, and bridges each relay-carried E2EE
// data session into the same internal/wsapi RPC dispatch table the
// WebSocket path already uses.
//
// This closes the gap docs/plans/active/mobile-app-milestone-1.md's
// Context calls out: internal/relay/client is a real, tested Go library,
// but nothing previously called it from `smind serve` itself.
package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	configFileName = "relay-connect.json"
	configVersion  = 1
)

// Config is the daemon's persisted relay-client configuration: everything
// `smind relay connect` gathers, so `smind serve` can dial the relay
// without any further input.
type Config struct {
	// RelayAddress is the relay's host:port (as dialled by
	// internal/relay/client.Dial).
	RelayAddress string
	// WorkspaceID is the workspace this daemon is enrolled as, per `smind
	// relay workspace new`.
	WorkspaceID string
	// SecretHex is the hex-encoded workspace secret `smind relay workspace
	// new` printed once.
	SecretHex string
	// Fingerprint is the relay's TLS certificate fingerprint (hex SHA-256
	// of the DER cert), captured via trust-on-first-use at `relay connect`
	// time (see FetchFingerprint) and pinned on every dial thereafter,
	// exactly like internal/relay/client.Dial already requires.
	Fingerprint string
}

// persistedConfig is the on-disk JSON shape.
type persistedConfig struct {
	V           int    `json:"v"`
	Address     string `json:"relayAddress"`
	WorkspaceID string `json:"workspaceId"`
	SecretHex   string `json:"secretHex"`
	Fingerprint string `json:"fingerprint"`
}

// ConfigPath returns the location of the persisted relay-connect config
// under dir (the daemon's $SMIND_HOME, i.e. internal/config.Dir()) -- the
// same directory internal/relay/e2ee's daemon keypair lives in.
func ConfigPath(dir string) string {
	return filepath.Join(dir, configFileName)
}

// SaveConfig persists cfg under dir (mode 0600 -- it carries the workspace
// secret in the clear, same sensitivity as the e2ee keypair and auth
// token this package's sibling packages already persist alongside it).
func SaveConfig(dir string, cfg Config) error {
	data, err := json.Marshal(persistedConfig{
		V:           configVersion,
		Address:     cfg.RelayAddress,
		WorkspaceID: cfg.WorkspaceID,
		SecretHex:   cfg.SecretHex,
		Fingerprint: cfg.Fingerprint,
	})
	if err != nil {
		return fmt.Errorf("bridge: encode config: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("bridge: create %s: %w", dir, err)
	}
	if err := os.WriteFile(ConfigPath(dir), data, 0o600); err != nil {
		return fmt.Errorf("bridge: write config: %w", err)
	}
	return nil
}

// LoadConfig reads the relay-connect config from dir. ok is false (with a
// nil error) when no config has been saved yet -- the normal case, and
// what makes relay connectivity opt-in: `smind serve` calls this once at
// startup and only starts the relay bridge when ok is true.
func LoadConfig(dir string) (cfg Config, ok bool, err error) {
	data, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, fmt.Errorf("bridge: read config: %w", err)
	}
	var stored persistedConfig
	if err := json.Unmarshal(data, &stored); err != nil {
		return Config{}, false, fmt.Errorf("bridge: parse config: %w", err)
	}
	if stored.V != configVersion {
		return Config{}, false, fmt.Errorf("bridge: config has version %d, want %d", stored.V, configVersion)
	}
	return Config{
		RelayAddress: stored.Address,
		WorkspaceID:  stored.WorkspaceID,
		SecretHex:    stored.SecretHex,
		Fingerprint:  stored.Fingerprint,
	}, true, nil
}
