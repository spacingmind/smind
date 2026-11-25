package wsapi

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolverReturnsResolvedAddressesSortedWithoutZones(t *testing.T) {
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

func TestResolverEmptyInputStartsNoLookup(t *testing.T) {
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

func TestResolverReresolvesEveryRefreshAndKeepsOldAddressesForTheRetentionWindow(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	r := newNameResolver(func(ctx context.Context, host string) ([]string, error) {
		if calls.Add(1) == 1 {
			return []string{"192.0.2.1"}, nil
		}
		return []string{"192.0.2.2"}, nil
	})
	r.wait, r.timeout = 50*time.Millisecond, 200*time.Millisecond
	clock := time.Unix(1_800_000_000, 0)
	var mu sync.Mutex
	r.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	steps := []struct {
		advance time.Duration
		want    []string
	}{
		{0, []string{"192.0.2.1"}},                                        // the first answer
		{time.Second, []string{"192.0.2.1", "192.0.2.2"}},                 // the old address is kept inside the window
		{r.retention - 2*time.Second, []string{"192.0.2.1", "192.0.2.2"}}, // still inside it (59 s after it was last seen)
		{2 * time.Second, []string{"192.0.2.2"}},                          // past the window: only the current answer
	}
	for i, st := range steps {
		advance(st.advance)
		got, err := r.Addrs([]string{"peer.example"})
		if err != nil {
			t.Fatalf("Addrs call %d: %v", i+1, err)
		}
		if !slices.Equal(got["peer.example"], st.want) {
			t.Fatalf("Addrs call %d = %q, want %q", i+1, got["peer.example"], st.want)
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("lookup ran %d times, want one per Addrs call (4)", calls.Load())
	}
}

// A sinkholed name answers 0.0.0.0: that is no address (a deny entry for it would
// reserve the port everywhere), so the name has no good answer.
func TestResolverIgnoresUnspecifiedAddresses(t *testing.T) {
	t.Parallel()
	r := newNameResolver(func(context.Context, string) ([]string, error) { return []string{"0.0.0.0", "::"}, nil })
	r.wait, r.timeout = 100*time.Millisecond, time.Second
	if got, err := r.Addrs([]string{"sink.example"}); err == nil {
		t.Fatalf("an answer of only unspecified addresses is no answer: %v", got)
	}
	r2 := newNameResolver(func(context.Context, string) ([]string, error) { return []string{"0.0.0.0", "192.0.2.4"}, nil })
	r2.wait, r2.timeout = 100*time.Millisecond, time.Second
	got, err := r2.Addrs([]string{"sink.example"})
	if err != nil || !slices.Equal(got["sink.example"], []string{"192.0.2.4"}) {
		t.Fatalf("got %v, %v; want only the real address", got, err)
	}
}

// A lookup that panics is a failed lookup, not a crashed daemon.
func TestResolverPanickingLookupIsAFailure(t *testing.T) {
	t.Parallel()
	r := newNameResolver(func(context.Context, string) ([]string, error) { panic("boom") })
	r.wait, r.timeout = 100*time.Millisecond, time.Second
	if _, err := r.Addrs([]string{"peer.example"}); err == nil {
		t.Fatal("a panicking lookup with no earlier answer must fail closed")
	}
}

// A name that already has a good answer does not make a call wait for a lookup
// that is still pending (a slow resolver must not tax every refresh).
func TestResolverDoesNotWaitForAPendingLookupWhenAnAnswerExists(t *testing.T) {
	t.Parallel()
	var slow atomic.Bool
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	r := newNameResolver(func(ctx context.Context, _ string) ([]string, error) {
		if slow.Load() {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil, errors.New("slow")
		}
		return []string{"192.0.2.3"}, nil
	})
	r.wait, r.timeout = 2*time.Second, 5*time.Second
	if _, err := r.Addrs([]string{"peer.example"}); err != nil {
		t.Fatal(err)
	}
	slow.Store(true)
	if _, err := r.Addrs([]string{"peer.example"}); err != nil { // starts the slow lookup, waits for it (<= wait)
		t.Fatal(err)
	}
}

// Names no longer asked for are forgotten.
func TestResolverForgetsNamesThatAreNoLongerAsked(t *testing.T) {
	t.Parallel()
	r := newNameResolver(func(context.Context, string) ([]string, error) { return []string{"192.0.2.8"}, nil })
	r.wait, r.timeout = 100*time.Millisecond, time.Second
	if _, err := r.Addrs([]string{"a.example", "b.example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Addrs([]string{"a.example"}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	_, kept := r.state["b.example"]
	r.mu.Unlock()
	if kept {
		t.Fatal("a removed link's name must not stay in the resolver state")
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
	// The lookup timeout is far above the asserted bound: only the wait bound can end the call early.
	r.wait, r.timeout = 50*time.Millisecond, 5*time.Second

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
	// Release the blocked lookup only once every other caller has joined it.
	deadline := time.Now().Add(5 * time.Second)
	for (r.joins.Load() != callers-1 || calls.Load() != 1) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond) // sleep-ok: polling for the callers to join
	}
	if r.joins.Load() != callers-1 || calls.Load() != 1 {
		t.Fatalf("joins %d lookups %d, want %d joins of one lookup", r.joins.Load(), calls.Load(), callers-1)
	}
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

func TestResolverUnmapsIPv4MappedAnswers(t *testing.T) {
	t.Parallel()
	r := newNameResolver(func(context.Context, string) ([]string, error) { return []string{"::ffff:192.0.2.9"}, nil })
	r.wait, r.timeout = 200*time.Millisecond, time.Second
	got, err := r.Addrs([]string{"peer.example"})
	if err != nil || len(got["peer.example"]) != 1 || got["peer.example"][0] != "192.0.2.9" {
		t.Fatalf("got %v, %v; want the plain IPv4 address", got, err)
	}
}
