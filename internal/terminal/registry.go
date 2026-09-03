package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/creack/pty"

	"github.com/spacingmind/smind/internal/store"
)

// scrollbackCap bounds a session's retained backfill history: once its
// buffered output exceeds scrollbackHardCap (2x the nominal cap), it's
// compacted back down to the most recent scrollbackCap bytes. Unlike
// internal/runs' finishedRetentionCap (which bounds the *number of
// finished runs* kept, since each individual run's own history is never
// truncated -- a run naturally ends), a terminal session can run
// indefinitely, so its own backfill has to be capped by size, not just by
// session count. 256KiB is generous enough to backfill a genuinely useful
// amount of scrollback (many thousands of lines of typical shell output)
// without letting a single long-lived session's memory grow unbounded.
//
// Compacting only once every scrollbackCap bytes of overflow (rather than
// trimming back to exactly scrollbackCap on every single append) amortizes
// the O(n) copy cost of trimming across scrollbackCap bytes of output
// instead of paying it on every write, at the cost of transiently holding
// up to scrollbackHardCap bytes in memory.
const (
	scrollbackCap     = 256 * 1024
	scrollbackHardCap = 2 * scrollbackCap
)

// closedRetentionCap bounds how many closed sessions the Registry keeps
// around in memory (for List/a final Subscribe backfill of their last
// output) -- mirrors internal/runs.finishedRetentionCap's reasoning
// exactly: still-running sessions are never evicted, only closed ones,
// oldest-closed-first, once more than this many have accumulated.
//
// New also reuses this same constant to bound how many persisted sessions
// get rehydrated into memory at startup (see New's doc comment) -- one
// bound serving both "how many closed sessions may I hold in memory right
// now" and "how many may I load back in after a restart" is deliberate:
// they're the same in-memory cost, just incurred at a different time.
const closedRetentionCap = 200

// checkpointCadence bounds how much of a session's scrollback a crash (no
// graceful Close -- e.g. SIGKILL) can lose to "since the last checkpoint".
// Raw PTY output is a high-frequency byte firehose, unlike internal/runs'
// discrete, low-frequency taskrunner.Events, so persisting every chunk
// would put a disk write on the hot path of every keystroke/output chunk
// an interactive session produces -- checking on this fixed wall-clock
// cadence instead, and skipping the write entirely when nothing changed
// (see checkpointLoop), keeps that cost bounded and mostly zero for an idle
// session. 2 seconds is frequent enough that even a SIGKILL mid-typing
// loses at most a couple of terminal lines of context, while being many
// orders of magnitude cheaper than a persist-per-chunk on a session
// producing hundreds of chunks per second.
const checkpointCadence = 2 * time.Second

// readBufSize is the size of the buffer readLoop uses for each PTY read;
// it does not bound anything else (a single read producing readBufSize
// bytes of output is just one Event, broadcast and appended to history
// like any other).
const readBufSize = 4096

// Registry owns the lifetime of every terminal session started through
// it: Create spawns a session's background read loop (outliving whatever
// request called Create), and Subscribe/Write/Resize/Close/List let any
// caller -- on any connection, at any later time -- observe, drive, or
// kill it by ID. See the package doc comment for how this mirrors
// internal/runs.Registry.
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*session

	// closedOrder records closed session IDs in close order, so eviction
	// (see closedRetentionCap) always drops the oldest first.
	closedOrder []string

	// st persists every session's row and checkpointed scrollback so both
	// survive a daemon restart -- see New's doc comment for reconciliation/
	// rehydration and Create/checkpointLoop/finish for the write path.
	st *store.Store
}

// New returns a Registry backed by st for persistence, after two
// synchronous startup steps so the returned Registry is immediately
// consistent with what's on disk (mirrors internal/runs.New exactly):
//
//  1. Reconciliation: any persisted terminal_sessions row still status
//     "running" is transitioned to StatusInterrupted. Surviving to a fresh
//     process start with that status means the PTY subprocess that was
//     driving it is definitely gone (it was a real child of the old daemon
//     process, and nothing ties its lifetime to anything beyond that --
//     see CloseAll's doc comment), so "running" would be a lie and there is
//     no way to resume it.
//  2. Rehydration: the most recent closedRetentionCap persisted sessions
//     are loaded into the in-memory map as already-terminal sessions, with
//     their last-checkpointed (or final, if closed gracefully) scrollback
//     as backfill, so terminal.list/terminal.attach keep serving recent
//     sessions exactly as before the restart -- attaching to a rehydrated
//     session immediately delivers its scrollback as backfill then closes,
//     same as attaching to any other already-closed session.
func New(st *store.Store) (*Registry, error) {
	reg := &Registry{sessions: make(map[string]*session), st: st}

	if _, err := st.MarkRunningTerminalSessionsInterrupted(string(StatusInterrupted)); err != nil {
		return nil, fmt.Errorf("terminal: reconcile interrupted sessions: %w", err)
	}

	rows, err := st.ListRecentTerminalSessions(closedRetentionCap)
	if err != nil {
		return nil, fmt.Errorf("terminal: rehydrate: %w", err)
	}
	for _, row := range rows {
		s := rehydrateSession(row)
		reg.sessions[s.id] = s
		reg.closedOrder = append(reg.closedOrder, s.id)
	}
	// ListRecentTerminalSessions orders most-recent-first; closedOrder must
	// be oldest-first (retain assumes eviction drops index 0 first).
	for i, j := 0, len(reg.closedOrder)-1; i < j; i, j = i+1, j-1 {
		reg.closedOrder[i], reg.closedOrder[j] = reg.closedOrder[j], reg.closedOrder[i]
	}

	return reg, nil
}

// closedChan returns an already-closed channel, used for rehydrated
// sessions: they have no live readLoop/checkpointLoop to close closedCh for
// them, but the field must still be non-nil and already-signaled so a
// caller that happens to wait on it (Close skips them via its own
// running check, but the invariant should hold regardless) never blocks.
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// rehydrateSession rebuilds an in-memory session from a persisted
// store.TerminalSession row, for New's rehydration step. The result is
// always terminal (StatusRunning never survives reconciliation, which runs
// before this), so it carries no live cmd/ptmx and an already-closed
// closedCh.
func rehydrateSession(row store.TerminalSession) *session {
	return &session{
		id:          row.ID,
		taskID:      row.TaskID,
		startedAt:   row.StartedAt,
		closedCh:    closedChan(),
		status:      Status(row.Status),
		closedAt:    row.ClosedAt,
		history:     []byte(row.Scrollback),
		subscribers: make(map[int]*subQueue),
	}
}

// session is a Registry's internal bookkeeping for one terminal session:
// identity fields are immutable after Create, everything else is guarded
// by mu.
type session struct {
	id        string
	taskID    int64
	startedAt time.Time
	cmd       *exec.Cmd
	ptmx      *os.File

	// closedCh closes once this session's background read loop has
	// observed the shell exiting (naturally, or via Close) and finished
	// running finish -- see Close, which waits on it so a caller can rely
	// on "the process is actually gone" by the time Close returns, not
	// just "we asked it to die".
	closedCh chan struct{}

	mu          sync.Mutex
	status      Status
	closedAt    *time.Time
	history     []byte
	subscribers map[int]*subQueue
	nextSubID   int

	// historyVersion is bumped on every appendHistoryLocked call, so
	// checkpointLoop can tell whether history has changed since its last
	// checkpoint without diffing the buffer itself -- an idle session (no
	// new output between ticks) costs nothing beyond the version compare.
	historyVersion uint64
}

func (s *session) statusLocked() SessionStatus {
	return SessionStatus{
		ID:        s.id,
		TaskID:    s.taskID,
		StartedAt: s.startedAt,
		Status:    s.status,
		ClosedAt:  s.closedAt,
	}
}

// appendHistoryLocked appends data to s.history, compacting it back down
// to scrollbackCap bytes once it exceeds scrollbackHardCap -- see the
// scrollbackCap doc comment for why. Must be called with s.mu held.
func (s *session) appendHistoryLocked(data []byte) {
	s.history = append(s.history, data...)
	if len(s.history) > scrollbackHardCap {
		keep := len(s.history) - scrollbackCap
		trimmed := make([]byte, scrollbackCap)
		copy(trimmed, s.history[keep:])
		s.history = trimmed
	}
	s.historyVersion++
}

// Create spawns a real shell (resolveShell) as a child of the daemon
// process, attached to a new pseudo-terminal, with its cwd set to
// worktreePath, and returns the new session's ID immediately -- it does
// not block on anything the shell does after starting, mirroring
// runs.Registry.Start's "returns before the turn progresses" contract.
// The session's background read loop (owned by the Registry, not by
// whatever request called Create) is what drives output into history and
// subscribers from here on, and eventually detects the shell exiting.
//
// The terminal_sessions row is persisted before Create returns (and before
// the session is registered/driven) -- same immediacy guarantee as
// runs.Registry.Start's persisted runs row: a session recorded as started
// is queryable even if the daemon dies right after, and a failure to
// persist it fails Create itself (killing the just-spawned shell) rather
// than silently starting an unpersisted session.
func (reg *Registry) Create(taskID int64, worktreePath string) (string, error) {
	if worktreePath == "" {
		return "", fmt.Errorf("terminal: create: task %d has no worktree", taskID)
	}

	cmd := exec.Command(resolveShell())
	cmd.Dir = worktreePath
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", fmt.Errorf("terminal: create: start shell: %w", err)
	}

	id, err := newSessionID()
	if err != nil {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("terminal: create: %w", err)
	}

	startedAt := time.Now()
	if _, err := reg.st.CreateTerminalSession(store.TerminalSession{
		ID: id, TaskID: taskID, Status: string(StatusRunning), StartedAt: startedAt,
	}); err != nil {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("terminal: create: persist session: %w", err)
	}

	s := &session{
		id:          id,
		taskID:      taskID,
		startedAt:   startedAt,
		cmd:         cmd,
		ptmx:        ptmx,
		closedCh:    make(chan struct{}),
		status:      StatusRunning,
		subscribers: make(map[int]*subQueue),
	}

	reg.mu.Lock()
	reg.sessions[id] = s
	reg.mu.Unlock()

	go reg.readLoop(s)
	go reg.checkpointLoop(s)

	return id, nil
}

// readLoop is the one goroutine that ever reads s.ptmx: it pushes every
// chunk it reads into history/subscribers via record, until the read
// itself errors (the shell exited, naturally or because Close killed it
// and closed the pty), at which point it reaps the process and calls
// finish. Being the sole reader (and the sole writer of history via
// record, which serializes through s.mu) is what makes every subscriber
// see a single, globally consistent byte order with no interleaving to
// worry about here -- the harder ordering question, backfill racing a new
// subscriber joining, is Subscribe's, addressed in its own doc comment.
func (reg *Registry) readLoop(s *session) {
	buf := make([]byte, readBufSize)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			reg.record(s, data)
		}
		if err != nil {
			break
		}
	}

	// Reap the process so it doesn't linger as a zombie. Its exit status
	// (clean exit, or killed by Close's SIGKILL) isn't surfaced anywhere
	// today -- a terminal session has no analog to a Run's
	// success/error/stopped distinction, just running or closed.
	_ = s.cmd.Wait()

	reg.finish(s)
}

// checkpointLoop persists s's scrollback on checkpointCadence's bounded
// cadence, write-only-if-changed (tracked via historyVersion, so an idle
// session between ticks costs nothing but a version compare) -- see
// checkpointCadence's doc comment for the crash-loss bound this gives. It
// exits once s closes: finish already persists the final scrollback
// synchronously at that point (atomically with the status transition), so
// there's nothing left for a further checkpoint tick to do.
func (reg *Registry) checkpointLoop(s *session) {
	ticker := time.NewTicker(checkpointCadence)
	defer ticker.Stop()

	var lastCheckpointed uint64
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			version := s.historyVersion
			var snapshot []byte
			if version != lastCheckpointed {
				snapshot = append([]byte(nil), s.history...)
			}
			s.mu.Unlock()

			if snapshot == nil {
				continue
			}
			// Best-effort, same reasoning as internal/runs.Registry.record:
			// a transient persistence failure here doesn't affect the live
			// session, only how much scrollback a crash before the next
			// successful checkpoint would lose.
			if _, err := reg.st.UpdateTerminalSessionScrollback(s.id, string(snapshot)); err == nil {
				lastCheckpointed = version
			}
		case <-s.closedCh:
			return
		}
	}
}

// record appends data to s's history and broadcasts it to every
// subscriber current at the time of the append -- internal/runs.record's
// exact locking discipline, adapted from discrete Events to a raw byte
// stream.
func (reg *Registry) record(s *session, data []byte) {
	s.mu.Lock()
	s.appendHistoryLocked(data)
	subs := make([]*subQueue, 0, len(s.subscribers))
	for _, q := range s.subscribers {
		subs = append(subs, q)
	}
	s.mu.Unlock()

	for _, q := range subs {
		q.push(Event{Data: data})
	}
}

func (reg *Registry) finish(s *session) {
	now := time.Now()

	s.mu.Lock()
	finalHistory := append([]byte(nil), s.history...)
	s.mu.Unlock()

	// Persist before the in-memory status transition below becomes
	// visible, not after -- same reasoning as internal/runs.Registry.finish:
	// otherwise a caller that observes StatusClosed in memory (List, Close's
	// own callers) has no guarantee the persisted row already matches. This
	// is also what gives Close's "final scrollback persisted synchronously
	// before returning" contract: Close blocks on s.closedCh, which only
	// closes after this call has already run. Best-effort past this point,
	// same as runs.Registry.finish: nothing here has a caller able to act on
	// a persistence error, and the in-memory transition below (what every
	// other Registry method actually relies on) happens regardless.
	_, _ = reg.st.UpdateTerminalSessionStatus(s.id, string(StatusClosed), &now, string(finalHistory))

	s.mu.Lock()
	s.status = StatusClosed
	s.closedAt = &now
	subs := make([]*subQueue, 0, len(s.subscribers))
	for _, q := range s.subscribers {
		subs = append(subs, q)
	}
	s.subscribers = make(map[int]*subQueue)
	s.mu.Unlock()

	// Close every subscriber still attached: their relay goroutines drain
	// whatever's already queued (nothing is lost) and then close their out
	// channel, which is how Subscribe's caller learns the session closed
	// without polling.
	for _, q := range subs {
		q.closeQueue()
	}

	reg.retain(s.id)

	// Signal last: Close (see below) blocks on this to know the process is
	// actually gone and every subscriber has been notified, not just that
	// a kill signal was sent.
	close(s.closedCh)
}

func (reg *Registry) retain(id string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.closedOrder = append(reg.closedOrder, id)
	for len(reg.closedOrder) > closedRetentionCap {
		evict := reg.closedOrder[0]
		reg.closedOrder = reg.closedOrder[1:]
		delete(reg.sessions, evict)
	}
}

func (reg *Registry) get(id string) (*session, error) {
	reg.mu.Lock()
	s, ok := reg.sessions[id]
	reg.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// Subscribe returns id's output history so far, followed by its
// remaining live output as it arrives, with no gap and no duplicate
// between the two -- and unsubscribe, which detaches without closing the
// session (see Registry's doc comment). events closes once id reaches
// StatusClosed, whether that happens before or after this call.
//
// The race this has to get right is exactly the one
// internal/runs.Registry.Subscribe's doc comment proves a fix for, in
// this package's terms: between "read the current history" and "start
// receiving future broadcasts", a broadcast landing in that gap must
// appear exactly once in what the caller sees. The fix is the same one:
// hold s.mu across both steps -- pushing the current history into this
// subscriber's queue as backfill, and only then registering that queue to
// receive future ones -- for the entire duration of that hold. record
// (the only writer of new output) also takes s.mu before it can append to
// history or reach the subscriber map, so it cannot interleave with this
// critical section: either a given chunk of output is already in history
// when Subscribe reads it (delivered via the backfill push, once) or it
// isn't yet, in which case Subscribe's queue is registered before record
// gets a chance to add it, so it's delivered live (also once). The
// backfill push itself happens synchronously inside this critical section
// (subQueue.push never blocks), so it can't be preempted by a live push
// landing in between.
func (reg *Registry) Subscribe(id string) (<-chan Event, func(), error) {
	s, err := reg.get(id)
	if err != nil {
		return nil, nil, err
	}

	q := newSubQueue()

	s.mu.Lock()
	if len(s.history) > 0 {
		backfill := make([]byte, len(s.history))
		copy(backfill, s.history)
		q.push(Event{Data: backfill})
	}
	live := s.status == StatusRunning
	var subID int
	if live {
		subID = s.nextSubID
		s.nextSubID++
		s.subscribers[subID] = q
	}
	s.mu.Unlock()

	if !live {
		// Already closed: there will never be a live tail, so the backfill
		// just pushed is the whole story.
		q.closeQueue()
	}

	out := make(chan Event)
	done := make(chan struct{})
	go relay(q, out, done)

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			if live {
				s.mu.Lock()
				delete(s.subscribers, subID)
				s.mu.Unlock()
			}
			q.closeQueue()
			close(done)
		})
	}
	return out, unsubscribe, nil
}

// Write sends data to id's PTY as if it had been typed into the shell --
// this is how keystrokes/input from a client reach the shell. It's an
// error (ErrNotFound, or a wrapped write error) rather than a silent no-op
// if id is unknown or its session is no longer running -- whether it
// closed in this process or was rehydrated as already-closed/interrupted
// after a restart (see New's doc comment).
func (reg *Registry) Write(id string, data []byte) error {
	s, err := reg.get(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	running := s.status == StatusRunning
	s.mu.Unlock()
	if !running {
		return fmt.Errorf("terminal: write %s: session no longer running", id)
	}

	if _, err := s.ptmx.Write(data); err != nil {
		return fmt.Errorf("terminal: write %s: %w", id, err)
	}
	return nil
}

// Resize sets id's PTY window size to cols x rows via a real TIOCSWINSZ
// ioctl (pty.Setsize), so the shell (and any TUI program running inside
// it) sees a real SIGWINCH and picks up the new size -- not just a fixed
// default. Like Write, this errors rather than silently no-opping against
// a session that is no longer running: a rehydrated (post-restart) session
// has no live ptmx to resize at all.
func (reg *Registry) Resize(id string, cols, rows uint16) error {
	s, err := reg.get(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	running := s.status == StatusRunning
	s.mu.Unlock()
	if !running {
		return fmt.Errorf("terminal: resize %s: session no longer running", id)
	}

	if err := pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		return fmt.Errorf("terminal: resize %s: %w", id, err)
	}
	return nil
}

// Close kills id's shell process -- and, as best this Unix process model
// allows, everything it spawned (see killTree) -- and closes the PTY
// master fd, then blocks until the session's own background read loop has
// observed the resulting EOF/error and finished tearing down (status
// flipped to closed, every subscriber unblocked) before returning. A
// caller can rely on the process actually being gone by the time Close
// returns, not just that a kill signal was sent.
//
// It's not an error to close an already-closed session (mirrors
// runs.Registry.Stop's already-terminal-is-a-no-op contract) -- Close
// expresses intent ("this session should not still be running"), already
// satisfied once it no longer is.
func (reg *Registry) Close(id string) error {
	s, err := reg.get(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	running := s.status == StatusRunning
	s.mu.Unlock()
	if !running {
		return nil
	}

	killTree(s.cmd.Process.Pid)
	_ = s.ptmx.Close()

	<-s.closedCh
	return nil
}

// CloseAll closes every still-running session the Registry knows about --
// used at daemon shutdown so no shell process or PTY fd outlives the
// daemon process itself. Each session is closed concurrently (in its own
// goroutine) rather than serially, so one slow-to-reap session doesn't
// hold up shutdown behind the others; CloseAll returns once every one of
// them has actually finished closing.
func (reg *Registry) CloseAll() {
	reg.mu.Lock()
	ids := make([]string, 0, len(reg.sessions))
	for id, s := range reg.sessions {
		s.mu.Lock()
		running := s.status == StatusRunning
		s.mu.Unlock()
		if running {
			ids = append(ids, id)
		}
	}
	reg.mu.Unlock()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = reg.Close(id)
		}(id)
	}
	wg.Wait()
}

// List returns a summary of every session belonging to taskID the
// Registry currently knows about (closed sessions evicted per
// closedRetentionCap are absent), most recently started first -- mirrors
// runs.Registry.List, except filtered server-side by taskID: unlike a
// Run (which a UI lists across all tasks via run.list and filters
// client-side), a terminal session inherently belongs to exactly one
// task's worktree, so filtering here is both natural and lets a caller
// avoid fetching every task's sessions just to find its own.
func (reg *Registry) List(taskID int64) []SessionStatus {
	reg.mu.Lock()
	ss := make([]*session, 0, len(reg.sessions))
	for _, s := range reg.sessions {
		ss = append(ss, s)
	}
	reg.mu.Unlock()

	out := make([]SessionStatus, 0, len(ss))
	for _, s := range ss {
		s.mu.Lock()
		if s.taskID == taskID {
			out = append(out, s.statusLocked())
		}
		s.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}
