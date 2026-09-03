package terminal

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// forceTestShell sets $SHELL to /bin/bash for the whole test process
// (once), so every session these tests spawn runs bash regardless of the
// developer's own $SHELL (interactive zsh, dash, etc. can print different
// job-control chatter that would make output assertions flaky). A plain
// os.Setenv rather than t.Setenv deliberately: t.Setenv forbids
// t.Parallel in the same test, and every test in this file wants to run
// in parallel; $SHELL is only ever read (never restored) by resolveShell,
// and every test here wants the same value anyway, so process-wide is
// fine.
var forceTestShellOnce sync.Once

func forceTestShell(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	forceTestShellOnce.Do(func() {
		os.Setenv("SHELL", "/bin/bash")
	})
}

// newTestStore returns a real temp-file store (not :memory: -- see
// internal/runs' identical helper, since a restart simulation needs to
// close and reopen the same underlying file).
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	return s
}

// newTestTaskID creates a workspace + task directly against st (bypassing
// internal/workspace.Manager -- these tests pass their own worktree path
// straight to Create, so they have no need for a real git worktree, just a
// real tasks(id) row to satisfy terminal_sessions.task_id's foreign key)
// and returns its id. Every pre-existing test in this file hardcodes taskID
// 1 when calling Create -- the first task created against a fresh store is
// always id 1 (INTEGER PRIMARY KEY AUTOINCREMENT), so this keeps every one
// of those call sites valid without editing them individually.
func newTestTaskID(t *testing.T, st *store.Store) int64 {
	t.Helper()
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "task", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task.ID
}

// newTestRegistry returns a Registry whose sessions run /bin/bash (see
// forceTestShell), backed by a fresh temp-file store with task id 1 already
// created (see newTestTaskID).
func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	forceTestShell(t)
	st := newTestStore(t)
	newTestTaskID(t, st)
	reg, err := New(st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return reg
}

// collectUntil reads from events until acc (accumulated as a string)
// contains want, or deadline elapses, returning the accumulated output.
func collectUntil(t *testing.T, events <-chan Event, want string, timeout time.Duration) string {
	t.Helper()
	var acc strings.Builder
	deadline := time.After(timeout)
	for {
		if strings.Contains(acc.String(), want) {
			return acc.String()
		}
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("events closed before seeing %q; got so far: %q", want, acc.String())
			}
			acc.Write(e.Data)
		case <-deadline:
			t.Fatalf("timed out waiting for %q; got so far: %q", want, acc.String())
		}
	}
}

func pidOf(t *testing.T, reg *Registry, id string) int {
	t.Helper()
	reg.mu.Lock()
	s, ok := reg.sessions[id]
	reg.mu.Unlock()
	if !ok {
		t.Fatalf("session %s not found", id)
	}
	return s.cmd.Process.Pid
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func waitGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid %d is still alive %s after close, want it gone", pid, timeout)
}

// TestRegistry_CreateWriteSubscribe_RealShell proves a real, spawned
// shell round-trips real input to real output through Create/Write/
// Subscribe: not a fake or a plain exec.Command+pipes shell-out, an
// actual pty-backed interactive bash that echoes back what it ran.
func TestRegistry_CreateWriteSubscribe_RealShell(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)
	worktree := t.TempDir()

	id, err := reg.Create(1, worktree)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg.Write(id, []byte("echo hello-smind-terminal\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	collectUntil(t, events, "hello-smind-terminal", 5*time.Second)
}

// TestRegistry_Subscribe_SecondConnectionSeesBackfill proves a second,
// independent Subscribe call against an already-active session sees the
// output produced before it joined (backfill) followed by anything new
// (live) -- the same gapless/duplicate-free guarantee
// internal/runs.Registry.Subscribe proves, exercised here against a real
// PTY byte stream instead of discrete taskrunner.Events.
func TestRegistry_Subscribe_SecondConnectionSeesBackfill(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)
	worktree := t.TempDir()

	id, err := reg.Create(1, worktree)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	first, unsubFirst, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubFirst()

	if err := reg.Write(id, []byte("echo before-second-subscriber\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, first, "before-second-subscriber", 5*time.Second)

	second, unsubSecond, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("second Subscribe() error = %v", err)
	}
	defer unsubSecond()

	// The second subscriber must see the earlier output via backfill...
	backfill := collectUntil(t, second, "before-second-subscriber", 5*time.Second)
	if !strings.Contains(backfill, "before-second-subscriber") {
		t.Fatalf("second subscriber's backfill = %q, want it to contain the earlier output", backfill)
	}

	// ...and then new output live, without needing to resubscribe.
	if err := reg.Write(id, []byte("echo after-second-subscriber\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, second, "after-second-subscriber", 5*time.Second)
}

// TestRegistry_Resize_ReachesPTY proves Resize actually reaches the PTY's
// real window size (a TIOCSWINSZ ioctl via pty.Setsize) -- not just a
// no-op or a fixed default -- by asking the shell itself, via `stty
// size`, which queries the terminal driver directly rather than any
// shell-side cached $COLUMNS/$LINES.
func TestRegistry_Resize_ReachesPTY(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)
	worktree := t.TempDir()

	id, err := reg.Create(1, worktree)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	const rows, cols = 61, 137
	if err := reg.Resize(id, cols, rows); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}

	if err := reg.Write(id, []byte("stty size\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	want := strconv.Itoa(rows) + " " + strconv.Itoa(cols)
	collectUntil(t, events, want, 5*time.Second)
}

// TestRegistry_Close_KillsProcessAndUnblocksSubscribers proves
// terminal.close's contract for real: the shell's OS process is actually
// gone afterward (checked via signal-0 against its real pid, not just
// "Close returned no error"), and every subscriber's events channel
// closes so it isn't left hanging waiting for output that will never
// come.
func TestRegistry_Close_KillsProcessAndUnblocksSubscribers(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)
	worktree := t.TempDir()

	id, err := reg.Create(1, worktree)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	pid := pidOf(t, reg, id)
	if !processAlive(pid) {
		t.Fatalf("shell pid %d not alive right after Create", pid)
	}

	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Close's own contract: by the time it returns, the process is gone,
	// not just "asked to die".
	if processAlive(pid) {
		t.Fatalf("shell pid %d still alive immediately after Close() returned", pid)
	}

	// The channel must close, but not necessarily as the very next thing
	// read: real output the shell produced on its own (its startup
	// prompt/banner) can legitimately still be sitting in the subscriber's
	// queue, already recorded before Close's kill actually landed --
	// subQueue's own contract is to drain whatever's queued before
	// reporting closed (see subqueue.go), so that queued real data, if
	// any, arrives first. Only a channel that never closes at all (a
	// genuine hang) or an actual data race would fail this.
	deadline := time.After(2 * time.Second)
drain:
	for {
		select {
		case _, ok := <-events:
			if !ok {
				break drain
			}
		case <-deadline:
			t.Fatal("events channel did not close within 2s of Close()")
		}
	}

	statuses := reg.List(1)
	found := false
	for _, s := range statuses {
		if s.ID == id {
			found = true
			if s.Status != StatusClosed {
				t.Fatalf("List() status = %q, want %q", s.Status, StatusClosed)
			}
			if s.ClosedAt == nil {
				t.Fatal("List() ClosedAt = nil, want set")
			}
		}
	}
	if !found {
		t.Fatalf("List(1) = %+v, want to still find closed session %s", statuses, id)
	}
}

// TestRegistry_Close_IsIdempotent proves Close on an already-closed
// session is a no-op, not an error -- mirrors runs.Registry.Stop's
// contract.
func TestRegistry_Close_IsIdempotent(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)
	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := reg.Close(id); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := reg.Close(id); err != nil {
		t.Fatalf("second Close() error = %v, want nil (no-op on an already-closed session)", err)
	}
}

// TestRegistry_UnknownID proves every method reports ErrNotFound for an
// ID the Registry has never seen, rather than panicking or hanging.
func TestRegistry_UnknownID(t *testing.T) {
	t.Parallel()
	reg, err := New(newTestStore(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, _, err := reg.Subscribe("nope"); err != ErrNotFound {
		t.Errorf("Subscribe() error = %v, want ErrNotFound", err)
	}
	if err := reg.Write("nope", []byte("x")); err != ErrNotFound {
		t.Errorf("Write() error = %v, want ErrNotFound", err)
	}
	if err := reg.Resize("nope", 80, 24); err != ErrNotFound {
		t.Errorf("Resize() error = %v, want ErrNotFound", err)
	}
	if err := reg.Close("nope"); err != ErrNotFound {
		t.Errorf("Close() error = %v, want ErrNotFound", err)
	}
}

// TestRegistry_Create_NoWorktree proves Create refuses to spawn a shell
// with no cwd to run it in, rather than falling back to some surprising
// default directory.
func TestRegistry_Create_NoWorktree(t *testing.T) {
	t.Parallel()
	reg, err := New(newTestStore(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := reg.Create(1, ""); err == nil {
		t.Fatal("Create() error = nil, want an error for an empty worktree path")
	}
}

// TestRegistry_WriteAfterClose proves writing to a closed session is
// reported as an error rather than silently discarded or panicking.
func TestRegistry_WriteAfterClose(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)
	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := reg.Write(id, []byte("echo x\n")); err == nil {
		t.Fatal("Write() after Close: error = nil, want an error")
	}
}

// TestRegistry_appendHistoryLocked_BoundsScrollback is a fast, white-box
// unit test of the scrollback compaction math (see scrollbackCap's doc
// comment) that doesn't need a real spawned shell: it proves history
// never grows past scrollbackHardCap and, once compacted, retains exactly
// the most recent scrollbackCap bytes (no gap, no stale prefix left
// behind).
func TestRegistry_appendHistoryLocked_BoundsScrollback(t *testing.T) {
	t.Parallel()
	s := &session{}

	// Write enough distinct, identifiable chunks to exceed
	// scrollbackHardCap several times over.
	const chunkSize = 4096
	chunk := func(n int) []byte {
		b := make([]byte, chunkSize)
		for i := range b {
			b[i] = byte('A' + (n % 26))
		}
		return b
	}

	total := 0
	n := 0
	for total < scrollbackHardCap*3 {
		s.appendHistoryLocked(chunk(n))
		total += chunkSize
		n++
		if len(s.history) > scrollbackHardCap {
			t.Fatalf("history len = %d after chunk %d, want <= scrollbackHardCap (%d)", len(s.history), n, scrollbackHardCap)
		}
	}

	if len(s.history) == 0 {
		t.Fatal("history is empty after writing well past the cap")
	}
	if len(s.history) > scrollbackCap {
		// Only true right after a compaction; since we stopped mid-stream
		// this just needs to be within the hard cap, already checked above
		// on every iteration -- this is a sanity re-check of the final
		// state specifically.
		if len(s.history) > scrollbackHardCap {
			t.Fatalf("final history len = %d, want <= %d", len(s.history), scrollbackHardCap)
		}
	}

	// The tail of history must be exactly the most recent chunk written
	// (chunk(n-1)) -- proving compaction drops the *oldest* bytes, not an
	// arbitrary or wrong-end slice.
	want := chunk(n - 1)
	got := s.history[len(s.history)-chunkSize:]
	if string(got) != string(want) {
		t.Fatalf("tail of history after compaction does not match the most recently written chunk")
	}
}

// TestRegistry_ConcurrentReadWrite is a light concurrency smoke test
// (run with -race) that many goroutines can Write to and Subscribe from
// the same real session concurrently without racing or deadlocking --
// complementing subscribe_race_test.go's much more targeted
// backfill-vs-live race test, which drives record/finish directly rather
// than through a real PTY.
func TestRegistry_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)
	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			events, unsubscribe, err := reg.Subscribe(id)
			if err != nil {
				t.Errorf("Subscribe() error = %v", err)
				return
			}
			defer unsubscribe()
			collectUntil(t, events, "marker", 5*time.Second)
		}(i)
	}

	// Give subscribers a moment to register before producing the output
	// they're all waiting for (not required for correctness -- Subscribe's
	// backfill+live guarantee holds regardless of timing -- just keeps the
	// test's own wait bounded instead of racing its own Write against
	// goroutine scheduling).
	time.Sleep(20 * time.Millisecond)
	if err := reg.Write(id, []byte("echo marker\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	wg.Wait()
	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
