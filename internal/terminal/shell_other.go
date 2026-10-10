//go:build !windows

package terminal

import "os"

// resolveShell picks the shell a new session runs: $SHELL if it's set
// and actually exists on disk, else /bin/bash if that exists, else
// /bin/sh (expected to exist on any Unix system this daemon runs on).
// This mirrors how a normal interactive terminal emulator picks a shell
// -- respecting the user's configured $SHELL when available, never
// erroring out just because it isn't set or points somewhere that no
// longer exists.
func resolveShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	for _, candidate := range []string{"/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "/bin/sh"
}
