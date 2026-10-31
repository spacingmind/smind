package store

import (
	"errors"
	"testing"
	"time"
)

func TestFleetWatch_MirrorAndWatchOneTransaction(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	mirror := FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-1", LocalTaskID: 7, Title: "fix the thing", Branch: "fix", State: "running"}
	watch := FleetWatch{HostID: "h1", RemoteTaskID: "rt-1", RemoteRunID: "rr-1", LocalTaskID: 7, ChatID: 3, Cursor: "c0", Notify: true}

	watchID, err := s.PutFleetRemoteTaskAndWatch(mirror, &watch)
	if err != nil {
		t.Fatalf("PutFleetRemoteTaskAndWatch() error = %v", err)
	}
	if watchID == 0 {
		t.Fatal("watchID = 0, want a real id")
	}
	got, err := s.GetFleetRemoteTask("h1", "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "fix the thing" || got.Branch != "fix" || got.State != "running" || got.LocalTaskID != 7 {
		t.Fatalf("mirror = %+v", got)
	}
	if got.CostUSD != nil {
		t.Fatalf("CostUSD = %v, want nil (never estimated)", *got.CostUSD)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("mirror UpdatedAt zero, want stamped")
	}
	w, err := s.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if w.HostID != "h1" || w.RemoteTaskID != "rt-1" || w.RemoteRunID != "rr-1" || w.LocalTaskID != 7 ||
		w.ChatID != 3 || w.Cursor != "c0" || w.State != FleetWatchOpen || !w.Notify {
		t.Fatalf("watch = %+v", w)
	}
	open, err := s.ListOpenFleetWatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != watchID {
		t.Fatalf("open watches = %+v, want [%d]", open, watchID)
	}

	// A watch missing its host or remote task is rejected before anything
	// is written: the mirror must not land either.
	for _, bad := range []FleetWatch{
		{RemoteTaskID: "rt-2"},
		{HostID: "h1"},
	} {
		if _, err := s.PutFleetRemoteTaskAndWatch(
			FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-2", Title: "half"}, &bad,
		); err == nil {
			t.Fatalf("watch %+v accepted, want error", bad)
		}
	}
	if _, err := s.GetFleetRemoteTask("h1", "rt-2"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("mirror rt-2 = %v, want not found: the failed put left nothing", err)
	}

	// A mirror with no watch to open still lands on its own.
	if _, err := s.PutFleetRemoteTaskAndWatch(FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-3"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetFleetRemoteTask("h1", "rt-3"); err != nil {
		t.Fatal(err)
	}
	if open, err := s.ListOpenFleetWatches(); err != nil || len(open) != 1 {
		t.Fatalf("open watches = %d, %v; still want 1", len(open), err)
	}
}

func TestFleetWatch_SurvivesHostDelete(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	if err := s.CreateFleetPair(FleetHost{HostID: "h1", Alias: "alice", Pub: []byte{1}, State: "paired"}, nil); err != nil {
		t.Fatal(err)
	}
	watchID, err := s.PutFleetRemoteTaskAndWatch(
		FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-1"},
		&FleetWatch{HostID: "h1", RemoteTaskID: "rt-1", RemoteRunID: "rr-1"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteFleetHost("h1"); err != nil {
		t.Fatal(err)
	}
	open, err := s.ListOpenFleetWatches()
	if err != nil {
		t.Fatal(err)
	}
	// The watch outlives its host: this is what lets the poller report
	// "host unavailable" and close it, instead of it vanishing silently.
	if len(open) != 1 || open[0].ID != watchID || open[0].HostID != "h1" {
		t.Fatalf("open watches after host delete = %+v, want watch %d on h1", open, watchID)
	}
	if err := s.CloseFleetWatch(watchID, "host unavailable"); err != nil {
		t.Fatal(err)
	}
	if open, err = s.ListOpenFleetWatches(); err != nil || len(open) != 0 {
		t.Fatalf("open watches after close = %d, %v; want 0", len(open), err)
	}
}

func TestFleetWatch_UpdateCloseKeepsFirstReason(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	watchID, err := s.PutFleetRemoteTaskAndWatch(
		FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-1"},
		&FleetWatch{HostID: "h1", RemoteTaskID: "rt-1", RemoteRunID: "rr-1"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateFleetWatch(watchID, "cursor-2", 3); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Cursor != "cursor-2" || w.Failures != 3 || w.State != FleetWatchOpen {
		t.Fatalf("watch = %+v, want cursor-2/3/open", w)
	}
	if err := s.UpdateFleetWatch(999, "c", 0); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("UpdateFleetWatch() unknown = %v, want ErrFleetNotFound", err)
	}

	if err := s.CloseFleetWatch(watchID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseFleetWatch(watchID, "done again"); err != nil {
		t.Fatalf("close twice = %v, want the second close to be a no-op success", err)
	}
	w, err = s.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != FleetWatchClosed || w.ClosedReason != "done" {
		t.Fatalf("watch = %+v, want closed/done: the first reason stays", w)
	}
	if open, err := s.ListOpenFleetWatches(); err != nil || len(open) != 0 {
		t.Fatalf("open watches = %d, %v; want 0", len(open), err)
	}
	if err := s.CloseFleetWatch(999, "x"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("CloseFleetWatch() unknown = %v, want ErrFleetNotFound", err)
	}
}

func TestFleetWatch_DuplicateRunNotDuplicated(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	put := func(title, cursor string) int64 {
		t.Helper()
		id, err := s.PutFleetRemoteTaskAndWatch(
			FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-1", Title: title, State: "running"},
			&FleetWatch{HostID: "h1", RemoteTaskID: "rt-1", RemoteRunID: "rr-1", Cursor: cursor},
		)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := put("first", "c1")
	second := put("second", "c2")
	if first != second {
		t.Fatalf("watch ids = %d then %d, want the same watch reused", first, second)
	}

	open, err := s.ListOpenFleetWatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("open watches = %d, want 1 (no duplicate for the same remote run)", len(open))
	}
	if open[0].Cursor != "c1" {
		t.Fatalf("cursor = %q, want the existing watch's c1 unchanged", open[0].Cursor)
	}
	// The mirror still tracks the latest poll.
	got, err := s.GetFleetRemoteTask("h1", "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "second" {
		t.Fatalf("title = %q, want the upserted second", got.Title)
	}
}

func TestFleetRemoteTask_NilCostStaysNil(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	put := func(cost *float64) FleetRemoteTask {
		t.Helper()
		if _, err := s.PutFleetRemoteTaskAndWatch(FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-1", CostUSD: cost}, nil); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetFleetRemoteTask("h1", "rt-1")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := put(nil); got.CostUSD != nil {
		t.Fatalf("CostUSD = %v, want nil: unknown is not zero", *got.CostUSD)
	}
	known := 1.25
	if got := put(&known); got.CostUSD == nil || *got.CostUSD != 1.25 {
		t.Fatalf("CostUSD = %v, want 1.25", got.CostUSD)
	}
	// Back to unknown: the remote stopped reporting, so the mirror forgets
	// the stale number instead of serving it as fresh.
	if got := put(nil); got.CostUSD != nil {
		t.Fatalf("CostUSD = %v, want nil again", *got.CostUSD)
	}

	all, err := s.ListFleetRemoteTasks("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].CostUSD != nil {
		t.Fatalf("list all = %+v, want 1 row with nil cost", all)
	}
	if err := s.DeleteFleetRemoteTask("h1", "rt-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFleetRemoteTask("h1", "rt-1"); err != nil {
		t.Fatalf("delete twice = %v, want idempotent", err)
	}
	if _, err := s.GetFleetRemoteTask("h1", "rt-1"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("GetFleetRemoteTask() after delete = %v, want ErrFleetNotFound", err)
	}
}

func TestFleetIdem_SecondPutReturnsFirst(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	first, created, err := s.PutFleetIdem(FleetIdem{
		HostID: "h1", Key: "k1", Method: "chat.send", TaskID: "rt-1", RunID: "rr-1", Response: "ok-1", CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first put not created, want created=true")
	}
	if first.Response != "ok-1" || !first.CreatedAt.Equal(at) {
		t.Fatalf("first = %+v", first)
	}

	second, created, err := s.PutFleetIdem(FleetIdem{
		HostID: "h1", Key: "k1", Method: "chat.send", Response: "ok-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second put created, want created=false")
	}
	if second.Response != "ok-1" || second.RunID != "rr-1" || !second.CreatedAt.Equal(at) {
		t.Fatalf("second = %+v, want the first row back untouched", second)
	}

	// A different key is a different request.
	if _, created, err := s.PutFleetIdem(FleetIdem{HostID: "h1", Key: "k2", Method: "chat.send", Response: "ok-3"}); err != nil || !created {
		t.Fatalf("new key = %t, %v; want created", created, err)
	}
}

func TestFleetIdem_SweepOld(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	now := time.Now().UTC()
	old, created, err := s.PutFleetIdem(FleetIdem{HostID: "h1", Key: "old", Method: "chat.send", CreatedAt: now.Add(-40 * 24 * time.Hour)})
	if err != nil || !created {
		t.Fatalf("put old = %t, %v", created, err)
	}
	if old.Key != "old" {
		t.Fatalf("put old = %+v, want it echoed back", old)
	}
	if _, created, err := s.PutFleetIdem(FleetIdem{HostID: "h1", Key: "fresh", Method: "chat.send", CreatedAt: now}); err != nil || !created {
		t.Fatalf("put fresh = %t, %v", created, err)
	}

	n, err := s.SweepFleetIdem(now.Add(-30 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sweep deleted %d, want only the old row", n)
	}
	// The survivor still replays: the sweep only removed what expired.
	kept, created, err := s.PutFleetIdem(FleetIdem{HostID: "h1", Key: "fresh", Method: "chat.send"})
	if err != nil || created {
		t.Fatalf("fresh after sweep = %t, %v; want still present", created, err)
	}
	if kept.Key != "fresh" {
		t.Fatalf("kept = %+v, want the fresh row", kept)
	}
	if _, created, err := s.PutFleetIdem(FleetIdem{HostID: "h1", Key: "old", Method: "chat.send"}); err != nil || !created {
		t.Fatalf("old key after sweep = %t, %v; want re-created (it was swept)", created, err)
	}
}

func TestFleetOrigin_SetAndGetTaskAndRun(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	run, err := s.CreateRun(Run{ID: "run-1", TaskID: task.ID, Provider: "glm", Prompt: "hi", Status: "running", StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}

	// Before anything is set: a local task/run reads as no origin.
	host, trace, err := s.TaskOrigin(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" || trace != "" {
		t.Fatalf("task origin = %q/%q, want empty", host, trace)
	}

	if err := s.SetTaskOrigin(task.ID, "h1", "trace-1"); err != nil {
		t.Fatal(err)
	}
	host, trace, err = s.TaskOrigin(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if host != "h1" || trace != "trace-1" {
		t.Fatalf("task origin = %q/%q, want h1/trace-1", host, trace)
	}
	if err := s.SetRunOrigin(run.ID, "h1"); err != nil {
		t.Fatal(err)
	}
	if h, err := s.RunOrigin(run.ID); err != nil || h != "h1" {
		t.Fatalf("run origin = %q, %v; want h1", h, err)
	}

	if _, _, err := s.TaskOrigin(task.ID + 999); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("TaskOrigin() unknown = %v, want ErrFleetNotFound", err)
	}
	if err := s.SetTaskOrigin(task.ID+999, "h1", ""); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("SetTaskOrigin() unknown = %v, want ErrFleetNotFound", err)
	}
	if _, err := s.RunOrigin("nope"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("RunOrigin() unknown = %v, want ErrFleetNotFound", err)
	}
	if err := s.SetRunOrigin("nope", "h1"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("SetRunOrigin() unknown = %v, want ErrFleetNotFound", err)
	}
}
