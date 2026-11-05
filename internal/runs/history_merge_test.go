package runs

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// renderEvents is the comparison key of TestMergeRebuilt: type, text, tool id.
func renderEvents(evs []Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = fmt.Sprintf("%d:%s:%s", e.Type, e.Text, e.ToolCallID)
	}
	return out
}

func TestMergeRebuilt(t *testing.T) {
	rebuilt := []Event{
		{Type: taskrunner.EventTypeThinking, Text: "T"},
		{Type: taskrunner.EventTypeText, Text: "A"},
		{Type: taskrunner.EventTypeToolCall, ToolCallID: "c1"},
	}
	for _, tc := range []struct {
		name    string
		stored  []Event
		rebuilt []Event
		want    []Event
	}{
		{
			name: "rebuilt block takes the first transcript event's place",
			stored: []Event{
				{Type: taskrunner.EventTypeUserMessage, Text: "go"},
				{Type: taskrunner.EventTypeText, Text: "a"},
				{Type: taskrunner.EventTypeToolCall, ToolCallID: "c1"},
				{Type: taskrunner.EventTypePermissionRequest, PermissionRequestID: "p"},
				{Type: taskrunner.EventTypePermissionResolved, PermissionRequestID: "p"},
				{Type: taskrunner.EventTypeText, Text: "b"},
				{Type: taskrunner.EventTypeUsage},
				{Type: taskrunner.EventTypeDone, StopReason: "end_turn"},
			},
			rebuilt: rebuilt,
			want: []Event{
				{Type: taskrunner.EventTypeUserMessage, Text: "go"},
				{Type: taskrunner.EventTypeThinking, Text: "T"},
				{Type: taskrunner.EventTypeText, Text: "A"},
				{Type: taskrunner.EventTypeToolCall, ToolCallID: "c1"},
				{Type: taskrunner.EventTypePermissionRequest, PermissionRequestID: "p"},
				{Type: taskrunner.EventTypePermissionResolved, PermissionRequestID: "p"},
				{Type: taskrunner.EventTypeUsage},
				{Type: taskrunner.EventTypeDone, StopReason: "end_turn"},
			},
		},
		{
			name:    "no transcript event: insert before the trailing bookkeeping run",
			stored:  []Event{{Type: taskrunner.EventTypeUserMessage, Text: "go"}, {Type: taskrunner.EventTypeDone, StopReason: "end_turn"}},
			rebuilt: rebuilt,
			want: []Event{
				{Type: taskrunner.EventTypeUserMessage, Text: "go"},
				{Type: taskrunner.EventTypeThinking, Text: "T"},
				{Type: taskrunner.EventTypeText, Text: "A"},
				{Type: taskrunner.EventTypeToolCall, ToolCallID: "c1"},
				{Type: taskrunner.EventTypeDone, StopReason: "end_turn"},
			},
		},
		{
			name:    "no transcript event and no trailing bookkeeping run: append",
			stored:  []Event{{Type: taskrunner.EventTypeUserMessage, Text: "go"}},
			rebuilt: rebuilt,
			want: []Event{
				{Type: taskrunner.EventTypeUserMessage, Text: "go"},
				{Type: taskrunner.EventTypeThinking, Text: "T"},
				{Type: taskrunner.EventTypeText, Text: "A"},
				{Type: taskrunner.EventTypeToolCall, ToolCallID: "c1"},
			},
		},
		{
			name:    "empty rebuilt block returns the stored events",
			stored:  []Event{{Type: taskrunner.EventTypeUserMessage, Text: "go"}, {Type: taskrunner.EventTypeText, Text: "a"}, {Type: taskrunner.EventTypeDone, StopReason: "end_turn"}},
			rebuilt: nil,
			want: []Event{
				{Type: taskrunner.EventTypeUserMessage, Text: "go"},
				{Type: taskrunner.EventTypeText, Text: "a"},
				{Type: taskrunner.EventTypeDone, StopReason: "end_turn"},
			},
		},
	} {
		storedCopy := append([]Event(nil), tc.stored...)
		rebuiltCopy := append([]Event(nil), tc.rebuilt...)
		got := MergeRebuilt(tc.stored, tc.rebuilt)
		if !reflect.DeepEqual(renderEvents(got), renderEvents(tc.want)) {
			t.Errorf("%s:\n got %v\nwant %v", tc.name, renderEvents(got), renderEvents(tc.want))
		}
		if !reflect.DeepEqual(tc.stored, storedCopy) || !reflect.DeepEqual(tc.rebuilt, rebuiltCopy) {
			t.Errorf("%s: an input slice was mutated\n stored now %v\n rebuilt now %v", tc.name, renderEvents(tc.stored), renderEvents(tc.rebuilt))
		}
	}
}
