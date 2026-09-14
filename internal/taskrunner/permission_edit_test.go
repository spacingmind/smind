package taskrunner

import (
	"encoding/json"
	"testing"
)

// TestAutoAllowACPFileEdit covers the auto-safe edit gate for ACP-driven
// (GLM/Kimi) permission requests: only edit/move/delete kinds, only when
// every reported location stays inside the task's own worktree, and fail
// closed on anything unparsable or incomplete (see the 2026-09-14 GLM
// validation run where every file write timed out to auto-deny without
// this).
func TestAutoAllowACPFileEdit(t *testing.T) {
	t.Parallel()
	const wt = "/wt/task-1"

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"edit inside the worktree", `{"kind":"edit","locations":[{"path":"/wt/task-1/a.go"}]}`, true},
		{"nested path inside the worktree", `{"kind":"edit","locations":[{"path":"/wt/task-1/sub/b.go"}]}`, true},
		{"move inside the worktree", `{"kind":"move","locations":[{"path":"/wt/task-1/a.go"},{"path":"/wt/task-1/b.go"}]}`, true},
		{"delete inside the worktree", `{"kind":"delete","locations":[{"path":"/wt/task-1/a.go"}]}`, true},
		{"exact worktree root itself is not a file edit target", `{"kind":"edit","locations":[{"path":"/wt/task-1"}]}`, false},

		{"path outside the worktree", `{"kind":"edit","locations":[{"path":"/etc/passwd"}]}`, false},
		{"sibling directory sharing the prefix", `{"kind":"edit","locations":[{"path":"/wt/task-10/c.go"}]}`, false},
		{"one of several locations outside", `{"kind":"edit","locations":[{"path":"/wt/task-1/a.go"},{"path":"/elsewhere"}]}`, false},
		{"parent traversal", `{"kind":"edit","locations":[{"path":"/wt/task-1/../../etc/passwd"}]}`, false},
		{"relative path", `{"kind":"edit","locations":[{"path":"a.go"}]}`, false},

		{"execute kind is not auto-allowed", `{"kind":"execute","locations":[{"path":"/wt/task-1"}]}`, false},
		{"read kind is not covered", `{"kind":"read","locations":[{"path":"/wt/task-1/a.go"}]}`, false},
		{"missing kind", `{"locations":[{"path":"/wt/task-1/a.go"}]}`, false},
		{"no locations", `{"kind":"edit"}`, false},
		{"empty location path", `{"kind":"edit","locations":[{"path":""}]}`, false},
		{"unparsable json", `{`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := autoAllowACPFileEdit(json.RawMessage(tt.raw), wt); got != tt.want {
				t.Fatalf("autoAllowACPFileEdit(%s) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}

	t.Run("empty worktree root fails closed", func(t *testing.T) {
		t.Parallel()
		if autoAllowACPFileEdit(json.RawMessage(`{"kind":"edit","locations":[{"path":"/wt/a.go"}]}`), "") {
			t.Fatal("autoAllowACPFileEdit with empty worktree = true, want false")
		}
	})
}
