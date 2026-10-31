package serve

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

func f64p(f float64) *float64 { return &f }

// fleetWorkspace opens one workspace every origin task/run below shares.
func fleetWorkspace(t *testing.T, s *store.Store) int64 {
	t.Helper()
	ws, err := s.CreateWorkspace(store.Workspace{Path: filepath.Join(t.TempDir(), "repo"), Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	return ws.ID
}

// mkOriginTask creates a task stamped with the fleet origin (host, trace).
func mkOriginTask(t *testing.T, s *store.Store, wsID int64, host, trace string) int64 {
	t.Helper()
	task, err := s.CreateTask(store.Task{WorkspaceID: wsID, Title: "remote thing", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskOrigin(task.ID, host, trace); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

// mkOriginRun inserts a run row as given and stamps its origin host.
func mkOriginRun(t *testing.T, s *store.Store, id string, taskID int64, status string, startedAt time.Time, host string) {
	t.Helper()
	if _, err := s.CreateRun(store.Run{ID: id, TaskID: taskID, Provider: "glm", Status: status, StartedAt: startedAt}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunOrigin(id, host); err != nil {
		t.Fatal(err)
	}
}

// setRunCost reports the run's usage; nil cost is the NULL row the budget
// must ignore (unreported is never estimated).
func setRunCost(t *testing.T, s *store.Store, runID string, taskID int64, cost *float64) {
	t.Helper()
	if err := s.UpsertRunUsage(store.RunUsage{RunID: runID, TaskID: taskID, Provider: "glm", Source: "test", CostUSD: cost}); err != nil {
		t.Fatal(err)
	}
}

// budgetServer builds a Server whose Ledger is a real store, so the budget
// queries run against actual SQL and not a fake.
func budgetServer(clk *fakeClock, st *store.Store) *Server {
	return New(Options{HostID: "A", Authorizer: fakeAuthorizer{grant: registry.DefaultForwardGrant()},
		Audit: &memAudit{}, Reader: &fakeReader{}, Now: clk.now, Ledger: st})
}

func budgetRefusalOf(t *testing.T, err error) *Refusal {
	t.Helper()
	var rf *Refusal
	if !errors.As(err, &rf) {
		t.Fatalf("err = %v, want *Refusal", err)
	}
	if rf.Code != wire.CodeBudgetExceeded {
		t.Fatalf("code %q, want %q", rf.Code, wire.CodeBudgetExceeded)
	}
	return rf
}

func TestFleetBudgetPerOriginAcrossTraces(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	now := clk.now().UTC()
	s := budgetServer(clk, st)
	ws := fleetWorkspace(t, st)

	g := registry.Grant{Ops: []string{registry.OpRead, registry.OpCreate}, MaxLiveAgents: 2, MaxWallSeconds: 60, MaxCostUSD: 5}

	// Two live hostA runs on two tasks whose traces differ: the cap is per
	// origin host, so the second trace does not buy another agent.
	t1 := mkOriginTask(t, st, ws, "hostA", "t1")
	t2 := mkOriginTask(t, st, ws, "hostA", "t2")
	tb := mkOriginTask(t, st, ws, "hostB", "tb")
	mkOriginRun(t, st, "a1", t1, "running", now.Add(-time.Minute), "hostA")
	mkOriginRun(t, st, "a2", t2, "running", now.Add(-time.Minute), "hostA")
	rf := budgetRefusalOf(t, s.checkBudget("hostA", g))
	if rf.Msg != "budget exceeded: live_agents (2 of 2)" || !strings.Contains(rf.Msg, wire.BudgetLiveAgents) {
		t.Fatalf("msg %q", rf.Msg)
	}

	// hostB's live run never counts toward hostA.
	mkOriginRun(t, st, "b1", tb, "running", now.Add(-time.Minute), "hostB")
	if err := s.checkBudget("hostB", g); err != nil {
		t.Fatalf("hostB one live run: %v", err)
	}

	// hostA back to one live run is within budget.
	if _, err := st.UpdateRunStatus("a2", "done", &now, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.checkBudget("hostA", g); err != nil {
		t.Fatalf("one live run: %v", err)
	}

	// Reported cost is summed per origin across traces; a NULL-cost run
	// adds nothing.
	mkOriginRun(t, st, "a3", t1, "done", now.Add(-10*time.Minute), "hostA")
	setRunCost(t, st, "a3", t1, f64p(2.5))
	mkOriginRun(t, st, "a4", t1, "done", now.Add(-11*time.Minute), "hostA")
	setRunCost(t, st, "a4", t1, nil)
	if err := s.checkBudget("hostA", g); err != nil {
		t.Fatalf("cost 2.5 of 5: %v", err)
	}

	// 2.5 (trace t1) + 3 (trace t2) crosses the cap.
	setRunCost(t, st, "a2", t2, f64p(3))
	rf = budgetRefusalOf(t, s.checkBudget("hostA", g))
	if rf.Msg != "budget exceeded: cost (5.5 of 5)" || !strings.Contains(rf.Msg, wire.BudgetCost) {
		t.Fatalf("msg %q", rf.Msg)
	}

	// A run started 25 h ago is outside the 24 h window: its cost changes
	// neither the sum nor the refusal.
	mkOriginRun(t, st, "a5", t1, "done", now.Add(-25*time.Hour), "hostA")
	setRunCost(t, st, "a5", t1, f64p(1000))
	rf = budgetRefusalOf(t, s.checkBudget("hostA", g))
	if rf.Msg != "budget exceeded: cost (5.5 of 5)" {
		t.Fatalf("msg %q", rf.Msg)
	}

	// Zero wall means zero: refused before the ledger is even read.
	noWall := g
	noWall.MaxWallSeconds = 0
	rf = budgetRefusalOf(t, s.checkBudget("hostA", noWall))
	if rf.Msg != "budget exceeded: wall (0 of 0)" || !strings.Contains(rf.Msg, wire.BudgetWall) {
		t.Fatalf("msg %q", rf.Msg)
	}
}
