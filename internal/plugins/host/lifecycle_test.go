package host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLifecycle_CrashBackoff_Breaker(t *testing.T) {
	clk := newFakeClock()
	pol := fastPolicy()
	pol.BackoffBase, pol.BackoffCap = time.Second, 2*time.Second
	pol.BreakerCrashes, pol.BreakerWindow = 4, 10*time.Minute
	h := New(Options{Policy: pol, Clock: clk})
	defer h.Close()
	start := clk.Now()
	h.Ensure(spec(t, "demo", "crash"))

	// crash 1 -> retry after 1s, crash 2 -> 2s, crash 3 -> 2s (the cap), crash 4 -> failed
	wantDelay := []time.Duration{time.Second, 2 * time.Second, 2 * time.Second}
	for i, d := range wantDelay {
		st := waitRestarts(t, h, "demo", i+1)
		if got := st.RetryAt.Sub(clk.Now()); got != d {
			t.Fatalf("crash %d: retry in %s, want %s", i+1, got, d)
		}
		if st.Restarts != i+1 || !strings.Contains(st.LastError, "exited") {
			t.Fatalf("crash %d: %+v", i+1, st)
		}
		// Nothing restarts until the clock moves.
		time.Sleep(50 * time.Millisecond)
		if s2, _ := h.Status("demo"); s2.State != StateRestarting || s2.Restarts != i+1 {
			t.Fatalf("restarted before the backoff elapsed: %+v", s2)
		}
		waitFor(t, 5*time.Second, "the backoff timer", func() bool { return clk.pending() == 1 })
		clk.Advance(d)
	}
	st := waitState(t, h, "demo", StateFailed)
	if st.Restarts != 4 || !strings.Contains(st.LastError, "crashed 4 times within 10m0s") {
		t.Fatalf("breaker status: %+v", st)
	}
	if clk.Now().Sub(start) != 5*time.Second {
		t.Fatalf("unexpected fake time %s", clk.Now().Sub(start))
	}
	// A tripped breaker stays tripped: no more restarts, however long we wait.
	clk.Advance(time.Hour)
	time.Sleep(100 * time.Millisecond)
	if s2, _ := h.Status("demo"); s2.State != StateFailed || s2.Restarts != 4 {
		t.Fatalf("failed plugin restarted: %+v", s2)
	}
	// Restart is the way out (reload): a fresh supervisor, a fresh budget.
	h.Restart(spec(t, "demo", "crash"))
	waitState(t, h, "demo", StateRestarting)
	if s2, _ := h.Status("demo"); s2.Restarts != 1 {
		t.Fatalf("restart did not reset the budget: %+v", s2)
	}
}

func TestLifecycle_CrashesOutsideWindowDoNotTrip(t *testing.T) {
	clk := newFakeClock()
	pol := fastPolicy()
	pol.BackoffBase, pol.BackoffCap = time.Second, time.Second
	pol.BreakerCrashes, pol.BreakerWindow = 3, time.Minute
	h := New(Options{Policy: pol, Clock: clk})
	defer h.Close()
	h.Ensure(spec(t, "demo", "crash"))
	for i := 0; i < 6; i++ {
		waitRestarts(t, h, "demo", i+1)
		waitFor(t, 5*time.Second, "the backoff timer", func() bool { return clk.pending() == 1 })
		clk.Advance(40 * time.Second) // each crash is older than the window by the next one
	}
	if st, _ := h.Status("demo"); st.State == StateFailed {
		t.Fatalf("spaced crashes tripped the breaker: %+v", st)
	}
}

func TestLifecycle_RunningThenCrashRestarts(t *testing.T) {
	pol := fastPolicy()
	pol.BackoffBase, pol.BackoffCap = 10*time.Millisecond, 10*time.Millisecond
	pol.BreakerCrashes = 1000
	h := New(Options{Policy: pol})
	defer h.Close()
	h.Ensure(spec(t, "demo", "crash-after-ready"))
	st := waitState(t, h, "demo", StateRunning)
	first := st.PID
	waitFor(t, 10*time.Second, "a second run", func() bool {
		s, _ := h.Status("demo")
		return s.State == StateRunning && s.PID != first && s.Restarts >= 1
	})
	for _, l := range h.Logs("demo", 0) {
		if strings.Contains(l.Text, "crashed: exited") {
			return
		}
	}
	t.Fatal("the crash is not in the plugin log")
}

func TestLifecycle_StartTimeout(t *testing.T) {
	pol := fastPolicy()
	pol.StartTimeout = 300 * time.Millisecond
	pol.BackoffBase, pol.BackoffCap = time.Hour, time.Hour
	h := New(Options{Policy: pol})
	defer h.Close()
	h.Ensure(spec(t, "demo", "noinit"))
	st := waitState(t, h, "demo", StateRestarting)
	if !strings.Contains(st.LastError, "did not answer plugin.initialize") {
		t.Fatalf("%+v", st)
	}
}

func TestLifecycle_StartFailure_ProgramMissing(t *testing.T) {
	pol := fastPolicy()
	pol.BackoffBase, pol.BackoffCap = time.Hour, time.Hour
	h := New(Options{Policy: pol})
	defer h.Close()
	sp := spec(t, "demo", "sleep")
	sp.Command = []string{"definitely-not-a-program-xyz"}
	h.Ensure(sp)
	st := waitState(t, h, "demo", StateRestarting)
	if !strings.Contains(st.LastError, "not found on the plugin PATH") {
		t.Fatalf("%+v", st)
	}
}

func TestResolve_RelativeCommandConfinedToPluginDir(t *testing.T) {
	sp := spec(t, "demo", "sleep")
	for _, c := range [][]string{{"../outside"}, {"sub/../../outside"}} {
		sp.Command = c
		if _, _, err := resolve(sp, nil); err == nil || !strings.Contains(err.Error(), "leaves the plugin directory") {
			t.Errorf("%v: %v", c, err)
		}
	}
}

func TestLifecycle_StopEscalatesToKill_NoOrphans(t *testing.T) {
	rec := &recorder{}
	pol := fastPolicy()
	pol.StopGrace = 300 * time.Millisecond
	h := New(Options{Policy: pol, Registry: rec})
	defer h.Close()
	h.Ensure(spec(t, "demo", "ignore-term"))
	waitState(t, h, "demo", StateRunning)
	var pids struct{ Self, Grandchild int }
	if err := json.Unmarshal(rec.wait(t, "test.pids"), &pids); err != nil {
		t.Fatal(err)
	}
	if !alive(pids.Self) || !alive(pids.Grandchild) {
		t.Fatalf("setup: %+v not alive", pids)
	}
	began := time.Now()
	h.Stop("demo")
	if took := time.Since(began); took < 250*time.Millisecond {
		t.Errorf("a plugin that ignores SIGTERM was gone after %s: the grace period was skipped", took)
	}
	waitFor(t, 3*time.Second, "both processes to be gone", func() bool { return !alive(pids.Self) && !alive(pids.Grandchild) })
	if _, ok := h.Status("demo"); ok {
		t.Fatal("a stopped plugin still has a status")
	}
	var sawKill bool
	for _, l := range h.Logs("demo", 0) {
		sawKill = sawKill || strings.Contains(l.Text, "killing it")
	}
	if !sawKill {
		t.Error("the escalation is not in the plugin log")
	}
}

func TestLifecycle_GrandchildKilledEvenWhenLeaderExitsOnTerm(t *testing.T) {
	// A plugin that exits promptly on stop but leaves a child behind: the
	// group sweep after the leader's exit must still take the child down.
	rec := &recorder{}
	h := New(Options{Policy: fastPolicy(), Registry: rec})
	defer h.Close()
	h.Ensure(spec(t, "demo", "orphan-maker"))
	waitState(t, h, "demo", StateRunning)
	var pids struct{ Self, Grandchild int }
	if err := json.Unmarshal(rec.wait(t, "test.pids"), &pids); err != nil {
		t.Fatal(err)
	}
	h.Stop("demo")
	waitFor(t, 3*time.Second, "the leftover grandchild to be killed", func() bool { return !alive(pids.Grandchild) })
}

func TestGate_NothingSpawnedWhenClosed(t *testing.T) {
	open := false
	h := New(Options{Policy: fastPolicy(), Gate: func() bool { return open }})
	defer h.Close()
	h.Ensure(spec(t, "demo", "sleep"))
	time.Sleep(100 * time.Millisecond)
	if _, ok := h.Status("demo"); ok {
		t.Fatal("a plugin was started with the gate closed")
	}
	if len(h.Logs("demo", 0)) != 0 {
		t.Fatalf("log of a never-started plugin: %+v", h.Logs("demo", 0))
	}
	open = true
	h.Ensure(spec(t, "demo", "report"))
	waitState(t, h, "demo", StateRunning)
}

func TestGate_ClosingBetweenRestartsStopsTheLoop(t *testing.T) {
	clk := newFakeClock()
	var open = make(chan bool, 1)
	state := true
	gate := func() bool {
		select {
		case v := <-open:
			state = v
		default:
		}
		return state
	}
	pol := fastPolicy()
	pol.BreakerCrashes = 100
	h := New(Options{Policy: pol, Clock: clk, Gate: gate})
	defer h.Close()
	h.Ensure(spec(t, "demo", "crash"))
	waitState(t, h, "demo", StateRestarting)
	open <- false
	waitFor(t, 5*time.Second, "the backoff timer", func() bool { return clk.pending() == 1 })
	clk.Advance(time.Hour)
	waitFor(t, 5*time.Second, "the supervisor to give up", func() bool { _, ok := h.Status("demo"); return !ok })
}

func TestHost_StopAll_StopsEveryPlugin(t *testing.T) {
	rec := &recorder{}
	h := New(Options{Policy: fastPolicy(), Registry: rec})
	for _, id := range []string{"a", "b", "c"} {
		h.Ensure(spec(t, id, "ignore-term"))
		waitState(t, h, id, StateRunning)
	}
	waitFor(t, 5*time.Second, "three pid reports", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		n := 0
		for _, x := range rec.notes {
			if x.method == "test.pids" {
				n++
			}
		}
		return n == 3
	})
	var all []int
	rec.mu.Lock()
	for _, x := range rec.notes {
		var p struct{ Self, Grandchild int }
		_ = json.Unmarshal(x.params, &p)
		all = append(all, p.Self, p.Grandchild)
	}
	rec.mu.Unlock()
	h.Close()
	for _, pid := range all {
		pid := pid
		waitFor(t, 3*time.Second, "no orphan", func() bool { return !alive(pid) })
	}
}

func TestHost_EnsureSameSpecIsNoop_DifferentReplaces(t *testing.T) {
	h := New(Options{Policy: fastPolicy()})
	defer h.Close()
	sp := spec(t, "demo", "report")
	h.Ensure(sp)
	st := waitState(t, h, "demo", StateRunning)
	h.Ensure(sp)
	if s2, _ := h.Status("demo"); s2.PID != st.PID {
		t.Fatal("Ensure with an unchanged spec restarted the plugin")
	}
	sp.Version = "2.0.0"
	h.Ensure(sp)
	waitFor(t, 10*time.Second, "the replacement", func() bool {
		s2, ok := h.Status("demo")
		return ok && s2.State == StateRunning && s2.PID != st.PID
	})
	if _, err := h.Call(context.Background(), "nope", "echo", nil); err == nil {
		t.Fatal("call on an unknown plugin should fail")
	}
}

func TestChangeNotifier_FiresOnTransitions(t *testing.T) {
	got := make(chan string, 32)
	h := New(Options{Policy: fastPolicy(), OnChange: func(id string) { got <- id }})
	defer h.Close()
	h.Ensure(spec(t, "demo", "report"))
	waitState(t, h, "demo", StateRunning)
	select {
	case id := <-got:
		if id != "demo" {
			t.Fatal(id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change notification")
	}
}
