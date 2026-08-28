package runs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/store"
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

	// st persists every Run's row and event history so both survive a
	// daemon restart -- see New's doc comment for reconciliation/rehydration
	// and Start/record/finish for the write path.
	st *store.Store
}

// New returns a Registry backed by st for persistence, after two
// synchronous startup steps so the returned Registry is immediately
// consistent with what's on disk:
//
//  1. Reconciliation: any persisted run row still status "running" is
//     transitioned to StatusInterrupted. Surviving to a fresh process start
//     with that status means the subprocess that was driving it is
//     definitely gone (nothing ties a run.start-originated subprocess's
//     lifetime to the daemon's -- see CloseAll's doc comment), so "running"
//     would be a lie and there is no way to resume it.
//  2. Rehydration: the most recent finishedRetentionCap persisted runs
//     (each with its full event history) are loaded into the in-memory map
//     as already-terminal runs, so run.list/run.attach/run.logs keep
//     serving recent history exactly as before the restart -- attaching to
//     a rehydrated run immediately delivers its backfilled history then
//     closes, same as attaching to any other already-finished run.
func New(st *store.Store) (*Registry, error) {
	reg := &Registry{runs: make(map[string]*run), st: st}

	if _, err := st.MarkRunningRunsInterrupted(string(StatusInterrupted)); err != nil {
		return nil, fmt.Errorf("runs: reconcile interrupted runs: %w", err)
	}

	rows, err := st.ListRecentRuns(finishedRetentionCap)
	if err != nil {
		return nil, fmt.Errorf("runs: rehydrate: %w", err)
	}
	for _, row := range rows {
		r, err := rehydrateRun(st, row)
		if err != nil {
			return nil, fmt.Errorf("runs: rehydrate run %q: %w", row.ID, err)
		}
		reg.runs[r.id] = r
		reg.finishedOrder = append(reg.finishedOrder, r.id)
	}
	// ListRecentRuns orders most-recent-first; finishedOrder must be
	// oldest-first (retain/CloseAll assume eviction drops index 0 first).
	for i, j := 0, len(reg.finishedOrder)-1; i < j; i, j = i+1, j-1 {
		reg.finishedOrder[i], reg.finishedOrder[j] = reg.finishedOrder[j], reg.finishedOrder[i]
	}

	return reg, nil
}

// closedChan returns an already-closed channel, used for rehydrated runs:
// they have no live driving goroutine to close closedCh for them, but the
// field must still be non-nil and already-signaled so a caller that happens
// to wait on it (CloseAll skips them via status != StatusRunning, but the
// invariant should hold regardless) never blocks.
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// rehydrateRun rebuilds an in-memory run from a persisted store.Run row and
// its store.RunEvent history, for New's rehydration step. The result is
// always terminal (StatusRunning never survives reconciliation, which runs
// before this), so it carries no live ctx/cancel and an already-closed
// closedCh.
func rehydrateRun(st *store.Store, row store.Run) (*run, error) {
	storedEvents, err := st.ListRunEvents(row.ID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	history := make([]Event, 0, len(storedEvents))
	for _, se := range storedEvents {
		e, err := decodeEvent(se.EventData)
		if err != nil {
			return nil, fmt.Errorf("decode event seq %d: %w", se.Seq, err)
		}
		history = append(history, e)
	}
	var nextSeq int64
	if n := len(storedEvents); n > 0 {
		nextSeq = storedEvents[n-1].Seq + 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // rehydrated runs are always already-terminal; ctx is inert.

	return &run{
		id:                 row.ID,
		taskID:             row.TaskID,
		provider:           taskrunner.Provider(row.Provider),
		prompt:             row.Prompt,
		startedAt:          row.StartedAt,
		ctx:                ctx,
		cancel:             cancel,
		closedCh:           closedChan(),
		status:             Status(row.Status),
		finishedAt:         row.FinishedAt,
		stopReason:         row.StopReason,
		errMsg:             row.ErrMsg,
		history:            history,
		nextEventSeq:       nextSeq,
		subscribers:        make(map[int]*subQueue),
		pendingPermissions: make(map[string]chan string),
	}, nil
}

// run is a Registry's internal bookkeeping for one Run: identity fields
// are immutable after Start, everything else is guarded by mu.
type run struct {
	id        string
	taskID    int64
	provider  taskrunner.Provider
	prompt    string
	startedAt time.Time

	// ctx is this run's own background context (the one Start derived via
	// context.WithCancel and handed to drive/RunPrompt) -- cancel closes
	// it. runPermissionDecider selects on it directly (not just on
	// whatever ctx the provider itself passes to Decide) because ACP's own
	// session/request_permission dispatch runs with context.Background(),
	// not anything derived from this run's ctx -- see runPermissionDecider.
	ctx    context.Context
	cancel context.CancelFunc

	// closedCh closes once finish has run for this run (its subprocess has
	// exited and every subscriber has been notified) -- see CloseAll, which
	// blocks on it so a caller can rely on "the process is actually gone"
	// rather than just "we asked it to stop", the same guarantee
	// internal/terminal.Registry.Close already gives its own callers.
	closedCh chan struct{}

	mu            sync.Mutex
	status        Status
	finishedAt    *time.Time
	stopReason    string
	errMsg        string
	stopRequested bool

	history     []Event
	subscribers map[int]*subQueue
	nextSubID   int

	// nextEventSeq is the seq to assign the next persisted run_events row
	// (see Registry.record) -- strictly increasing per run, starting at 0,
	// carried forward from a rehydrated run's last persisted seq so a run
	// that somehow kept going past a restart (it can't today; reconciliation
	// always marks a still-"running" row interrupted first) wouldn't reuse
	// seq values. Guarded by mu for consistency with every other run field,
	// even though record's own single-writer-per-run guarantee (see its doc
	// comment) would make that unnecessary on its own.
	nextEventSeq int64

	// pendingPermissions holds one buffered(1) channel per permission
	// request currently awaiting an answer, keyed by request id -- see
	// runPermissionDecider and RespondPermission.
	pendingPermissions map[string]chan string
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
		id:                 id,
		taskID:             taskID,
		provider:           provider,
		prompt:             prompt,
		startedAt:          time.Now(),
		ctx:                runCtx,
		cancel:             cancel,
		closedCh:           make(chan struct{}),
		status:             StatusRunning,
		subscribers:        make(map[int]*subQueue),
		pendingPermissions: make(map[string]chan string),
	}

	// Persisted before this run is registered/driven, so Start fails fast
	// (and never starts an agent subprocess for a run whose row didn't make
	// it to disk) rather than only surfacing a persistence problem later,
	// silently, from inside the drive goroutine.
	if _, err := reg.st.CreateRun(store.Run{
		ID: id, TaskID: taskID, Provider: string(provider), Prompt: prompt,
		Status: string(StatusRunning), StartedAt: r.startedAt,
	}); err != nil {
		cancel()
		return "", fmt.Errorf("runs: start: persist run: %w", err)
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

	decider := runPermissionDecider{reg: reg, r: r}
	err := runner.RunPrompt(ctx, r.taskID, r.provider, r.prompt, decider, events)
	<-forwardDone
	reg.finish(r, err)
}

// runPermissionDecider implements taskrunner.PermissionDecider for one run,
// bridging the provider's own blocking Decide call to Registry.RespondPermission
// via a pending-response channel recorded on the run itself.
//
// This deliberately never touches the events chan<- taskrunner.Event that
// drive's forwarder goroutine owns: Decide runs on whatever goroutine the
// provider dispatches the permission callback on (a distinct goroutine from
// RunPrompt's own forwarding, and, for ACP, one that doesn't even share
// RunPrompt's ctx -- see acp/rpc.go's handleInboundRequest, dispatched with
// context.Background()). A second concurrent writer on that events channel
// would be exactly the close-vs-send race class this project has already
// hit; going through reg.record instead sidesteps it entirely, since record
// was already designed (by Stop/History/List) to be called safely from any
// goroutine.
type runPermissionDecider struct {
	reg *Registry
	r   *run
}

func (d runPermissionDecider) Decide(ctx context.Context, summary string, options []taskrunner.PermissionOption) (string, error) {
	requestID, err := newRunID()
	if err != nil {
		return "", fmt.Errorf("runs: permission request id: %w", err)
	}

	ch := make(chan string, 1)
	d.r.mu.Lock()
	d.r.pendingPermissions[requestID] = ch
	d.r.mu.Unlock()

	d.reg.record(d.r, taskrunner.Event{
		Type:                taskrunner.EventTypePermissionRequest,
		PermissionRequestID: requestID,
		PermissionSummary:   summary,
		PermissionOptions:   options,
	})

	select {
	case optionID := <-ch:
		// Recorded here, on the same goroutine right after Decide wakes up
		// -- not by RespondPermission directly -- so there's exactly one
		// code path appending both the request and its resolution to
		// history, in the correct order relative to Decide actually
		// returning (see RespondPermission's own doc comment).
		d.reg.record(d.r, taskrunner.Event{
			Type:                taskrunner.EventTypePermissionResolved,
			PermissionRequestID: requestID,
			PermissionOptionID:  optionID,
		})
		return optionID, nil

	case <-ctx.Done():
		d.abandon(requestID)
		return "", ctx.Err()

	case <-d.r.ctx.Done():
		// The run itself was stopped. ctx above is the provider's own
		// per-request context, which for ACP is context.Background() and
		// so would never fire here on its own -- d.r.ctx (the run's own
		// cancellable context, cancelled by Registry.Stop) is what
		// guarantees this Decide call unblocks instead of hanging forever
		// once a pending permission request's run is stopped.
		d.abandon(requestID)
		return "", d.r.ctx.Err()
	}
}

// abandon removes requestID's pending channel so a RespondPermission call
// that arrives after Decide already gave up (via ctx cancellation) gets a
// clear "unknown/already resolved" error instead of silently succeeding
// into a channel nobody will ever read from again.
func (d runPermissionDecider) abandon(requestID string) {
	d.r.mu.Lock()
	delete(d.r.pendingPermissions, requestID)
	d.r.mu.Unlock()
}

// RespondPermission answers runID's pending permission request requestID
// with optionID, from any caller regardless of which connection (if any) is
// watching the run -- mirroring Stop's cross-connection reasoning. The
// blocked Decide call (see runPermissionDecider) wakes up and itself
// records the EventTypePermissionResolved event once it does.
//
// Looking up the channel and sending into it happen under the same r.mu
// critical section as deleting the map entry on success, which is what
// makes double-answering (or answering after Decide already gave up)
// reliably detectable as an error rather than racy: the buffered(1) channel
// starts empty, so the first successful send both delivers the answer and
// atomically removes requestID from the map before releasing the lock: any
// second call -- whether truly concurrent (blocked on the same mutex) or
// merely later -- finds no entry and returns a clear error, never a silent
// no-op, a panic, or (worse) a second value nobody will ever receive.
func (reg *Registry) RespondPermission(runID, requestID, optionID string) error {
	r, err := reg.get(runID)
	if err != nil {
		return err
	}

	r.mu.Lock()
	ch, ok := r.pendingPermissions[requestID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("runs: permission request %q: %w", requestID, ErrNotFound)
	}
	select {
	case ch <- optionID:
		delete(r.pendingPermissions, requestID)
		r.mu.Unlock()
		return nil
	default:
		r.mu.Unlock()
		return fmt.Errorf("runs: permission request %q already resolved", requestID)
	}
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
	seq := r.nextEventSeq
	r.nextEventSeq++
	subs := make([]*subQueue, 0, len(r.subscribers))
	for _, q := range r.subscribers {
		subs = append(subs, q)
	}
	r.mu.Unlock()

	// Best-effort: a transient persistence failure here doesn't corrupt or
	// stop the run itself (the in-memory history/subscriber delivery above
	// already happened), and record has no caller able to act on an error --
	// it runs on drive's internal forwarding goroutine, not on behalf of any
	// request. A run whose events failed to persist just won't survive a
	// restart with full history; it still finishes and streams normally in
	// the current process.
	if data, err := encodeEvent(e); err == nil {
		_, _ = reg.st.AppendRunEvent(r.id, seq, data)
	}

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
	status, finishedAt, stopReason, errMsg := string(r.status), r.finishedAt, r.stopReason, r.errMsg
	r.mu.Unlock()

	// Close every subscriber still attached: their relay goroutines drain
	// whatever's already queued (nothing is lost) and then close their out
	// channel, which is how Subscribe's caller learns the run went
	// terminal without polling.
	for _, q := range subs {
		q.closeQueue()
	}

	// Best-effort, same reasoning as record's persistence call: finish has
	// no caller able to act on a persistence error, and the in-memory
	// status transition above (what every other Registry method actually
	// relies on) already happened regardless.
	_, _ = reg.st.UpdateRunStatus(r.id, status, finishedAt, stopReason, errMsg)

	reg.retain(r.id)

	// Signal last, same reasoning as internal/terminal.Registry's own
	// finish: CloseAll blocks on this to know the run's subprocess is
	// actually gone and every subscriber has been notified, not just that
	// a stop signal was sent.
	close(r.closedCh)
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

// CloseAll stops every still-running Run the Registry knows about -- used
// at daemon shutdown so no agent subprocess outlives the daemon process
// itself. Without this, a Run started via run.start (the path both the CLI
// and the web UI's prompt form use) has no connection tying its lifetime
// to anything, and Go's os/exec sets no death-signal/process-group
// propagation for the subprocess it spawns -- so the daemon process exiting
// would not, on its own, terminate an in-flight agent subprocess. Each Run
// is stopped concurrently (in its own goroutine) rather than serially, so
// one slow-to-exit Run doesn't hold up shutdown behind the others;
// CloseAll returns once every one of them has actually finished stopping
// (blocking on each run's closedCh, mirroring
// internal/terminal.Registry.CloseAll's exact same reasoning and shape).
func (reg *Registry) CloseAll() {
	reg.mu.Lock()
	running := make([]*run, 0, len(reg.runs))
	for _, r := range reg.runs {
		r.mu.Lock()
		isRunning := r.status == StatusRunning
		r.mu.Unlock()
		if isRunning {
			running = append(running, r)
		}
	}
	reg.mu.Unlock()

	var wg sync.WaitGroup
	for _, r := range running {
		wg.Add(1)
		go func(r *run) {
			defer wg.Done()
			_ = reg.Stop(r.id)
			<-r.closedCh
		}(r)
	}
	wg.Wait()
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
