package watch

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

const (
	bmin = 10 * time.Millisecond
	bmax = 30 * time.Millisecond
	poll = 7 * time.Millisecond

	remoteTask   = "42"
	remoteTaskID = int64(42)
)

type step struct {
	res wire.RunWaitResult
	err error
}

// fakeObserver plays its script one Wait at a time, then blocks until ctx is
// cancelled; exhausted closes on the first Wait past the script.
type fakeObserver struct {
	mu        sync.Mutex
	script    []step
	calls     int
	exhausted chan struct{}
	once      sync.Once
	status    wire.RunStatus
	events    []wire.Event
	t         *testing.T
	taskIDs   []int64
}

func newObserver(script ...step) *fakeObserver {
	return &fakeObserver{script: script, exhausted: make(chan struct{})}
}

func (f *fakeObserver) Wait(ctx context.Context, ref string, _ client.CallOpts, _ wire.RunWaitParams) (client.Host, wire.RunWaitResult, error) {
	f.mu.Lock()
	f.calls++
	if len(f.script) == 0 {
		f.mu.Unlock()
		f.once.Do(func() { close(f.exhausted) })
		<-ctx.Done()
		return client.Host{}, wire.RunWaitResult{}, ctx.Err()
	}
	s := f.script[0]
	f.script = f.script[1:]
	f.mu.Unlock()
	return client.Host{HostID: ref, Alias: "box"}, s.res, s.err
}

func (f *fakeObserver) RunStatus(_ context.Context, _ string, _ client.CallOpts, runID string, taskID int64) (wire.RunStatus, error) {
	if runID != "" && taskID != 0 {
		f.t.Errorf("RunStatus got both run id %q and task id %d", runID, taskID)
	}
	return f.status, nil
}

func (f *fakeObserver) Activity(_ context.Context, _ string, _ client.CallOpts, taskID int64, _ string, _ int64, _ int) (wire.Activity, error) {
	f.mu.Lock()
	f.taskIDs = append(f.taskIDs, taskID)
	f.mu.Unlock()
	return wire.Activity{Events: f.events}, nil
}

type fakeNotifier struct {
	mu    sync.Mutex
	facts []runs.RemoteChildFacts
	fn    func(store.FleetWatch, runs.RemoteChildFacts) error
}

func (n *fakeNotifier) NotifyRemoteChild(w store.FleetWatch, f runs.RemoteChildFacts) error {
	n.mu.Lock()
	n.facts = append(n.facts, f)
	n.mu.Unlock()
	if n.fn != nil {
		return n.fn(w, f)
	}
	return nil
}

func (n *fakeNotifier) got() []runs.RemoteChildFacts {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]runs.RemoteChildFacts(nil), n.facts...)
}

type fakeHosts map[string]string

type errHosts struct{}

func (errHosts) Alias(string) (string, bool, error) { return "", false, errors.New("registry down") }

func (h fakeHosts) Alias(id string) (string, bool, error) {
	a, ok := h[id]
	return a, ok, nil
}

// recStore records the failure counts persisted through UpdateFleetWatch.
type recStore struct {
	*store.Store
	mu       sync.Mutex
	failures []int
}

func (r *recStore) UpdateFleetWatch(id int64, cursor string, failures int) error {
	r.mu.Lock()
	r.failures = append(r.failures, failures)
	r.mu.Unlock()
	return r.Store.UpdateFleetWatch(id, cursor, failures)
}

type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.d = append(s.d, d)
	s.mu.Unlock()
	return ctx.Err()
}

func (s *sleeps) got() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.d...)
}

type env struct {
	st    *recStore
	obs   *fakeObserver
	not   *fakeNotifier
	sl    *sleeps
	watch int64
	w     *Watcher
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newEnv(t *testing.T, notify bool, hosts Hosts, obs *fakeObserver) *env {
	t.Helper()
	obs.t = t
	e := &env{st: &recStore{Store: openStore(t)}, obs: obs, not: &fakeNotifier{}, sl: &sleeps{}}
	id, err := e.st.PutFleetRemoteTaskAndWatch(
		store.FleetRemoteTask{HostID: "host-0001-xyz", RemoteTaskID: remoteTask, Title: "remote work", State: "running"},
		&store.FleetWatch{HostID: "host-0001-xyz", RemoteTaskID: remoteTask, RemoteRunID: "rr-1", Notify: notify},
	)
	if err != nil {
		t.Fatal(err)
	}
	e.watch = id
	if hosts == nil {
		hosts = fakeHosts{"host-0001-xyz": "box"}
	}
	e.w = New(Options{
		Store: e.st, Observer: obs, Notifier: e.not, Hosts: hosts,
		BackoffMin: bmin, BackoffMax: bmax, PollMin: poll, FailureLimit: 3, Sleep: e.sl.sleep,
	})
	return e
}

func (e *env) idle() bool {
	e.w.mu.Lock()
	defer e.w.mu.Unlock()
	return len(e.w.running) == 0
}

// settle waits until the watch goroutine has left or the observer ran out of
// script (the watch is then polling forever), then stops the watcher.
func (e *env) settle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !e.idle() {
		select {
		case <-e.obs.exhausted:
			e.w.Stop()
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("watch did not settle")
		}
		time.Sleep(time.Millisecond)
	}
	e.w.Stop()
}

func (e *env) start(t *testing.T) {
	t.Helper()
	if err := e.w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func connectingErr(t *testing.T) error {
	t.Helper()
	c := client.New(client.Options{
		Resolve: func(ref string) (client.Host, error) { return client.Host{HostID: ref, Alias: "box"}, nil },
		Conn:    func(string) (client.Caller, client.LinkState) { return nil, client.LinkConnecting },
	})
	_, _, err := c.Wait(context.Background(), "h", client.CallOpts{}, wire.RunWaitParams{RunID: "rr-1"})
	if !client.IsConnecting(err) {
		t.Fatalf("setup: err = %v, want connecting", err)
	}
	return err
}

var (
	running = step{res: wire.RunWaitResult{State: "running", NextCursor: 3}}
	done    = step{res: wire.RunWaitResult{State: "done", Done: true, NextCursor: 9}}
	timeout = step{err: &client.Error{Code: client.CodeTimeout}}
)

func eqDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFleetWatchObservationBackoffAndConnectingNotCounted(t *testing.T) {
	t.Parallel()
	ce := connectingErr(t)
	e := newEnv(t, true, nil, newObserver(step{err: ce}, step{err: ce}, step{err: ce}, running, done))
	e.start(t)
	e.settle(t)

	if got := e.not.got(); len(got) != 1 || got[0].Kind != runs.RemoteFinished {
		t.Fatalf("notices = %+v, want one finished", got)
	}
	if want := []time.Duration{bmin, bmin, bmin, poll}; !eqDurations(e.sl.got(), want) {
		t.Fatalf("sleeps = %v, want %v", e.sl.got(), want)
	}
	for _, f := range e.st.failures {
		// Only cursor advances write here, and always with failures 0.
		if f != 0 {
			t.Fatalf("persisted failures = %v, want only zeros", e.st.failures)
		}
	}

	hu := &client.Error{Code: client.CodeHostUnavailable}
	e = newEnv(t, true, nil, newObserver(step{err: hu}, step{err: hu}, step{err: hu}, done))
	e.w.o.FailureLimit = 4
	e.start(t)
	e.settle(t)
	if want := []time.Duration{bmin, 2 * bmin, bmax}; !eqDurations(e.sl.got(), want) {
		t.Fatalf("sleeps = %v, want %v", e.sl.got(), want)
	}
}

func TestFleetWatchThreeFailuresStatusUnknownNeverFinished(t *testing.T) {
	t.Parallel()
	t.Run("three in a row", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, true, nil, newObserver(timeout, timeout, timeout))
		e.start(t)
		e.settle(t)
		got := e.not.got()
		if len(got) != 1 || got[0].Kind != runs.RemoteStatusUnknown || got[0].Failures != 3 {
			t.Fatalf("notices = %+v, want one status_unknown with 3 failures", got)
		}
		if want := []int{1, 2, 3}; len(e.st.failures) != 3 || e.st.failures[0] != want[0] || e.st.failures[1] != want[1] {
			t.Fatalf("persisted failures = %v, want 1,2 before the notice", e.st.failures)
		}
	})
	t.Run("success resets", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, true, nil, newObserver(timeout, timeout, running, timeout, timeout, done))
		e.start(t)
		e.settle(t)
		got := e.not.got()
		if len(got) != 1 || got[0].Kind != runs.RemoteFinished {
			t.Fatalf("notices = %+v, want one finished", got)
		}
	})
}

func TestFleetWatchAliasErrorsFallBackToHostIDPrefix(t *testing.T) {
	t.Parallel()
	e := newEnv(t, true, errHosts{}, newObserver())
	e.start(t)
	e.settle(t)
	got := e.not.got()
	if len(got) != 1 || got[0].Kind != runs.RemoteStatusUnknown || got[0].Host != "host-000" {
		t.Fatalf("notices = %+v, want one status_unknown from host-000", got)
	}
}

func TestFleetWatchHostRemovedUnavailable(t *testing.T) {
	t.Parallel()
	t.Run("alias lookup", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, true, fakeHosts{}, newObserver())
		e.start(t)
		e.settle(t)
		got := e.not.got()
		if len(got) != 1 || got[0].Kind != runs.RemoteHostUnavailable || got[0].Host != "host-000" || got[0].Title != "remote work" {
			t.Fatalf("notices = %+v", got)
		}
		if e.obs.calls != 0 {
			t.Fatalf("observer called %d times, want 0", e.obs.calls)
		}
	})
	t.Run("unknown_host from wait", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, true, nil, newObserver(step{err: &client.Error{Code: client.CodeUnknownHost}}))
		e.start(t)
		e.settle(t)
		got := e.not.got()
		if len(got) != 1 || got[0].Kind != runs.RemoteHostUnavailable {
			t.Fatalf("notices = %+v", got)
		}
	})
}

func TestFleetWatchTypedRefusalsEndWatch(t *testing.T) {
	t.Parallel()
	ng := step{err: &client.Error{Code: "not_granted"}}
	e := newEnv(t, true, nil, newObserver(ng, ng, ng))
	e.start(t)
	e.settle(t)
	got := e.not.got()
	if len(got) != 1 || got[0].Kind != runs.RemoteStatusUnknown || got[0].Failures != 3 {
		t.Fatalf("notices = %+v, want one status_unknown", got)
	}

	rl := step{err: &client.Error{Code: wire.CodeRateLimited}}
	e = newEnv(t, true, nil, newObserver(rl, rl, rl))
	e.start(t)
	e.settle(t)
	if got := e.not.got(); len(got) != 0 {
		t.Fatalf("rate limited notices = %+v, want none", got)
	}
	if want := []time.Duration{bmin, bmin, bmin}; !eqDurations(e.sl.got(), want) {
		t.Fatalf("sleeps = %v, want %v", e.sl.got(), want)
	}
}

func TestFleetWatchFinishDeliveredOnce(t *testing.T) {
	t.Parallel()
	obs := newObserver(step{res: wire.RunWaitResult{State: "done", Done: true, StopReason: "end_turn"}})
	obs.events = []wire.Event{{Type: "text", Text: "all good"}}
	e := newEnv(t, true, nil, obs)
	e.not.fn = func(w store.FleetWatch, _ runs.RemoteChildFacts) error {
		_, err := e.st.CloseFleetWatchIfOpen(w.ID, "finished")
		return err
	}
	e.start(t)
	e.settle(t)
	got := e.not.got()
	if len(got) != 1 || got[0].Kind != runs.RemoteFinished || got[0].State != "done" || got[0].Host != "box" ||
		got[0].LastText != "all good" || got[0].StopReason != "end_turn" {
		t.Fatalf("notices = %+v", got)
	}
	if len(obs.taskIDs) != 1 || obs.taskIDs[0] != remoteTaskID {
		t.Fatalf("activity task ids = %v, want [%d]", obs.taskIDs, remoteTaskID)
	}

	e.w.Add(e.watch)
	if err := e.w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.w.Stop()
	if got := e.not.got(); len(got) != 1 || e.obs.calls != 1 {
		t.Fatalf("after re-add: notices = %d, waits = %d, want 1 and 1", len(got), e.obs.calls)
	}
}

func TestFleetWatchLastTextIsTrailingTextEvents(t *testing.T) {
	t.Parallel()
	obs := newObserver(done)
	obs.events = []wire.Event{{Type: "text", Text: "a"}, {Type: "tool_call"}, {Type: "text", Text: "b"}, {Type: "text", Text: "c"}, {Type: "done"}}
	e := newEnv(t, true, nil, obs)
	e.start(t)
	e.settle(t)
	if got := e.not.got(); len(got) != 1 || got[0].LastText != "bc" {
		t.Fatalf("notices = %+v, want LastText bc", got)
	}
	if len(obs.taskIDs) != 1 || obs.taskIDs[0] != remoteTaskID {
		t.Fatalf("activity task ids = %v, want [%d]", obs.taskIDs, remoteTaskID)
	}
}

func TestFleetCallerRestartStillGetsFinishNotification(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/tmp/w", Title: "w"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := st.CreateChat(task.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	watchID, err := st.PutFleetRemoteTaskAndWatch(
		store.FleetRemoteTask{HostID: "h1", RemoteTaskID: remoteTask, LocalTaskID: task.ID, Title: "remote work"},
		&store.FleetWatch{HostID: "h1", RemoteTaskID: remoteTask, RemoteRunID: "rr-1", LocalTaskID: task.ID, ChatID: chat.ID, Notify: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	notifier := notifierFunc(func(w store.FleetWatch, f runs.RemoteChildFacts) error {
		_, err := st.EnqueueChildEventClosingWatch(store.ChildEventInput{
			ChatID: w.ChatID, Block: runs.BuildRemoteChildEventBlock(f), RunConfig: "{}",
			Now: time.Now(), MaxHold: time.Minute,
		}, w.ID, string(f.Kind))
		if errors.Is(err, store.ErrChildEventAlreadyNotified) {
			return nil
		}
		return err
	})
	mk := func(obs *fakeObserver) *Watcher {
		return New(Options{
			Store: st, Observer: obs, Notifier: notifier, Hosts: fakeHosts{"h1": "box"},
			BackoffMin: bmin, PollMin: poll, Sleep: (&sleeps{}).sleep,
		})
	}

	// Watcher #1 polls a run that never settles and is killed mid-wait.
	obs1 := newObserver(running)
	w1 := mk(obs1)
	if err := w1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-obs1.exhausted:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher 1 never reached its blocking wait")
	}
	w1.Stop()
	row, err := st.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != store.FleetWatchOpen || row.Cursor != "3" {
		t.Fatalf("row after kill = %+v, want open with cursor 3", row)
	}

	// Watcher #2 is the restarted daemon: it resumes and delivers.
	obs2 := newObserver(done)
	obs2.events = []wire.Event{{Type: "text", Text: "result"}}
	w2 := mk(obs2)
	if err := w2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, _ := st.GetFleetWatch(watchID); r.State == store.FleetWatchClosed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watch never closed")
		}
		time.Sleep(time.Millisecond)
	}
	w2.Stop()

	w3 := mk(newObserver(done))
	if err := w3.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	w3.Stop()

	items, err := st.ListChatQueue(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	var events []store.ChatQueueItem
	for _, it := range items {
		if it.Source == store.ChatQueueSourceChildEvent {
			events = append(events, it)
		}
	}
	if len(events) != 1 || !strings.Contains(events[0].Prompt, "box:"+remoteTask) || !strings.Contains(events[0].Prompt, `untrusted="true"`) {
		t.Fatalf("child events = %+v, want exactly one with the alias and untrusted text", events)
	}
}

type notifierFunc func(store.FleetWatch, runs.RemoteChildFacts) error

func (f notifierFunc) NotifyRemoteChild(w store.FleetWatch, r runs.RemoteChildFacts) error {
	return f(w, r)
}
