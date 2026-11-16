//go:build windows

package runsock

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsTree reads a process's parent and creation time from a toolhelp
// snapshot and GetProcessTimes. Written for Windows; NOT run on a Windows
// machine in the lane that added it (see the 3b-2 plan).
type windowsTree struct{}

// SystemProcTree returns the process tree of the running system.
func SystemProcTree() ProcTree { return windowsTree{} }

func (windowsTree) Info(pid int) (ProcInfo, error) {
	if pid <= 0 {
		return ProcInfo{}, ErrNoProcess
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ProcInfo{}, fmt.Errorf("runsock: process snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	found := false
	var ppid uint32
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if int(pe.ProcessID) == pid {
			found, ppid = true, pe.ParentProcessID
			break
		}
	}
	if !found {
		return ProcInfo{}, ErrNoProcess
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return ProcInfo{}, ErrNoProcess
		}
		return ProcInfo{}, fmt.Errorf("runsock: open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return ProcInfo{}, fmt.Errorf("runsock: process times %d: %w", pid, err)
	}
	return ProcInfo{PPID: int(ppid), Start: uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)}, nil
}
