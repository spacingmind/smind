// Package taskrunner ties a store.Task to a real running agent, spawning
// either internal/acp (GLM, or any other ACP-speaking agent) or
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
)
