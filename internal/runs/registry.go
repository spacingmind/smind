package runs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// finishedRetentionCap bounds how many finished runs the Registry keeps in
// memory: once more than this many runs have finished, the oldest
// finished ones (by finish order) are evicted -- their entries removed
// from the registry entirely, so History/Subscribe/Stop on an evicted ID
// return ErrNotFound.
//
// This is the Registry's only memory bound, deliberately: still-running
// runs and their history are never truncated (a run's own output is never
// silently incomplete), and a real daemon's active-run count is bounded by
// how many turns are actually in flight at once, not by uptime. Only the
// *retention* of finished runs' history grows with uptime, and 200 finished
// runs' worth of chunk history is a small, fixed amount of memory -- ample
// for the "did my recent run finish, what did it say" use case run.list
// and run.logs exist for, without needing real eviction-by-age or
// persistence for this pass.
const finishedRetentionCap = 200

// Registry owns the lifetime of every Run started through it: Start
// launches a Run's background goroutine (outliving whatever request
// called Start), and Subscribe/History/Stop/List let any caller -- on any
// connection, at any later time -- observe or stop it by ID.
type Registry struct {
	mu   sync.Mutex
	runs map[string]*run

	// finishedOrder records finished run IDs in finish order, so eviction
	// (see finishedRetentionCap) always drops the oldest first.
	finishedOrder []string
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{runs: make(map[string]*run)}
}

// run is a Registry's internal bookkeeping for one Run: identity fields
// are immutable after Start, everything else is guarded by mu.
type run struct {
	id        string
	taskID    int64
	provider  taskrunner.Provider
	prompt    string
	startedAt time.Time
	cancel    context.CancelFunc

	mu            sync.Mutex
	status        Status
	finishedAt    *time.Time
	stopReason    string
	errMsg        string
	stopRequested bool

	history     []Event
	subscribers map[int]*subQueue
	nextSubID   int
}

func (r *run) statusLocked() RunStatus {
	return RunStatus{
		ID:         r.id,
		TaskID:     r.taskID,
		Provider:   r.provider,
		Prompt:     r.prompt,
		Status:     r.status,
		StartedAt:  r.startedAt,
		FinishedAt: r.finishedAt,
		StopReason: r.stopReason,
		Err:        r.errMsg,
	}
}

// Start allocates a Run for a taskID/provider/prompt turn and drives it to
// completion in a background goroutine owned by the Registry, not by the
// caller: ctx is the run's own background context (Stop cancels it; it is
// not, and must not be, a request's own context, since the whole point is
// that the run outlives whatever request started it). Start itself returns
// as soon as the Run is registered, with the new run's ID -- it does not
// wait for the turn to finish.
//
// wm is used only to fail fast on an unknown taskID before committing to a
// background goroutine; runner.RunPrompt performs the same lookup itself,
// so this is a redundant, cheap check purely for a synchronous error
// return instead of one only surfacing asynchronously.
func (reg *Registry) Start(ctx context.Context, wm *workspace.Manager, runner *taskrunner.Runner, taskID int64, provider taskrunner.Provider, prompt string) (string, error) {
	if _, err := wm.GetTask(taskID); err != nil {
		return "", fmt.Errorf("runs: start: %w", err)
	}

	id, err := newRunID()
	if err != nil {
		return "", fmt.Errorf("runs: start: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	r := &run{
		id:          id,
		taskID:      taskID,
		provider:    provider,
		prompt:      prompt,
		startedAt:   time.Now(),
		cancel:      cancel,
		status:      StatusRunning,
		subscribers: make(map[int]*subQueue),
	}

	reg.mu.Lock()
	reg.runs[id] = r
	reg.mu.Unlock()

	go reg.drive(runCtx, r, runner)

	return id, nil
}

func (reg *Registry) drive(ctx context.Context, r *run, runner *taskrunner.Runner) {
	events := make(chan taskrunner.Event)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for e := range events {
			reg.record(r, e)
		}
	}()

	err := runner.RunPrompt(ctx, r.taskID, r.provider, r.prompt, events)
	<-forwardDone
	reg.finish(r, err)
}

// record appends e to r's history and broadcasts it to every subscriber
// current at the time of the append. It's only ever called serially, in
// finish order, by the one drive goroutine for r -- so subscribers always
// see a single, globally consistent event order with no interleaving to
// worry about here (the harder ordering question, backfill racing a new
// subscriber joining, is Subscribe's, addressed in its own doc comment).
func (reg *Registry) record(r *run, e Event) {
	r.mu.Lock()
	r.history = append(r.history, e)
	if e.Type == taskrunner.EventTypeDone {
		r.stopReason = e.StopReason
	}
	subs := make([]*subQueue, 0, len(r.subscribers))
	for _, q := range r.subscribers {
		subs = append(subs, q)
	}
	r.mu.Unlock()

	for _, q := range subs {
		q.push(e)
	}
}

func (reg *Registry) finish(r *run, err error) {
	r.mu.Lock()
	now := time.Now()
	r.finishedAt = &now
	switch {
	case r.stopRequested:
		r.status = StatusStopped
	case err != nil:
		r.status = StatusError
		r.errMsg = err.Error()
	default:
		r.status = StatusDone
	}
	subs := make([]*subQueue, 0, len(r.subscribers))
	for _, q := range r.subscribers {
		subs = append(subs, q)
	}
	r.subscribers = make(map[int]*subQueue)
	r.mu.Unlock()

	// Close every subscriber still attached: their relay goroutines drain
	// whatever's already queued (nothing is lost) and then close their out
	// channel, which is how Subscribe's caller learns the run went
	// terminal without polling.
	for _, q := range subs {
		q.closeQueue()
	}

	reg.retain(r.id)
}

func (reg *Registry) retain(id string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.finishedOrder = append(reg.finishedOrder, id)
	for len(reg.finishedOrder) > finishedRetentionCap {
		evict := reg.finishedOrder[0]
		reg.finishedOrder = reg.finishedOrder[1:]
		delete(reg.runs, evict)
	}
}

func (reg *Registry) get(runID string) (*run, error) {
	reg.mu.Lock()
	r, ok := reg.runs[runID]
	reg.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

// Subscribe returns runID's event history so far, followed by its
// remaining live events as they arrive, with no gap and no duplicate
// between the two -- and unsubscribe, which detaches without stopping the
// run (see Registry's doc comment). events closes once runID reaches a
// terminal state, whether that happens before or after this call.
//
// The race this has to get right: between "read the current history" and
// "start receiving future broadcasts", a broadcast landing in that gap
// must appear exactly once in what the caller sees, not zero times (a
// gap) and not twice (once in the stale backfill snapshot, once live).
// The fix is holding r.mu across both steps -- copying (in fact, pushing)
// every already-recorded event into this subscriber's queue, and only
// then registering that queue to receive future ones -- for the entire
// duration of that hold. record (the only writer of new events) also
// takes r.mu before it can append to history or reach the subscriber map,
// so it cannot interleave with this critical section: either a given
// event is already in history when Subscribe reads it (delivered via the
// backfill push, once) or it isn't yet, in which case Subscribe's queue is
// registered before record gets a chance to add it, so it's delivered
// live (also once). The backfill push itself happens synchronously inside
// this critical section (subQueue.push never blocks), so it can't be
// preempted by a live push landing in between.
func (reg *Registry) Subscribe(runID string) (<-chan Event, func(), error) {
	r, err := reg.get(runID)
	if err != nil {
		return nil, nil, err
	}

	q := newSubQueue()

	r.mu.Lock()
	for _, e := range r.history {
		q.push(e)
	}
	live := r.status == StatusRunning
	var id int
	if live {
		id = r.nextSubID
		r.nextSubID++
		r.subscribers[id] = q
	}
	r.mu.Unlock()

	if !live {
		// Already terminal: there will never be a live tail, so the
		// backfill just pushed is the whole story.
		q.closeQueue()
	}

	out := make(chan Event)
	done := make(chan struct{})
	go relay(q, out, done)

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			if live {
				r.mu.Lock()
				delete(r.subscribers, id)
				r.mu.Unlock()
			}
			q.closeQueue()
			close(done)
		})
	}
	return out, unsubscribe, nil
}

// History returns runID's full recorded event history and current status
// snapshot, without blocking on the run reaching any particular state --
// it works identically whether the run is still going or already
// finished.
func (reg *Registry) History(runID string) ([]Event, RunStatus, error) {
	r, err := reg.get(runID)
	if err != nil {
		return nil, RunStatus{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	hist := append([]Event(nil), r.history...)
	return hist, r.statusLocked(), nil
}

// Stop cancels runID's background context, from any caller regardless of
// which connection (if any) started it. It's a no-op, not an error, if the
// run has already reached a terminal state -- Stop expresses intent
// ("this run should not still be going"), which is already satisfied once
// it's no longer running.
func (reg *Registry) Stop(runID string) error {
	r, err := reg.get(runID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.status != StatusRunning {
		r.mu.Unlock()
		return nil
	}
	r.stopRequested = true
	cancel := r.cancel
	r.mu.Unlock()
	cancel()
	return nil
}

// List returns a summary of every Run the Registry currently knows about
// (including runs evicted per finishedRetentionCap are absent), most
// recently started first.
func (reg *Registry) List() []RunSummary {
	reg.mu.Lock()
	rs := make([]*run, 0, len(reg.runs))
	for _, r := range reg.runs {
		rs = append(rs, r)
	}
	reg.mu.Unlock()

	out := make([]RunSummary, 0, len(rs))
	for _, r := range rs {
		r.mu.Lock()
		out = append(out, r.statusLocked())
		r.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func newRunID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
