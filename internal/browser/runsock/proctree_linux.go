//go:build linux

package runsock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// procfsTree reads process parentage from a procfs mount (root is overridable
// so tests can point it at a fixture directory).
type procfsTree struct{ root string }

// newProcfsTree returns a ProcTree backed by the procfs mount at root.
func newProcfsTree(root string) ProcTree { return procfsTree{root: root} }

// SystemProcTree returns the process tree of the running system.
func SystemProcTree() ProcTree { return procfsTree{root: "/proc"} }

func (t procfsTree) Info(pid int) (ProcInfo, error) {
	if pid <= 0 {
		return ProcInfo{}, ErrNoProcess
	}
	b, err := os.ReadFile(filepath.Join(t.root, strconv.Itoa(pid), "stat"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProcInfo{}, ErrNoProcess
		}
		return ProcInfo{}, fmt.Errorf("runsock: reading stat for pid %d: %w", pid, err)
	}
	info, err := parseProcStat(b)
	if err != nil {
		return ProcInfo{}, fmt.Errorf("runsock: pid %d: %w", pid, err)
	}
	return info, nil
}
