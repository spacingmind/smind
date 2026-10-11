package workspace

import (
	"errors"
	"sync"
)

// setupLogCap is the maximum number of bytes of a script's merged
// stdout+stderr kept as tasks.setup_log (ADR-0024).
const setupLogCap = 64 << 10

// ErrSetupUnsupported is returned where setup/teardown scripts would have to
// run on a platform that cannot run them (Windows, v1).
var ErrSetupUnsupported = errors.New("setup scripts are not supported on Windows")

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
