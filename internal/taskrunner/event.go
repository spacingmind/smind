package taskrunner

import "encoding/json"

// EventType discriminates which fields of an Event are populated.
//
// New values are always appended, never inserted -- EventType is persisted
// as its bare integer value (internal/runs/persist.go's persistedEvent),
// so inserting a new constant earlier in this list would silently
// reinterpret the type of every already-persisted event on the next
// decode. See docs/decisions/0008-structured-run-events.md's Compatibility
// strategy.
type EventType int

const (
	// EventTypeText is a chunk of assistant text streamed from the agent
	// mid-turn.
	EventTypeText EventType = iota

	// EventTypeDone is the terminal event for a turn: exactly one is sent,
	// last, before the events channel passed to Runner.RunPrompt is closed.
	EventTypeDone

	// EventTypePermissionRequest records a pending human-in-the-loop
	// permission request raised mid-turn (see PermissionDecider). Unlike
	// EventTypeText/EventTypeDone, this is never sent on the events channel
	// RunPrompt owns -- internal/runs constructs a PermissionDecider whose
	// Decide method records this event (and EventTypePermissionResolved)
	// directly via Registry.record, from whichever goroutine the provider
	// dispatches the permission callback on. See internal/runs/registry.go's
	// runPermissionDecider for why that's safe and EventTypeText/EventTypeDone's
	// single-writer events channel is not.
	EventTypePermissionRequest

	// EventTypePermissionResolved records the resolution of a prior
	// EventTypePermissionRequest (same PermissionRequestID). Recorded the
	// same way as EventTypePermissionRequest, immediately after the
	// decider's blocked Decide call wakes up with an answer.
	EventTypePermissionResolved

	// EventTypeUserMessage is a user-turn text chunk, populated by the ACP
	// path's "user_message_chunk" (e.g. a multi-agent GLM/Kimi session
	// synthesizing a follow-up user turn). Claude Code native does not
	// produce this: its UserMessage echoes back the client's own prompt,
	// which the UI already has -- there is nothing new to surface. See
	// docs/decisions/0008-structured-run-events.md.
	EventTypeUserMessage

	// EventTypeThinking is a reasoning/thinking text chunk: Claude Code's
	// ThinkingBlock, or ACP's "agent_thought_chunk".
	EventTypeThinking

	// EventTypeToolCall records one tool call's identity, input, lifecycle
	// status, and result. ToolCallID correlates multiple EventTypeToolCall
	// events for the same call -- typically one with ToolStatus
	// ToolStatusRunning when the call starts, followed later by one with
	// ToolStatusSuccess or ToolStatusFailure carrying ToolResult -- so a
	// UI merges them into one card rather than creating a new one per
	// status change. A later event may omit fields (ToolName, ToolInput)
	// that an earlier one for the same ToolCallID already carried -- ACP's
	// tool_call_update is a partial update, not a restatement of the full
	// call -- so a merge-by-ID, not replace-by-ID, is required.
	//
	// Not produced by Codex-native turns in this pass: internal/codex only
	// models Codex's text-delta notifications today, not its
	// item-lifecycle protocol. See
	// docs/decisions/0008-structured-run-events.md's Decision for why that
	// gap is deliberate.
	EventTypeToolCall
)

// Unified ToolStatus values for Event.ToolStatus, spanning both providers'
// own status vocabularies: Claude Code has no explicit tool-call status at
// all (a ToolUseBlock's completion is only ever known once its matching
// ToolResultBlock arrives), and ACP's ToolCallStatus
// ("pending"/"in_progress"/"completed"/"failed") is richer than a UI card
// needs to distinguish. ToolStatusRunning covers both ACP's "pending" and
// "in_progress" -- neither has a result yet, and a UI's running-state
// rendering (e.g. a spinner) doesn't need to distinguish "not started" from
// "in flight".
const (
	ToolStatusRunning = "running"
	ToolStatusSuccess = "success"
	ToolStatusFailure = "failure"
)

// Event is the provider-agnostic streaming update Runner.RunPrompt emits,
// translated from whichever backend (acp.SessionUpdate for GLM/ACP, or
// claudecode.Message/ResultMessage for Claude Code native) is actually
// driving the task.
//
// Text/user-message/thinking/tool-call events cover what
// docs/decisions/0008-structured-run-events.md's audit found both
// supported providers can actually produce: a text chunk, a reasoning
// chunk, and a tool call's identity/input/status/result. ACP and Claude
// Code each also carry a few things this type still doesn't model (ACP's
// plan updates, Claude Code's other content block kinds) -- Raw carries
// the original update so a caller that needs backend-specific detail
// beyond what's typed here can still get at it, without Event having to
// grow a field per variant speculatively ahead of a caller that needs it.
type Event struct {
	Type EventType

	// Text is populated for EventTypeText, EventTypeUserMessage, and
	// EventTypeThinking -- the three event types that are just "a chunk of
	// text from some role", differing only in Type.
	Text string

	// StopReason is populated for EventTypeDone: ACP's Prompt stop reason,
	// or Claude Code's ResultMessage.StopReason.
	StopReason string

	// Raw is the backend-native value this Event was translated from: an
	// acp.SessionUpdate, or a claudecode.Message (claudecode.AssistantMessage
	// for EventTypeText) / claudecode.ResultMessage (for EventTypeDone).
	// Nil for ACP's EventTypeDone, since ACP's turn-ending signal is just
	// the stop reason string already captured in StopReason. Always nil for
	// the permission event types, which have no single backend-native value
	// (their PermissionOptions are already the provider-agnostic shape).
	Raw any

	// PermissionRequestID identifies one pending permission request,
	// populated for both EventTypePermissionRequest and
	// EventTypePermissionResolved so a resolution can be correlated with
	// the request it answers.
	PermissionRequestID string

	// PermissionSummary describes what's being requested, populated for
	// EventTypePermissionRequest.
	PermissionSummary string

	// PermissionOptions are the choices offered, populated for
	// EventTypePermissionRequest.
	PermissionOptions []PermissionOption

	// PermissionOptionID is the option that was chosen, populated for
	// EventTypePermissionResolved.
	PermissionOptionID string

	// PermissionResolution says *how* PermissionOptionID was decided,
	// populated for EventTypePermissionResolved -- see PermissionResolution's
	// doc comment. This is what lets a run's event/timeline (and run.logs)
	// distinguish an auto-resolution (policy match, or a timeout) from an
	// actual human clicking a button, per
	// docs/plans/active/task-permission-ux.md Item 2.
	PermissionResolution PermissionResolution

	// ToolCallID identifies one tool call, populated for EventTypeToolCall.
	// Correlates a "running" event with the later "success"/"failure"
	// event for the same call -- see EventTypeToolCall's doc comment.
	ToolCallID string

	// ToolName is populated for EventTypeToolCall: the wire tool name for
	// Claude Code (e.g. "Bash", "Read", "Edit") or Codex, or ACP's ToolKind
	// string (e.g. "execute", "read") when the update carries one -- ACP
	// has no separate tool-name field, so ToolKind is the closest analog.
	// May be empty on a later ACP tool_call_update that only changes
	// status/result and doesn't repeat it.
	ToolName string

	// ToolTitle is an optional human-readable summary, populated for
	// EventTypeToolCall from ACP's ToolCallUpdate.Title. Always empty for
	// Claude Code, which has no equivalent field -- a UI falls back to
	// deriving a summary from ToolName/ToolInput itself.
	ToolTitle string

	// ToolStatus is one of ToolStatusRunning/ToolStatusSuccess/
	// ToolStatusFailure, populated for EventTypeToolCall.
	ToolStatus string

	// ToolInput is the tool call's raw arguments, populated for
	// EventTypeToolCall in the producing provider's own shape (Claude
	// Code's ToolUseBlock.Input marshaled back to JSON, or ACP's
	// rawInput) -- deliberately not normalized further; see Event's own
	// doc comment for why speculative per-provider field-chasing is
	// avoided here.
	ToolInput json.RawMessage

	// ToolResult is the tool call's raw output, populated for
	// EventTypeToolCall once ToolStatus leaves ToolStatusRunning: Claude
	// Code's ToolResultBlock.Content, or ACP's tool-call Content array.
	// Its shape carries the failure detail when ToolStatus is
	// ToolStatusFailure (there is no separate error-message field) --
	// same "provider-native, not normalized" reasoning as ToolInput.
	ToolResult json.RawMessage
}

// PermissionResolution categorizes how an EventTypePermissionResolved
// event's PermissionOptionID was decided.
type PermissionResolution string

const (
	// PermissionResolvedByHuman is a real person answering via
	// run.respondPermission (internal/runs.Registry.RespondPermission)
	// before any auto-resolution fired.
	PermissionResolvedByHuman PermissionResolution = "human"

	// PermissionResolvedByAutoSafe is ApprovalPolicyAutoSafe auto-allowing
	// the request itself, because its command matched AllowlistedCommand --
	// no human was ever asked.
	PermissionResolvedByAutoSafe PermissionResolution = "auto_safe"

	// PermissionResolvedByTimeout is the request having gone unanswered
	// long enough (see internal/runs's permission-timeout constant) that it
	// was auto-resolved to a deny option instead of blocking the run
	// forever -- never an allow, regardless of ApprovalPolicy.
	PermissionResolvedByTimeout PermissionResolution = "timeout"
)
