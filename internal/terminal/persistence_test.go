package terminal

import (
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// waitForPersistedScrollback polls st for id's persisted terminal_sessions
// row until its scrollback contains want, or timeout elapses -- used to
// observe a real checkpointLoop tick landing (see checkpointCadence's doc
// comment), not a mocked one.
func waitForPersistedScrollback(t *testing.T, st *store.Store, id, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		row, err := st.GetTerminalSession(id)
		if err != nil {
			t.Fatalf("GetTerminalSession(%q) error = %v", id, err)
		}
		if strings.Contains(row.Scrollback, want) {
			return row.Scrollback
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for persisted scrollback for %q to contain %q; got so far: %q", id, want, row.Scrollback)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// killShellForCleanup kills a session's shell process directly by pid --
// used by tests simulating a crash (discarding a Registry without calling
// Close/CloseAll) so the real spawned shell doesn't linger past the test.
func killShellForCleanup(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// TestRegistry_Checkpoint_PersistsScrollback proves the bounded-cadence
// checkpoint write path for real: real PTY output through a session, then
// waiting for a real checkpointCadence tick to land, confirms the
// persisted scrollback matches what the in-memory buffer holds.
func TestRegistry_Checkpoint_PersistsScrollback(t *testing.T) {
	t.Parallel()
	st := newTestStore(t)
	newTestTaskID(t, st)
	reg, err := New(st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	forceTestShell(t)

	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg.Write(id, []byte("echo checkpoint-marker\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, events, "checkpoint-marker", 5*time.Second)

	got := waitForPersistedScrollback(t, st, id, "checkpoint-marker", 5*time.Second)

	reg.mu.Lock()
	s := reg.sessions[id]
	reg.mu.Unlock()
	s.mu.Lock()
	inMemory := string(s.history)
	s.mu.Unlock()

	if got != inMemory {
		t.Fatalf("persisted scrollback = %q, want it to match in-memory history %q", got, inMemory)
	}

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestRegistry_GracefulClose_PersistsFinalState proves terminal.close's
// persistence contract: the final status ("closed"), closed_at, and final
// scrollback are all persisted -- and, since Close doesn't return until
// finish has run (see Close's own doc comment), are guaranteed already
// consistent with the store by the time Close returns.
func TestRegistry_GracefulClose_PersistsFinalState(t *testing.T) {
	t.Parallel()
	st := newTestStore(t)
	newTestTaskID(t, st)
	reg, err := New(st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	forceTestShell(t)

	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg.Write(id, []byte("echo before-graceful-close\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, events, "before-graceful-close", 5*time.Second)

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	row, err := st.GetTerminalSession(id)
	if err != nil {
		t.Fatalf("GetTerminalSession() error = %v", err)
	}
	if row.Status != string(StatusClosed) {
		t.Fatalf("GetTerminalSession().Status = %q, want %q", row.Status, StatusClosed)
	}
	if row.ClosedAt == nil {
		t.Fatal("GetTerminalSession().ClosedAt = nil, want set")
	}
	if !strings.Contains(row.Scrollback, "before-graceful-close") {
		t.Fatalf("GetTerminalSession().Scrollback = %q, want it to contain the final output", row.Scrollback)
	}
}

// TestRegistry_RestartSimulation_ScrollbackSurvivesAcrossRegistries mirrors
// internal/runs' TestRegistry_RestartSimulation_HistorySurvivesAcrossRegistries:
// a session driven to a graceful close by one Registry is fully visible
// (List + Subscribe backfill, identical scrollback) from a brand-new
// Registry built later against the same store -- simulating a daemon
// restart. terminal.write/terminal.resize against the rehydrated session
// must return a clear error, not a hang or silent success.
func TestRegistry_RestartSimulation_ScrollbackSurvivesAcrossRegistries(t *testing.T) {
	t.Parallel()
	forceTestShell(t)
	st := newTestStore(t)
	newTestTaskID(t, st)

	reg1, err := New(st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	id, err := reg1.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events, unsubscribe, err := reg1.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := reg1.Write(id, []byte("echo restart-sim-marker\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, events, "restart-sim-marker", 5*time.Second)
	unsubscribe()

	if err := reg1.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// reg1 is simply discarded here -- nothing about Registry itself is
	// persistent; only what went through st is.
	reg2, err := New(st)
	if err != nil {
		t.Fatalf("New() (reg2) error = %v", err)
	}

	sessions := reg2.List(1)
	found := false
	for _, s := range sessions {
		if s.ID == id {
			found = true
			if s.Status != StatusClosed {
				t.Fatalf("reg2.List() status = %q, want %q", s.Status, StatusClosed)
			}
		}
	}
	if !found {
		t.Fatalf("reg2.List(1) = %+v, want to still find session %s", sessions, id)
	}

	rehydratedEvents, rehydratedUnsub, err := reg2.Subscribe(id)
	if err != nil {
		t.Fatalf("reg2.Subscribe() error = %v", err)
	}
	defer rehydratedUnsub()

	var backfill strings.Builder
	deadline := time.After(5 * time.Second)
drain:
	for {
		select {
		case e, ok := <-rehydratedEvents:
			if !ok {
				break drain
			}
			backfill.Write(e.Data)
		case <-deadline:
			t.Fatal("rehydrated Subscribe: events channel did not close (no live tail expected)")
		}
	}
	if !strings.Contains(backfill.String(), "restart-sim-marker") {
		t.Fatalf("rehydrated Subscribe backfill = %q, want it to contain the pre-restart output", backfill.String())
	}

	if err := reg2.Write(id, []byte("echo x\n")); err == nil {
		t.Fatal("reg2.Write() against a rehydrated session: error = nil, want an error")
	}
	if err := reg2.Resize(id, 80, 24); err == nil {
		t.Fatal("reg2.Resize() against a rehydrated session: error = nil, want an error")
	}
}

// TestRegistry_InterruptedReconciliation_LosesOnlySinceLastCheckpoint
// proves both halves of the crash-persistence contract at once: a session
// left "running" in the store (its Registry discarded without Close/
// CloseAll, simulating a crash -- e.g. SIGKILL) comes back StatusInterrupted
// from a new Registry, not StatusRunning; and its rehydrated scrollback is
// exactly what was checkpointed before the simulated crash, not the full
// pre-crash output -- concretely demonstrating checkpointCadence's "loses
// only since the last checkpoint" bound, not just asserting it in prose.
func TestRegistry_InterruptedReconciliation_LosesOnlySinceLastCheckpoint(t *testing.T) {
	t.Parallel()
	forceTestShell(t)
	st := newTestStore(t)
	newTestTaskID(t, st)

	reg1, err := New(st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	id, err := reg1.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	pid := pidOf(t, reg1, id)
	t.Cleanup(func() { killShellForCleanup(pid) })

	events, unsubscribe, err := reg1.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg1.Write(id, []byte("echo before-checkpoint\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, events, "before-checkpoint", 5*time.Second)
	// Wait for a real checkpoint tick to persist this output, so the store
	// has a known-good "last checkpoint" to fall back to.
	waitForPersistedScrollback(t, st, id, "before-checkpoint", 5*time.Second)

	// Write more output immediately after the checkpoint we just observed,
	// well within checkpointCadence of the next tick, then simulate a crash
	// (discard reg1, no Close/CloseAll) before that next tick can land --
	// this output must NOT survive.
	if err := reg1.Write(id, []byte("echo after-checkpoint-before-crash\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, events, "after-checkpoint-before-crash", 5*time.Second)

	row, err := st.GetTerminalSession(id)
	if err != nil {
		t.Fatalf("GetTerminalSession() before reconciliation error = %v", err)
	}
	if row.Status != string(StatusRunning) {
		t.Fatalf("precondition: persisted Status = %q, want %q", row.Status, StatusRunning)
	}
	if strings.Contains(row.Scrollback, "after-checkpoint-before-crash") {
		t.Fatalf("precondition violated: persisted scrollback already contains post-checkpoint output (test raced a real checkpoint tick): %q", row.Scrollback)
	}

	reg2, err := New(st)
	if err != nil {
		t.Fatalf("New() (reg2) error = %v", err)
	}

	sessions := reg2.List(1)
	var got *SessionStatus
	for i := range sessions {
		if sessions[i].ID == id {
			got = &sessions[i]
		}
	}
	if got == nil {
		t.Fatalf("reg2.List(1) = %+v, want to find session %s", sessions, id)
	}
	if got.Status != StatusInterrupted {
		t.Fatalf("rehydrated Status = %q, want %q", got.Status, StatusInterrupted)
	}

	reconciled, err := st.GetTerminalSession(id)
	if err != nil {
		t.Fatalf("GetTerminalSession() after reconciliation error = %v", err)
	}
	if reconciled.Status != string(StatusInterrupted) {
		t.Fatalf("persisted Status after reconciliation = %q, want %q", reconciled.Status, StatusInterrupted)
	}
	if !strings.Contains(reconciled.Scrollback, "before-checkpoint") {
		t.Fatalf("rehydrated scrollback = %q, want it to still contain the checkpointed output", reconciled.Scrollback)
	}
	if strings.Contains(reconciled.Scrollback, "after-checkpoint-before-crash") {
		t.Fatalf("rehydrated scrollback = %q, want it to NOT contain output written after the last checkpoint (proves the crash-loss bound)", reconciled.Scrollback)
	}

	// The rehydrated session must behave like any other terminal session:
	// write/resize return a clear error rather than a hang or silent
	// success.
	if err := reg2.Write(id, []byte("echo x\n")); err == nil {
		t.Fatal("reg2.Write() against an interrupted session: error = nil, want an error")
	}
	if err := reg2.Resize(id, 80, 24); err == nil {
		t.Fatal("reg2.Resize() against an interrupted session: error = nil, want an error")
	}
}
