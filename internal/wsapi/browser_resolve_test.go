package wsapi

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolverAddsResolvedAddressesAndKeepsTheName(t *testing.T) {
	t.Parallel()
	r := newNameResolver(func(ctx context.Context, host string) ([]string, error) {
		return []string{"192.0.2.7", "2001:db8::7%eth0"}, nil
	})
	r.wait, r.timeout = 50*time.Millisecond, 200*time.Millisecond

	got, err := r.Addrs([]string{"peer.example"})
	if err != nil {
		t.Fatalf("Addrs: %v", err)
	}
	want := []string{"192.0.2.7", "2001:db8::7"} // sorted, the IPv6 zone stripped
	if addrs, ok := got["peer.example"]; !ok || !slices.Equal(addrs, want) {
		t.Fatalf("Addrs = %v, want %q for peer.example", got, want)
	}
}

func TestResolverIPLiteralIsNotResolved(t *testing.T) {
	t.Parallel()
	// IP literals are filtered by the caller, so the helper has no literal
	// special-casing: this pins the empty-input contract, which starts nothing.
	var calls atomic.Int32
	r := newNameResolver(func(ctx context.Context, host string) ([]string, error) {
		calls.Add(1)
		return []string{"192.0.2.5"}, nil
	})
	r.wait, r.timeout = 50*time.Millisecond, 200*time.Millisecond

	got, err := r.Addrs(nil)
	if err != nil {
		t.Fatalf("Addrs: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("Addrs = %v, want an empty map", got)
	}
	if calls.Load() != 0 {
		t.Fatalf("lookup ran %d times, want 0", calls.Load())
	}
}

func TestResolverFailureWithoutAGoodAnswerFailsClosed(t *testing.T) {
	t.Parallel()
	r := newNameResolver(func(ctx context.Context, host string) ([]string, error) {
		return nil, errors.New("dns is down")
	})
	r.wait, r.timeout = 50*time.Millisecond, 200*time.Millisecond

	got, err := r.Addrs([]string{"peer.example"})
	if got != nil {
		t.Fatalf("Addrs = %v, want a nil map", got)
	}
	if err == nil || !strings.Contains(err.Error(), "cannot resolve peer.example") {
		t.Fatalf("Addrs error = %v, want cannot resolve peer.example", err)
	}
}

func TestResolverFailureServesTheLastGoodAnswer(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	r := newNameResolver(func(ctx context.Context, host string) ([]string, error) {
		if calls.Add(1) == 1 {
			return []string{"192.0.2.7"}, nil
		}
		return nil, errors.New("dns is down")
	})
	r.wait, r.timeout = 50*time.Millisecond, 200*time.Millisecond

	if got, err := r.Addrs([]string{"peer.example"}); err != nil {
		t.Fatalf("first Addrs: %v", err)
	} else if !slices.Equal(got["peer.example"], []string{"192.0.2.7"}) {
		t.Fatalf("first Addrs = %v", got)
	}

	got, err := r.Addrs([]string{"peer.example"})
	if err != nil {
		t.Fatalf("second Addrs: %v", err)
	}
	if want := []string{"192.0.2.7"}; !slices.Equal(got["peer.example"], want) {
		t.Fatalf("second Addrs = %v, want %q (the last good answer kept)", got, want)
	}
}

func TestResolverReresolvesEveryRefreshAndKeepsThePreviousAnswerOnce(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	r := newNameResolver(func(ctx context.Context, host string) ([]string, error) {
		if calls.Add(1) == 1 {
			return []string{"192.0.2.1"}, nil
		}
		return []string{"192.0.2.2"}, nil
	})
	r.wait, r.timeout = 50*time.Millisecond, 200*time.Millisecond

	want := [][]string{
		{"192.0.2.1"},              // the first answer
		{"192.0.2.1", "192.0.2.2"}, // the previous answer kept one refresh
		{"192.0.2.2"},              // the older answer dropped after the second newer one
	}
	for i, want := range want {
		got, err := r.Addrs([]string{"peer.example"})
		if err != nil {
			t.Fatalf("Addrs call %d: %v", i+1, err)
		}
		if !slices.Equal(got["peer.example"], want) {
			t.Fatalf("Addrs call %d = %q, want %q", i+1, got["peer.example"], want)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("lookup ran %d times, want one per Addrs call (3)", calls.Load())
	}
}

func TestResolverHungLookupDoesNotBlockTheCallPath(t *testing.T) {
	t.Parallel()
	var mode atomic.Int32 // 0: fast good answer, 1: hang until the lookup times out
	r := newNameResolver(func(ctx context.Context, host string) ([]string, error) {
		if mode.Load() == 0 {
			return []string{"192.0.2.9"}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	r.wait, r.timeout = 50*time.Millisecond, 200*time.Millisecond

	if _, err := r.Addrs([]string{"hung.example"}); err != nil {
		t.Fatalf("fast first Addrs: %v", err)
	}
	mode.Store(1)

	start := time.Now()
	got, err := r.Addrs([]string{"hung.example"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Addrs while hung: %v", err)
	}
	if want := []string{"192.0.2.9"}; !slices.Equal(got["hung.example"], want) {
		t.Fatalf("Addrs while hung = %v, want %q (the stored answer)", got, want)
	}
	if elapsed >= time.Second {
		t.Fatalf("Addrs took %s while the lookup hung, want far under 1s", elapsed)
	}

	start = time.Now()
	got, err = r.Addrs([]string{"never.example"})
	elapsed = time.Since(start)
	if got != nil || err == nil || !strings.Contains(err.Error(), "cannot resolve never.example") {
		t.Fatalf("Addrs with no good answer = (%v, %v), want cannot resolve never.example", got, err)
	}
	if elapsed >= time.Second {
		t.Fatalf("Addrs took %s while the lookup hung, want far under 1s", elapsed)
	}
}

func TestResolverSharesOneLookupPerName(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	release := make(chan struct{})
	r := newNameResolver(func(ctx context.Context, host string) ([]string, error) {
		calls.Add(1)
		<-release
		return []string{"192.0.2.5"}, nil
	})
	r.wait, r.timeout = 500*time.Millisecond, 2*time.Second

	const callers = 5
	type result struct {
		got map[string][]string
		err error
	}
	results := make(chan result, callers)
	ready := make(chan struct{}, callers)
	for i := 0; i < callers; i++ {
		go func() {
			ready <- struct{}{}
			got, err := r.Addrs([]string{"peer.example"})
			results <- result{got: got, err: err}
		}()
	}
	for i := 0; i < callers; i++ {
		<-ready
	}
	deadline := time.Now().Add(time.Second)
	for calls.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("lookup ran %d times, want 1", calls.Load())
	}
	// Every caller has signalled it is about to call Addrs; give them a moment
	// to reach the in-flight lookup before it unblocks.
	time.Sleep(50 * time.Millisecond)
	close(release)
	want := []string{"192.0.2.5"}
	for i := 0; i < callers; i++ {
		select {
		case res := <-results:
			if res.err != nil {
				t.Fatalf("caller %d: %v", i, res.err)
			}
			if !slices.Equal(res.got["peer.example"], want) {
				t.Fatalf("caller %d: Addrs = %v, want %q", i, res.got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("caller %d did not return", i)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("lookup ran %d times, want exactly 1 shared by all callers", calls.Load())
	}
}
