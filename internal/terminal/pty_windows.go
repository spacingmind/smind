//go:build windows

package terminal

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// startPty creates a ConPTY (this package's own wrapper, ADR-0022) and
// spawns cmd attached to it, already inside a fresh kill-on-close Job
// Object (see killHandle's doc comment) -- the job is created first and
// handed to CreateProcess, so there is no window in which the shell or
// anything it spawns runs outside it. onExit closes only the
// pseudoconsole: ConPTY does NOT close its output pipe when the child
// exits, and closing the pseudoconsole is what makes conhost flush and
// exit, so readLoop drains the final output and then reads EOF (AC4).
func startPty(cmd *exec.Cmd) (*ptySession, error) {
	job, err := newKillOnCloseJob()
	if err != nil {
		return nil, err
	}
	c, err := newConPty(80, 24, pseudoconsoleResizeQuirk)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	if err := c.spawn(cmd, job); err != nil {
		_ = c.Close()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &ptySession{
		ptyIO:  c,
		cmd:    cmd,
		onExit: c.closeConsole,
		job:    newKillHandle(job),
	}, nil
}

// wait blocks until the started process has exited, recording its
// ProcessState like exec.Cmd.Wait would. cmd.Wait itself can't be used:
// the process wasn't started by exec.Cmd.Start (see conPty.spawn).
// Called exactly once per session, by waitLoop (or killAndReap for
// never-registered sessions).
func (s *ptySession) wait() error {
	state, err := s.cmd.Process.Wait()
	s.cmd.ProcessState = state
	return err
}

// killHandle is the Windows flavor of the per-OS kill bookkeeping a
// ptySession carries (see pty_other.go's empty Unix flavor): a
// kill-on-close Job Object holding the shell and everything it spawns.
// Closing the handle (closeOnce-guarded -- Close and a natural exit
// both release it, and Windows recycles handle values, so a second
// unguarded CloseHandle could close an unrelated handle in the daemon)
// makes the OS terminate the whole tree; that's why a Job Object rather
// than a process-tree walk or taskkill /T: no process-spawn per kill,
// no PID-reuse race, and a daemon crash closes the handle for us,
// taking the tree down too.
type killHandle struct {
	job       windows.Handle
	closeOnce sync.Once
}

func newKillHandle(job windows.Handle) killHandle {
	return killHandle{job: job}
}

// killTree terminates the shell and every process it spawned by closing
// the Job Object handle -- the once-guard means concurrent Closes (or a
// Close racing the natural-exit release in waitLoop) are harmless.
// Best-effort in the same sense as the Unix killTree implementations: a
// tree that's already gone (natural exit) is exactly the desired end
// state, so every error is ignored. Note the handle has no pid
// parameter: the handle, which only the session holds, is the kill
// switch. Termination is asynchronous from this call; waitLoop observes
// the shell's actual exit and then closes the pseudoconsole, letting
// readLoop finish.
func (h *killHandle) killTree(_ int) {
	h.closeOnce.Do(func() { _ = windows.CloseHandle(h.job) })
}

// release closes the job handle after the process has been naturally
// reaped (waitLoop) -- the same once-guarded close as killTree, so
// exactly one of the two paths ever closes the handle and natural exit
// doesn't leak one handle per session. On natural exit the process is
// already gone, so the kill-on-close semantics are a no-op for it; any
// descendants it left behind (an orphaned `ping -t`) are still taken
// down, matching Close's contract as closely as the OS allows.
func (h *killHandle) release() {
	h.killTree(0)
}

// newKillOnCloseJob creates a Job Object whose processes are killed when
// its last handle closes, and returns our handle -- deliberately the only
// one, so closing it is what triggers the kill.
func newKillOnCloseJob() (windows.Handle, error) {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
