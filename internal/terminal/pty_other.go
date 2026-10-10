//go:build !windows

package terminal

import (
	"os/exec"

	"github.com/charmbracelet/x/xpty"
)

// newPty creates the Unix PTY (creack/pty under xpty's hood).
func newPty(width, height int) (xpty.Pty, error) {
	return xpty.NewPty(width, height)
}

// newPtySession finishes constructing the Unix flavor of the ptySession
// seam: waiting is plain cmd.Wait (xpty's Unix Start is an ordinary
// exec.Cmd.Start, and WaitProcess just calls it), and there is no
// waiter-side exit hook -- readLoop's final Read errors with EOF once
// the shell (session leader on the slave side) exits and the kernel
// closes the slave side, and readLoop then waits on reaped (which
// waitLoop, running the same WaitProcess, closes) before finish.
func newPtySession(p xpty.Pty, cmd *exec.Cmd) (*ptySession, error) {
	return &ptySession{Pty: p, cmd: cmd}, nil
}

// killHandle is the Unix flavor of the per-OS kill bookkeeping a
// ptySession carries: nothing stateful. killTree works off the shell's
// pid -- a /proc parent-child walk on Linux (kill_linux.go), a
// process-group signal elsewhere (kill_other.go) -- so there is no
// handle to guard or release.
type killHandle struct{}

// killTree terminates rootPid and, as best the platform allows, its
// descendants. See kill_linux.go / kill_other.go for the actual Unix
// implementations; on this side of the seam it only dispatches.
func (h killHandle) killTree(rootPid int) {
	killTree(rootPid)
}

// release matches the Windows killHandle's post-reap hook (see
// pty_windows.go) and is a no-op here.
func (h killHandle) release() {}
