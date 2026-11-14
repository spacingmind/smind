//go:build !windows

package crash

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// crash-list-refuses-non-regular-newest: a planted FIFO named like a report
// must not block `smind doctor`.
func TestListRefusesNonRegularNewest(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "crash-20991231T235959.999-daemon-x.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create a fifo: %v", err)
	}
	done := make(chan Summary, 1)
	go func() { done <- List(dir) }()
	select {
	case s := <-done:
		if s.Err == "" {
			t.Fatalf("a FIFO as the newest report must be an error, got %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("List blocked on a FIFO")
	}
}
