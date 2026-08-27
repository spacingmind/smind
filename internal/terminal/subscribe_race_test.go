package terminal

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// registerTestSession installs a session directly into reg's map,
// bypassing Create (and so the real pty/exec dependencies it needs), so
// this file can drive record/finish with tight, deterministic timing to
// stress Subscribe's backfill-vs-live race specifically -- adapted from
// internal/runs/subscribe_race_test.go's registerTestRun. See
// Registry.Subscribe's doc comment in registry.go for the invariant under
// test here.
func registerTestSession(reg *Registry, id string) *session {
	s := &session{
		id:          id,
		startedAt:   time.Now(),
		status:      StatusRunning,
		closedCh:    make(chan struct{}),
		subscribers: make(map[int]*subQueue),
	}
	reg.mu.Lock()
	reg.sessions[id] = s
	reg.mu.Unlock()
	return s
}

// TestRegistry_Subscribe_ConcurrentBackfillLiveRace is
// internal/runs/subscribe_race_test.go's
// TestRegistry_Subscribe_ConcurrentBackfillLiveRace adapted to this
// package's raw-byte-stream Event instead of discrete typed events: one
// producer appending a long, strictly increasing sequence of numbered
// "lines" (each its own record call, standing in for one PTY read) while
// many subscribers Subscribe at staggered, overlapping times -- some
// before production starts, some mid-stream, some after it (and after the
// session closes). Every subscriber's received byte stream,
// concatenated and split back into lines, must be a gapless,
// duplicate-free run ending at the final line, regardless of exactly when
// it joined: that's the property Subscribe's doc comment claims holding
// s.mu across the backfill-push and the live-registration guarantees.
// Run with -race to also catch any actual data race in
// record/Subscribe/finish.
func TestRegistry_Subscribe_ConcurrentBackfillLiveRace(t *testing.T) {
	t.Parallel()
	reg := New()
	s := registerTestSession(reg, "sess-1")

	const total = 300
	const subscribers = 40

	var wg sync.WaitGroup
	results := make([][]int, subscribers)
	errs := make([]error, subscribers)

	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i > 0 {
				time.Sleep(time.Duration(i%20) * time.Millisecond)
			}
			events, unsubscribe, err := reg.Subscribe("sess-1")
			if err != nil {
				errs[i] = err
				return
			}
			defer unsubscribe()

			var acc strings.Builder
			deadline := time.After(10 * time.Second)
		loop:
			for {
				select {
				case e, ok := <-events:
					if !ok {
						break loop
					}
					acc.Write(e.Data)
				case <-deadline:
					errs[i] = errTimeout
					break loop
				}
			}

			got, err := parseLines(acc.String())
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = got
		}(i)
	}

	for n := 0; n < total; n++ {
		reg.record(s, []byte(strconv.Itoa(n)+"\n"))
	}
	reg.finish(s)

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("subscriber %d: %v (got %d lines)", i, err, len(results[i]))
		}
	}
	for i, got := range results {
		if len(got) == 0 {
			t.Fatalf("subscriber %d: received no lines at all, want at least the final one", i)
		}
		for j := 1; j < len(got); j++ {
			if got[j] != got[j-1]+1 {
				t.Fatalf("subscriber %d: gap or duplicate between position %d (%d) and %d (%d): %v", i, j-1, got[j-1], j, got[j], got)
			}
		}
		if got[len(got)-1] != total-1 {
			t.Fatalf("subscriber %d: last line received = %d, want %d (missed the tail)", i, got[len(got)-1], total-1)
		}
	}
}

// parseLines splits acc on "\n" (dropping the trailing empty element from
// the final newline) and parses each line as an int.
func parseLines(acc string) ([]int, error) {
	lines := strings.Split(acc, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	out := make([]int, 0, len(lines))
	for _, l := range lines {
		n, err := strconv.Atoi(l)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

var errTimeout = &timeoutError{}

type timeoutError struct{}

func (*timeoutError) Error() string { return "timed out waiting for events" }
