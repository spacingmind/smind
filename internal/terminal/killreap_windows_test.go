//go:build windows

package terminal

import (
	"os/exec"
	"strconv"
	"testing"
)

// spawnSleepTestProcess spawns a long-running process via startPty --
// the exact shape Create's killAndReap error paths used to leak an
// un-reaped process with (see TestKillAndReap_NoZombieLeft). Windows
// flavor: `ping -n <count> 127.0.0.1` is the portable long-runner.
func spawnSleepTestProcess(t *testing.T, seconds string) (*exec.Cmd, *ptySession, error) {
	t.Helper()
	n, err := strconv.Atoi(seconds)
	if err != nil {
		return nil, nil, err
	}
	count := strconv.Itoa(n*10 + 1)
	return spawnSleepTestProcessForCommand(t, exec.Command("ping", "-n", count, "127.0.0.1"))
}
