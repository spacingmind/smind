//go:build windows

package qaframes

import (
	"os"
	"os/exec"
	"strconv"
)

// Windows has no process groups here: the recorded PID is killed with its
// tree by taskkill. This path builds and vets but is not exercised on Windows.
func setProcessGroup(cmd *exec.Cmd) {}

func taskkill(pid int) {
	if pid <= 0 {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}

func terminate(pid int, group bool) { taskkill(pid) }
func forceKill(pid int, group bool) { taskkill(pid) }

func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
