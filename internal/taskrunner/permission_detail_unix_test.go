//go:build !windows

package taskrunner

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Split out of permission_detail_test.go: syscall.Mkfifo does not exist on
// Windows, so even a runtime skip there fails `go vet` in the Windows job.
func TestPermissionDetail_NonRegularExistingFileIsNotRead(t *testing.T) {
	wt := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(wt, "pipe"), 0o644); err != nil {
		t.Skip(err)
	}
	if err := os.Mkdir(filepath.Join(wt, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, name := range []string{"pipe", "adir"} {
			d := BuildPermissionDetail(wt, writeCall(t, "edit", name, "x\n"))
			if !d.Redacted || d.OutsideWorktree {
				t.Errorf("%s: %+v", name, d)
				continue
			}
			assertHiddenOverwrite(t, d)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("BuildPermissionDetail blocked on a non-regular file")
	}
}
