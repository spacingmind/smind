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
