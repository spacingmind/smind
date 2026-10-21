package contrast

import (
	"encoding/json"
	"testing"

	"github.com/spacingmind/smind/internal/learning/window"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func ev(seq int64, e taskrunner.Event) window.Event { return window.Event{Seq: seq, E: e} }

func toolEv(seq int64, id, name, status string, result json.RawMessage) window.Event {
	return ev(seq, taskrunner.Event{
		Type:       taskrunner.EventTypeToolCall,
		ToolCallID: id,
		ToolName:   name,
		ToolStatus: status,
		ToolResult: result,
	})
}

func TestMetrics_CountsDistinctToolCalls(t *testing.T) {
	in := ArmInput{
		Events: []window.Event{
			toolEv(1, "c1", "Bash", taskrunner.ToolStatusRunning, nil),
			toolEv(2, "c1", "Bash", taskrunner.ToolStatusSuccess, json.RawMessage(`"ok"`)),
			toolEv(3, "c1", "", "", nil),
		},
	}
	m := ComputeMetrics(in)
	if m.ToolCalls != 1 {
		t.Fatalf("ToolCalls = %d, want 1 (three events of one call)", m.ToolCalls)
	}
	if m.FailedToolCalls != 0 || m.PermissionDenials != 0 {
		t.Fatalf("unexpected %+v", m)
	}
}

func TestMetrics_FailedAndDenied(t *testing.T) {
	in := ArmInput{
		Turns:           4,
		ReportedDenials: 1,
		Events: []window.Event{
			toolEv(1, "f1", "Bash", taskrunner.ToolStatusFailure, json.RawMessage(`"haven't granted bash"`)),
			toolEv(2, "f2", "Bash", taskrunner.ToolStatusFailure, json.RawMessage(`"permission denied: also not allowed"`)),
			toolEv(3, "s1", "Read", taskrunner.ToolStatusSuccess, json.RawMessage(`"ok"`)),
			ev(4, taskrunner.Event{Type: taskrunner.EventTypePermissionRequest, PermissionRequestID: "q"}),
		},
	}
	m := ComputeMetrics(in)
	if m.ToolCalls != 3 {
		t.Fatalf("ToolCalls = %d, want 3", m.ToolCalls)
	}
	if m.FailedToolCalls != 2 {
		t.Fatalf("FailedToolCalls = %d, want 2", m.FailedToolCalls)
	}
	// 1 reported + 1 permission request + 2 denied failed calls (f2 counts
	// once despite matching two markers) = 4.
	if m.PermissionDenials != 4 {
		t.Fatalf("PermissionDenials = %d, want 4", m.PermissionDenials)
	}
	if m.Turns != 4 {
		t.Fatalf("Turns = %d, want 4", m.Turns)
	}
}

func TestMetrics_EmptyEvents(t *testing.T) {
	m := ComputeMetrics(ArmInput{CostUSD: 0.1, DurationMs: 5, DiffBytes: 7})
	if m != (Metrics{CostUSD: 0.1, DurationMs: 5, DiffBytes: 7}) {
		t.Fatalf("unexpected %+v", m)
	}
}
