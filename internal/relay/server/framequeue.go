package server

import (
	"context"
	"sync"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// frameQueue is a bounded FIFO of Frames. It is the single outbound path
// for one side of a route: while that side has a live stream, the stream's
// pump drains it; while it doesn't, it *is* the reconnect-grace buffer and
// keeps at most cap frames, evicting the oldest past that (ADR-0007 (a)).
// push never blocks — a dumb pipe degrades by eviction, not by stalling.
type frameQueue struct {
	mu     sync.Mutex
	notify chan struct{}
	items  []*relaypb.Frame
	cap    int
}

func newFrameQueue(capacity int) *frameQueue {
	return &frameQueue{
		notify: make(chan struct{}, 1),
		items:  make([]*relaypb.Frame, 0, capacity),
		cap:    capacity,
	}
}

func (q *frameQueue) push(f *relaypb.Frame) {
	q.mu.Lock()
	if len(q.items) == q.cap {
		q.items = q.items[1:] // evict oldest
	}
	q.items = append(q.items, f)
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// pop blocks until a frame is available or ctx is done.
func (q *frameQueue) pop(ctx context.Context) (*relaypb.Frame, error) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			f := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return f, nil
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-q.notify:
		}
	}
}

func (q *frameQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}
