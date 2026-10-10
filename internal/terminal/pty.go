package terminal

import (
	"context"
	"os/exec"

	"github.com/charmbracelet/x/xpty"
)

// ptySession is the platform seam between Registry and the PTY backend
// (xpty): it owns the started command plus whatever per-OS process-tree
// bookkeeping killing it needs (a kill-on-close Job Object handle on
// Windows; nothing on Unix). Registry touches the PTY itself only through
// the embedded xpty.Pty (Read/Write/Close/Resize) and the process only
// through the methods below, so no registry.go code branches on
// runtime.GOOS -- that all lives in pty_other.go / pty_windows.go /
// kill_*.go.
//
// The exit flow is deliberately identical on both platforms, with one
// platform-specific hook:
//
//   - waitLoop (one goroutine per session, started by Create) waits for
//     the process via xpty.WaitProcess (which is cmd.Wait on Unix and
//     the ConPTY-correct process wait on Windows -- cmd.Wait is invalid
//     for ConPTY-started attribute-list processes), then releases the
//     job handle (killTree), closes the reaped channel, and finally
//     invokes onExit if non-nil.
//   - onExit is the Windows-only hook: ConPTY does NOT close its output
//     pipe when the child exits, so closing the pty here is what unblocks
//     readLoop's blocked Read (AC4). It's nil on Unix, where readLoop's
//     Read errors on its own once the shell exits and the kernel closes
//     the slave side -- closing the master there instead could drop
//     trailing output still buffered in the pty.
//   - readLoop, on its final Read error, waits on reaped (guaranteeing
//     the process is fully reaped and the job handle released, whichever
//     exited first) before calling finish.
type ptySession struct {
	xpty.Pty
	cmd *exec.Cmd

	// onExit, when non-nil, is called by waitLoop once the process has
	// been waited and reaped -- Windows closes the pty here (see the type
	// doc comment); nil on Unix.
	onExit func()

	// job is the per-OS kill bookkeeping (see killHandle in pty_other.go
	// / pty_windows.go); killTree dispatches through it.
	job killHandle
}

// startPty creates a new PTY (creack/pty under xpty's hood on Unix,
// ConPTY on Windows) and starts cmd attached to it, returning the
// session seam. Any per-OS process-tree setup (Windows: kill-on-close
// Job Object assignment) happens in newPtySession, immediately after
// start, so a session returned successfully is already fully killable
// via killTree -- and a failure in that setup kills and reaps what was
// just started rather than returning a half-armed session.
func startPty(cmd *exec.Cmd) (*ptySession, error) {
	p, err := xpty.NewPty(80, 24)
	if err != nil {
		return nil, err
	}
	if err := p.Start(cmd); err != nil {
		_ = p.Close()
		return nil, err
	}
	return newPtySession(p, cmd)
}

// wait blocks until the started process has exited and been reaped,
// returning its (discarded-by-every-caller) wait error. Called exactly
// once per session, by waitLoop (or killAndReap for never-registered
// sessions).
func (s *ptySession) wait() error {
	return xpty.WaitProcess(context.Background(), s.cmd)
}

// killAndReap kills the session's whole process tree and waits for the
// shell itself to exit and be reaped, discarding every error (the
// process may have already exited; its exit status is irrelevant -- the
// caller is abandoning this session entirely, before it was ever
// registered). The canceled context makes WaitProcess kill-then-reap
// deterministically on both platforms. Used by Create's failure paths,
// which run after a real shell has already been spawned: killing it
// without also reaping it leaves a zombie process under the daemon
// (Unix) or an un-waited handle (Windows), since nothing else ever
// waits on it -- mirrors readLoop/waitLoop's reap for the normal-exit
// path.
func (s *ptySession) killAndReap() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // WaitProcess with a canceled ctx kills + reaps.
	s.job.killTree(s.pid())
	_ = xpty.WaitProcess(ctx, s.cmd)
	s.job.release()
	_ = s.Close()
}

// kill terminates the shell process itself (not its descendants -- that's
// killTree). Best-effort: the process may have already exited.
func (s *ptySession) kill() {
	_ = s.cmd.Process.Kill()
}

// pid returns the shell process's OS pid.
func (s *ptySession) pid() int {
	return s.cmd.Process.Pid
}
