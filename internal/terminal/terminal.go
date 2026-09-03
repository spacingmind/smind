// Package terminal gives each task a real, interactive shell running
// inside that task's git worktree, backed by a real pseudo-terminal
// (github.com/creack/pty) rather than a plain exec.Command+pipes
// shell-out, so interactive programs (an editor, `less`, shell job
// control, ANSI colors, real window-size-aware TUIs) work correctly.
//
// This is deliberately modeled on internal/runs.Registry -- see that
// package's doc comment and docs/plans/completed/run-registry-and-cli.md
// for the two rounds of adversarial review its backfill+live design
// already went through. The shape of the problem is identical: a
// long-lived thing (there, a Run's subprocess; here, a PTY session) whose
// lifetime is independent of any one WebSocket connection, with
// backfill-then-live delivery to however many connections/tabs are
// currently attached, and no gap/no duplicate between the two. Registry
// here reuses that same locking discipline (record/Subscribe hold the
// same session mutex across "copy backfill" and "register for live", for
// the same reason -- see Registry.Subscribe's doc comment).
//
// What's genuinely different from a Run, and why:
//
//   - Bidirectional I/O: a Run is server -> client only, until it
//     naturally ends. A terminal also needs client -> server input
//     (Write) and real resize support (Resize), since a shell (and
//     whatever TUI programs run inside it) needs to know its own
//     window size.
//   - Bounded scrollback: a Run's history is naturally bounded by its
//     turn ending, but a terminal session can run indefinitely, so its
//     backfill buffer is capped (see scrollbackCap) rather than growing
//     forever.
//   - Close actually has to kill something: closing a Run's subscriber
//     just detaches; terminal.close (via Registry.Close) has to
//     genuinely terminate the shell process (and, as best as a Unix
//     process model allows, everything it spawned -- see killTree) and
//     close the PTY fd, not just stop watching it.
//   - Persistence is bounded-cadence, not write-through: internal/runs
//     persists each discrete taskrunner.Event as it's recorded, but raw PTY
//     output is a high-frequency byte firehose, not discrete low-frequency
//     events -- see registry.go's checkpointCadence doc comment for the
//     concrete bound this trades away.
package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"time"
)

// Event is one chunk of raw PTY output, exactly as read off the pty
// master fd -- no framing, no guarantee of alignment with any notion of
// "line" or "message". It's intentionally not text: a PTY's byte stream
// isn't guaranteed to be valid UTF-8 at arbitrary read boundaries (a
// multi-byte character can be split across two reads, or a program
// running inside the shell can emit genuinely binary output), so callers
// that need to ship this over a text-based wire protocol (see
// internal/wsapi's terminal.attach) must treat Data as opaque bytes (e.g.
// base64), not decode/re-encode it as a string.
type Event struct {
	Data []byte
}

// Status is a terminal session's lifecycle state.
type Status string

const (
	// StatusRunning is a session's state from Create until its shell
	// process exits or Close is called.
	StatusRunning Status = "running"

	// StatusClosed is a session whose shell process has exited, whether
	// because the user typed `exit`, the process was killed some other
	// way, or Close was called.
	StatusClosed Status = "closed"

	// StatusInterrupted is a session whose persisted row was still
	// "running" when a new Registry started up (see New's doc comment) --
	// its PTY subprocess was a real child of the old daemon process and
	// cannot have survived a restart, so its fate genuinely isn't "closed"
	// (nothing observed it exit) but reporting it as "running" would be a
	// lie. Distinct from StatusClosed the same way internal/runs'
	// StatusInterrupted is distinct from StatusDone/StatusStopped: this
	// status is never set by anything other than New's reconciliation
	// step.
	StatusInterrupted Status = "interrupted"
)

// SessionStatus is a point-in-time snapshot of a terminal session's
// identity and lifecycle state, returned by List.
type SessionStatus struct {
	ID        string
	TaskID    int64
	StartedAt time.Time
	Status    Status

	// ClosedAt is populated once Status is StatusClosed.
	ClosedAt *time.Time
}

// ErrNotFound is returned by Subscribe, Write, Resize, and Close when no
// session with the given ID is known to the Registry -- either it never
// existed, or it closed long enough ago to have been evicted (see
// Registry's doc comment on closedRetentionCap).
var ErrNotFound = errors.New("terminal: session not found")

// resolveShell picks the shell a new session runs: $SHELL if it's set and
// actually exists on disk, else /bin/bash if that exists, else /bin/sh
// (expected to exist on any Unix system this daemon runs on). This
// mirrors how a normal interactive terminal emulator picks a shell --
// respecting the user's configured $SHELL when available, never erroring
// out just because it isn't set or points somewhere that no longer
// exists.
func resolveShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	for _, candidate := range []string{"/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "/bin/sh"
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
