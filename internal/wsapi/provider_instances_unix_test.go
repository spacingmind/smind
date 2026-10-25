//go:build !windows

package wsapi

import (
	"syscall"
	"testing"
)

func makeFIFO(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
}
