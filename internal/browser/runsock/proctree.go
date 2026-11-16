package runsock

import "errors"

// ProcInfo is what the walk needs to know about one process.
type ProcInfo struct {
	PPID  int    // parent pid
	Start uint64 // an opaque start time: equal for the same process, different for a reused pid
}

// ProcTree reads process information.
type ProcTree interface {
	Info(pid int) (ProcInfo, error)
}

var (
	// ErrNoProcess: the process does not exist (any more).
	ErrNoProcess = errors.New("runsock: no such process")
	// ErrTreeUnsupported: this platform cannot read process information.
	ErrTreeUnsupported = errors.New("runsock: process information is not available on this platform")
)

// FuncProcTree adapts a function to ProcTree (a fake for tests, here and in other packages).
type FuncProcTree func(pid int) (ProcInfo, error)

func (f FuncProcTree) Info(pid int) (ProcInfo, error) { return f(pid) }
