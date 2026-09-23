package taskrunner

// ThinkingLevel controls how much extended-thinking budget a Claude Code
// native run gets, via claude-agent-sdk-go's own thinking Options (see
// runClaudeNative). It's Claude-only: GLM/Kimi's equivalent is an entirely
// different, live-session-scoped mechanism (ACP's ConfigOption, see
// internal/taskrunner/config_options.go), and Codex has no reachable
// per-turn reasoning-effort knob at all (confirmed against codex-rs's
// app-server protocol -- see
// docs/plans/active/task-move-approval-thinking.md's Context). So this type
// only ever matters when Provider is ProviderClaudeNative; every other
// provider ignores it.
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

// IsValid reports whether l is one of the ThinkingLevel values this package
// knows how to apply -- same rejection-over-silent-fallback rationale as
// ApprovalPolicy.IsValid (see its doc comment): an unrecognized value from
// a wire caller should surface as an error, not silently collapse to
// ThinkingLevelUnspecified.
func (l ThinkingLevel) IsValid() bool {
	switch l {
	case ThinkingLevelUnspecified, ThinkingLevelOff, ThinkingLevelStandard, ThinkingLevelExtended:
		return true
	default:
		return false
	}
}
