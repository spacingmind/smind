//go:build !windows

package terminal

import (
	"testing"
	"time"
)

// TestRegistry_CtrlC_InterruptsForegroundProcess proves a ^C byte written to
// the PTY reaches the foreground process as SIGINT (the shell reports exit
// status 130), which needs the shell to own a session and controlling tty.
func TestRegistry_CtrlC_InterruptsForegroundProcess(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)

	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Close(id) })

	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg.Write(id, []byte(writeLine("sleep 60"))); err != nil {
		t.Fatalf("Write(sleep) error = %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := reg.Write(id, []byte("\x03")); err != nil {
		t.Fatalf("Write(^C) error = %v", err)
	}
	if err := reg.Write(id, []byte(writeLine("echo ctrlc-rc=$?"))); err != nil {
		t.Fatalf("Write(echo) error = %v", err)
	}

	collectUntil(t, events, "ctrlc-rc=130", 8*time.Second)
}
