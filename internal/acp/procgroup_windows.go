//go:build windows

package acp

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// setProcessGroup arranges for cmd's whole process tree to be killable
// together: on Windows that means a kill-on-close Job Object, and the child
// is created suspended (see attachProcessGroup) so there is no window in
// which it, or anything it spawns, runs outside the job.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
}

// processGroup is the Windows flavor of the per-conn kill bookkeeping: a
// kill-on-close Job Object holding the agent and every process it spawns
// (MCP servers included). Closing the handle (once-guarded -- Windows
// recycles handle values, so a second unguarded CloseHandle could close an
// unrelated handle in the daemon) makes the OS terminate the whole tree;
// a daemon crash closes the handle too, taking the tree down with it.
type processGroup struct {
	job       windows.Handle
	closeOnce sync.Once
}

var ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

// attachProcessGroup must be called exactly once, immediately after a
// successful cmd.Start() of a cmd prepared by setProcessGroup: it creates
// the job, assigns the still-suspended child to it, and only then resumes
// the child's main thread (NtResumeProcess -- the thread handle from
// CreateProcess isn't reachable through os/exec). On any failure the child
// is killed and reaped so a suspended orphan can never be left behind.
func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	killChild := func() {
		_ = cmd.Process.Kill() // TerminateProcess works on a suspended process
		_, _ = cmd.Process.Wait()
	}

	job, err := newKillOnCloseJob()
	if err != nil {
		killChild()
		return nil, fmt.Errorf("acp: create job object: %w", err)
	}

	// Opened by pid while cmd.Process still holds its own handle, so the
	// pid can't have been recycled. os/exec's handle doesn't necessarily
	// carry PROCESS_SET_QUOTA (AssignProcessToJobObject's requirement), so
	// this is our own handle, closed right after the assign; the job keeps
	// its own reference to the process.
	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME,
		false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		killChild()
		return nil, fmt.Errorf("acp: open process %d: %w", cmd.Process.Pid, err)
	}
	defer windows.CloseHandle(proc)

	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		killChild()
		return nil, fmt.Errorf("acp: assign process %d to job: %w", cmd.Process.Pid, err)
	}

	// The child is inside the job now; let it run. A nonzero NTSTATUS here
	// means the child already exited (the job is attached either way) or an
	// exotic failure conn.close's kill path would cover -- not fatal.
	ntResumeProcess.Call(uintptr(proc))

	return &processGroup{job: job}, nil
}

// kill terminates the agent and every process it spawned by closing the
// Job Object handle. Termination is asynchronous; the caller still reaps
// via cmd.Wait. A tree that already exited is the desired end state, so
// nothing here errors.
func (g *processGroup) kill() {
	if g == nil {
		return
	}
	g.closeOnce.Do(func() { _ = windows.CloseHandle(g.job) })
}

// release closes the job handle after the process was naturally reaped --
// the same once-guarded close as kill, so exactly one of the two paths ever
// closes it and natural exits don't leak one handle per conn. Any
// descendants left behind are still taken down by the kill-on-close
// semantics, matching the Unix process-group contract as closely as the
// OS allows.
func (g *processGroup) release() {
	g.kill()
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
