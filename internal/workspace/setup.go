package workspace

import (
	"errors"
	"sync"
)

// setupLogCap is the maximum number of bytes of a script's merged
// stdout+stderr kept as tasks.setup_log (ADR-0024).
const setupLogCap = 64 << 10

// ErrSetupUnsupported is returned when a workspace has a setup/teardown
// script configured but no POSIX shell can be found to run it (Windows
// without Git for Windows' sh.exe, ADR-0028 D1). Scripts are never
// silently skipped.
var ErrSetupUnsupported = errors.New("setup/teardown scripts need `sh`; install Git for Windows or add its bin directory to PATH")

// tailBuffer keeps only the last cap bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	cap int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.cap; over > 0 {
		b.buf = append(b.buf[:0], b.buf[over:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
