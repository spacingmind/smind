package store

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// fleetOriginTask creates a task under one shared workspace, stamped with
// the fleet origin (host and trace) when given.
func fleetOriginTask(t *testing.T, s *Store, wsID int64, originHost, trace string) Task {
	t.Helper()
	task, err := s.CreateTask(Task{WorkspaceID: wsID, Title: "remote thing", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if originHost != "" || trace != "" {
		if err := s.SetTaskOrigin(task.ID, originHost, trace); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

// fleetOriginRun inserts a run row as given and stamps its origin host
// ("" leaves the local default).
func fleetOriginRun(t *testing.T, s *Store, id string, taskID int64, status string, startedAt time.Time, originHost string) {
	t.Helper()
	if _, err := s.CreateRun(Run{ID: id, TaskID: taskID, Provider: "glm", Status: status, StartedAt: startedAt}); err != nil {
		t.Fatal(err)
	}
	if originHost != "" {
		if err := s.SetRunOrigin(id, originHost); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreListLiveOriginRuns(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	taskA := fleetOriginTask(t, s, ws.ID, "hostA", "tr-a")
	taskB := fleetOriginTask(t, s, ws.ID, "hostB", "tr-b")

	t1 := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	fleetOriginRun(t, s, "a1", taskA.ID, "running", t1, "hostA")
	fleetOriginRun(t, s, "a2", taskA.ID, "done", t2, "hostA")    // finished: never live
	fleetOriginRun(t, s, "b0", taskB.ID, "running", t1, "hostB") // ties a1: id breaks it
	fleetOriginRun(t, s, "b1", taskB.ID, "running", t2, "hostB")
	fleetOriginRun(t, s, "local", taskA.ID, "running", t2, "") // local: never fleet-live

	ids := func(runs []OriginRun) []string {
		out := make([]string, len(runs))
		for i, r := range runs {
			out[i] = r.ID
		}
		return out
	}

	gotA, err := s.ListLiveOriginRuns("hostA")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotA) != 1 || gotA[0].ID != "a1" || gotA[0].OriginHost != "hostA" {
		t.Fatalf("hostA live runs = %+v, want only a1", gotA)
	}
	if gotA[0].TaskID != taskA.ID || !gotA[0].StartedAt.Equal(t1) || gotA[0].StartedAt.Location() != time.UTC {
		t.Fatalf("a1 = %+v, want task %d at %v UTC", gotA[0], taskA.ID, t1)
	}

	gotB, err := s.ListLiveOriginRuns("hostB")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotB) != 2 {
		t.Fatalf("hostB live runs = %v, want b0 and b1", ids(gotB))
	}

	all, err := s.ListLiveOriginRuns("")
	if err != nil {
		t.Fatal(err)
	}
	// Every non-empty origin's live runs, local excluded, ordered by
	// started_at then id -- so a1 ties b0 and loses nothing by id order.
	want := []string{"a1", "b0", "b1"}
	if got := ids(all); !slices.Equal(got, want) {
		t.Fatalf("all live origin runs = %v, want %v (no local, no finished)", got, want)
	}
}

func TestStoreOriginCostSince(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task1 := fleetOriginTask(t, s, ws.ID, "hostA", "tr-1")
	task2 := fleetOriginTask(t, s, ws.ID, "hostA", "tr-2")
	taskB := fleetOriginTask(t, s, ws.ID, "hostB", "tr-b")

	since := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	mk := func(id string, task Task, host string, at time.Time, cost *float64) {
		t.Helper()
		fleetOriginRun(t, s, id, task.ID, "done", at, host)
		if err := s.UpsertRunUsage(RunUsage{RunID: id, TaskID: task.ID, Provider: "glm", Source: "test", CostUSD: cost}); err != nil {
			t.Fatal(err)
		}
	}
	mk("c1", task1, "hostA", since.Add(5*time.Minute), ptr(1.25))  // counts: another trace, still hostA
	mk("c2", task2, "hostA", since.Add(10*time.Minute), ptr(2.5))  // counts
	mk("c3", task1, "hostA", since.Add(-time.Hour), ptr(100.0))    // older than since
	mk("c4", task1, "hostA", since.Add(15*time.Minute), nil)       // NULL cost adds nothing
	mk("c5", taskB, "hostB", since.Add(10*time.Minute), ptr(50.0)) // another origin

	sum, err := s.OriginCostSince("hostA", since)
	if err != nil {
		t.Fatal(err)
	}
	if sum != 3.75 {
		t.Fatalf("hostA cost since %v = %v, want 3.75 (1.25 + 2.5 only)", since, sum)
	}
	other, err := s.OriginCostSince("hostB", since)
	if err != nil {
		t.Fatal(err)
	}
	if other != 50 {
		t.Fatalf("hostB cost = %v, want 50", other)
	}
	none, err := s.OriginCostSince("nobody", since)
	if err != nil {
		t.Fatal(err)
	}
	if none != 0 {
		t.Fatalf("unknown origin cost = %v, want 0", none)
	}
}

func TestStoreGetFleetIdem(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)

	got, ok, err := s.GetFleetIdem("h1", "k1")
	if err != nil || ok || got != (FleetIdem{}) {
		t.Fatalf("GetFleetIdem() absent = %+v, %v, %v; want zero, false, nil", got, ok, err)
	}

	createdAt := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	put := FleetIdem{HostID: "h1", Key: "k1", Method: "fleet.task.create", TaskID: "7", RunID: "r9", Response: `{"taskId":"7"}`, CreatedAt: createdAt}
	if _, created, err := s.PutFleetIdem(put); err != nil || !created {
		t.Fatalf("PutFleetIdem() = %v, %v; want created", created, err)
	}

	got, ok, err = s.GetFleetIdem("h1", "k1")
	if err != nil || !ok {
		t.Fatalf("GetFleetIdem() after put = %v, %v; want ok", ok, err)
	}
	if got.Method != put.Method || got.TaskID != put.TaskID || got.RunID != put.RunID || got.Response != put.Response {
		t.Fatalf("stored idem = %+v, want %+v", got, put)
	}
	if !got.CreatedAt.Equal(createdAt) || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v, want %v in UTC", got.CreatedAt, createdAt)
	}

	if _, ok, err := s.GetFleetIdem("h1", "other"); err != nil || ok {
		t.Fatalf("GetFleetIdem() other key = %v, %v; want absent without error", ok, err)
	}
	if _, ok, err := s.GetFleetIdem("h2", "k1"); err != nil || ok {
		t.Fatalf("GetFleetIdem() other host = %v, %v; want absent without error", ok, err)
	}
}

func TestStoreMarkRunWallLimit(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task := fleetOriginTask(t, s, ws.ID, "hostA", "tr-a")
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	fleetOriginRun(t, s, "r-live", task.ID, "running", at, "hostA")
	fleetOriginRun(t, s, "r-done", task.ID, "done", at, "hostA")

	// A still-running run is not stamped: the sweeper stops it first.
	if err := s.MarkRunWallLimit("r-live"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("MarkRunWallLimit(running) = %v, want ErrFleetNotFound", err)
	}
	live, err := s.GetRun("r-live")
	if err != nil {
		t.Fatal(err)
	}
	if live.StopReason != "" || live.Status != "running" {
		t.Fatalf("running run after refused stamp = %q/%q, want untouched", live.Status, live.StopReason)
	}

	if err := s.MarkRunWallLimit("r-done"); err != nil {
		t.Fatalf("MarkRunWallLimit(done) error = %v", err)
	}
	done, err := s.GetRun("r-done")
	if err != nil {
		t.Fatal(err)
	}
	if done.StopReason != "wall_limit" || done.Status != "done" {
		t.Fatalf("done run = %q/%q, want done/wall_limit", done.Status, done.StopReason)
	}

	if err := s.MarkRunWallLimit("nope"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("MarkRunWallLimit(unknown) = %v, want ErrFleetNotFound", err)
	}
}
