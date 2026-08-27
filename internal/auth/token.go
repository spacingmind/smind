// Package auth implements smind's daemon-level bearer token: a single
// shared secret gating the HTTP API, not a user/session system.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	tokenFileName = "token"
	tokenBytes    = 32
)

// TokenPath returns the location of the persisted auth token under dir.
func TokenPath(dir string) string {
	return filepath.Join(dir, tokenFileName)
}

// LoadOrCreateToken reads the auth token from dir/token, generating and
// persisting a new random one (0600) if none exists yet. Reusing an
// existing token means restarting the daemon doesn't invalidate clients
// that already have it saved.
func LoadOrCreateToken(dir string) (string, error) {
	path := TokenPath(dir)

	data, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read token: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", fmt.Errorf("write token: %w", err)
	}
	return token, nil
}

func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
