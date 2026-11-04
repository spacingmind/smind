package taskrunner

// ThinkingLevel is a run's reasoning level, scoped to the provider (and
// model) it was validated for -- see ResolveLevel. For a Claude Code native
// run it is off/standard/extended, applied through claude-agent-sdk-go's
// thinking Options (see runClaudeNative). For a Codex native run it is one of
// the model's own reasoning-effort ids as model/list advertises them
// (low/medium/high/xhigh/..., never translated), applied as turn/start's
// `effort`. GLM/Kimi's equivalent is a live-session-scoped ACP ConfigOption
// (internal/taskrunner/config_options.go), so every other provider drops the
// level. The zero value means "the provider's own default" everywhere.
type ThinkingLevel string

const (
	// ThinkingLevelUnspecified is the zero value: no thinking Option is
	// added to the Claude Code session at all, preserving whatever the SDK
	// itself defaults to. This is what an older client (or any non-Claude
	// provider) gets by omitting the field entirely -- see
	// runClaudeNative's ThinkingLevel switch.
	ThinkingLevelUnspecified ThinkingLevel = ""

	// ThinkingLevelOff explicitly disables thinking (claudecode.WithDisabledThinking).
	ThinkingLevelOff ThinkingLevel = "off"

	// ThinkingLevelStandard enables adaptive thinking (claudecode.WithAdaptiveThinking):
	// the model itself decides how much budget a given turn needs, rather
	// than a fixed cap -- a reasonable middle tier between off and a large
	// fixed budget.
	ThinkingLevelStandard ThinkingLevel = "standard"

	// ThinkingLevelExtended enables a large fixed thinking budget
	// (claudecode.WithThinkingBudget(extendedThinkingBudgetTokens)) for
	// turns that need to reason at length before acting.
	ThinkingLevelExtended ThinkingLevel = "extended"
)

// extendedThinkingBudgetTokens is the fixed token budget
// ThinkingLevelExtended passes to claudecode.WithThinkingBudget. 32000 is a
// deliberately generous cap (Claude's extended-thinking guidance commonly
// cites budgets in this range for genuinely hard, multi-step reasoning) --
// this tier exists specifically for tasks where ThinkingLevelStandard's
// adaptive budget isn't enough, so a small number here would defeat the
// point of offering a separate "go big" tier at all.
const extendedThinkingBudgetTokens = 32000

// maxLevelLen bounds a level string's length.
const maxLevelLen = 32

// IsValid reports whether l has the shape of a level: empty, or a lowercase
// token. It is deliberately syntax-only -- which levels exist depends on the
// provider and model, so membership is ResolveLevel's job. A malformed wire
// value still surfaces as an error instead of being passed on.
func (l ThinkingLevel) IsValid() bool {
	if l == "" {
		return true
	}
	if len(l) > maxLevelLen {
		return false
	}
	for i, c := range l {
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-'):
		default:
			return false
		}
	}
	return true
}
