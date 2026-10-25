package store

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func newTestRunFor(t *testing.T, s *Store, taskID int64, id string, startedAt time.Time) {
	t.Helper()
	if _, err := s.CreateRun(Run{ID: id, TaskID: taskID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: startedAt}); err != nil {
		t.Fatalf("CreateRun(%s) error = %v", id, err)
	}
}

func TestRunUsage_UpsertAndSummary(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	newTestRunFor(t, s, task.ID, "r1", base)
	newTestRunFor(t, s, task.ID, "r2", base.Add(time.Hour))

	// First upsert, then a second that replaces it.
	if err := s.UpsertRunUsage(RunUsage{RunID: "r1", TaskID: task.ID, Provider: "glm", Source: "claude_result", InputTokens: ptr(int64(1))}); err != nil {
		t.Fatalf("UpsertRunUsage() error = %v", err)
	}
	if err := s.UpsertRunUsage(RunUsage{RunID: "r1", TaskID: task.ID, Provider: "glm", Source: "claude_result",
		InputTokens: ptr(int64(100)), OutputTokens: ptr(int64(20)), CostUSD: ptr(0.5), Model: ptr("m1")}); err != nil {
		t.Fatalf("UpsertRunUsage() error = %v", err)
	}
	if err := s.UpsertRunUsage(RunUsage{RunID: "r2", TaskID: task.ID, Provider: "glm", Source: "acp_usage_update",
		InputTokens: ptr(int64(50))}); err != nil {
		t.Fatalf("UpsertRunUsage() error = %v", err)
	}

	got, err := s.GetRunUsage("r1")
	if err != nil {
		t.Fatalf("GetRunUsage() error = %v", err)
	}
	if got.InputTokens == nil || *got.InputTokens != 100 {
		t.Errorf("InputTokens = %v, want 100 (second upsert replaces)", got.InputTokens)
	}
	if got.WorkspaceID == nil || *got.WorkspaceID != task.WorkspaceID {
		t.Errorf("WorkspaceID = %v, want %d", got.WorkspaceID, task.WorkspaceID)
	}
	if got.CachedInputTokens != nil || got.ReasoningTokens != nil || got.ContextUsed != nil || got.SessionSnapshot != nil || got.ChatID != nil {
		t.Errorf("unreported fields must stay NULL, got %+v", got)
	}

	if _, err := s.GetRunUsage("nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetRunUsage(missing) error = %v, want sql.ErrNoRows", err)
	}

	m, err := s.ListRunUsage([]string{"r1", "r2", "nope"})
	if err != nil || len(m) != 2 {
		t.Fatalf("ListRunUsage() = %v, %v; want 2 rows", m, err)
	}

	sums, err := s.SummarizeRunUsage(nil, nil, "task")
	if err != nil {
		t.Fatalf("SummarizeRunUsage() error = %v", err)
	}
	if len(sums) != 1 || sums[0].Count != 2 || sums[0].InputTokens != 150 || sums[0].OutputTokens != 20 || sums[0].CostUSD == nil || *sums[0].CostUSD != 0.5 {
		t.Fatalf("task summary = %+v, want one row count=2 in=150 out=20 cost=0.5", sums)
	}

	// since inclusive / until exclusive on runs.started_at.
	until := base.Add(time.Hour)
	sums, err = s.SummarizeRunUsage(&base, &until, "run")
	if err != nil || len(sums) != 1 || sums[0].Key != "r1" {
		t.Fatalf("windowed summary = %+v, %v; want only r1", sums, err)
	}

	// r2 alone reported no cost: its group's cost is unknown (nil), not 0.
	sums, err = s.SummarizeRunUsage(nil, nil, "run")
	if err != nil || len(sums) != 2 || sums[0].Key != "r1" || sums[1].Key != "r2" || sums[0].CostUSD == nil || sums[1].CostUSD != nil {
		t.Fatalf("per-run summary = %+v, %v; want r1 cost set, r2 cost nil", sums, err)
	}

	if _, err := s.SummarizeRunUsage(nil, nil, "account"); err == nil {
		t.Error("SummarizeRunUsage(groupBy=account) error = nil, want invalid groupBy")
	}
}

// TestSummarizeRunUsage_UnreportedFields covers the U0 scenario: a summary
// group names the token fields no row in the group reported (NULL in every
// row), while a reported 0 keeps the field off the list.
func TestSummarizeRunUsage_UnreportedFields(t *testing.T) {
	t.Parallel()

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
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	seed := func(runID string, taskID int64, u RunUsage) {
		t.Helper()
		newTestRunFor(t, s, taskID, runID, base)
		u.RunID, u.TaskID, u.Provider, u.Source = runID, taskID, "glm", "claude_result"
		if err := s.UpsertRunUsage(u); err != nil {
			t.Fatalf("UpsertRunUsage(%s) error = %v", runID, err)
		}
	}

	// Group A: reasoning is NULL in every row; everything else reported.
	seed("a1", taskA.ID, RunUsage{Model: ptr("m"), InputTokens: ptr(int64(1)), OutputTokens: ptr(int64(2)),
		CachedInputTokens: ptr(int64(3)), CacheWriteTokens: ptr(int64(4))})
	seed("a2", taskA.ID, RunUsage{Model: ptr("m"), InputTokens: ptr(int64(10)), OutputTokens: ptr(int64(20)),
		CachedInputTokens: ptr(int64(30)), CacheWriteTokens: ptr(int64(40))})
	// Group B: an explicit reasoning 0 counts as reported.
	seed("b1", taskB.ID, RunUsage{Model: ptr("m"), InputTokens: ptr(int64(5)), OutputTokens: ptr(int64(6)),
		CachedInputTokens: ptr(int64(7)), CacheWriteTokens: ptr(int64(8)), ReasoningTokens: ptr(int64(0))})

	sums, err := s.SummarizeRunUsage(nil, nil, "task")
	if err != nil {
		t.Fatalf("SummarizeRunUsage() error = %v", err)
	}
	byTask := map[string]RunUsageSummary{}
	for _, u := range sums {
		byTask[u.Key] = u
	}

	gotA := byTask[fmt.Sprintf("%d", taskA.ID)]
	if len(gotA.Unreported) != 1 || gotA.Unreported[0] != "reasoningTokens" {
		t.Errorf("group A Unreported = %v, want [reasoningTokens]", gotA.Unreported)
	}
	gotB := byTask[fmt.Sprintf("%d", taskB.ID)]
	if len(gotB.Unreported) != 0 {
		t.Errorf("group B Unreported = %v, want nil (reasoning 0 is reported)", gotB.Unreported)
	}

	// A fully reported group and a NULL+reported mix, per run.
	seed("a3", taskA.ID, RunUsage{Model: ptr("m"), InputTokens: ptr(int64(1)), OutputTokens: ptr(int64(2)),
		CachedInputTokens: ptr(int64(3)), CacheWriteTokens: ptr(int64(4)), ReasoningTokens: ptr(int64(9))})
	sums, err = s.SummarizeRunUsage(nil, nil, "task")
	if err != nil {
		t.Fatalf("SummarizeRunUsage() error = %v", err)
	}
	for _, u := range sums {
		if u.Key == fmt.Sprintf("%d", taskA.ID) && len(u.Unreported) != 0 {
			t.Errorf("fully reported group Unreported = %v, want nil", u.Unreported)
		}
	}
}

func TestSummarizeRunUsage_ReturnsDistinctProvidersPerGroup(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	other := newTestTaskForRuns(t, s)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, c := range []struct {
		id       string
		task     int64
		provider string
	}{
		{"p1", task.ID, "glm"}, {"p2", task.ID, "claude-native"}, {"p3", task.ID, "glm"}, {"p4", other.ID, "codex-native"},
	} {
		newTestRunFor(t, s, c.task, c.id, base.Add(time.Duration(i)*time.Minute))
		if err := s.UpsertRunUsage(RunUsage{RunID: c.id, TaskID: c.task, Provider: c.provider, Source: "t", InputTokens: ptr(int64(1))}); err != nil {
			t.Fatalf("UpsertRunUsage() error = %v", err)
		}
	}
	sums, err := s.SummarizeRunUsage(nil, nil, "task")
	if err != nil || len(sums) != 2 {
		t.Fatalf("SummarizeRunUsage() = %+v, %v; want 2 groups", sums, err)
	}
	byKey := map[string][]string{}
	for _, u := range sums {
		byKey[u.Key] = u.Providers
	}
	if got := byKey[fmt.Sprint(task.ID)]; len(got) != 2 || got[0] != "claude-native" || got[1] != "glm" {
		t.Errorf("task providers = %v, want [claude-native glm]", got)
	}
	if got := byKey[fmt.Sprint(other.ID)]; len(got) != 1 || got[0] != "codex-native" {
		t.Errorf("other providers = %v, want [codex-native]", got)
	}
}
