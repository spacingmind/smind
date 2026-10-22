package gitstate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

type fakeSource struct {
	mu    sync.Mutex
	tasks map[int64]store.Task
}

func newFakeSource(ts ...store.Task) *fakeSource {
	f := &fakeSource{tasks: map[int64]store.Task{}}
	for _, t := range ts {
		f.tasks[t.ID] = t
	}
	return f
}

func (f *fakeSource) set(t store.Task) {
	f.mu.Lock()
	f.tasks[t.ID] = t
	f.mu.Unlock()
}

func (f *fakeSource) GetTask(id int64) (store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return store.Task{}, errors.New("no task")
	}
	return t, nil
}

func (f *fakeSource) GetWorkspace(id int64) (store.Workspace, error) {
	return store.Workspace{ID: id, Path: "/repo", DefaultBase: "develop"}, nil
}

func (f *fakeSource) ListTasks(ws int64) ([]store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Task
	for id := int64(1); id < 100; id++ {
		if t, ok := f.tasks[id]; ok && t.WorkspaceID == ws && t.Status != "archived" {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeSource) TaskDiffStat(int64) (int, int, int, error) { return 0, 0, 0, errors.New("nope") }

func task(id int64, sha string) store.Task {
	return store.Task{ID: id, WorkspaceID: 1, Status: "running", BaseRef: "develop", BaseSha: sha}
}

// countRunner counts git invocations by subcommand. Every call fails (so
// Compute yields only the task-derived fields) except where hook says
// otherwise.
type countRunner struct {
	mu     sync.Mutex
	counts map[string]int
	hook   func(ctx context.Context, args []string) // may block
}

func (r *countRunner) run(ctx context.Context, _ string, args ...string) (string, error) {
	r.mu.Lock()
	if r.counts == nil {
		r.counts = map[string]int{}
	}
	r.counts[args[0]]++
	h := r.hook
	r.mu.Unlock()
	if h != nil {
		h(ctx, args)
	}
	return "", errors.New("fake")
}

func (r *countRunner) count(sub string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[sub]
}

func (r *countRunner) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.counts {
		n += c
	}
	return n
}

type fakeInterest struct {
	mu      sync.Mutex
	watched bool
	focused []int64
}

func (i *fakeInterest) Watched() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.watched
}

func (i *fakeInterest) Focused() []int64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]int64(nil), i.focused...)
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

type events struct {
	mu  sync.Mutex
	got []TaskGit
}

func (e *events) publish(_ int64, g TaskGit) {
	e.mu.Lock()
	e.got = append(e.got, g)
	e.mu.Unlock()
}

func (e *events) n() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.got)
}

func (e *events) last() TaskGit {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.got[len(e.got)-1]
}

// fakeTimers is an After that never fires on its own.
type fakeTimers struct {
	mu  sync.Mutex
	fns []func()
}

func (f *fakeTimers) after(_ time.Duration, fn func()) func() {
	f.mu.Lock()
	f.fns = append(f.fns, fn)
	f.mu.Unlock()
	return func() {}
}

func (f *fakeTimers) fireAll() {
	f.mu.Lock()
	fns := f.fns
	f.fns = nil
	f.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (f *fakeTimers) pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fns)
}

func newSvc(t *testing.T, src Source, r *countRunner, in Interest, clk *fakeClock, mod func(*Options)) *Service {
	t.Helper()
	o := Options{Source: src, Runner: r.run, Now: clk.Now, Interest: in, FetchEvery: time.Minute, FetchFloor: time.Minute}
	if mod != nil {
		mod(&o)
	}
	s := New(o)
	t.Cleanup(s.Stop)
	return s
}

func TestFetch_FloorAndSingleFlight_CountAsserting(t *testing.T) {
	src := newFakeSource(task(1, "a"))
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	r := &countRunner{}
	r.hook = func(ctx context.Context, args []string) {
		if args[0] == "fetch" {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}
	clk := newClock()
	s := newSvc(t, src, r, &fakeInterest{watched: true}, clk, nil)
	if _, err := s.Get(1); err != nil {
		t.Fatal(err)
	}

	s.Tick()
	<-started
	if got := r.count("fetch"); got != 1 {
		t.Fatalf("fetch after first tick = %d, want 1", got)
	}
	clk.Advance(61 * time.Second)
	s.Tick() // due, but the first fetch is still in flight: single-flight
	if got := r.count("fetch"); got != 1 {
		t.Fatalf("fetch while one is in flight = %d, want 1", got)
	}
	close(release)
	s.Wait()

	s.Tick() // 61 s after the first: due again
	s.Wait()
	if got := r.count("fetch"); got != 2 {
		t.Fatalf("fetch after floor elapsed = %d, want 2", got)
	}
	clk.Advance(10 * time.Second)
	s.Tick()
	s.Wait()
	if got := r.count("fetch"); got != 2 {
		t.Fatalf("fetch inside the floor = %d, want 2", got)
	}
}

func TestFetch_NoSubscribersNoCalls(t *testing.T) {
	src := newFakeSource(task(1, "a"))
	r := &countRunner{}
	in := &fakeInterest{focused: []int64{1}}
	clk := newClock()
	s := newSvc(t, src, r, in, clk, nil)
	if _, err := s.Get(1); err != nil {
		t.Fatal(err)
	}
	before := r.total()
	for range 3 {
		clk.Advance(5 * time.Minute)
		s.Tick()
	}
	s.Wait()
	if r.total() != before || r.count("fetch") != 0 {
		t.Fatalf("calls with no subscriber: total %d -> %d, fetch %d", before, r.total(), r.count("fetch"))
	}
}

func TestFetch_HungRemoteDoesNotBlockRead(t *testing.T) {
	src := newFakeSource(task(1, "a"), task(2, "b"))
	started := make(chan struct{}, 1)
	r := &countRunner{}
	r.hook = func(ctx context.Context, args []string) {
		if args[0] == "fetch" {
			started <- struct{}{}
			<-ctx.Done() // a remote that never answers
		}
	}
	s := newSvc(t, src, r, &fakeInterest{watched: true}, newClock(), nil)
	if _, err := s.Get(1); err != nil {
		t.Fatal(err)
	}
	s.Tick()
	<-started

	done := make(chan error, 1)
	go func() {
		if _, err := s.Get(1); err != nil {
			done <- err
			return
		}
		_, err := s.Get(2) // uncached: computes local-only while the fetch hangs
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read blocked behind a hung fetch")
	}
	s.Stop() // cancels the hung fetch and joins its goroutine
}

func TestTaskGitEvent_PublishedOnChangeOnly(t *testing.T) {
	src := newFakeSource(task(1, "a"))
	r := &countRunner{}
	ev := &events{}
	clk := newClock()
	s := newSvc(t, src, r, &fakeInterest{watched: true}, clk, func(o *Options) { o.Publisher = ev.publish })

	s.InvalidateSync(1)
	if ev.n() != 1 {
		t.Fatalf("first compute published %d events, want 1", ev.n())
	}
	for range 3 {
		clk.Advance(2 * time.Second) // computedAt moves, content does not
		s.InvalidateSync(1)
	}
	if ev.n() != 1 {
		t.Fatalf("unchanged recomputes published %d events, want 1", ev.n())
	}
	src.set(task(1, "b"))
	clk.Advance(2 * time.Second)
	s.InvalidateSync(1)
	if ev.n() != 2 || ev.last().Base.Sha != "b" {
		t.Fatalf("changed state: %d events, last %+v", ev.n(), ev.last().Base)
	}
}

func TestTaskGitEvent_CoalescedPerSecond(t *testing.T) {
	src := newFakeSource(task(1, "a"))
	r := &countRunner{}
	ev := &events{}
	tm := &fakeTimers{}
	clk := newClock()
	s := newSvc(t, src, r, &fakeInterest{watched: true}, clk, func(o *Options) {
		o.Publisher = ev.publish
		o.After = tm.after
	})

	s.InvalidateSync(1) // event 1
	for _, sha := range []string{"b", "c", "d"} {
		clk.Advance(100 * time.Millisecond)
		src.set(task(1, sha))
		s.InvalidateSync(1)
	}
	if ev.n() != 1 {
		t.Fatalf("events inside one second = %d, want 1", ev.n())
	}
	if tm.pending() != 1 {
		t.Fatalf("trailing timers = %d, want exactly 1", tm.pending())
	}
	clk.Advance(time.Second)
	tm.fireAll()
	if ev.n() != 2 || ev.last().Base.Sha != "d" {
		t.Fatalf("after trailing flush: %d events, last sha %q (want 2, d)", ev.n(), ev.last().Base.Sha)
	}

	// A change that is undone inside the window is not news.
	src.set(task(1, "e"))
	s.InvalidateSync(1)
	src.set(task(1, "d"))
	s.InvalidateSync(1)
	clk.Advance(time.Second)
	tm.fireAll()
	if ev.n() != 2 {
		t.Fatalf("flapped-back state published: %d events, want 2", ev.n())
	}
}

func TestTaskGitList_ReturnsAllWorkspaceTasks(t *testing.T) {
	sys := task(3, "s")
	sys.Kind = store.TaskKindSystem
	arch := task(4, "x")
	arch.Status = "archived"
	other := task(5, "o")
	other.WorkspaceID = 2
	src := newFakeSource(task(1, "a"), task(2, "b"), sys, arch, other)
	s := newSvc(t, src, &countRunner{}, &fakeInterest{}, newClock(), nil)

	got, err := s.List(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TaskID != 1 || got[1].TaskID != 2 {
		t.Fatalf("List = %+v, want tasks 1 and 2 only", got)
	}
	if _, err := s.Get(3); !errors.Is(err, ErrNotTracked) {
		t.Fatalf("system task Get err = %v, want ErrNotTracked", err)
	}
	if _, err := s.Get(4); !errors.Is(err, ErrNotTracked) {
		t.Fatalf("archived task Get err = %v, want ErrNotTracked", err)
	}
}

func TestTaskGit_UntrustedFlag(t *testing.T) {
	untrusted := task(1, "a")
	untrusted.UntrustedSource = true
	s := newSvc(t, newFakeSource(untrusted, task(2, "b")), &countRunner{}, &fakeInterest{}, newClock(), nil)

	g, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Untrusted {
		t.Fatal("TaskGit.untrusted = false for a task with untrusted_source set")
	}
	if g, err = s.Get(2); err != nil || g.Untrusted {
		t.Fatalf("trusted task: untrusted = %v, err = %v, want false", g.Untrusted, err)
	}
	list, err := s.List(1)
	if err != nil || len(list) != 2 || !list[0].Untrusted || list[1].Untrusted {
		t.Fatalf("List untrusted flags = %+v (err %v), want [true false]", list, err)
	}
}
