package server

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// waitUntil polls cond every few milliseconds until it's true or the
// timeout expires, failing the test in the latter case -- used here to
// synchronize with the writer's background drain goroutine without a
// fixed, potentially-flaky sleep.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(time.Millisecond)
	}
}

// blockingStore's CreateRequestLog blocks until release is closed,
// counting each call it enters -- lets a test hold the writer's single
// drain goroutine hostage so the channel buffer can be deterministically
// filled and overflowed, exercising the "queue full" path without racing
// a real database.
type blockingStore struct {
	release chan struct{}
	calls   atomic.Int64
}

func (b *blockingStore) CreateRequestLog(r store.RequestLog) (store.RequestLog, error) {
	b.calls.Add(1)
	<-b.release
	return r, nil
}

func TestRequestLogWriter_QueueFullDropsAndCounts(t *testing.T) {
	t.Parallel()

	bs := &blockingStore{release: make(chan struct{})}
	w := newRequestLogWriter(bs, 1)

	// The first row is picked up by the drain goroutine immediately and
	// blocks inside CreateRequestLog, freeing the channel buffer again.
	w.enqueue(store.RequestLog{SessionKey: "first"})
	waitUntil(t, time.Second, func() bool { return bs.calls.Load() == 1 })

	// Fills the buffer (capacity 1).
	w.enqueue(store.RequestLog{SessionKey: "second"})
	// The queue is now full (one in flight, one buffered): this one must
	// be dropped, not block.
	w.enqueue(store.RequestLog{SessionKey: "third"})

	if got := w.Dropped(); got != 1 {
		t.Fatalf("Dropped() = %d, want 1", got)
	}

	close(bs.release)
	w.Close()

	if got := bs.calls.Load(); got != 2 {
		t.Errorf("CreateRequestLog calls = %d, want 2 (the dropped row must never reach the store)", got)
	}
}

// erroringStore always fails, simulating a closed/unreachable database.
type erroringStore struct{}

func (erroringStore) CreateRequestLog(store.RequestLog) (store.RequestLog, error) {
	return store.RequestLog{}, errors.New("database is closed")
}

func TestRequestLogWriter_WriteFailureCounts(t *testing.T) {
	t.Parallel()

	w := newRequestLogWriter(erroringStore{}, 4)
	w.enqueue(store.RequestLog{})
	w.enqueue(store.RequestLog{})
	w.Close() // blocks until both rows have been attempted and failed

	if got := w.Dropped(); got != 2 {
		t.Errorf("Dropped() = %d, want 2", got)
	}
}

// TestRequestLogWriter_CloseDrainsGoroutine guards against a goroutine
// leak: Close must not return until the drain goroutine has actually
// exited, not merely until the channel is closed.
func TestRequestLogWriter_CloseDrainsGoroutine(t *testing.T) {
	t.Parallel()

	acceptingStore := &blockingStore{release: make(chan struct{})}
	close(acceptingStore.release) // never actually blocks
	w := newRequestLogWriter(acceptingStore, 4)
	for i := 0; i < 3; i++ {
		w.enqueue(store.RequestLog{})
	}
	w.Close()

	if got := acceptingStore.calls.Load(); got != 3 {
		t.Errorf("CreateRequestLog calls = %d, want 3", got)
	}
}
