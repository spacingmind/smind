//go:build !windows

package files

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestFileStat_RejectsFIFO(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, err := Stat(root, "pipe"); done <- err }()
	select {
	case err := <-done:
		if !asInvalid(err) {
			t.Errorf("fifo: err = %v, want InvalidParamsError", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stat on a FIFO blocked (opened before the regular-file check)")
	}
}
