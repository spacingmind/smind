//go:build !linux && !darwin

package runsock

// unsupportedTree answers every query with ErrTreeUnsupported.
type unsupportedTree struct{}

// SystemProcTree returns the process tree of the running system.
func SystemProcTree() ProcTree { return unsupportedTree{} }

func (unsupportedTree) Info(int) (ProcInfo, error) { return ProcInfo{}, ErrTreeUnsupported }
