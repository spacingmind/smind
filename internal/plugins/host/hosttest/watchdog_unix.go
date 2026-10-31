//go:build unix

package hosttest

import (
	"os"
	"strconv"
	"syscall"
	"time"
)

// startWatchdog makes this helper exit once the test binary that launched the
// tree no longer exists. The top-level helper takes its parent as the root;
// anything it spawns is handed the root explicitly (childEnv): the helper's own
// environment is left alone, tests assert what a plugin may see in it.
func startWatchdog() {
	root, err := strconv.Atoi(os.Getenv(rootPIDEnv))
	if err != nil || root <= 1 {
		root = os.Getppid()
	}
	rootPID = root
	if root <= 1 {
		os.Exit(6) // already orphaned
	}
	go func() {
		for {
			time.Sleep(200 * time.Millisecond)
			if syscall.Kill(root, 0) != nil {
				os.Exit(6)
			}
		}
	}()
}

// childEnv is the environment of a process a helper spawns: its own plus the
// root pid.
func childEnv() []string { return append(os.Environ(), rootPIDEnv+"="+strconv.Itoa(rootPID)) }
