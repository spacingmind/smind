//go:build !windows

package listeners

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO (or any non-regular file) named like a listener file is ignored and
// never opened, so a reader cannot be made to block.
func TestReadIgnoresNonRegularFilesWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "1-evil-aaaaaaaa.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	link := filepath.Join(dir, "2-link-bbbbbbbb.json")
	target := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(target, []byte(`{"name":"x","addr":"127.0.0.1:4000","pid":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	done := make(chan []Entry, 1)
	go func() { done <- Read(dir, func(int) bool { return true }) }()
	select {
	case got := <-done:
		if len(got) != 0 {
			t.Fatalf("a FIFO and a symlink are ignored: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read blocked on a FIFO")
	}
}
