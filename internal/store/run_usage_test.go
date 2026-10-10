package store

import (
	"database/sql"
	"errors"
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
