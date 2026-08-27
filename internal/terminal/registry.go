package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/creack/pty"
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
const closedRetentionCap = 200

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
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{sessions: make(map[string]*session)}
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
}

// Create spawns a real shell (resolveShell) as a child of the daemon
// process, attached to a new pseudo-terminal, with its cwd set to
// worktreePath, and returns the new session's ID immediately -- it does
// not block on anything the shell does after starting, mirroring
// runs.Registry.Start's "returns before the turn progresses" contract.
// The session's background read loop (owned by the Registry, not by
// whatever request called Create) is what drives output into history and
// subscribers from here on, and eventually detects the shell exiting.
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

	s := &session{
		id:          id,
		taskID:      taskID,
		startedAt:   time.Now(),
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
	s.mu.Lock()
	now := time.Now()
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
// if id is unknown or its session has already closed.
func (reg *Registry) Write(id string, data []byte) error {
	s, err := reg.get(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	running := s.status == StatusRunning
	s.mu.Unlock()
	if !running {
		return fmt.Errorf("terminal: write %s: session closed", id)
	}

	if _, err := s.ptmx.Write(data); err != nil {
		return fmt.Errorf("terminal: write %s: %w", id, err)
	}
	return nil
}

// Resize sets id's PTY window size to cols x rows via a real TIOCSWINSZ
// ioctl (pty.Setsize), so the shell (and any TUI program running inside
// it) sees a real SIGWINCH and picks up the new size -- not just a fixed
// default.
func (reg *Registry) Resize(id string, cols, rows uint16) error {
	s, err := reg.get(id)
	if err != nil {
		return err
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
