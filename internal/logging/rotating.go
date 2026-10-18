// Package logging holds the daemon's own log file: a size-rotated writer
// (`smind serve` writes $SMIND_HOME/daemon.log next to stderr) and the
// tail/follow helpers `smind service logs` uses to read it back on every OS
// (ADR-0031 decision 7).
package logging

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"sync"
)

// FileName is the daemon log's name inside the smind home directory.
const FileName = "daemon.log"

// Rotating is an io.Writer over a log file that rotates itself: when a write
// would take the active file past maxSize, daemon.log becomes daemon.log.1,
// daemon.log.1 becomes daemon.log.2, and so on, and the file that would become
// number maxFiles is deleted. maxFiles counts the active file. Files are
// created 0600 (agent output can contain anything the user's repos do).
// It is safe for concurrent use.
type Rotating struct {
	mu       sync.Mutex
	path     string
	maxSize  int64
	maxFiles int
	f        *os.File
	size     int64
	closed   bool
	onRotate func()
}

// OpenRotating opens (creating if needed) the log at path for appending.
// maxSize and maxFiles must be >= 1.
func OpenRotating(path string, maxSize int64, maxFiles int) (*Rotating, error) {
	if maxSize < 1 || maxFiles < 1 {
		return nil, fmt.Errorf("log rotation needs maxSize >= 1 and maxFiles >= 1, got %d and %d", maxSize, maxFiles)
	}
	r := &Rotating{path: path, maxSize: maxSize, maxFiles: maxFiles}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

// OnRotate sets a callback run after every successful rotation, once the new
// active file is open. It is used to reopen a crash-output handle that still
// points at the renamed file. fn must not call back into this Rotating
// (write, close, OnRotate): it runs with r.mu held.
func (r *Rotating) OnRotate(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRotate = fn
}

func (r *Rotating) open() error {
	f, err := os.OpenFile(r.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	// An existing file keeps whatever mode it was created with; tighten it.
	// No-op (and ignored) on Windows.
	_ = f.Chmod(0o600)
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, fi.Size()
	return nil
}

// Write appends p, rotating first when it would not fit. A rotation that
// cannot reopen the file (directory briefly gone, disk full) loses only the
// lines written while it is unavailable: the next Write tries again, so the
// log recovers by itself.
func (r *Rotating) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, os.ErrClosed
	}
	if r.f != nil && r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		r.rotate()
	}
	if r.f == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate shifts the numbered files up and starts a new active file. Errors
// are tolerated step by step: the worst outcome is a log that grows past
// maxSize until the next successful rotation, or (when the new file cannot
// be created) lines lost until the next Write manages to reopen it.
func (r *Rotating) rotate() {
	_ = r.f.Close()
	r.f = nil
	if r.maxFiles == 1 {
		_ = os.Remove(r.path)
	} else {
		_ = os.Remove(r.path + "." + strconv.Itoa(r.maxFiles-1))
		for i := r.maxFiles - 2; i >= 1; i-- {
			from := r.path + "." + strconv.Itoa(i)
			if err := os.Rename(from, r.path+"."+strconv.Itoa(i+1)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				_ = os.Remove(from)
			}
		}
		if err := os.Rename(r.path, r.path+".1"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_ = os.Remove(r.path)
		}
	}
	if err := r.open(); err != nil {
		// Could not start a fresh file: r.f stays nil and the next Write retries.
		r.f, r.size = nil, 0
		return
	}
	if r.onRotate != nil {
		r.onRotate()
	}
}

// Close closes the active file. Writes after Close fail with os.ErrClosed.
func (r *Rotating) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// BestEffort wraps w so that its errors never reach the caller. The daemon
// logs to stderr and to the file through one io.MultiWriter; a full disk
// must not make the stderr copy (or the process) fail.
type BestEffort struct{ W io.Writer }

// Write forwards to W and always reports success.
func (b BestEffort) Write(p []byte) (int, error) {
	_, _ = b.W.Write(p)
	return len(p), nil
}
