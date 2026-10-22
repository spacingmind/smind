package gitstate

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
)

// Cadences (ADR-0039 §4). Local status of focused tasks every TickEvery;
// `git fetch origin <base>` per workspace every FetchEvery, never faster than
// FetchFloor; at most one task.git event per task per publishGap.
const (
	TickEvery  = 30 * time.Second
	FetchEvery = 180 * time.Second
	FetchFloor = 60 * time.Second
	publishGap = time.Second
)

// ErrNotTracked is returned for a task gitstate does not compute: archived,
// system, or gone.
var ErrNotTracked = errors.New("gitstate: task is archived, system or missing")

// Source is what the service needs from the workspace layer; wsapi adapts
// *workspace.Manager to it (gitstate must not import internal/workspace).
type Source interface {
	GetTask(id int64) (store.Task, error)
	GetWorkspace(id int64) (store.Workspace, error)
	// ListTasks returns the workspace's non-archived tasks.
	ListTasks(workspaceID int64) ([]store.Task, error)
	TaskDiffStat(id int64) (files, insertions, deletions int, err error)
}

// Interest tells the service whether anyone is looking. With no watcher the
// loop neither recomputes nor fetches (ADR §4 hard rule 1).
type Interest interface {
	// Watched reports whether any connection is subscribed to task.git.
	Watched() bool
	// Focused returns the task ids connections currently focus.
	Focused() []int64
}

// Options configures a Service. Zero values take production defaults except
// Source and Runner, which are required.
type Options struct {
	Source    Source
	Runner    Runner
	Now       func() time.Time
	Publisher func(taskID int64, g TaskGit)
	Interest  Interest
	// Tick, when non-nil, replaces the 30 s ticker (tests drive it).
	Tick <-chan time.Time
	// After schedules f once after d and returns its cancel func; default
	// time.AfterFunc. Tests substitute a fake to hold the clock still.
	After        func(d time.Duration, f func()) (stop func())
	FetchEvery   time.Duration
	FetchFloor   time.Duration
	FetchTimeout time.Duration

	// Forge and Ledger turn on the PR poller (ADR-0039 §4); without both the
	// service reports no PR state. Forge returns the current provider (nil =
	// none), since wsapi lets tests swap it.
	Forge  func() forge.Provider
	Ledger Ledger
	// PollTick, when non-nil, replaces the poller's 5 s ticker.
	PollTick <-chan time.Time
	// Jitter returns [-1, 1] for the poller's ±10 % spread; default random.
	Jitter func() float64
	// OnAdopt is called after the poller adopts a PR for a task (wsapi
	// publishes task.updated).
	OnAdopt func(taskID int64)
}

type entry struct {
	g       TaskGit
	key     string // canonical key of g
	pubKey  string // canonical key of the last published snapshot
	pubAt   time.Time
	pending bool // g differs from the published one, publish deferred
	stop    func()
	ws      int64
	base    string
}

type flight struct{ again bool }

// Service keeps the in-memory TaskGit snapshot per task and recomputes it on
// invalidation and on the interest-gated loop. Reads never run network git.
type Service struct {
	opts   Options
	now    func() time.Time
	after  func(time.Duration, func()) func()
	run    Runner
	ctx    context.Context
	cancel context.CancelFunc

	conflicts *ConflictCache
	poller    *Poller // nil without Options.Forge/Ledger

	mu         sync.Mutex
	closed     bool
	cache      map[int64]*entry
	flights    map[int64]*flight
	fetching   map[int64]bool
	lastFetch  map[int64]time.Time
	wg         sync.WaitGroup
	loopOnce   sync.Once
	stopOnce   sync.Once
	loopCancel context.CancelFunc
}

// New builds a stopped Service; call Start to run the loop.
func New(o Options) *Service {
	s := &Service{
		opts: o, now: o.Now, after: o.After, run: o.Runner,
		conflicts: NewConflictCache(),
		cache:     map[int64]*entry{}, flights: map[int64]*flight{},
		fetching: map[int64]bool{}, lastFetch: map[int64]time.Time{},
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.run == nil {
		s.run = DefaultRunner
	}
	if s.after == nil {
		s.after = func(d time.Duration, f func()) func() {
			t := time.AfterFunc(d, f)
			return func() { t.Stop() }
		}
	}
	if s.opts.FetchEvery <= 0 {
		s.opts.FetchEvery = FetchEvery
	}
	if s.opts.FetchFloor <= 0 {
		s.opts.FetchFloor = FetchFloor
	}
	if s.opts.FetchTimeout <= 0 {
		s.opts.FetchTimeout = NetworkTimeout
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	if o.Forge != nil && o.Ledger != nil && o.Source != nil {
		s.poller = NewPoller(PollerOptions{
			Source: o.Source, Interest: o.Interest, Runner: o.Runner,
			Provider: o.Forge, Ledger: o.Ledger, Now: o.Now, Jitter: o.Jitter,
			Tasks:    s.cachedIDs,
			OnChange: s.Invalidate,
			OnAdopt:  o.OnAdopt,
		})
	}
	return s
}

// cachedIDs lists the tasks someone has read: the poller's watched set.
func (s *Service) cachedIDs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int64, 0, len(s.cache))
	for id := range s.cache {
		ids = append(ids, id)
	}
	return ids
}

// Start runs the local-status loop and, when configured, the PR poller loop
// until ctx is done or Stop is called. Idempotent.
func (s *Service) Start(ctx context.Context) {
	s.loopOnce.Do(func() {
		s.startLoop(ctx, s.opts.Tick, TickEvery, s.Tick)
		if s.poller != nil {
			s.startLoop(ctx, s.opts.PollTick, PollEvery, s.poller.Tick)
		}
	})
}

func (s *Service) startLoop(ctx context.Context, tick <-chan time.Time, every time.Duration, beat func()) {
	var ticker *time.Ticker
	if tick == nil {
		ticker = time.NewTicker(every)
		tick = ticker.C
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		if ticker != nil {
			ticker.Stop()
		}
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		if ticker != nil {
			defer ticker.Stop()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.ctx.Done():
				return
			case <-tick:
				beat()
			}
		}
	}()
}

// Stop (like Drop and Invalidate) is a no-op on a nil Service. It cancels in-flight git, stops timers and waits for every goroutine.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		for _, e := range s.cache {
			if e.stop != nil {
				e.stop()
			}
		}
		s.mu.Unlock()
		s.poller.Stop()
		s.cancel()
		s.wg.Wait()
	})
}

// Wait blocks until every background recompute and fetch has finished (tests).
func (s *Service) Wait() { s.wg.Wait() }

// Get returns the cached snapshot, or computes it once, local-only.
func (s *Service) Get(taskID int64) (TaskGit, error) {
	s.mu.Lock()
	if e := s.cache[taskID]; e != nil {
		g := e.g
		s.mu.Unlock()
		return g, nil
	}
	s.mu.Unlock()
	g, ws, base, err := s.compute(taskID)
	if err != nil {
		return TaskGit{}, err
	}
	key := canonicalKey(g)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.cache[taskID]; e != nil {
		return e.g, nil // someone else filled it meanwhile
	}
	if s.closed {
		return g, nil
	}
	// The reader has now seen this state, so an identical recompute is not news.
	s.cache[taskID] = &entry{g: g, key: key, pubKey: key, pubAt: s.now(), ws: ws, base: base}
	return g, nil
}

// List returns the snapshot of every non-archived, non-system task of the
// workspace, in id order.
func (s *Service) List(workspaceID int64) ([]TaskGit, error) {
	tasks, err := s.opts.Source.ListTasks(workspaceID)
	if err != nil {
		return nil, err
	}
	// Uncached tasks cost several local git calls each (one of them builds a
	// snapshot index), so a cold workspace is computed by a few workers, like
	// the old task.stats, with results kept in id order.
	var ids []int64
	for _, t := range tasks {
		if t.Kind != store.TaskKindSystem {
			ids = append(ids, t.ID)
		}
	}
	res := make([]*TaskGit, len(ids))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range listWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if g, err := s.Get(ids[i]); err == nil {
					res[i] = &g
				}
			}
		}()
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	out := make([]TaskGit, 0, len(ids))
	for _, g := range res {
		if g != nil {
			out = append(out, *g)
		}
	}
	return out, nil
}

// listWorkers bounds the concurrent cold computations in List.
const listWorkers = 4

// Drop forgets a task's snapshot (archive/delete).
func (s *Service) Drop(taskID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.dropLocked(taskID)
	s.mu.Unlock()
	s.poller.Forget(taskID)
}

func (s *Service) dropLocked(taskID int64) {
	if e := s.cache[taskID]; e != nil && e.stop != nil {
		e.stop()
	}
	delete(s.cache, taskID)
}

// Invalidate schedules a recompute of taskID and returns immediately (it is
// called from notifier hooks that must not block). Concurrent invalidations of
// one task coalesce into at most one trailing recompute. A task nobody has
// read and nobody watches is not computed.
func (s *Service) Invalidate(taskID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	_, cached := s.cache[taskID]
	if !cached && (s.opts.Interest == nil || !s.opts.Interest.Watched()) {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		s.InvalidateSync(taskID)
	}()
}

// InvalidateSync is Invalidate run in the caller's goroutine (single-flight
// per task: a call that finds one running marks it to run once more and returns).
func (s *Service) InvalidateSync(taskID int64) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if f := s.flights[taskID]; f != nil {
		f.again = true
		s.mu.Unlock()
		return
	}
	f := &flight{}
	s.flights[taskID] = f
	s.mu.Unlock()
	for {
		s.recompute(taskID)
		s.mu.Lock()
		if f.again && !s.closed {
			f.again = false
			s.mu.Unlock()
			continue
		}
		delete(s.flights, taskID)
		s.mu.Unlock()
		return
	}
}

func (s *Service) recompute(taskID int64) {
	g, ws, base, err := s.compute(taskID)
	if err != nil {
		s.Drop(taskID) // archived, system or deleted
		return
	}
	key := canonicalKey(g)
	now := s.now()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	e := s.cache[taskID]
	if e == nil {
		e = &entry{}
		s.cache[taskID] = e
	}
	e.g, e.key, e.ws, e.base = g, key, ws, base
	if key == e.pubKey {
		e.pending = false
		s.mu.Unlock()
		return
	}
	if e.pubAt.IsZero() || now.Sub(e.pubAt) >= publishGap {
		e.pubKey, e.pubAt, e.pending = key, now, false
		s.mu.Unlock()
		s.publish(taskID, g)
		return
	}
	if !e.pending {
		e.pending = true
		e.stop = s.after(publishGap-now.Sub(e.pubAt), func() { s.flush(taskID) })
	}
	s.mu.Unlock()
}

// flush publishes the latest snapshot of a task whose event was held back by
// the per-second limit.
func (s *Service) flush(taskID int64) {
	s.mu.Lock()
	e := s.cache[taskID]
	if s.closed || e == nil || !e.pending {
		s.mu.Unlock()
		return
	}
	e.pending = false
	if e.key == e.pubKey {
		s.mu.Unlock()
		return
	}
	e.pubKey, e.pubAt = e.key, s.now()
	g := e.g
	s.mu.Unlock()
	s.publish(taskID, g)
}

func (s *Service) publish(taskID int64, g TaskGit) {
	if s.opts.Publisher != nil {
		s.opts.Publisher(taskID, g)
	}
}

// compute builds the local snapshot of one task; it never touches the network.
func (s *Service) compute(taskID int64) (g TaskGit, ws int64, base string, err error) {
	src := s.opts.Source
	t, err := src.GetTask(taskID)
	if err != nil || t.Kind == store.TaskKindSystem || t.Status == "archived" {
		return TaskGit{}, 0, "", ErrNotTracked
	}
	w, err := src.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return TaskGit{}, 0, "", ErrNotTracked
	}
	in := Input{
		TaskID: t.ID, RepoDir: w.Path, BaseRef: t.BaseRef, BaseSha: t.BaseSha,
		BranchOwned: t.BranchOwned, Conflicts: s.conflicts, Now: s.now,
		DiffStat: func() (int, int, int, error) { return src.TaskDiffStat(taskID) },
	}
	if in.BaseRef == "" {
		in.BaseRef = w.DefaultBase
	}
	if t.WorktreePath != nil {
		in.WorktreePath = *t.WorktreePath
	}
	if t.Branch != nil {
		in.Branch = *t.Branch
	}
	if t.PRNumber > 0 {
		in.Forge = t.PRForge
		in.PR = s.poller.PR(t) // cached snapshot only; never calls the forge
	}
	return Compute(s.ctx, s.run, in), t.WorkspaceID, in.BaseRef, nil
}

// Refresh is task.gitRefresh: one forced poll of the task's workspace under
// the explicit-refresh floor and the host budget, then the recomputed
// snapshot. A forge failure is returned only when the task has no cached PR
// snapshot to show; otherwise the snapshot carries its stale flag. Over its
// floor or hourly cap it returns *RefreshLimitedError.
func (s *Service) Refresh(taskID int64) (TaskGit, error) {
	t, err := s.opts.Source.GetTask(taskID)
	if err != nil || t.Kind == store.TaskKindSystem || t.Status == "archived" {
		return TaskGit{}, ErrNotTracked
	}
	if s.poller != nil {
		if err := s.poller.Refresh(taskID); err != nil {
			var lim *RefreshLimitedError
			if errors.As(err, &lim) || s.poller.PR(t) == nil {
				return TaskGit{}, err
			}
		}
	}
	s.InvalidateSync(taskID)
	return s.Get(taskID)
}

// Tick is one loop beat: nothing without a watcher; otherwise recompute the
// focused tasks (local git) and start any due per-workspace fetch. It never
// waits on the network.
func (s *Service) Tick() {
	in := s.opts.Interest
	if in == nil || !in.Watched() {
		return
	}
	for _, id := range in.Focused() {
		s.InvalidateSync(id)
	}
	s.fetchDue(in.Focused())
}

// fetchDue starts a background `git fetch origin <base>` for each workspace
// with a watched task whose last fetch is older than the interval.
func (s *Service) fetchDue(focused []int64) {
	type target struct {
		ws    int64
		bases map[string]bool
	}
	targets := map[int64]*target{}
	add := func(ws int64, base string) {
		if base == "" {
			return
		}
		tg := targets[ws]
		if tg == nil {
			tg = &target{ws: ws, bases: map[string]bool{}}
			targets[ws] = tg
		}
		tg.bases[base] = true
	}
	s.mu.Lock()
	for _, e := range s.cache {
		add(e.ws, e.base)
	}
	s.mu.Unlock()
	for _, id := range focused {
		s.mu.Lock()
		e := s.cache[id]
		s.mu.Unlock()
		if e != nil {
			add(e.ws, e.base)
		}
	}
	every := max(s.opts.FetchEvery, s.opts.FetchFloor)
	now := s.now()
	for ws, tg := range targets {
		s.mu.Lock()
		if s.closed || s.fetching[ws] || (!s.lastFetch[ws].IsZero() && now.Sub(s.lastFetch[ws]) < every) {
			s.mu.Unlock()
			continue
		}
		s.fetching[ws] = true
		s.lastFetch[ws] = now // failures respect the floor too
		s.wg.Add(1)
		s.mu.Unlock()
		bases := make([]string, 0, len(tg.bases))
		for b := range tg.bases {
			bases = append(bases, b)
		}
		sort.Strings(bases)
		go s.fetch(ws, bases)
	}
}

func (s *Service) fetch(ws int64, bases []string) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.fetching, ws)
		s.mu.Unlock()
	}()
	w, err := s.opts.Source.GetWorkspace(ws)
	if err != nil {
		return
	}
	for _, b := range bases {
		ctx, cancel := context.WithTimeout(s.ctx, s.opts.FetchTimeout)
		_, _ = s.run(ctx, w.Path, "fetch", "--no-tags", originRemote, "+refs/heads/"+b+":refs/remotes/origin/"+b)
		cancel()
	}
	if s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	var ids []int64
	for id, e := range s.cache {
		if e.ws == ws {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.InvalidateSync(id)
	}
}

// canonicalKey is the snapshot's JSON without the timestamps that change on
// every recompute (computedAt, conflict.checkedAt, pr.fetchedAt, pr.refreshAvailableAt); equal keys = no news.
func canonicalKey(g TaskGit) string {
	g.ComputedAt = time.Time{}
	if g.PR != nil {
		pr := *g.PR
		pr.FetchedAt, pr.RefreshAvailableAt = time.Time{}, ""
		g.PR = &pr
	}
	if g.Conflict != nil {
		c := *g.Conflict
		c.CheckedAt = time.Time{}
		g.Conflict = &c
	}
	b, err := json.Marshal(g)
	if err != nil {
		return ""
	}
	return string(b)
}
