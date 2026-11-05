package taskrunner

import (
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
)

// NativeSession says where a provider keeps the session of a chat's latest
// turn (ADR-0053 native_ref): enough for a later reader to find it, never the
// session's content.
type NativeSession struct {
	Harness   string // "claude-code", "codex-app-server" or "acp"
	SessionID string
	// ConfigDir is the CLAUDE_CONFIG_DIR the claude CLI ran with (a private one
	// for a compatible-endpoint instance); CodexHome the CODEX_HOME for codex.
	ConfigDir string
	CodexHome string
}

// NativeSession returns the session handle of chatID's provider and the
// directories the CLI was (or will be) launched with. false when the chat has
// no handle for provider (a handle of another provider is never reported).
func (r *Runner) NativeSession(chatID int64, provider Provider) (NativeSession, bool) {
	handle, ok := r.sessionStore.Get(chatID)
	if !ok || handle.SessionID == "" || handle.Provider != provider {
		return NativeSession{}, false
	}
	spec, _ := r.providerRegistry().Get(string(provider))
	out := NativeSession{Harness: string(spec.Harness()), SessionID: handle.SessionID}
	env := r.ProviderEnv(provider)
	switch spec.Harness() {
	case providers.HarnessClaudeCode:
		out.ConfigDir = claudeConfigDirFor(spec, env)
	case providers.HarnessCodexAppServer:
		out.CodexHome = codexHomeFor(spec, env)
	}
	return out, true
}

// claudeConfigDirFor is the CLAUDE_CONFIG_DIR a claude run of spec uses, the
// same decision claudeConfigDirEntries makes at launch, without creating it.
func claudeConfigDirFor(spec providers.Spec, env map[string]string) string {
	if v := env["CLAUDE_CONFIG_DIR"]; v != "" {
		return v
	}
	if spec.IsInstance() && !spec.InheritClaudeConfig && env["ANTHROPIC_BASE_URL"] != "" {
		return filepath.Join(config.Dir(), "providers", spec.ID, "claude")
	}
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".claude")
	}
	return ""
}

// codexHomeFor is the CODEX_HOME a codex run of spec uses, as codexEndpointLaunch.
func codexHomeFor(spec providers.Spec, env map[string]string) string {
	if v := env["CODEX_HOME"]; v != "" {
		return v
	}
	if spec.HasEndpoint() {
		return filepath.Join(config.Dir(), "providers", spec.ID, "codex")
	}
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".codex")
	}
	return ""
}

// SessionRoots lists the config roots a run of provider/harness is launched with
// right now: where its session files may be read from (ADR-0053). A stored
// native_ref cannot name any other directory.
func (r *Runner) SessionRoots(provider, harness string) []string {
	spec, ok := r.providerRegistry().Get(provider)
	if !ok {
		return nil
	}
	env := r.ProviderEnv(Provider(provider))
	var dir string
	switch providers.Harness(harness) {
	case providers.HarnessClaudeCode:
		dir = claudeConfigDirFor(spec, env)
	case providers.HarnessCodexAppServer:
		dir = codexHomeFor(spec, env)
	}
	if dir == "" {
		return nil
	}
	return []string{dir}
}

// ProviderSecretValues are the exact secret values configured for provider, for
// scrubbing text that must not show them.
func (r *Runner) ProviderSecretValues(provider string) []string {
	return r.providerSecretValues(Provider(provider))
}
