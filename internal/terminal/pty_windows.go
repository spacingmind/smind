//go:build windows

package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"github.com/charmbracelet/x/conpty"
	"github.com/charmbracelet/x/xpty"
	"golang.org/x/sys/windows"
)

// pseudoconsoleResizeQuirk is CreatePseudoConsole's undocumented
// PSEUDOCONSOLE_RESIZE_QUIRK flag (Windows Terminal's conpty-static.h),
// telling ConPTY the attached terminal reflows on resize itself so it
// doesn't fight that with its own repaint. Codex passes it
// unconditionally with a build-17763 minimum (refs/codex
// utils/pty/src/win/psuedocon.rs).
const pseudoconsoleResizeQuirk = 0x2

// newPty creates the ConPTY directly rather than via xpty.NewPty:
// xpty v0.1.4's PtyOption takes its Options by value, so no option can
// ever set the CreatePseudoConsole flags. xpty.ConPty's embedded
// *conpty.ConPty is exported, so wrapping it keeps every other xpty
// behavior (Start, Resize, Close) unchanged.
func newPty(width, height int) (xpty.Pty, error) {
	c, err := conpty.New(width, height, pseudoconsoleResizeQuirk)
	if err != nil {
		return nil, err
	}
	return &xpty.ConPty{ConPty: c}, nil
}

// newPtySession finishes constructing the Windows flavor of the
// ptySession seam: the shell is assigned to a kill-on-close Job Object
// immediately after start (see killHandle's doc comment), and onExit is
// set to close the pty once the process exits -- ConPTY does NOT close
// its output pipe when the child exits, so readLoop's Read would
// otherwise block forever and the session would never reach
// StatusClosed (AC4). Assignment failure fails Create entirely: a
// session that can't be killed tree-wide must not be created, so the
// just-started process is killed and reaped before returning the error.
func newPtySession(p xpty.Pty, cmd *exec.Cmd) (*ptySession, error) {
	job, err := assignJobObject(cmd)
	if err != nil {
		// WaitProcess with a canceled ctx kills + reaps (see killAndReap);
		// closing the pty alone would leak the process handle.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = xpty.WaitProcess(ctx, cmd)
		_ = p.Close()
		return nil, fmt.Errorf("terminal: create: assign job object: %w", err)
	}

	return &ptySession{
		Pty:    p,
		cmd:    cmd,
		onExit: func() { _ = p.Close() },
		job:    newKillHandle(job),
	}, nil
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
// switch. Termination is asynchronous from this call; waitLoop's
// WaitProcess observes the shell's actual exit and then closes the pty,
// unblocking readLoop.
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

// assignJobObject creates a Job Object whose processes are killed when
// its last handle closes, assigns the just-started shell to it, and
// returns our handle -- deliberately the only handle, so closing it is
// what triggers the kill. cmd.Process's handle came from xpty setting
// it via os.FindProcess(pid) (see xpty's conpty_windows.go), which is a
// full process handle, so a leaner PROCESS_SET_QUOTA|PROCESS_TERMINATE
// handle is opened on the pid just for the assignment and closed right
// after. Accepted window: descendants the shell spawns between start
// and this assignment are outside the job -- that's shell startup only
// (microseconds), before the shell has run a single command, so there
// is nothing to miss in practice.
func assignJobObject(cmd *exec.Cmd) (windows.Handle, error) {
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

	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	defer windows.CloseHandle(h)

	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
