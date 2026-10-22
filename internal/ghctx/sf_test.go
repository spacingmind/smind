package ghctx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGithubSearch_CachedAndSingleFlight counts runner calls: 10
// concurrent identical Search calls produce one pr list invocation; a
// repeat within TTL adds none; advancing the injected clock past TTL
// makes the next call re-run; errors are never cached.
func TestGithubSearch_CachedAndSingleFlight(t *testing.T) {
	var mu sync.Mutex
	listCalls := 0
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		mu.Lock()
		listCalls++
		mu.Unlock()
		return `[{"number":5,"title":"T","state":"OPEN","author":{"login":"a"},"url":"u","updatedAt":"t"}]`, nil
	}}
	now := time.Now()
	c := New(WithRunner(s.run), WithNow(func() time.Time { return now }))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := c.Search(context.Background(), "/w", "pr", "q", 0)
			if err != nil || len(res.Items) != 1 {
				t.Errorf("Search: err=%v items=%d", err, len(res.Items))
			}
		}()
	}
	wg.Wait()
	if listCalls != 1 {
		t.Fatalf("pr list calls after 10 concurrent searches = %d, want 1", listCalls)
	}

	// Second identical call within TTL: no new call.
	if _, err := c.Search(context.Background(), "/w", "pr", "q", 0); err != nil {
		t.Fatal(err)
	}
	if listCalls != 1 {
		t.Fatalf("pr list calls within TTL = %d, want 1", listCalls)
	}

	// Past TTL: one new call.
	now = now.Add(11 * time.Second)
	if _, err := c.Search(context.Background(), "/w", "pr", "q", 0); err != nil {
		t.Fatal(err)
	}
	if listCalls != 2 {
		t.Fatalf("pr list calls after TTL expiry = %d, want 2", listCalls)
	}
}

// TestGithubSearch_ErrorsNotCached proves a failed search isn't cached:
// the next identical call runs the runner again and can succeed.
func TestGithubSearch_ErrorsNotCached(t *testing.T) {
	var calls atomic.Int32
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		if calls.Add(1) == 1 {
			return "", errors.New("gh: transient network failure")
		}
		return `[{"number":5,"title":"T","state":"OPEN","author":{"login":"a"},"url":"u","updatedAt":"t"}]`, nil
	}}
	c := New(WithRunner(s.run))
	if _, err := c.Search(context.Background(), "/w", "pr", "err", 0); err == nil {
		t.Fatal("first Search error = nil, want the runner error")
	}
	res, err := c.Search(context.Background(), "/w", "pr", "err", 0)
	if err != nil || len(res.Items) != 1 {
		t.Fatalf("second Search: err=%v items=%d, want success", err, len(res.Items))
	}
	if calls.Load() != 2 { // fail + succeed (the repo view is not counted)
		t.Fatalf("list calls = %d, want 2", calls.Load())
	}
}
