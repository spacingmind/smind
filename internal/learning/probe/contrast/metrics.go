// Package contrast computes the deterministic A/A'/B contrasts of a probe
// replay (ADR-0037 §4) from decoded run events. It is pure: no database, no
// network, no clock, no filesystem.
package contrast

import (
	"encoding/json"
	"strings"

	"github.com/spacingmind/smind/internal/learning/window"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Role names the arms of a probe.
type Role string

// Arm roles.
const (
	RoleA  Role = "A"
	RoleA2 Role = "A2"
	RoleB  Role = "B"
)

// CheckResult is the outcome of the workspace's probeCheck for one arm.
type CheckResult struct{ Passed bool }

// ArmInput is one arm's decoded run, ready for metric computation.
type ArmInput struct {
	Role   Role
	Events []window.Event // decoded run events of the arm's run, ordered by Seq
	Turns  int            // from the CLI result; not derivable from events
	// ReportedDenials is the permission_denials the CLI result reported.
	ReportedDenials int
	CostUSD         float64
	DurationMs      int64
	DiffBytes       int64        // bytes of the arm's redacted diff
	Check           *CheckResult // nil when no probeCheck is configured
}

// Metrics are the deterministic per-arm facts of ADR-0037 §4.
type Metrics struct {
	ToolCalls         int
	FailedToolCalls   int
	PermissionDenials int
	Turns             int
	CostUSD           float64
	DurationMs        int64
	DiffBytes         int64
}

// denialMarkers mark a failed call whose result text reads as a denial.
var denialMarkers = []string{
	"haven't granted",
	"operation not permitted",
	"permission denied",
	"not allowed",
}

// mergedCall is one tool call merged from its events by ToolCallID: last
// status wins, name from the first non-empty.
type mergedCall struct {
	name   string
	status string
	result json.RawMessage
}

func mergeCalls(events []window.Event) []*mergedCall {
	var calls []*mergedCall
	byID := map[string]*mergedCall{}
	for _, ev := range events {
		e := ev.E
		if e.Type != taskrunner.EventTypeToolCall {
			continue
		}
		var c *mergedCall
		if e.ToolCallID != "" {
			c = byID[e.ToolCallID]
		}
		if c == nil {
			c = &mergedCall{}
			if e.ToolCallID != "" {
				byID[e.ToolCallID] = c
			}
			calls = append(calls, c)
		}
		if e.ToolName != "" {
			c.name = e.ToolName
		}
		if e.ToolStatus != "" {
			c.status = e.ToolStatus
		}
		if len(e.ToolResult) > 0 {
			c.result = e.ToolResult
		}
	}
	return calls
}

// ComputeMetrics derives the arm's metrics from its events and the
// CLI-reported facts (Turns, ReportedDenials).
//
// PermissionDenials = ReportedDenials + the number of permission-request
// events + the failed calls whose result text reads as a denial; each failed
// call counts at most once no matter how many markers it matches, and never
// more than once alongside its own permission request.
func ComputeMetrics(in ArmInput) Metrics {
	m := Metrics{
		Turns:             in.Turns,
		CostUSD:           in.CostUSD,
		DurationMs:        in.DurationMs,
		DiffBytes:         in.DiffBytes,
		PermissionDenials: in.ReportedDenials,
	}
	for _, ev := range in.Events {
		if ev.E.Type == taskrunner.EventTypePermissionRequest {
			m.PermissionDenials++
		}
	}
	for _, c := range mergeCalls(in.Events) {
		m.ToolCalls++
		if c.status != taskrunner.ToolStatusFailure {
			continue
		}
		m.FailedToolCalls++
		if deniedResultText(window.ResultText(c.result)) {
			m.PermissionDenials++
		}
	}
	return m
}

func deniedResultText(s string) bool {
	if s == "" {
		return false
	}
	s = strings.ToLower(s)
	for _, marker := range denialMarkers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}
