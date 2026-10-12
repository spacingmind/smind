// Package testutil holds small helpers shared by tests across packages.
// It is imported only from _test.go files.
package testutil

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
)

// errorPrivilegeNotHeld is Windows' ERROR_PRIVILEGE_NOT_HELD (1314), what
// CreateSymbolicLink returns without SeCreateSymbolicLinkPrivilege (admin,
// or Developer Mode) -- the common case on CI runners and dev machines.
const errorPrivilegeNotHeld = syscall.Errno(1314)

// Link creates newname pointing at oldname for tests that exercise
// path-escape protection. It returns the kind of link it made.
//
// It is a symlink where the OS allows one. On Windows without symlink
// privilege, a link to a directory becomes a directory junction (mklink
// /J), which needs no privilege. That is the escape vector an attacker on
// Windows actually has, so escape tests keep running against it rather
// than being skipped. A link to a file can't be a junction; that case
// skips the test.
func Link(t testing.TB, oldname, newname string) string {
	t.Helper()
	err := os.Symlink(oldname, newname)
	if err == nil {
		return "symlink"
	}
	var errno syscall.Errno
	if runtime.GOOS != "windows" || !errors.As(err, &errno) || errno != errorPrivilegeNotHeld {
		t.Fatalf("symlink %s -> %s: %v", newname, oldname, err)
	}
	info, statErr := os.Stat(oldname)
	if statErr != nil || !info.IsDir() {
		t.Skipf("symlink %s -> %s needs SeCreateSymbolicLinkPrivilege on Windows, and a file link can't be a junction: %v", newname, oldname, err)
	}
	if out, jerr := exec.Command("cmd", "/c", "mklink", "/J", newname, oldname).CombinedOutput(); jerr != nil {
		t.Fatalf("mklink /J %s %s: %v: %s", newname, oldname, jerr, out)
	}
	return "junction"
}
