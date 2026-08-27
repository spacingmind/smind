package taskrunner

// EventType discriminates which fields of an Event are populated.
type EventType int

const (
	// EventTypeText is a chunk of text streamed from the agent mid-turn.
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
)

// Event is the provider-agnostic streaming update Runner.RunPrompt emits,
// translated from whichever backend (acp.SessionUpdate for GLM/ACP, or
// claudecode.Message/ResultMessage for Claude Code native) is actually
// driving the task.
//
// Only these two event types exist because a text chunk and a terminal
// stop-reason are the only things both wire protocols agree on. ACP also
// streams tool-call notifications and plan updates; Claude Code also
// streams tool-use blocks and other content block types. Modeling every one
// of those into this type now would mean chasing two protocols' full
// variant sets before any caller exists that needs them. Raw carries the
// original update so a caller that does need backend-specific detail can
// still get at it, without Event having to grow a field per variant
// speculatively.
type Event struct {
	Type EventType

	// Text is populated for EventTypeText.
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
}
