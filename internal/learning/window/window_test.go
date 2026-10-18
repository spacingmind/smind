package window

import (
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestCountToolCalls_DistinctIDs(t *testing.T) {
	tc := func(id, status string) taskrunner.Event {
		return taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolStatus: status}
	}
	evs := []taskrunner.Event{
		{Type: taskrunner.EventTypeText, Text: "hi"},
		tc("a", taskrunner.ToolStatusRunning), tc("a", taskrunner.ToolStatusSuccess),
		tc("b", taskrunner.ToolStatusFailure),
		tc("", taskrunner.ToolStatusSuccess),
		{Type: taskrunner.EventTypeUserMessage, ToolCallID: "z"},
	}
	if got := CountToolCalls(evs); got != 2 {
		t.Errorf("CountToolCalls=%d want 2", got)
	}
	if got := CountToolCalls(nil); got != 0 {
		t.Errorf("nil: %d", got)
	}
}

func TestCapsFromConfig(t *testing.T) {
	c := CapsFromConfig(config.DefaultLearning())
	want := Caps{RecordBytes: 4096, InputBytes: 1024, ResultBytes: 1024, FailureResultBytes: 2048, WindowBytes: 120 * 1024}
	if c != want {
		t.Errorf("%+v want %+v", c, want)
	}
}
