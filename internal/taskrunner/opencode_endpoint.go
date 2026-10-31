package taskrunner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/securefile"
)

// openCodeHomeDirs are the XDG roots OpenCode keeps its state under; an
// endpoint instance gets all four inside its own private home.
var openCodeHomeDirs = []struct{ env, dir string }{
	{"XDG_CONFIG_HOME", "config"},
	{"XDG_DATA_HOME", "data"},
	{"XDG_STATE_HOME", "state"},
	{"XDG_CACHE_HOME", "cache"},
}

// openCodeEndpointEntries returns the launch env of an OpenCode instance that
// points at a chat-completions endpoint (ADR-0046: the "chat-only vendor"
// route); nil for any other provider. It is the whole translation of the
// instance into OpenCode's own configuration:
//
//   - the endpoint, models and a key REFERENCE become an inline config in
//     OPENCODE_CONFIG_CONTENT; the key itself is the SMIND_OPENCODE_API_KEY
//     entry resolved from the secret store by launchEnvEntries, so it exists
//     only in the child's environment, never in a config, a file or an argv;
//   - the instance gets a private home under <SMIND_HOME>/providers/<id>/opencode
//     (XDG_*), so it neither reads nor writes the user's own OpenCode
//     config, login or sessions, and what it stores (conversations) is
//     readable by the current user only (securefile.RestrictDir: 0700, or an
//     inheritable user+SYSTEM ACL on Windows);
//   - the repository's own opencode.json is ignored (a repo could otherwise
//     name a provider of its own that reads {env:SMIND_OPENCODE_API_KEY} and
//     so receive the key), and update, share, LSP-download and models-catalog
//     fetches are switched off.
func (r *Runner) openCodeEndpointEntries(spec providers.Spec, provider Provider) ([]string, error) {
	if !spec.IsOpenCodeChat() {
		return nil, nil
	}
	cfg, err := providers.OpenCodeChatConfig(spec, r.openCodeKeyConfigured(spec, provider))
	if err != nil {
		return nil, fmt.Errorf("taskrunner: %w", err)
	}
	home := filepath.Join(config.Dir(), "providers", spec.ID, "opencode")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("taskrunner: provider %q: opencode home: %w", spec.ID, err)
	}
	// The root first, so every directory and file created below inherits it.
	if err := securefile.RestrictDir(home); err != nil {
		return nil, fmt.Errorf("taskrunner: provider %q: opencode home: %w", spec.ID, err)
	}
	entries := make([]string, 0, len(openCodeHomeDirs)+6)
	for _, d := range openCodeHomeDirs {
		dir := filepath.Join(home, d.dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("taskrunner: provider %q: opencode home: %w", spec.ID, err)
		}
		if err := securefile.RestrictDir(dir); err != nil {
			return nil, fmt.Errorf("taskrunner: provider %q: opencode home: %w", spec.ID, err)
		}
		entries = append(entries, d.env+"="+dir)
	}
	entries = append(entries,
		"OPENCODE_CONFIG_CONTENT="+cfg,
		"OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_SHARE=1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD=1",
		"OPENCODE_DISABLE_MODELS_FETCH=1",
	)
	return entries, nil
}

// openCodeKeyConfigured reports whether the instance supplies a key for the
// endpoint: a stored secret, a literal or a passthrough variable bound to
// SMIND_OPENCODE_API_KEY. Without one the generated config carries no apiKey
// (a local server that needs none).
func (r *Runner) openCodeKeyConfigured(spec providers.Spec, provider Provider) bool {
	if spec.SecretEnv[providers.OpenCodeKeyEnv] != "" || r.ProviderEnv(provider)[providers.OpenCodeKeyEnv] != "" {
		return true
	}
	for _, name := range spec.PassthroughEnv {
		if name == providers.OpenCodeKeyEnv {
			return true
		}
	}
	return false
}

// acpModelFor is the model id an ACP run asks its agent for: the instance's
// model id as listed, except for an OpenCode endpoint instance, whose agent
// knows it as "<provider key>/<model>".
func (r *Runner) acpModelFor(provider Provider, model string) string {
	if spec, ok := r.providerRegistry().Get(string(provider)); ok && spec.IsOpenCodeChat() {
		return providers.OpenCodeModelID(spec, model)
	}
	return model
}
