package mirror

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, h := range []store.FleetHost{
		{HostID: "h-box", Alias: "box", Pub: []byte{1}, State: "paired"},
		{HostID: "h-win", Alias: "win", Pub: []byte{2}, State: "paired"},
	} {
		if err := s.CreateFleetPair(h, nil); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func putRunning(t *testing.T, s *store.Store, host, id string, cost *float64) {
	t.Helper()
	_, err := s.PutFleetRemoteTaskAndWatch(store.FleetRemoteTask{
		HostID: host, RemoteTaskID: id, LocalTaskID: 3, Title: "t", State: "running", CostUSD: cost,
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

type fakeObs struct {
	mu    sync.Mutex
	calls map[string]int
	fn    func(ref string, taskID int64) (wire.RunStatus, error)
}

func (f *fakeObs) RunStatus(_ context.Context, ref string, _ client.CallOpts, _ string, taskID int64) (wire.RunStatus, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[ref]++
	f.mu.Unlock()
	return f.fn(ref, taskID)
}

func f64(v float64) *float64 { return &v }

func TestOwnerHostIsSingle(t *testing.T) {
	s := openStore(t)
	hb, rb, err := Owner(s, "box:7")
	if err != nil {
		t.Fatal(err)
	}
	hw, rw, err := Owner(s, "win:7")
	if err != nil {
		t.Fatal(err)
	}
	if hb == hw || rb != "7" || rw != "7" {
		t.Fatalf("owners = %s/%s, %s/%s", hb, rb, hw, rw)
	}
	for _, bad := range []string{"7", "nope:7", "box:", ":7", "Box:7", "box:a:b"} {
		if _, _, err := Owner(s, bad); err == nil {
			t.Fatalf("Owner(%q) succeeded", bad)
		}
	}
	if _, _, err := Owner(s, "nope:7"); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("nope err = %v, want ErrUnknownHost", err)
	}
	putRunning(t, s, "h-box", "7", nil)
	putRunning(t, s, "h-win", "7", nil)
	rows, err := s.ListFleetRemoteTasks("")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
}

func TestRefresherUpdatesOnlyOwnerRowOnSuccess(t *testing.T) {
	s := openStore(t)
	putRunning(t, s, "h-box", "7", nil)
	putRunning(t, s, "h-win", "7", nil)
	obs := &fakeObs{fn: func(ref string, _ int64) (wire.RunStatus, error) {
		if ref == "h-box" {
			// Fields naming other ids must not matter.
			return wire.RunStatus{State: "done", CostUSD: f64(0.5), TaskID: 99, RunID: "h-win"}, nil
		}
		return wire.RunStatus{}, errors.New("link down")
	}}
	r := New(Options{Store: s, Observer: obs})
	if n := r.Pass(context.Background()); n != 1 {
		t.Fatalf("updated = %d, want 1", n)
	}
	b, _ := s.GetFleetRemoteTask("h-box", "7")
	if b.State != "done" || b.CostUSD == nil || *b.CostUSD != 0.5 {
		t.Fatalf("box = %+v", b)
	}
	w, _ := s.GetFleetRemoteTask("h-win", "7")
	if w.State != "running" || w.CostUSD != nil {
		t.Fatalf("win = %+v", w)
	}
	if _, err := s.GetFleetRemoteTask("h-box", "99"); err == nil {
		t.Fatal("response id created a row")
	}
	rows, _ := s.ListFleetRemoteTasks("")
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRefresherErrorLeavesRowUntouched(t *testing.T) {
	s := openStore(t)
	putRunning(t, s, "h-box", "7", f64(1.25))
	putRunning(t, s, "h-win", "8", nil)
	_, err := s.PutFleetRemoteTaskAndWatch(store.FleetRemoteTask{HostID: "h-win", RemoteTaskID: "9", State: "done"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.ListFleetRemoteTasks("")

	obs := &fakeObs{fn: func(string, int64) (wire.RunStatus, error) { return wire.RunStatus{}, errors.New("boom") }}
	r := New(Options{Store: s, Observer: obs})
	if n := r.Pass(context.Background()); n != 0 {
		t.Fatalf("updated = %d", n)
	}
	after, _ := s.ListFleetRemoteTasks("")
	for i := range before {
		if before[i].State != after[i].State || !before[i].UpdatedAt.Equal(after[i].UpdatedAt) ||
			!sameCost(before[i].CostUSD, after[i].CostUSD) {
			t.Fatalf("row changed: %+v -> %+v", before[i], after[i])
		}
	}
	if obs.calls["h-win"] != 1 || obs.calls["h-box"] != 1 {
		t.Fatalf("calls = %v (terminal row must not be queried)", obs.calls)
	}

	// A nil-cost answer keeps the old cost; a state change is written.
	obs.fn = func(string, int64) (wire.RunStatus, error) {
		return wire.RunStatus{State: "error", CostUSD: nil}, nil
	}
	if n := r.Pass(context.Background()); n != 2 {
		t.Fatalf("updated = %d, want 2", n)
	}
	b, _ := s.GetFleetRemoteTask("h-box", "7")
	if b.State != "error" || b.CostUSD == nil || *b.CostUSD != 1.25 {
		t.Fatalf("box = %+v", b)
	}
	w, _ := s.GetFleetRemoteTask("h-win", "8")
	if w.CostUSD != nil {
		t.Fatalf("win cost = %v, want nil", *w.CostUSD)
	}
	// Terminal now: no further queries.
	r.Pass(context.Background())
	if obs.calls["h-box"] != 2 {
		t.Fatalf("calls = %v", obs.calls)
	}
}
