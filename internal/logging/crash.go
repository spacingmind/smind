package logging

import (
	"runtime/debug"
	"sync"
)

// CrashOutput routes the Go runtime's fatal output (unhandled panic, fatal
// runtime error, SIGQUIT dump) into the daemon log, in addition to stderr,
// via runtime/debug.SetCrashOutput. The runtime duplicates the fd, so the
// *os.File can be closed right after the call: only the path is kept, and
// Reopen re-points the runtime at the file's current incarnation after a
// rotation.
type CrashOutput struct {
	mu   sync.Mutex
	path string
}

// InstallCrashOutput opens path for append and points the runtime's crash
// output at it.
func InstallCrashOutput(path string) (*CrashOutput, error) {
	c := &CrashOutput{path: path}
	if err := c.Reopen(); err != nil {
		return nil, err
	}
	return c, nil
}

// Reopen re-points the runtime at the current file at c.path (call it after
// the log rotated). On error the previous crash output stays in place.
func (c *CrashOutput) Reopen() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, err := openCrashFile(c.path)
	if err != nil {
		return err
	}
	debug.SetCrashOutput(f, debug.CrashOptions{})
	_ = f.Close()
	return nil
}

// Close unsets the crash output (fatal output goes to stderr only again).
func (c *CrashOutput) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	debug.SetCrashOutput(nil, debug.CrashOptions{})
}
