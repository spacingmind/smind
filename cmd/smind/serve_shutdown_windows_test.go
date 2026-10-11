//go:build windows

package main

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestServe_GracefulShutdown_CtrlBreak_Windows is AC3's best-effort
// console-control variant: a daemon started with CREATE_NEW_PROCESS_GROUP
// and signalled with CTRL_BREAK_EVENT (which Go delivers as SIGTERM) must
// run the same graceful shutdown as `smind stop` -- process exits,
// terminal session persisted `closed`. If the test process has no console
// (GenerateConsoleCtrlEvent needs one shared with the target), it skips:
// the API-based TestServe_GracefulShutdown is the mandatory scenario.
func TestServe_GracefulShutdown_CtrlBreak_Windows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	port := freePort(t)
	writePortConfig(t, home, port)
	bin := buildSmind(t)

	serve := exec.Command(bin, "serve")
	serve.Env = smindEnv(home)
	serve.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
	var serveOut stdBuffer
	serve.Stdout = &serveOut
	serve.Stderr = &serveOut
	if err := serve.Start(); err != nil {
		t.Fatalf("start smind serve: %v", err)
	}
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = serve.Process.Kill()
		}
		_, _ = serve.Process.Wait()
	})
	waitDaemonHealthy(t, port, 20*time.Second)

	client := dialShutdownDaemon(t, home, port)
	ctx := context.Background()
	var wsID struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(ctx, "workspace.create", map[string]any{"path": newShutdownRepo(t), "title": "W"}, &wsID); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	var task struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(ctx, "task.create", map[string]any{"workspaceId": wsID.ID, "title": "T"}, &task); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	var term struct {
		TerminalID string `json:"terminalId"`
	}
	if err := client.Call(ctx, "terminal.create", map[string]any{"taskId": task.ID}, &term); err != nil {
		t.Fatalf("terminal.create: %v", err)
	}

	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(serve.Process.Pid)); err != nil {
		t.Skipf("cannot deliver CTRL_BREAK_EVENT without a console (the API-based TestServe_GracefulShutdown covers AC3): %v", err)
	}

	waitProc := make(chan error, 1)
	go func() { waitProc <- serve.Wait() }()
	select {
	case <-waitProc:
		exited = true
	case <-time.After(30 * time.Second):
		t.Fatalf("daemon still running 30s after CTRL_BREAK_EVENT (serve log: %s)", serveOut.String())
	}

	row, err := openHomeStore(t, home).GetTerminalSession(term.TerminalID)
	if err != nil {
		t.Fatalf("GetTerminalSession(%q) error = %v", term.TerminalID, err)
	}
	if row.Status != "closed" {
		t.Fatalf("terminal session status after CTRL_BREAK_EVENT = %q, want %q", row.Status, "closed")
	}
}
