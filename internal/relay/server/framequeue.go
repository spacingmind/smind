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
// It is bounded by frame count AND by payload bytes (maxBytes <= 0 disables
// the byte bound): whichever is hit first evicts the oldest frames, but the
// newest frame is always kept, so a single frame larger than maxBytes is
// still delivered (gRPC's own message limit bounds it).
// push never blocks — a dumb pipe degrades by eviction, not by stalling.
type frameQueue struct {
	mu       sync.Mutex
	notify   chan struct{}
	items    []*relaypb.Frame
	cap      int
	maxBytes int
	bytes    int
}

func newFrameQueue(capacity int) *frameQueue { return newByteBoundedFrameQueue(capacity, 0) }

func newByteBoundedFrameQueue(capacity, maxBytes int) *frameQueue {
	return &frameQueue{
		notify:   make(chan struct{}, 1),
		items:    make([]*relaypb.Frame, 0, capacity),
		cap:      capacity,
		maxBytes: maxBytes,
	}
}

func frameBytes(f *relaypb.Frame) int { return len(f.GetPayload()) }

// shrinkLocked evicts from the front while a bound is exceeded, never
// dropping the last remaining frame.
func (q *frameQueue) shrinkLocked() {
	for len(q.items) > 1 && (len(q.items) > q.cap || (q.maxBytes > 0 && q.bytes > q.maxBytes)) {
		q.bytes -= frameBytes(q.items[0])
		q.items = q.items[1:]
	}
}

func (q *frameQueue) push(f *relaypb.Frame) {
	q.mu.Lock()
	q.items = append(q.items, f)
	q.bytes += frameBytes(f)
	q.shrinkLocked()
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// pushFront re-queues a frame at the head of the queue -- for a frame that
// was already popped by a pump whose stream turned out to be (or just
// became) stale, so it isn't lost: it's delivered by whichever pump is
// actually current instead, exactly like a never-popped frame would be.
// Ahead of any frame pushed since, matching the order it would have kept
// had it never been popped at all.
func (q *frameQueue) pushFront(f *relaypb.Frame) {
	q.mu.Lock()
	q.items = append([]*relaypb.Frame{f}, q.items...)
	q.bytes += frameBytes(f)
	// Evict the newest overflow; this frame is the oldest and stays.
	for len(q.items) > 1 && (len(q.items) > q.cap || (q.maxBytes > 0 && q.bytes > q.maxBytes)) {
		last := len(q.items) - 1
		q.bytes -= frameBytes(q.items[last])
		q.items = q.items[:last]
	}
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// pop blocks until a frame is available or ctx is done. ctx is checked
// even when a frame is immediately available (not only while waiting):
// a pump whose context was *just* cancelled (its stream superseded by a
// reconnect, see attachRoute) must not still walk away with a frame that
// was sitting in the queue at that exact moment -- silently handing it to
// a pump about to abandon a dying stream is exactly how a frame gets lost
// (grpc's Send can return success into a connection that's already dead,
// with the actual failure only surfacing later or never, since sends are
// buffered asynchronously beneath the Send call).
func (q *frameQueue) pop(ctx context.Context) (*relaypb.Frame, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		q.mu.Lock()
		if len(q.items) > 0 {
			f := q.items[0]
			q.items = q.items[1:]
			q.bytes -= frameBytes(f)
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
