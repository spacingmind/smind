//go:build !windows

package terminal

import (
	"os/exec"
	"testing"
)

// spawnSleepTestProcess spawns the resolved shell running `sleep
// seconds` via startPty -- the exact shape Create's killAndReap error
// paths used to leak a zombie with (see TestKillAndReap_NoZombieLeft).
// Unix flavor: force bash for determinism, `-c "sleep <n>"`.
func spawnSleepTestProcess(t *testing.T, seconds string) (*exec.Cmd, *ptySession, error) {
	t.Helper()
	forceTestShell(t)
	return spawnSleepTestProcessForCommand(t, exec.Command(resolveShell(), "-c", "sleep "+seconds))
}
