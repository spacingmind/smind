//go:build darwin

package runsock

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// darwinTree reads process parentage via sysctls.
type darwinTree struct{}

// SystemProcTree returns the process tree of the running system.
func SystemProcTree() ProcTree { return darwinTree{} }

func (darwinTree) Info(pid int) (ProcInfo, error) {
	if pid <= 0 {
		return ProcInfo{}, ErrNoProcess
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		// ENOENT/ESRCH say the pid is unknown; EIO is what x/sys returns
		// when the kernel answers with zero bytes (also a missing pid).
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH) || errors.Is(err, unix.EIO) {
			return ProcInfo{}, ErrNoProcess
		}
		return ProcInfo{}, fmt.Errorf("runsock: kern.proc.pid for pid %d: %w", pid, err)
	}
	if kp.Proc.P_pid == 0 || int(kp.Proc.P_pid) != pid {
		return ProcInfo{}, ErrNoProcess
	}
	return ProcInfo{
		PPID:  int(kp.Eproc.Ppid),
		Start: uint64(kp.Proc.P_starttime.Sec)*1_000_000 + uint64(kp.Proc.P_starttime.Usec),
	}, nil
}
