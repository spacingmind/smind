package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_TasksBudgetUSDNullable(t *testing.T) {
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

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(1)
	if err != nil {
		t.Fatal(err)
	}
	if task.BudgetUSD != nil {
		t.Fatalf("pre-existing task BudgetUSD = %v, want nil", *task.BudgetUSD)
	}
	v := 5.5
	if err := s.SetTaskBudget(1, &v); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// A repeat Open keeps the value and does not fail on the existing column.
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err = s.GetTask(1)
	if err != nil {
		t.Fatal(err)
	}
	if task.BudgetUSD == nil || *task.BudgetUSD != 5.5 {
		t.Fatalf("BudgetUSD after reopen = %v, want 5.5", task.BudgetUSD)
	}
	if err := s.SetTaskBudget(1, nil); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetTask(1)
	if task.BudgetUSD != nil {
		t.Fatalf("BudgetUSD after clear = %v, want nil", *task.BudgetUSD)
	}
	if err := s.SetTaskBudget(99, &v); err == nil {
		t.Fatal("SetTaskBudget on a missing task: want error")
	}
}

// budgetRun adds a finished (or running) user run to taskID, with usage when
// cost != nil, or a usage row with a NULL cost when nullCost.
func budgetRun(t *testing.T, s *Store, taskID int64, id, status string, cost *float64, usageRow bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := s.CreateRun(Run{ID: id, TaskID: taskID, Provider: "claude-native", Prompt: "p", Status: status, StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if usageRow {
		if err := s.UpsertRunUsage(RunUsage{RunID: id, TaskID: taskID, Provider: "claude-native", CostUSD: cost, Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTreeKnownCost_SumsSubtreeIncludingArchived(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	mk := func(parent *int64) Task {
		task, err := s.CreateTask(Task{WorkspaceID: ws.ID, ParentTaskID: parent, Title: "t", Status: "created"})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	root := mk(nil)
	child := mk(&root.ID)
	grand := mk(&child.ID)
	other := mk(nil) // a separate tree must not leak in
	f := func(v float64) *float64 { return &v }
	budgetRun(t, s, root.ID, "r1", "done", f(1.25), true)
	budgetRun(t, s, child.ID, "r2", "done", f(2.5), true)
	budgetRun(t, s, grand.ID, "r3", "done", f(0.25), true)
	budgetRun(t, s, other.ID, "r4", "done", f(100), true)
	if _, err := s.ArchiveTask(grand.ID); err != nil {
		t.Fatal(err)
	}

	known, unknown, err := s.TreeKnownCost(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if known != 4.0 || unknown != 0 {
		t.Fatalf("TreeKnownCost(root) = %v, %d, want 4, 0", known, unknown)
	}
	known, _, _ = s.TreeKnownCost(child.ID)
	if known != 2.75 {
		t.Fatalf("TreeKnownCost(child) = %v, want 2.75 (subtree only)", known)
	}
}

func TestTreeKnownCost_UnknownRunsPartialNotCounted(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	root, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "root", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	f := func(v float64) *float64 { return &v }
	budgetRun(t, s, root.ID, "ok", "done", f(1), true)
	budgetRun(t, s, root.ID, "nousage", "done", nil, false)     // no usage row
	budgetRun(t, s, root.ID, "nullcost", "done", nil, true)     // usage row, NULL cost
	budgetRun(t, s, root.ID, "live", "running", f(50), true)    // running with a usage row: counted
	budgetRun(t, s, root.ID, "liveless", "running", nil, false) // running without usage: not unknown

	known, unknown, err := s.TreeKnownCost(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if known != 51 || unknown != 2 {
		t.Fatalf("TreeKnownCost = %v, %d, want 51, 2", known, unknown)
	}
}

func TestTreeBudgetCountsRemoteSpend(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	mk := func(parent *int64) Task {
		task, err := s.CreateTask(Task{WorkspaceID: ws.ID, ParentTaskID: parent, Title: "t", Status: "created"})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	root := mk(nil)
	child := mk(&root.ID)
	other := mk(nil)
	f := func(v float64) *float64 { return &v }
	budgetRun(t, s, root.ID, "r1", "done", f(1.0), true)
	put := func(remoteID string, local int64, state string, cost *float64) {
		t.Helper()
		if _, err := s.PutFleetRemoteTaskAndWatch(FleetRemoteTask{HostID: "h1", RemoteTaskID: remoteID, LocalTaskID: local, State: state, CostUSD: cost}, nil); err != nil {
			t.Fatal(err)
		}
	}
	check := func(wantKnown float64, wantUnknown int) {
		t.Helper()
		known, unknown, err := s.TreeKnownCost(root.ID)
		if err != nil {
			t.Fatal(err)
		}
		if known != wantKnown || unknown != wantUnknown {
			t.Fatalf("TreeKnownCost = %v, %d, want %v, %d", known, unknown, wantKnown, wantUnknown)
		}
	}

	check(1.0, 0)
	put("7", child.ID, "running", f(0.5))
	check(1.5, 0)
	put("8", child.ID, "done", nil)
	check(1.5, 1)
	put("9", child.ID, "running", nil) // not finished: not unknown
	check(1.5, 1)
	put("10", other.ID, "done", f(100)) // outside the tree
	check(1.5, 1)
}
