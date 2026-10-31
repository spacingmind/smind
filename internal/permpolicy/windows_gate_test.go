package permpolicy

import (
	"runtime"
	"testing"
)

// skipPOSIXPathsOnWindows skips a test (or subtest) whose paths assume POSIX semantics
// (/tmp, /dev/null, /etc). The Windows-native equivalents live in the *_windows_* tests.
func skipPOSIXPathsOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only path; the Windows-native equivalent runs instead")
	}
}

// hostSplit splits a command under the host flavour: on Windows an unquoted backslash before a path
// character is literal, so a command that interpolates t.TempDir() (D:\x\y) keeps its path.
func hostSplit(cmd string) Parsed { return splitCommand(cmd, runtime.GOOS == "windows") }

// setHome points the process's home at dir for ResolvePath's `~` on every OS (HOME on POSIX, USERPROFILE on Windows).
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
