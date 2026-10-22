package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_TaskPRColumns_Idempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, q := range []string{
		`CREATE TABLE workspaces (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, title TEXT NOT NULL, routing_policy TEXT NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`,
		`CREATE TABLE tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, workspace_id INTEGER NOT NULL, space_id INTEGER, title TEXT NOT NULL, status TEXT NOT NULL, worktree_path TEXT, branch TEXT, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL, archived_at TIMESTAMP)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO workspaces VALUES (1,'/r','r','hard',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks (id, workspace_id, title, status, created_at, updated_at) VALUES (1,1,'t','created',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	for i := 0; i < 2; i++ { // second Open must change nothing
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		task, err := s.GetTask(1)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && (task.PRForge != "" || task.PRRepo != "" || task.PRNumber != 0 || task.PRURL != "" || task.PRHeadRef != "") {
			t.Fatalf("pre-existing task PR = %+v, want empty", task)
		}
		if err := s.SetTaskPR(1, "github", "o/r", 7, "https://github.com/o/r/pull/7", "feat/x"); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveTaskPRState(TaskPRState{TaskID: 1}); err != nil {
			t.Fatalf("task_pr_state table missing: %v", err)
		}
		if _, err := s.AddForgeCalls("github.com", 1, now); err != nil {
			t.Fatalf("forge_budget table missing: %v", err)
		}
		s.Close()
	}
}

func TestTaskPR_SetClearRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskPR(task.ID, "github", "o/r", 42, "https://github.com/o/r/pull/42", "feat/x"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(task.ID)
	if got.PRForge != "github" || got.PRRepo != "o/r" || got.PRNumber != 42 ||
		got.PRURL != "https://github.com/o/r/pull/42" || got.PRHeadRef != "feat/x" {
		t.Fatalf("after SetTaskPR %+v", got)
	}
	if err := s.ClearTaskPR(task.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(task.ID)
	if got.PRForge != "" || got.PRNumber != 0 || got.PRURL != "" || got.PRHeadRef != "" || got.PRRepo != "" {
		t.Fatalf("after ClearTaskPR %+v", got)
	}
	if err := s.SetTaskPR(9999, "github", "o/r", 1, "", ""); !errorsIsNoRows(err) {
		t.Fatalf("SetTaskPR missing task err = %v, want ErrNoRows", err)
	}
}

func TestTaskPRState_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	task, _ := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})

	if _, ok, err := s.GetTaskPRState(task.ID); err != nil || ok {
		t.Fatalf("GetTaskPRState(no row) = %v, %v, want false, nil", ok, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	st := TaskPRState{
		TaskID:              task.ID,
		Snapshot:            `{"number":7}`,
		FetchedAt:           now,
		NextAllowedAt:       now.Add(time.Minute),
		ConsecutiveFailures: 2,
		LastErrorCode:       "forge_rate_limited",
		ForcedAt:            []int64{1, 2, 3},
	}
	if err := s.SaveTaskPRState(st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetTaskPRState(task.ID)
	if err != nil || !ok {
		t.Fatalf("GetTaskPRState = %v, %v", ok, err)
	}
	if got.Snapshot != st.Snapshot || !got.FetchedAt.Equal(now) || !got.NextAllowedAt.Equal(st.NextAllowedAt) ||
		got.ConsecutiveFailures != 2 || got.LastErrorCode != st.LastErrorCode || len(got.ForcedAt) != 3 || got.ForcedAt[2] != 3 {
		t.Fatalf("got %+v, want %+v", got, st)
	}

	// Upsert with zero times -> NULL, defaults; forced_at trimmed to newest 12.
	forced := make([]int64, 20)
	for i := range forced {
		forced[i] = int64(i)
	}
	if err := s.SaveTaskPRState(TaskPRState{TaskID: task.ID, ForcedAt: forced}); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetTaskPRState(task.ID)
	if got.Snapshot != "" || !got.FetchedAt.IsZero() || !got.NextAllowedAt.IsZero() || got.ConsecutiveFailures != 0 ||
		len(got.ForcedAt) != 12 || got.ForcedAt[0] != 8 || got.ForcedAt[11] != 19 {
		t.Fatalf("after overwrite %+v", got)
	}

	if err := s.DeleteTaskPRState(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetTaskPRState(task.ID); ok {
		t.Fatal("row survived DeleteTaskPRState")
	}
	if err := s.DeleteTaskPRState(task.ID); err != nil {
		t.Fatalf("delete of missing row: %v", err)
	}
	if err := s.SaveTaskPRState(TaskPRState{TaskID: 9999}); err == nil {
		t.Fatal("SaveTaskPRState for a missing task succeeded, want FK error")
	}
}

func TestForgeBudget_RollingHour(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	t0 := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	if n, start, err := s.GetForgeBudget("github.com", t0); err != nil || n != 0 || !start.IsZero() {
		t.Fatalf("empty budget = %d, %v, %v", n, start, err)
	}
	if n, err := s.AddForgeCalls("github.com", 3, t0); err != nil || n != 3 {
		t.Fatalf("add 3 = %d, %v", n, err)
	}
	if n, err := s.AddForgeCalls("github.com", 2, t0.Add(59*time.Minute)); err != nil || n != 5 {
		t.Fatalf("add 2 inside window = %d, %v, want 5", n, err)
	}
	if n, _ := s.AddForgeCalls("ghe.example.com", 1, t0); n != 1 {
		t.Fatalf("other host = %d, want 1 (hosts are independent)", n)
	}
	n, start, err := s.GetForgeBudget("github.com", t0.Add(59*time.Minute))
	if err != nil || n != 5 || !start.Equal(t0) {
		t.Fatalf("get inside window = %d, %v, %v", n, start, err)
	}
	// Exactly one hour after window_start the row resets.
	if n, _, _ := s.GetForgeBudget("github.com", t0.Add(time.Hour)); n != 0 {
		t.Fatalf("get after window = %d, want 0", n)
	}
	if n, err := s.AddForgeCalls("github.com", 1, t0.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("add after window = %d, %v, want 1", n, err)
	}
	if n, start, _ := s.GetForgeBudget("github.com", t0.Add(61*time.Minute)); n != 1 || !start.Equal(t0.Add(time.Hour)) {
		t.Fatalf("new window = %d, %v", n, start)
	}
}

func TestDeleteTask_RemovesPRState(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	task := createTestTaskWithRunData(t, s, ws.ID, nil, "pr")
	if err := s.SaveTaskPRState(TaskPRState{TaskID: task.ID, Snapshot: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask with a task_pr_state row: %v", err)
	}
	if _, ok, _ := s.GetTaskPRState(task.ID); ok {
		t.Fatal("task_pr_state row survived DeleteTask")
	}
}

func errorsIsNoRows(err error) bool { return err != nil && errors.Is(err, sql.ErrNoRows) }
