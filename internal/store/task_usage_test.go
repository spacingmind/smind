package store

import (
	"testing"
	"time"
)

func newTestRunForTaskUsage(t *testing.T, s *Store, taskID int64, id string) {
	t.Helper()
	if _, err := s.CreateRun(Run{ID: id, TaskID: taskID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateRun(%s) error = %v", id, err)
	}
}

// TestSumRunUsageByTask_UnreportedIsNil pins the nullable-not-zero rule
// (ADR-0020) through the per-task sums: a field no run of the task
// reported stays nil even when other fields (or other tasks) did report,
// and a reported 0 sums to a real 0, never nil.
func TestSumRunUsageByTask_UnreportedIsNil(t *testing.T) {
	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	taskA, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "a", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	taskB, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "b", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	newTestRunForTaskUsage(t, s, taskA.ID, "r1")
	newTestRunForTaskUsage(t, s, taskA.ID, "r2")
	newTestRunForTaskUsage(t, s, taskB.ID, "r3")

	seed := func(runID string, taskID int64, u RunUsage) {
		t.Helper()
		u.RunID, u.TaskID, u.Provider, u.Source = runID, taskID, "glm", "claude_result"
		if err := s.UpsertRunUsage(u); err != nil {
			t.Fatalf("UpsertRunUsage(%s) error = %v", runID, err)
		}
	}
	seed("r1", taskA.ID, RunUsage{InputTokens: ptr(int64(100)), CostUSD: ptr(0.5)})
	// r2 reports a real 0 input: taskA's sum stays a value, not nil.
	seed("r2", taskA.ID, RunUsage{InputTokens: ptr(int64(0))})
	// r3 reports only output tokens for taskB: its input stays nil.
	seed("r3", taskB.ID, RunUsage{OutputTokens: ptr(int64(7))})

	sums, err := s.SumRunUsageByTask(ws.ID)
	if err != nil {
		t.Fatalf("SumRunUsageByTask() error = %v", err)
	}
	a, ok := sums[taskA.ID]
	if !ok {
		t.Fatalf("task %d absent from sums %+v", taskA.ID, sums)
	}
	if a.Runs != 2 {
		t.Errorf("a.Runs = %d, want 2", a.Runs)
	}
	if a.InputTokens == nil || *a.InputTokens != 100 {
		t.Errorf("a.InputTokens = %v, want 100 (reported 0 sums to a value)", a.InputTokens)
	}
	if a.OutputTokens != nil {
		t.Errorf("a.OutputTokens = %v, want nil (never reported)", a.OutputTokens)
	}
	if a.CacheReadTokens != nil || a.CacheWriteTokens != nil || a.ReasoningTokens != nil {
		t.Errorf("a token nil-ness violated: %+v", a)
	}
	if a.CostUSD == nil || *a.CostUSD != 0.5 {
		t.Errorf("a.CostUSD = %v, want 0.5", a.CostUSD)
	}

	b := sums[taskB.ID]
	if b.InputTokens != nil {
		t.Errorf("b.InputTokens = %v, want nil (task B never reported input)", b.InputTokens)
	}
	if b.OutputTokens == nil || *b.OutputTokens != 7 {
		t.Errorf("b.OutputTokens = %v, want 7", b.OutputTokens)
	}
	if b.CostUSD != nil {
		t.Errorf("b.CostUSD = %v, want nil", b.CostUSD)
	}
}
