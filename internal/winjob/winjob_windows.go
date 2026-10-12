//go:build windows

// Package winjob runs child processes inside a kill-on-close Windows Job
// Object, so a whole process tree dies together: when the Job's handle is
// closed -- by Close, or by the OS when the daemon itself dies -- every
// process in it is terminated. It is the Windows replacement for the Unix
// process-group kill (kill(-pgid)) used for agent CLIs, setup scripts and
// MCP servers, which spawn grandchildren (npx -> node -> agent -> server)
// that Process.Kill alone would orphan.
//
// The child is created suspended, assigned to the Job, and only then
// resumed, so neither it nor anything it spawns can run outside the Job.
package winjob

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job is a kill-on-close Job Object. The zero value is unusable; get one
// from New, Attach or Start.
type Job struct {
	h         windows.Handle
	closeOnce sync.Once
}

// NewHandle creates a Job Object whose processes are killed when its last
// handle closes, and returns that handle -- deliberately the only one, so
// closing it is what triggers the kill. Callers that create the process
// themselves (internal/terminal passes it as PROC_THREAD_ATTRIBUTE_JOB_LIST)
// use this directly and own closing it exactly once.
func NewHandle() (windows.Handle, error) {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("winjob: create job object: %w", err)
	}
	if _, err := windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(h)
		return 0, fmt.Errorf("winjob: set kill-on-close: %w", err)
	}
	return h, nil
}

// Prepare makes cmd start suspended (plus any extra creation flags, e.g.
// windows.CREATE_NEW_PROCESS_GROUP), keeping whatever SysProcAttr the
// caller already set. Call it before cmd.Start, then Attach right after a
// successful Start -- or just use Start.
func Prepare(cmd *exec.Cmd, extraFlags uint32) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED | extraFlags
}

// ntResumeProcess resumes every thread of a suspended process (ntdll is not
// wrapped by golang.org/x/sys/windows; os/exec doesn't expose the main
// thread's handle either).
var ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

// Attach must be called exactly once, right after a successful cmd.Start of
// a cmd prepared by Prepare. It creates the Job, assigns the still-suspended
// child to it, and resumes the child. On any failure the child is killed and
// reaped (never left suspended and unwaited) and the error says which step
// failed.
func Attach(cmd *exec.Cmd) (*Job, error) {
	killChild := func() {
		_ = cmd.Process.Kill() // TerminateProcess works on a suspended process
		_, _ = cmd.Process.Wait()
	}

	h, err := NewHandle()
	if err != nil {
		killChild()
		return nil, err
	}

	// Opened by pid while cmd.Process still holds its own handle, so the pid
	// can't have been recycled. os/exec's handle doesn't carry
	// PROCESS_SET_QUOTA (AssignProcessToJobObject's requirement), so this is
	// our own handle, closed below; the Job keeps its own reference.
	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME,
		false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(h)
		killChild()
		return nil, fmt.Errorf("winjob: open process %d: %w", cmd.Process.Pid, err)
	}
	defer windows.CloseHandle(proc)

	if err := windows.AssignProcessToJobObject(h, proc); err != nil {
		_ = windows.CloseHandle(h)
		killChild()
		return nil, fmt.Errorf("winjob: assign process %d to job: %w", cmd.Process.Pid, err)
	}
	// A nonzero NTSTATUS means the child was NOT resumed: it would sit
	// suspended forever with its caller waiting on it, so treat it as a
	// failed start.
	if status, _, _ := ntResumeProcess.Call(uintptr(proc)); status != 0 {
		_ = windows.CloseHandle(h)
		killChild()
		return nil, fmt.Errorf("winjob: resume process %d: NTSTATUS 0x%x", cmd.Process.Pid, status)
	}
	return &Job{h: h}, nil
}

// Start is Prepare + cmd.Start + Attach for callers that have nothing to do
// between Start and Attach (agent connections wire pipes first and use the
// three steps separately).
func Start(cmd *exec.Cmd, extraFlags uint32) (*Job, error) {
	Prepare(cmd, extraFlags)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return Attach(cmd)
}

// Close terminates every process in the Job and releases it. Safe to call
// more than once and from several goroutines: the handle is closed exactly
// once, because Windows recycles handle values and a second CloseHandle
// could close an unrelated handle in the daemon. Termination is
// asynchronous; the caller still reaps the direct child with cmd.Wait.
// A nil Job is a no-op.
func (j *Job) Close() {
	if j == nil {
		return
	}
	j.closeOnce.Do(func() { _ = windows.CloseHandle(j.h) })
}
