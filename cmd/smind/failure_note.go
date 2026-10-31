package main

import "github.com/spacingmind/smind/internal/runs/failure"

// failureNote is the one-line routing note of a failed run, for MCP output:
// "Run r-1 failed: auth_expired. Next: <action label>. <what not to do>".
// "" for a run without a typed failure, and for cli_exited, whose
// needs-continue message already says it.
func failureNote(r runLogsResult) string {
	if r.Status != "error" || !failure.IsKnown(r.FailureKind) || r.FailureKind == failure.KindCLIExited {
		return ""
	}
	note := "Run " + r.RunID + " failed: " + r.FailureKind + "."
	if r.Action != nil && r.Action.Label != "" {
		note += " Next: " + r.Action.Label + "."
	}
	return note + " " + failure.Guidance(r.FailureKind)
}
