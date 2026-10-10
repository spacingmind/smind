package taskrunner

// UsageSource says which backend report a Usage was read from (ADR-0020).
type UsageSource string

const (
	// UsageSourceClaudeResult is the stream-json "result" message at the
	// end of a claude-native turn.
	UsageSourceClaudeResult UsageSource = "claude_result"
	// UsageSourceCodexTokenUsage is codex's thread/tokenUsage/updated.
	UsageSourceCodexTokenUsage UsageSource = "codex_token_usage"
	// UsageSourceACPPromptUsage is ACP's PromptResponse.usage (cumulative
	// for the session; the Usage holds the per-run difference).
	UsageSourceACPPromptUsage UsageSource = "acp_prompt_usage"
	// UsageSourceACPUsageUpdate is ACP's usage_update notification (context
	// used/size and cumulative cost), used when PromptResponse.usage is absent.
	UsageSourceACPUsageUpdate UsageSource = "acp_usage_update"
	// UsageSourceNotReported means the turn finished but the backend
	// reported nothing: every other field is nil. Never estimated.
	UsageSourceNotReported UsageSource = "not_reported"
)

// Usage is the provider-agnostic usage one run's turn consumed, normalized
// from the agent's own events (ADR-0020). Every numeric field is a pointer:
// nil means the backend did not report it. Values are never estimated.
type Usage struct {
	Input       *int64      `json:"input,omitempty"`
	CachedInput *int64      `json:"cachedInput,omitempty"` // cache read
	CacheWrite  *int64      `json:"cacheWrite,omitempty"`
	Output      *int64      `json:"output,omitempty"`
	Reasoning   *int64      `json:"reasoning,omitempty"`
	CostUSD     *float64    `json:"costUsd,omitempty"`
	Model       *string     `json:"model,omitempty"`
	ContextUsed *int64      `json:"contextUsed,omitempty"`
	ContextSize *int64      `json:"contextSize,omitempty"`
	Source      UsageSource `json:"source"`

	// SessionSnapshot is the ACP session-cumulative counters (and the
	// session id they belong to) as of this turn's end, kept so the next run
	// on the same session can difference against it. ACP only; nil elsewhere.
	SessionSnapshot *UsageSnapshot `json:"sessionSnapshot,omitempty"`
}

// UsageSnapshot is the session-cumulative counters an ACP agent reported at
// the end of a run. SessionID scopes it: a snapshot for a different session
// is ignored (a new session starts from zero).
type UsageSnapshot struct {
	SessionID   string   `json:"sessionId"`
	Input       *int64   `json:"input,omitempty"`
	CachedInput *int64   `json:"cachedInput,omitempty"`
	CacheWrite  *int64   `json:"cacheWrite,omitempty"`
	Output      *int64   `json:"output,omitempty"`
	Reasoning   *int64   `json:"reasoning,omitempty"`
	CostUSD     *float64 `json:"costUsd,omitempty"`
}
