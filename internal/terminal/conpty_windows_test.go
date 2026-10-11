//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// These are the conPty-level scenarios from
// docs/plans/active/windows-conpty-wrapper.md (ADR-0022); the
// Registry-level ones live in windows_test.go.

// startTestConPty spawns cmd.exe with args on a fresh conPty inside a
// fresh kill-on-close job, closing both at cleanup.
func startTestConPty(t *testing.T, args ...string) (*conPty, *exec.Cmd, windows.Handle) {
	t.Helper()
	job, err := newKillOnCloseJob()
	if err != nil {
		t.Fatalf("newKillOnCloseJob() error = %v", err)
	}
	c, err := newConPty(80, 24, pseudoconsoleResizeQuirk)
	if err != nil {
		_ = windows.CloseHandle(job)
		t.Fatalf("newConPty() error = %v", err)
	}
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), args...)
	cmd.Dir = t.TempDir()
	if err := c.spawn(cmd, job); err != nil {
		_ = c.Close()
		_ = windows.CloseHandle(job)
		t.Fatalf("spawn() error = %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		_ = windows.CloseHandle(job)
		_, _ = cmd.Process.Wait()
	})
	return c, cmd, job
}

// readResult is what readUntilError reports: everything read, and the
// error that ended the reading.
type readResult struct {
	out string
	err error
}

func readUntilError(c *conPty) <-chan readResult {
	done := make(chan readResult, 1)
	go func() {
		var acc strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := c.Read(buf)
			acc.Write(buf[:n])
			if err != nil {
				done <- readResult{acc.String(), err}
				return
			}
		}
	}()
	return done
}

func TestConPty_ReadAfterCloseErrors(t *testing.T) {
	t.Parallel()
	c, _, _ := startTestConPty(t, "/k")

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	done := make(chan error, 2)
	go func() {
		_, err := c.Read(make([]byte, 16))
		done <- err
	}()
	go func() {
		_, err := c.Write([]byte("echo x\r"))
		done <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if !errors.Is(err, os.ErrClosed) {
				t.Fatalf("Read/Write after Close: error = %v, want os.ErrClosed", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Read/Write after Close did not return within 5s")
		}
	}
}

func TestConPty_CloseUnblocksPendingRead(t *testing.T) {
	t.Parallel()
	c, _, _ := startTestConPty(t, "/k")

	done := readUntilError(c)
	// Let the banner/prompt drain so the reader is parked in a Read on an
	// idle shell.
	time.Sleep(1 * time.Second)

	closed := make(chan struct{})
	go func() {
		_ = c.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return within 5s with a Read pending")
	}
	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("pending Read returned nil error after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending Read did not return within 5s of Close")
	}
}

func TestConPty_ExitDrainsToEOF(t *testing.T) {
	t.Parallel()
	// /c: no typed input to echo, so the marker can only come from output.
	c, cmd, _ := startTestConPty(t, "/c", "echo DRAIN-MARK")

	done := readUntilError(c)
	if _, err := cmd.Process.Wait(); err != nil {
		t.Fatalf("Process.Wait() error = %v", err)
	}
	// The exit path: only the pseudoconsole is closed; our pipe ends
	// stay open so the reader can drain.
	c.closeConsole()

	select {
	case r := <-done:
		if r.err != io.EOF {
			t.Fatalf("final Read error = %v, want io.EOF", r.err)
		}
		if !strings.Contains(r.out, "DRAIN-MARK") {
			t.Fatalf("output before EOF = %q, want it to contain DRAIN-MARK", r.out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reader did not reach EOF within 10s of closeConsole")
	}
}

func TestConPty_ResizeAfterCloseErrors(t *testing.T) {
	t.Parallel()
	c, _, _ := startTestConPty(t, "/k")

	if err := c.Resize(120, 40); err != nil {
		t.Fatalf("Resize() on a live console: error = %v", err)
	}
	c.closeConsole()
	if err := c.Resize(100, 30); !errors.Is(err, errConsoleClosed) {
		t.Fatalf("Resize() after closeConsole: error = %v, want errConsoleClosed", err)
	}
	_ = c.Close()
	if err := c.Resize(100, 30); !errors.Is(err, errConsoleClosed) {
		t.Fatalf("Resize() after Close: error = %v, want errConsoleClosed", err)
	}
}

var procIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func inJob(t *testing.T, pid int, job windows.Handle) bool {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatalf("OpenProcess(%d) error = %v", pid, err)
	}
	defer windows.CloseHandle(h)
	var result int32
	if r, _, err := procIsProcessInJob.Call(uintptr(h), uintptr(job), uintptr(unsafe.Pointer(&result))); r == 0 {
		t.Fatalf("IsProcessInJob(%d) error = %v", pid, err)
	}
	return result != 0
}

func TestConPty_ChildStartsInJob(t *testing.T) {
	t.Parallel()
	// The shell starts ping immediately -- with post-start job assignment
	// that grandchild could be spawned before the shell joined the job.
	_, cmd, job := startTestConPty(t, "/c", "ping -n 30 127.0.0.1")

	if !inJob(t, cmd.Process.Pid, job) {
		t.Fatal("shell is not in its job right after spawn")
	}

	var pingPid int
	deadline := time.Now().Add(10 * time.Second)
	for pingPid == 0 && time.Now().Before(deadline) {
		out, err := combinedOutput("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(`(Get-CimInstance Win32_Process -Filter "Name='PING.EXE'" | Where-Object { $_.ParentProcessId -eq %d }).ProcessId`, cmd.Process.Pid))
		if err == nil {
			pingPid, _ = strconv.Atoi(strings.TrimSpace(out))
		}
		if pingPid == 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if pingPid == 0 {
		t.Fatal("could not find the shell's ping child within 10s")
	}
	if !inJob(t, pingPid, job) {
		t.Fatalf("ping (pid %d) started by the shell is not in the job", pingPid)
	}
}

// TestWindows_ManySessionsNoCrossTalk churns sessions in parallel so
// handle values get recycled between them: each one must only ever see
// its own marker. A Read on a closed, recycled raw handle is exactly
// how one session's output used to land in another's. At 8x3 it did not
// catch a deliberately reintroduced read-after-close bug; at 16x6 it did
// (see the plan's Validation). One registry (own SQLite store) per
// worker: 16 writers on one store hit SQLITE_BUSY under full-suite load,
// and handle values are recycled process-wide anyway.
func TestWindows_ManySessionsNoCrossTalk(t *testing.T) {
	t.Parallel()
	const workers, rounds = 16, 6

	regs := make([]*Registry, workers)
	for w := range regs {
		regs[w] = newTestRegistry(t)
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		reg := regs[w]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				id, err := reg.Create(1, t.TempDir())
				if err != nil {
					t.Errorf("Create() error = %v", err)
					return
				}
				events, unsubscribe, err := reg.Subscribe(id)
				if err != nil {
					t.Errorf("Subscribe() error = %v", err)
					return
				}
				// The caret makes the printed marker differ from the
				// typed one, so only real output counts.
				mine := fmt.Sprintf("xtalk-%d-%d", w, r)
				typed := fmt.Sprintf("xtalk^-%d-%d", w, r)
				if err := reg.Write(id, []byte(writeLine("echo "+typed))); err != nil {
					t.Errorf("Write() error = %v", err)
				}
				var acc strings.Builder
				timeout := time.After(15 * time.Second)
			collect:
				for !strings.Contains(acc.String(), mine) {
					select {
					case e, ok := <-events:
						if !ok {
							break collect
						}
						acc.Write(e.Data)
					case <-timeout:
						break collect
					}
				}
				unsubscribe()
				_ = reg.Close(id)

				out := acc.String()
				if !strings.Contains(out, mine) {
					t.Errorf("session %s never saw its own marker %q", id, mine)
				}
				for _, m := range markersIn(out) {
					if m != mine {
						t.Errorf("session %s (marker %q) saw another session's output %q", id, mine, m)
					}
				}
			}
		}()
	}
	wg.Wait()
}

// markersIn returns every printed "xtalk-<w>-<r>" marker in out.
func markersIn(out string) []string {
	var found []string
	for rest := out; ; {
		i := strings.Index(rest, "xtalk-")
		if i < 0 {
			return found
		}
		rest = rest[i:]
		j := len("xtalk-")
		for j < len(rest) && (rest[j] == '-' || (rest[j] >= '0' && rest[j] <= '9')) {
			j++
		}
		found = append(found, rest[:j])
		rest = rest[j:]
	}
}
