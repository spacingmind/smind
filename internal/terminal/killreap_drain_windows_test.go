//go:build windows

package terminal

import (
	"os/exec"
	"testing"
	"time"
)

// TestKillAndReap_UnreadOutputDoesNotHang covers killAndReap's no-reader
// path (Create's error paths run it before any readLoop exists): with a
// child flooding the console and nobody reading the pty, ConPTY's output
// pipe fills, and ClosePseudoConsole can wait for that output to be
// drained -- Microsoft documents that Close may wait indefinitely when
// hOutput isn't drained, and xpty closes our read end only after that
// call. killAndReap must still return promptly. Passes on Windows 11
// 26200; it is here to catch builds where Close does block. Don't "fix"
// that with a goroutine draining the pty around Close: xpty reads raw
// handles, so a Read issued after Close can hit a recycled handle value
// and steal another session's output (seen as lost echoes in this
// package's parallel tests).
func TestKillAndReap_UnreadOutputDoesNotHang(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("cmd.exe", "/c", "for /L %i in (1,1,10000000) do @echo flood-line-%i")
	_, pty, err := spawnSleepTestProcessForCommand(t, cmd)
	if err != nil {
		t.Fatalf("startPty() error = %v", err)
	}
	// Let the flood fill the pipe buffer while nothing reads it.
	time.Sleep(1 * time.Second)

	done := make(chan struct{})
	go func() {
		pty.killAndReap()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("killAndReap did not return within 15s with unread pty output")
	}
}
