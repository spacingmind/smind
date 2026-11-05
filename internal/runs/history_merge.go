package runs

import "github.com/spacingmind/smind/internal/taskrunner"

// MergeRebuilt puts a rebuilt turn into a run's stored events: every
// transcript-class event the stored history holds (text, thinking, tool
// calls -- all the skeleton collapsed or dropped) is replaced by the rebuilt
// block, which carries only those. The rebuilt block takes the position of
// the first stored transcript event, or, when the stored history has none,
// the slot just before its trailing bookkeeping run. Neither input is
// mutated.
func MergeRebuilt(stored, rebuilt []Event) []Event {
	if len(rebuilt) == 0 {
		return append([]Event(nil), stored...)
	}
	insert := -1
	for i, e := range stored {
		if isTranscriptEvent(e.Type) {
			insert = i
			break
		}
	}
	if insert < 0 {
		insert = len(stored)
		for insert > 0 && isTerminalBookkeeping(stored[insert-1].Type) {
			insert--
		}
	}
	out := make([]Event, 0, len(stored)+len(rebuilt))
	out = append(out, stored[:insert]...)
	out = append(out, rebuilt...)
	for _, e := range stored[insert:] {
		if !isTranscriptEvent(e.Type) {
			out = append(out, e)
		}
	}
	return out
}

func isTranscriptEvent(t taskrunner.EventType) bool {
	switch t {
	case taskrunner.EventTypeText, taskrunner.EventTypeThinking, taskrunner.EventTypeToolCall:
		return true
	}
	return false
}

// isTerminalBookkeeping names the event types a run's history may end with:
// the rebuilt block is inserted before that trailing run.
func isTerminalBookkeeping(t taskrunner.EventType) bool {
	switch t {
	case taskrunner.EventTypeDone, taskrunner.EventTypeUsage, taskrunner.EventTypeRateLimit,
		taskrunner.EventTypeMcpServers, taskrunner.EventTypeSessionNote:
		return true
	}
	return false
}
