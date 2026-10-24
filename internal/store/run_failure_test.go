package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunColumnsAs(t *testing.T) {
	t.Parallel()

	got := runColumnsAs("r")
	if !strings.HasPrefix(got, "r.id, r.task_id") {
		t.Fatalf("runColumnsAs(r) = %q, want prefix %q", got, "r.id, r.task_id")
	}
	if strings.Count(got, ",") != strings.Count(runColumns, ",") {
		t.Fatalf("runColumnsAs(r) has %d commas, runColumns has %d", strings.Count(got, ","), strings.Count(runColumns, ","))
	}
}

func TestMigrate_RunFailureColumns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old-failure.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	task := newTestTaskForRuns(t, s)
	for _, col := range []string{"failure_kind", "failure_window", "failure_reset_at", "failure_reset_inferred"} {
		if _, err := s.db.Exec(`ALTER TABLE runs DROP COLUMN ` + col); err != nil {
			t.Fatalf("drop runs.%s: %v", col, err)
		}
	}
	if _, err := s.db.Exec(
		`INSERT INTO runs (id, task_id, provider, prompt, status, started_at) VALUES ('old-run', ?, 'glm', 'hi', 'done', ?)`,
		task.ID, time.Now().UTC(),
	); err != nil {
		t.Fatalf("seed old run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for i := 0; i < 2; i++ { // second Open proves idempotence
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		got, err := s.GetRun("old-run")
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if got.FailureResetInferred {
			t.Fatalf("cleared failure still marked inferred")
		}
		if got.FailureKind != "" || got.FailureWindow != "" || got.FailureResetAt != nil {
			t.Fatalf("Open() #%d failure = %q/%q/%v, want empty/empty/nil", i, got.FailureKind, got.FailureWindow, got.FailureResetAt)
		}
		s.Close()
	}
}

func newFailureTestRun(t *testing.T, s *Store, id string) {
	t.Helper()
	task := newTestTaskForRuns(t, s)
	if _, err := s.CreateRun(Run{
		ID: id, TaskID: task.ID, Provider: "glm", Prompt: "hi",
		Status: "error", StartedAt: time.Now().UTC(), Purpose: "user",
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
}

func TestStore_UpdateRunFailureRoundTrip(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	newFailureTestRun(t, s, "run-fail")

	reset := time.Date(2026, 10, 23, 18, 30, 0, 0, time.FixedZone("UTC+7", 7*3600))
	if err := s.UpdateRunFailure("run-fail", "rate_limited", "5h", &reset, true); err != nil {
		t.Fatalf("UpdateRunFailure() error = %v", err)
	}
	got, err := s.GetRun("run-fail")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.FailureKind != "rate_limited" || got.FailureWindow != "5h" {
		t.Fatalf("failure = %q/%q, want rate_limited/5h", got.FailureKind, got.FailureWindow)
	}
	if got.FailureResetAt == nil || !got.FailureResetAt.Equal(reset) {
		t.Fatalf("FailureResetAt = %v, want %v", got.FailureResetAt, reset)
	}

	if !got.FailureResetInferred {
		t.Fatalf("FailureResetInferred = false, want true")
	}

	if err := s.UpdateRunFailure("run-fail", "", "", nil, false); err != nil {
		t.Fatalf("UpdateRunFailure(clear) error = %v", err)
	}
	got, err = s.GetRun("run-fail")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.FailureKind != "" || got.FailureWindow != "" || got.FailureResetAt != nil {
		t.Fatalf("cleared failure = %q/%q/%v, want empty/empty/nil", got.FailureKind, got.FailureWindow, got.FailureResetAt)
	}
}

func TestStore_ListRecentRunsCarriesFailure(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	newFailureTestRun(t, s, "run-list")
	reset := time.Now().UTC().Add(time.Hour)
	if err := s.UpdateRunFailure("run-list", "rate_limited", "weekly", &reset, true); err != nil {
		t.Fatalf("UpdateRunFailure() error = %v", err)
	}
	runs, err := s.ListRecentRuns(10)
	if err != nil {
		t.Fatalf("ListRecentRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("ListRecentRuns() = %d runs, want 1", len(runs))
	}
	r := runs[0]
	if !r.FailureResetInferred {
		t.Fatalf("listed FailureResetInferred = false, want true")
	}
	if r.FailureKind != "rate_limited" || r.FailureWindow != "weekly" || r.FailureResetAt == nil || !r.FailureResetAt.Equal(reset) {
		t.Fatalf("listed failure = %q/%q/%v, want rate_limited/weekly/%v", r.FailureKind, r.FailureWindow, r.FailureResetAt, reset)
	}
}
