package push

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingSender blocks every Send until its context is cancelled.
type blockingSender struct{ entered chan struct{} }

func (b *blockingSender) Send(ctx context.Context, _ []Message) ([]Result, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

type signalSender struct{ called chan struct{} }

func (s *signalSender) Send(_ context.Context, msgs []Message) ([]Result, error) {
	out := make([]Result, len(msgs))
	for i, m := range msgs {
		out[i] = Result{Token: m.Token, OK: true}
	}
	s.called <- struct{}{}
	return out, nil
}

func perm(key string) Input { return Input{Kind: KindPermission, Key: key, TaskID: 1} }

func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s took longer than %v", what, d)
	}
}

func TestEnqueueDeliversAsynchronously(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	s := &signalSender{called: make(chan struct{}, 4)}
	h.n.d.Sender = s
	h.n.Start()
	h.n.Start() // idempotent
	defer h.n.Close()
	if !h.n.Enqueue(perm("k")) {
		t.Fatal("Enqueue = false")
	}
	select {
	case <-s.called:
	case <-time.After(2 * time.Second):
		t.Fatal("sender not called")
	}
}

func TestEnqueueNeverBlocksWhenSenderIsStuck(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	s := &blockingSender{entered: make(chan struct{}, 1)}
	h.n.d.Sender = s
	h.n.Start()
	var falses int
	start := time.Now()
	for i := 0; i < 1000; i++ {
		if !h.n.Enqueue(perm(fmt.Sprintf("k%d", i))) {
			falses++
		}
	}
	if time.Since(start) > time.Second {
		t.Fatalf("enqueue loop took %v", time.Since(start))
	}
	if falses < 700 {
		t.Fatalf("only %d enqueues refused", falses)
	}
	within(t, 2*time.Second, "Close", h.n.Close)
}

func TestCloseWithoutStartDoesNotHang(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	within(t, 2*time.Second, "Close", h.n.Close)
}

func TestEnqueueAfterCloseReturnsFalse(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.n.Start()
	h.n.Close()
	if h.n.Enqueue(perm("k")) {
		t.Fatal("Enqueue after Close = true")
	}
	h.n.Start() // must not relaunch or hang
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.n.Start()
	within(t, 2*time.Second, "double Close", func() { h.n.Close(); h.n.Close() })
}

func TestWorkerSurvivesPanic(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	s := &signalSender{called: make(chan struct{}, 4)}
	var calls atomic.Int32
	var mu sync.Mutex
	var logs []string
	n := NewNotifier(Deps{
		Registry: h.reg,
		Sender:   s,
		Task: func(int64) (TaskInfo, bool) {
			if calls.Add(1) == 1 {
				panic("boom")
			}
			return TaskInfo{Title: "t", Root: true}, true
		},
		Logf: func(f string, a ...any) {
			mu.Lock()
			logs = append(logs, fmt.Sprintf(f, a...))
			mu.Unlock()
		},
	})
	n.Start()
	defer n.Close()
	n.Enqueue(perm("a"))
	n.Enqueue(perm("b"))
	select {
	case <-s.called:
	case <-time.After(2 * time.Second):
		t.Fatal("second event not delivered after panic")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) == 0 || logs[0] != "push: notifier panic recovered" {
		t.Fatalf("logs = %v", logs)
	}
}

func TestQueueDropLogIsRateLimited(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	h.n.d.Sender = &blockingSender{entered: make(chan struct{}, 1)}
	h.n.Start()
	for i := 0; i < 5000; i++ {
		h.n.Enqueue(perm(fmt.Sprintf("k%d", i)))
	}
	h.n.Close()
	h.mu.Lock()
	defer h.mu.Unlock()
	lines := 0
	for _, l := range h.logs {
		if strings.Contains(l, "queue full") {
			lines++
		}
	}
	if lines == 0 || lines > 6 {
		t.Fatalf("queue full log lines = %d", lines)
	}
}
