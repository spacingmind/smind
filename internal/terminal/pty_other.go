//go:build !windows

package terminal

import (
	"os/exec"

	"github.com/charmbracelet/x/xpty"
)

// startPty creates a Unix PTY (creack/pty under xpty's hood) and starts
// cmd attached to it, returning the session seam. There is no
// waiter-side exit hook: readLoop's final Read errors with EOF once the
// shell (session leader on the slave side) exits and the kernel closes
// the slave side, and readLoop then waits on reaped (which waitLoop
// closes) before finish.
func startPty(cmd *exec.Cmd) (*ptySession, error) {
	p, err := xpty.NewPty(80, 24)
	if err != nil {
		return nil, err
	}
	if err := p.Start(cmd); err != nil {
		_ = p.Close()
		return nil, err
	}
	return &ptySession{ptyIO: p, cmd: cmd}, nil
}

// wait blocks until the started process has exited and been reaped
// (xpty's Unix Start is an ordinary exec.Cmd.Start). Called exactly once
// per session, by waitLoop (or killAndReap for never-registered
// sessions).
func (s *ptySession) wait() error {
	return s.cmd.Wait()
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
