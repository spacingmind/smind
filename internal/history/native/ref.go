// Package native rebuilds the full history of a finished run from the
// provider's own session file (ADR-0053 D4): the Claude Code JSONL transcript
// and the Codex rollout. It is read-only, runs only on the daemon that owns the
// run, and never errors for a session it cannot use: the caller gets a Reason
// and falls back to the stored skeleton.
//
// Nothing here trusts a path it was not derived for: a file is found from the
// run's own native_ref and an allow-listed config root, resolved, and checked
// to be a regular file inside that root before it is opened.
package native

import "time"

// Ref is runs.native_ref: where the provider keeps the session this run was a
// turn of. The daemon writes it at run end (internal/runs).
type Ref struct {
	Provider  string `json:"provider"`
	Harness   string `json:"harness"`
	SessionID string `json:"sessionId"`
	// ConfigDir / CodexHome are the CLI's config roots as launched.
	ConfigDir string `json:"configDir,omitempty"`
	CodexHome string `json:"codexHome,omitempty"`
	// TranscriptPath is the session file when it existed at capture time. It is
	// only a hint: the file actually read is re-derived and must agree.
	TranscriptPath  string     `json:"transcriptPath,omitempty"`
	Cwd             string     `json:"cwd,omitempty"`
	ProviderVersion string     `json:"providerVersion,omitempty"`
	TurnWindow      TurnWindow `json:"turnWindow"`
	PromptSha256    string     `json:"promptSha256"`
	CapturedAt      time.Time  `json:"capturedAt"`
}

// TurnWindow is when the run started and finished.
type TurnWindow struct {
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

// Harness names of Ref.Harness (providers.Harness values).
const (
	HarnessClaudeCode = "claude-code"
	HarnessCodex      = "codex-app-server"
	HarnessACP        = "acp"
)

// Reason says why a history could not be rebuilt; the caller shows the stored
// skeleton with it (ADR-0053 section 5).
type Reason string

const (
	ReasonNone            Reason = ""
	ReasonCannotRebuild   Reason = "provider_cannot_rebuild" // no reader for this harness, or no native_ref
	ReasonSessionMissing  Reason = "native_session_missing"  // file gone, outside the root, not a regular file
	ReasonSliceNotFound   Reason = "slice_not_found"         // no turn matches the prompt hash and window, or several do
	ReasonFormatDrift     Reason = "format_drift"            // the session failed the self-check, or the breaker is open
	ReasonFailed          Reason = "rebuild_failed"          // unreadable, timeout, or over the size bound
	ReasonBusy            Reason = "rebuild_busy"            // the concurrency bound is full
	ReasonHumanOnly       Reason = "rebuild_not_requested"   // asked by an agent or automation (H7)
	ReasonSessionInUse    Reason = "native_session_in_use"   // the chat has a running run
	ReasonConfirmRequired Reason = "confirm_required"        // the load would start an agent and the human has not confirmed
	ReasonForkUnsupported Reason = "fork_unsupported"        // the harness cannot fork a session at a turn (ADR-0057)
)

// TimeTolerance is how far a turn may lie outside the run's window: a clock
// that moved, or a turn that started a moment before smind recorded the run.
const TimeTolerance = 2 * time.Minute
