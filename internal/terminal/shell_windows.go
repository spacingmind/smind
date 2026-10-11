//go:build windows

package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
)

// resolveShell picks the shell a new session runs on Windows: pwsh.exe
// if it's on PATH, else powershell.exe if that's on PATH, else
// %COMSPEC%, else cmd.exe. $SHELL is deliberately ignored -- it's a
// Unix-ism that, if set at all on Windows (some cross-platform tools set
// it), points at a Unix path meaningless here. Same never-error contract
// as the Unix flavor: cmd.exe is the guaranteed-last fallback, exactly
// like /bin/sh there.
func resolveShell() string {
	if shellOverride != "" {
		return shellOverride
	}
	return resolveShellWindows(exec.LookPath, os.Getenv("COMSPEC"), os.Getenv("SystemRoot"))
}

// shellOverride, when non-empty, is returned by resolveShell as-is. It is
// the Windows counterpart of the Unix tests setting $SHELL (which Windows
// ignores): helpers_windows_test.go sets it to cmd.exe in an init func, so
// it is written once before any test runs and only read afterwards.
// Never set in production.
var shellOverride string

// resolveShellWindows is the testable core of resolveShell: same
// decision order, but with the PATH lookup and COMSPEC value injected so
// TestResolveShell_Windows can exercise each branch deterministically
// without mutating the real environment. lookPath is exec.LookPath's
// signature; pass a fake that reports whether each candidate "exists on
// PATH".
func resolveShellWindows(lookPath func(string) (string, error), comspec, systemRoot string) string {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := lookPath(name); err == nil {
			return path
		}
	}
	if comspec != "" {
		return comspec
	}
	return filepath.Join(systemRoot, "System32", "cmd.exe")
}
