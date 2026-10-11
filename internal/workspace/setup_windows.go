//go:build windows

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// shCandidates lists sh.exe locations relative to the directory holding
// git.exe, covering the standard Git for Windows layouts (git.exe in
// <root>/cmd, <root>/bin or <root>/mingw64/bin) without hardcoding an
// install path.
var shCandidates = []string{"sh.exe", "../bin/sh.exe", "../usr/bin/sh.exe", "../../usr/bin/sh.exe"}

// findSh locates the POSIX shell: `sh` on PATH first, else Git for
// Windows' sh.exe derived from git.exe's location (ADR-0028 D1).
func findSh() (string, error) {
	if sh, err := exec.LookPath("sh"); err == nil {
		return sh, nil
	}
	// Derive Git for Windows' install dir from git.exe on PATH.
	git, err := exec.LookPath("git")
	if err != nil {
		return "", ErrSetupUnsupported
	}
	dir := filepath.Dir(git)
	for _, rel := range shCandidates {
		p := filepath.Join(dir, rel)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", ErrSetupUnsupported
}

// defaultShLookup is the Windows flavor of the shell seam; Manager tests
// inject a failing lookup to simulate a machine without one.
func defaultShLookup() (string, error) {
	return findSh()
}

// ntResumeProcess resumes a process created suspended (ntdll is not in
// golang.org/x/sys/windows).
var ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

// runScript is the Windows mirror of setup_unix.go's contract: the same
// POSIX script runs as `sh -c <script>` with cwd = worktree, and env
// entries ("K=V") are appended to the daemon's environment and only ever
// passed as environment variables, never interpolated into the script
// text. Note the env values are native Windows paths (e.g.
// C:\Users\...\worktrees\1\task); scripts that need POSIX paths use
// cygpath. It returns the tail (<= logCap bytes) of merged stdout+stderr.
// A timeout or cancellation kills the whole tree via a kill-on-close Job
// Object. checkSetupSupported has already verified sh exists; a cache
// miss (sh uninstalled mid-run) surfaces its error here.
func runScript(ctx context.Context, worktree, script string, env []string, timeout time.Duration, logCap int) (string, error) {
	sh, err := defaultShLookup()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(sh, "-c", script)
	cmd.Dir = worktree
	cmd.Env = append(os.Environ(), env...)
	out := &tailBuffer{cap: logCap}
	cmd.Stdout, cmd.Stderr = out, out
	// Bound the wait for output pipes held open by orphaned grandchildren.
	cmd.WaitDelay = 2 * time.Second

	job, err := startInKillOnCloseJob(cmd)
	if err != nil {
		return "", fmt.Errorf("start script: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case err := <-done:
		// Reap any stragglers left in the job (an orphaned grandchild).
		job.kill()
		if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
			return out.String(), fmt.Errorf("script failed: %w", err)
		}
		return out.String(), nil
	case <-timer:
		job.kill()
		<-done
		return out.String(), fmt.Errorf("script timed out after %s", timeout)
	case <-ctx.Done():
		job.kill()
		<-done
		return out.String(), fmt.Errorf("script cancelled: %w", ctx.Err())
	}
}

// startInKillOnCloseJob starts cmd created suspended in its own process
// group, assigns it to a fresh kill-on-close Job Object, and only then
// resumes it (NtResumeProcess) -- so there is no window in which the
// shell or anything it spawns runs outside the job: a grandchild cannot
// start before the shell is in the job. The returned job is the kill
// switch: closing its handle terminates the whole tree.
func startInKillOnCloseJob(cmd *exec.Cmd) (*killOnCloseJob, error) {
	job, err := newKillOnCloseJob()
	if err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_SUSPEND_RESUME|windows.PROCESS_TERMINATE,
		false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, proc)
		if err == nil {
			if r1, _, _ := ntResumeProcess.Call(uintptr(proc)); r1 != 0 {
				err = fmt.Errorf("NtResumeProcess: NTSTATUS 0x%x", r1)
			}
		}
	}
	_ = windows.CloseHandle(proc)
	if err != nil {
		killFailedStart(cmd, job)
		return nil, err
	}
	return &killOnCloseJob{handle: job}, nil
}

// killFailedStart reaps a child that was created but never resumed into
// the job successfully, and drops the job with it.
func killFailedStart(cmd *exec.Cmd, job windows.Handle) {
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	_ = windows.CloseHandle(job)
}

// killOnCloseJob holds a kill-on-close Job Object containing the shell
// and everything it spawns (see internal/terminal's identical helper --
// kept local per package until deduplicated).
type killOnCloseJob struct {
	handle    windows.Handle
	closeOnce sync.Once
}

// kill closes the job handle, making the OS terminate the whole tree.
// Once-guarded because the natural-exit path releases the same handle and
// Windows recycles handle values: a second unguarded CloseHandle could
// close an unrelated handle in the daemon.
func (j *killOnCloseJob) kill() {
	j.closeOnce.Do(func() { _ = windows.CloseHandle(j.handle) })
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
