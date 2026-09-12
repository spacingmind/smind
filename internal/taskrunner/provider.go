// Package taskrunner ties a store.Task to a real running agent, spawning
// either internal/acp (GLM, Kimi, or any other ACP-speaking agent) or
// github.com/spacingmind/claude-agent-sdk-go (Claude Code's native headless
// protocol) and exposing one unified streaming interface to callers
// regardless of which backend a given task's agent actually speaks.
package taskrunner

// Provider identifies which backend agent protocol drives a task's turn.
type Provider string

const (
	// ProviderClaudeNative drives a task via Claude Code's native headless
	// CLI protocol (github.com/spacingmind/claude-agent-sdk-go).
	ProviderClaudeNative Provider = "claude-native"

	// ProviderGLM drives a task via the GLM ACP agent, spoken over the
	// Agent Client Protocol (internal/acp).
	ProviderGLM Provider = "glm"

	// ProviderKimi drives a task via Moonshot AI's Kimi CLI, also spoken
	// over the Agent Client Protocol (internal/acp) -- Kimi's CLI speaks
	// ACP natively, the same as GLM's, just a different spawned command.
	ProviderKimi Provider = "kimi"

	// ProviderCodexNative drives a task via OpenAI's Codex CLI, spoken over
	// its own native "app-server" JSON-RPC-over-stdio protocol
	// (internal/codex) -- not ACP, unlike GLM/Kimi; see internal/codex's
	// package doc comment for why.
	ProviderCodexNative Provider = "codex-native"
)

// ProviderKind classifies how a Provider's authentication is managed, for
// clients that need to render it (see ProviderInfo.Kind).
type ProviderKind string

// ProviderKindCLI marks a Provider that's spawned as an external CLI
// subprocess which manages its own authentication out of band (e.g. GLM's
// `npx -y glm-acp-agent`, see internal/acp/glm.go) -- the daemon tracks no
// credential for it at all, so there's nothing for account.add/
// account.oauthStart to do. Clients should render this as "managed
// externally", not offer a credential form or flag a missing key.
const ProviderKindCLI ProviderKind = "cli"

// ProviderInfo describes one supported Provider for clients (the wsapi
// provider.list method's wire shape): the Provider id itself, plus an
// optional human-facing label and an optional Kind. Kind is empty for
// providers whose auth is handled through the separate account-credential
// system (internal/accounts, distinct ID vocabulary -- see
// accounts-dialog.tsx's doc comment) rather than by this package.
type ProviderInfo struct {
	ID    Provider     `json:"id"`
	Label string       `json:"label,omitempty"`
	Kind  ProviderKind `json:"kind,omitempty"`
}

// SupportedProviders is the single source of truth for which providers the
// daemon supports -- every provider RunPrompt can dispatch to, in dropdown
// display order, with their human labels. provider.list serves this; the
// web UI renders its dropdown from it. A provider added here (and to
// RunPrompt's switch) shows up in the UI without client changes.
func SupportedProviders() []ProviderInfo {
	return []ProviderInfo{
		{ID: ProviderClaudeNative, Label: "Claude Code"},
		{ID: ProviderGLM, Label: "GLM", Kind: ProviderKindCLI},
		{ID: ProviderKimi, Label: "Kimi"},
		{ID: ProviderCodexNative, Label: "Codex"},
	}
}
