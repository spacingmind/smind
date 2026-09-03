//go:build linux

package terminal

import (
	"os"
	"testing"
	"time"
)

// TestRegistry_Create_CreateTerminalSessionFailure_NoZombieLeft proves
// Create's more realistic failure path -- CreateTerminalSession returning
// an error after pty.Start has already spawned a real shell -- doesn't
// leak a zombie process, using the same /proc-walking buildChildrenMap
// killTree already relies on (see kill_linux.go) to observe this test
// binary's real child processes directly, rather than just trusting
// killAndReap's return.
//
// Not parallel: walking /proc for this test binary's own child pids would
// be confused by another parallel test's own concurrently-spawned shell
// appearing/disappearing at the same time. Every other test in this
// package calls t.Parallel() as its first statement, so (per the same
// reasoning TestRegistry_Finish_SupersedesInFlightStaleCheckpoint's doc
// comment relies on) none of them have spawned a shell yet by the time
// this test's own body -- which runs to completion before any of them
// resume concurrently -- finishes.
func TestRegistry_Create_CreateTerminalSessionFailure_NoZombieLeft(t *testing.T) {
	forceTestShell(t)
	st := newTestStore(t)
	// Deliberately not calling newTestTaskID: taskID 999999 references no
	// row in tasks, so CreateTerminalSession fails on the foreign_keys
	// constraint -- the realistic failure path killAndReap fixes (as
	// opposed to newSessionID failing, which needs crypto/rand.Read
	// itself to fail and isn't exercised here).
	reg, err := New(st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	self := os.Getpid()
	before := len(buildChildrenMap()[self])

	if _, err := reg.Create(999999, t.TempDir()); err == nil {
		t.Fatal("Create() with an unknown taskId: error = nil, want an error")
	}

	// killAndReap's Wait() call is synchronous and already completed by
	// the time Create() returned above, so the child should already be
	// gone -- this poll is just a defensive margin against /proc's own
	// bookkeeping lag, not evidence anything here is asynchronous.
	deadline := time.Now().Add(2 * time.Second)
	for {
		after := len(buildChildrenMap()[self])
		if after <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("children of this test binary after a failed Create() = %d, want no more than %d (before) -- a zombie was left behind", after, before)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
