// Package window renders the redacted, capped, origin-tagged transcript the
// reflector reads (ADR-0026 section 2) together with the matching gate.Window,
// so evidence quotes are verified against exactly what the reflector saw.
//
// It is a pure builder: callers load runs and decode events; nothing here
// touches the database, the network or the runs/store packages.
package window

import (
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Origin says who authored a run's prompt. Only OriginHuman prompts may
// become user_message evidence.
type Origin string

const (
	OriginHuman        Origin = "human"
	OriginOrchestrator Origin = "orchestrator"
	OriginAgent        Origin = "agent"
	OriginUnknown      Origin = "unknown"
)

// Event is one decoded run event. ID is run_events.id (global), Seq the
// per-run sequence.
type Event struct {
	ID  int64
	Seq int64
	E   taskrunner.Event
}

// Run is one already-loaded run. Events must belong to the run, ordered by Seq.
type Run struct {
	ID, Provider, Prompt, Status, StopReason, ErrMsg string
	// Origin is who authored Prompt; OriginUnknown (or empty) never
	// qualifies as user_message.
	Origin Origin
	// Interrupt is true when the prompt was delivered mid-run.
	Interrupt bool
	Events    []Event
}

// SkillRef is a skill's description-index entry; bodies are never passed.
type SkillRef struct{ Name, Description string }

// Caps are the byte caps applied while rendering.
type Caps struct {
	RecordBytes, InputBytes, ResultBytes, FailureResultBytes, WindowBytes int
}

// CapsFromConfig converts the KB knobs to bytes.
func CapsFromConfig(c config.LearningConfig) Caps {
	return Caps{
		RecordBytes:        c.RecordKB * 1024,
		InputBytes:         c.InputKB * 1024,
		ResultBytes:        c.ResultKB * 1024,
		FailureResultBytes: c.FailureResultKB * 1024,
		WindowBytes:        c.WindowKB * 1024,
	}
}

// Input is everything Build needs.
type Input struct {
	Runs []Run
	// Redact is mandatory: Build fails closed (empty Output) without it.
	Redact         func(string) string
	Caps           Caps
	Learned, Human []SkillRef
	// LoadedByRun maps run id -> learned skill names loaded in that run.
	LoadedByRun map[string][]string
	// PriorUserTurns are up to 10 preceding human turns, background only.
	PriorUserTurns []string
}

// Output is the rendered window.
type Output struct {
	Text        string
	Window      gate.Window
	Truncated   bool
	LastEventID int64
	ToolCalls   int
}

// CountToolCalls counts distinct non-empty ToolCallIDs among tool-call events
// (a call has several events: start, update, result).
func CountToolCalls(events []taskrunner.Event) int {
	seen := make(map[string]struct{})
	for _, e := range events {
		if e.Type == taskrunner.EventTypeToolCall && e.ToolCallID != "" {
			seen[e.ToolCallID] = struct{}{}
		}
	}
	return len(seen)
}
