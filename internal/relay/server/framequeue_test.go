package server

// framequeue_test.go targets the exact mechanism behind a real,
// reproduced bug: a daemon-side reconnect racing a not-yet-detected dead
// connection could lose an in-flight application message, because (a)
// frameQueue.pop returned an already-queued item without ever consulting
// ctx, so a pump whose context had *just* been cancelled by a superseding
// attachRoute call could still walk away with a frame meant for the new
// stream, and (b) a frame popped that way, or one grpc's Send silently
// swallowed into an already-dead connection, was gone for good -- pop()
// removes it from the queue unconditionally. Reproduced directly via
// internal/relay/server/server.go's attachRoute/OpenData with diagnostic
// logging against a real relay + internal/relay/bridge's own reconnect
// integration test before this fix landed.

import (
	"context"
	"errors"
	"testing"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

func TestFrameQueuePopRejectsAlreadyCancelledContextEvenWithItemReady(t *testing.T) {
	q := newFrameQueue(10)
	q.push(&relaypb.Frame{Sequence: 1})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled *before* pop is ever called.

	_, err := q.pop(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pop() with a pre-cancelled ctx = %v, want context.Canceled", err)
	}
	// The frame must not have been consumed: a pump whose context is
	// already cancelled must walk away with nothing, not the frame that
	// happened to be sitting there.
	if got := q.len(); got != 1 {
		t.Fatalf("queue len after rejected pop = %d, want 1 (frame must not be lost)", got)
	}
}

func TestFrameQueuePushFrontDeliversBeforeLaterFrames(t *testing.T) {
	q := newFrameQueue(10)
	q.push(&relaypb.Frame{Sequence: 2})
	// Simulates re-queueing a frame a superseded pump popped but never
	// actually delivered (server.go's OpenData does this on both the
	// pumpCtx.Err() check and a stream.Send error).
	q.pushFront(&relaypb.Frame{Sequence: 1})

	first, err := q.pop(context.Background())
	if err != nil {
		t.Fatalf("pop() first: %v", err)
	}
	if first.GetSequence() != 1 {
		t.Fatalf("first popped sequence = %d, want 1 (re-queued frame must come first)", first.GetSequence())
	}

	second, err := q.pop(context.Background())
	if err != nil {
		t.Fatalf("pop() second: %v", err)
	}
	if second.GetSequence() != 2 {
		t.Fatalf("second popped sequence = %d, want 2", second.GetSequence())
	}
}

func TestFrameQueuePushFrontEvictsNewestOnOverflow(t *testing.T) {
	q := newFrameQueue(2)
	q.push(&relaypb.Frame{Sequence: 10})
	q.push(&relaypb.Frame{Sequence: 11})
	// Queue is now full (cap 2): re-queueing a frame at the front must
	// keep it and the oldest-of-the-rest, evicting the newest -- the
	// re-queued frame is, by construction, older than everything already
	// in the queue (it was popped before any of them arrived).
	q.pushFront(&relaypb.Frame{Sequence: 9})

	if got := q.len(); got != 2 {
		t.Fatalf("queue len after overflowing pushFront = %d, want 2", got)
	}
	first, _ := q.pop(context.Background())
	second, _ := q.pop(context.Background())
	if first.GetSequence() != 9 || second.GetSequence() != 10 {
		t.Fatalf("got order %d, %d; want 9, 10", first.GetSequence(), second.GetSequence())
	}
}

// TestFrameQueuePushedCountsEvictedFrames: the buffer length stops at cap once
// it evicts, so "len == cap" cannot tell that all frames were processed; the
// pushed counter keeps counting (what the eviction test waits on).
func TestFrameQueuePushedCountsEvictedFrames(t *testing.T) {
	q := newFrameQueue(5)
	for i := 0; i < 8; i++ {
		q.push(&relaypb.Frame{Sequence: uint64(i + 1)})
		if i == 4 && (len(q.items) != 5 || q.pushed != 5) {
			t.Fatalf("after 5 pushes: len %d pushed %d, want 5/5", len(q.items), q.pushed)
		}
	}
	if len(q.items) != 5 || q.pushed != 8 || q.items[0].GetSequence() != 4 {
		t.Fatalf("after 8 pushes: len %d pushed %d first seq %d, want 5/8/4", len(q.items), q.pushed, q.items[0].GetSequence())
	}
}
