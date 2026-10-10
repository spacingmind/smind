package terminal

import (
	"io"
	"os/exec"
)

// ptyIO is the part of a PTY that Registry touches: the byte stream plus
// resize. On Unix it's an xpty.Pty (creack/pty); on Windows it's this
// package's own conPty (ADR-0022).
type ptyIO interface {
	io.ReadWriteCloser
	Resize(width, height int) error
}

// ptySession is the platform seam between Registry and the PTY backend:
// it owns the started command plus whatever per-OS process-tree
// bookkeeping killing it needs (a kill-on-close Job Object handle on
// Windows; nothing on Unix). Registry touches the PTY itself only through
// the embedded ptyIO (Read/Write/Close/Resize) and the process only
// through the methods below, so no registry.go code branches on
// runtime.GOOS -- that all lives in pty_other.go / pty_windows.go /
// kill_*.go.
//
// The exit flow is deliberately identical on both platforms, with one
// platform-specific hook:
//
//   - waitLoop (one goroutine per session, started by Create) waits for
//     the process (wait, per OS), then releases the job handle, closes
//     the reaped channel, and finally invokes onExit if non-nil.
//   - onExit is the Windows-only hook: ConPTY does NOT close its output
//     pipe when the child exits, so closing the pseudoconsole here is
//     what lets readLoop drain the final output and then hit EOF (AC4).
//     It's nil on Unix, where readLoop's Read errors on its own once the
//     shell exits and the kernel closes the slave side -- closing the
//     master there instead could drop trailing output still buffered in
//     the pty.
//   - readLoop, on its final Read error, closes the pty and waits on
//     reaped (guaranteeing the process is fully reaped and the job handle
//     released, whichever exited first) before calling finish.
type ptySession struct {
	ptyIO
	cmd *exec.Cmd

	// onExit, when non-nil, is called by waitLoop once the process has
	// been waited and reaped -- Windows closes the pseudoconsole here (see
	// the type doc comment); nil on Unix.
	onExit func()

	// job is the per-OS kill bookkeeping (see killHandle in pty_other.go
	// / pty_windows.go); killTree dispatches through it.
	job killHandle
}

// killAndReap kills the session's whole process tree and waits for the
// shell itself to exit and be reaped, discarding every error (the
// process may have already exited; its exit status is irrelevant -- the
// caller is abandoning this session entirely, before it was ever
// registered). Used by Create's failure paths, which run after a real
// shell has already been spawned: killing it without also reaping it
// leaves a zombie process under the daemon (Unix) or an un-waited handle
// (Windows), since nothing else ever waits on it -- mirrors
// readLoop/waitLoop's reap for the normal-exit path.
func (s *ptySession) killAndReap() {
	s.job.killTree(s.pid())
	s.kill()
	_ = s.wait()
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
