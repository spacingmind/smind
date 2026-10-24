package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProviderEnvKey is one launch-env variable a user may set for a provider
// (providers.<id>.env.<Key>). Every key is a filesystem path.
type ProviderEnvKey struct {
	Key string
	// Label is the Settings field label.
	Label string
	// Short names the setting in one-line diagnostics ("config dir").
	Short string
	// DefaultDir is the CLI's own location when the variable is unset,
	// relative to the user's home directory.
	DefaultDir string
}

// ProviderEnvKeys is the allowlist: the only launch env a user can set, per
// provider id. Adding another key is one line here. No tokens, PATH or API
// keys: these are config directories only.
var ProviderEnvKeys = map[string][]ProviderEnvKey{
	"claude-native": {{Key: "CLAUDE_CONFIG_DIR", Label: "Claude config directory", Short: "config dir", DefaultDir: ".claude"}},
	"codex-native":  {{Key: "CODEX_HOME", Label: "Codex home", Short: "codex home", DefaultDir: ".codex"}},
}

// ProviderEnvKeyNames lists the allowed keys of provider (empty when it has none).
func ProviderEnvKeyNames(provider string) []string {
	var names []string
	for _, k := range ProviderEnvKeys[provider] {
		names = append(names, k.Key)
	}
	return names
}

// ProviderEnvKeyError is a rejected key: not allowed for the provider.
type ProviderEnvKeyError struct {
	Provider, Key string
}

func (e *ProviderEnvKeyError) Error() string {
	allowed := ProviderEnvKeyNames(e.Provider)
	if len(allowed) == 0 {
		return fmt.Sprintf("provider %q has no settable launch env", e.Provider)
	}
	return fmt.Sprintf("env key %q is not allowed for provider %q (allowed: %s)", e.Key, e.Provider, strings.Join(allowed, ", "))
}

// ValidateProviderEnv checks one providers.<provider>.env entry and returns
// the value to store: a leading "~/" is expanded against the daemon user's
// home and the path is cleaned. The key must be on the allowlist; the value
// must be an absolute path with no NUL or control characters. The directory
// need not exist (a drive may be unmounted).
func ValidateProviderEnv(provider, key, value string) (string, error) {
	if err := checkProviderEnvKey(provider, key); err != nil {
		return "", err
	}
	for _, c := range value {
		if c < 0x20 || c == 0x7f {
			return "", fmt.Errorf("%s: the path must not contain NUL or control characters", key)
		}
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%s: cannot expand ~: %w", key, err)
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~"))
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s: the path must be absolute (or start with ~/), got %q", key, value)
	}
	return filepath.Clean(value), nil
}

func checkProviderEnvKey(provider, key string) error {
	for _, k := range ProviderEnvKeys[provider] {
		if k.Key == key {
			return nil
		}
	}
	return &ProviderEnvKeyError{Provider: provider, Key: key}
}

// SetProviderEnv sets (or, for a blank value, removes)
// providers.<provider>.env.<key> through the same code path as
// SetProviderDefaultModel and returns the stored (expanded) value.
func SetProviderEnv(provider, key, value string) (string, error) {
	known := false
	for _, id := range DefaultModelProviders {
		known = known || id == provider
	}
	if !known {
		return "", fmt.Errorf("unknown provider %q", provider)
	}
	if err := checkProviderEnvKey(provider, key); err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", mutate(func(root *yaml.Node) error {
			removeNode(root, []string{"providers", provider, "env", key})
			return nil
		})
	}
	clean, err := ValidateProviderEnv(provider, key, strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return clean, mutate(func(root *yaml.Node) error {
		setNode(root, []string{"providers", provider, "env", key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: clean})
		return nil
	})
}

// ProviderEnv returns providers.<provider>.env, nil when unset.
func (c Config) ProviderEnv(provider string) map[string]string {
	return c.Providers[provider].Env
}
