package serve

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

// runStopper records every stop call and finishes the run, like the real
// adapter's StopRun (which stops and waits for the run to end), so
// MarkRunWallLimit finds it no longer running.
type runStopper struct {
	mu    sync.Mutex
	st    *store.Store
	done  time.Time
	calls []string
}

func (f *runStopper) stop(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	_, err := f.st.UpdateRunStatus(id, "done", &f.done, "", "")
	return err
}

func (f *runStopper) stopped() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestFleetWallLimitStopsRun(t *testing.T) {
	st := openStore(t)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ws := fleetWorkspace(t, st)
	task := mkOriginTask(t, st, ws, "hostA", "tr-a")
	mkOriginRun(t, st, "over", task, "running", now.Add(-2*time.Hour), "hostA")
	mkOriginRun(t, st, "fresh", task, "running", now.Add(-time.Minute), "hostA")

	f := &runStopper{st: st, done: now}
	n, err := SweepWall(st, fakeAuthorizer{grant: registry.Grant{MaxWallSeconds: 3600}}, st, f.stop, now)
	if err != nil || n != 1 {
		t.Fatalf("stopped %d, err %v; want 1, nil", n, err)
	}
	if got := f.stopped(); len(got) != 1 || got[0] != "over" {
		t.Fatalf("stop calls %v, want only over", got)
	}
	if r, err := st.GetRun("over"); err != nil || r.Status != "done" || r.StopReason != wire.StopReasonWallLimit {
		t.Fatalf("stopped run = %q/%q, err %v; want done/wall_limit", r.Status, r.StopReason, err)
	}
	if r, err := st.GetRun("fresh"); err != nil || r.Status != "running" || r.StopReason != "" {
		t.Fatalf("fresh run = %q/%q, err %v; want untouched", r.Status, r.StopReason, err)
	}

	rows, err := st.ListFleetAudit("", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Direction != "in" || row.HostID != "hostA" || row.Method != "wall_limit" ||
		row.RunID != "over" || row.Outcome != "ok" || row.Code != wire.StopReasonWallLimit || !row.TS.Equal(now) {
		t.Fatalf("row %+v", row)
	}

	// A run whose grant cannot be resolved is skipped, never stopped.
	n, err = SweepWall(st, fakeAuthorizer{err: errors.New("host revoked")}, st, f.stop, now)
	if err != nil || n != 0 {
		t.Fatalf("stopped %d, err %v; want 0, nil", n, err)
	}
	if got := f.stopped(); len(got) != 1 {
		t.Fatalf("stop calls after revoked grant %v", got)
	}
}

func TestFleetWallSweeperRuns(t *testing.T) {
	st := openStore(t)
	ws := fleetWorkspace(t, st)
	task := mkOriginTask(t, st, ws, "hostA", "tr-a")
	mkOriginRun(t, st, "late", task, "running", time.Now().UTC().Add(-2*time.Hour), "hostA")

	f := &runStopper{st: st, done: time.Now().UTC()}
	stop := StartWallSweeper(st, fakeAuthorizer{grant: registry.Grant{MaxWallSeconds: 3600}}, st, f.stop, 10*time.Millisecond, nil)
	deadline := time.Now().Add(2 * time.Second)
	for {
		runs, err := st.ListLiveOriginRuns("hostA")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("overdue run was not stopped")
		}
		time.Sleep(5 * time.Millisecond)
	}
	done := make(chan struct{})
	go func() { stop(); stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not return")
	}
	if got := f.stopped(); len(got) != 1 || got[0] != "late" {
		t.Fatalf("stop calls %v", got)
	}
	if r, err := st.GetRun("late"); err != nil || r.StopReason != wire.StopReasonWallLimit {
		t.Fatalf("late run stop reason %q, err %v", r.StopReason, err)
	}
}
