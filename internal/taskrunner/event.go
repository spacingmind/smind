package taskrunner

// EventType discriminates which fields of an Event are populated.
type EventType int

const (
	// EventTypeText is a chunk of text streamed from the agent mid-turn.
	EventTypeText EventType = iota

	// EventTypeDone is the terminal event for a turn: exactly one is sent,
	// last, before the events channel passed to Runner.RunPrompt is closed.
	EventTypeDone
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
	// the stop reason string already captured in StopReason.
	Raw any
}
