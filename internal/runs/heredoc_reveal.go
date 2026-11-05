package runs

import (
	"errors"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// ErrNoHeredoc is returned when a reveal names a permission or tool call that
// is not there (not pending, unknown, or an index past its elided bodies).
var ErrNoHeredoc = errors.New("runs: no such heredoc")

// HeredocReveal is one raw heredoc body and where it came from. The text is
// unredacted: the caller (a human-only RPC) redacts it before it leaves.
type HeredocReveal struct {
	RunID  string
	TaskID int64
	Text   string
}

// PendingHeredoc returns body index of the permission request requestID, only
// while that request is still pending: once it is answered, timed out or its
// run ended, the body is no longer available through this path.
func (reg *Registry) PendingHeredoc(requestID string, index int) (HeredocReveal, error) {
	for _, p := range reg.PendingPermissions() {
		if p.RequestID != requestID {
			continue
		}
		if p.Detail == nil || index < 0 || index >= len(p.Detail.RevealBodies) {
			return HeredocReveal{}, ErrNoHeredoc
		}
		return HeredocReveal{RunID: p.RunID, TaskID: p.TaskID, Text: p.Detail.RevealBodies[index]}, nil
	}
	return HeredocReveal{}, ErrNoHeredoc
}

// ToolCallHeredoc returns body index of the execute call toolCallID in run
// runID, for a live run from its history and for a finished one from the
// persisted log. The index counts bodies the way taskrunner.ElideToolCall does.
func (reg *Registry) ToolCallHeredoc(runID, toolCallID string, index int) (HeredocReveal, error) {
	input, err := reg.toolCallInput(runID, toolCallID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return HeredocReveal{}, ErrNoHeredoc
		}
		return HeredocReveal{}, err
	}
	if input == nil || index < 0 {
		return HeredocReveal{}, ErrNoHeredoc
	}
	for _, key := range []string{"command", "cmd"} {
		cmd, ok := taskrunner.StringField(input, key)
		if !ok {
			continue
		}
		if _, bodies := permpolicy.ElideHeredocs(cmd); len(bodies) > 0 {
			if index >= len(bodies) {
				return HeredocReveal{}, ErrNoHeredoc
			}
			return HeredocReveal{RunID: runID, Text: bodies[index].Text}, nil
		}
	}
	return HeredocReveal{}, ErrNoHeredoc
}
