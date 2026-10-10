//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pseudoconsoleResizeQuirk is CreatePseudoConsole's undocumented
// PSEUDOCONSOLE_RESIZE_QUIRK flag (Windows Terminal's conpty-static.h),
// telling ConPTY the attached terminal reflows on resize itself so it
// doesn't fight that with its own repaint. Codex passes it
// unconditionally with a build-17763 minimum (refs/codex
// utils/pty/src/win/psuedocon.rs).
const pseudoconsoleResizeQuirk = 0x2

// procThreadAttributeJobList is PROC_THREAD_ATTRIBUTE_JOB_LIST
// (ProcThreadAttributeJobList = 13, an input attribute), which
// golang.org/x/sys/windows doesn't define.
const procThreadAttributeJobList = 0x0002000D

var errConsoleClosed = errors.New("terminal: pseudoconsole closed")

// conPty is smind's own ConPTY wrapper (ADR-0022), replacing
// charmbracelet/x/xpty + conpty on Windows. What it does differently, and
// why each matters:
//
//   - Our pipe ends are *os.File, not raw handles. os.File's Close cancels
//     and waits out an in-flight Read/Write and fails every later one, so
//     a Read racing or following Close can never touch a closed handle
//     whose value Windows has already recycled for another session's pipe.
//   - Close ordering is ours: closeConsole (the exit path) only closes the
//     pseudoconsole and lets the reader drain to EOF; Close (teardown)
//     closes our pipe ends first, so ClosePseudoConsole can't wait on an
//     undrained pipe when nothing is reading.
//   - hpc is guarded by mu, so Resize racing closeConsole gets an error
//     rather than calling ResizePseudoConsole on a freed HPCON.
//   - spawn creates the child already inside its Job Object.
type conPty struct {
	in  *os.File // our write end of the input pipe
	out *os.File // our read end of the output pipe

	mu  sync.Mutex
	hpc windows.Handle // 0 once the pseudoconsole is closed

	closeOnce sync.Once
}

// newConPty creates a pseudoconsole of cols x rows and the pipes to talk
// to it.
func newConPty(cols, rows int16, flags uint32) (*conPty, error) {
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("terminal: conpty: input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		closeHandles(inR, inW)
		return nil, fmt.Errorf("terminal: conpty: output pipe: %w", err)
	}

	var hpc windows.Handle
	err := windows.CreatePseudoConsole(windows.Coord{X: cols, Y: rows}, inR, outW, flags, &hpc)
	// conhost holds its own duplicates of the pty-side ends now; ours
	// must go, or the output pipe could never reach EOF.
	closeHandles(inR, outW)
	if err != nil {
		closeHandles(inW, outR)
		return nil, fmt.Errorf("terminal: conpty: create pseudoconsole: %w", err)
	}

	return &conPty{
		in:  os.NewFile(uintptr(inW), "conpty-in"),
		out: os.NewFile(uintptr(outR), "conpty-out"),
		hpc: hpc,
	}, nil
}

// spawn starts cmd attached to the pseudoconsole, inside job, and sets
// cmd.Process. cmd is never Start-ed through os/exec (CreateProcess needs
// the attribute list), so callers must wait via cmd.Process.Wait.
func (c *conPty) spawn(cmd *exec.Cmd, job windows.Handle) error {
	if cmd.Err != nil {
		return cmd.Err
	}

	c.mu.Lock()
	hpc := c.hpc
	c.mu.Unlock()
	if hpc == 0 {
		return errConsoleClosed
	}

	attrs, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return fmt.Errorf("terminal: conpty: attribute list: %w", err)
	}
	defer attrs.Delete()
	// PSEUDOCONSOLE takes the HPCON value itself, not a pointer to it;
	// reinterpret rather than convert so vet's unsafeptr check stays
	// meaningful elsewhere. The HPCON lives outside the Go heap.
	if err := attrs.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		*(*unsafe.Pointer)(unsafe.Pointer(&hpc)),
		unsafe.Sizeof(hpc),
	); err != nil {
		return fmt.Errorf("terminal: conpty: pseudoconsole attribute: %w", err)
	}
	// JOB_LIST: the child is created already in the job -- no window in
	// which it (or anything it starts) runs uncontained.
	jobs := []windows.Handle{job}
	if err := attrs.Update(
		procThreadAttributeJobList,
		unsafe.Pointer(&jobs[0]),
		unsafe.Sizeof(jobs[0]),
	); err != nil {
		return fmt.Errorf("terminal: conpty: job list attribute: %w", err)
	}

	app, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return err
	}
	var dir *uint16
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return err
		}
	}

	si := new(windows.StartupInfoEx)
	si.Cb = uint32(unsafe.Sizeof(*si))
	// Null std handles + USESTDHANDLES: the child must talk to the
	// pseudoconsole, not inherit whatever std handles the daemon has.
	si.Flags = windows.STARTF_USESTDHANDLES
	si.ProcThreadAttributeList = attrs.List()

	var pi windows.ProcessInformation
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(
		app, cmdline, nil, nil, false, flags,
		envBlock(cmd.Environ()), dir, &si.StartupInfo, &pi,
	); err != nil {
		return fmt.Errorf("terminal: conpty: create process: %w", err)
	}
	defer closeHandles(pi.Thread, pi.Process)

	// Opened while pi.Process is still held, so the pid can't have been
	// recycled; the os.Process gives the rest of the package the usual
	// Wait/Kill/Pid.
	p, err := os.FindProcess(int(pi.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return fmt.Errorf("terminal: conpty: open process: %w", err)
	}
	cmd.Process = p
	return nil
}

func (c *conPty) Read(p []byte) (int, error)  { return c.out.Read(p) }
func (c *conPty) Write(p []byte) (int, error) { return c.in.Write(p) }

// Resize resizes the pseudoconsole; an error once it has been closed.
func (c *conPty) Resize(width, height int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hpc == 0 {
		return errConsoleClosed
	}
	return windows.ResizePseudoConsole(c.hpc, windows.Coord{X: clampInt16(width), Y: clampInt16(height)})
}

// closeConsole closes the pseudoconsole, once. conhost flushes its final
// frame and exits, closing the last write end of the output pipe, so a
// reader still draining gets everything and then EOF. Used on its own by
// the exit path (ptySession.onExit); Close calls it last.
func (c *conPty) closeConsole() {
	c.mu.Lock()
	hpc := c.hpc
	c.hpc = 0
	c.mu.Unlock()
	if hpc != 0 {
		windows.ClosePseudoConsole(hpc)
	}
}

// Close tears everything down, once: our pipe ends first (see the type
// doc comment -- no raw-handle reuse, and with the read end gone
// conhost's remaining writes fail instead of ClosePseudoConsole waiting
// for a drain), then the pseudoconsole.
func (c *conPty) Close() error {
	c.closeOnce.Do(func() {
		_ = c.out.Close()
		_ = c.in.Close()
		c.closeConsole()
	})
	return nil
}

// envBlock builds a CreateProcess environment block (UTF-16, each entry
// NUL-terminated, the block double-NUL-terminated). env comes from
// exec.Cmd.Environ, already deduplicated with SYSTEMROOT ensured.
// Entries containing NUL can't be represented and are skipped.
func envBlock(env []string) *uint16 {
	var b []uint16
	for _, kv := range env {
		u, err := windows.UTF16FromString(kv)
		if err != nil {
			continue
		}
		b = append(b, u...)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	b = append(b, 0)
	return &b[0]
}

func clampInt16(v int) int16 {
	switch {
	case v < 1:
		return 1
	case v > math.MaxInt16:
		return math.MaxInt16
	}
	return int16(v)
}

func closeHandles(hs ...windows.Handle) {
	for _, h := range hs {
		_ = windows.CloseHandle(h)
	}
}
