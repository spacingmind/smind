package runs

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// registerTestRun installs a run directly into reg's map, bypassing Start
// (and so the real workspace/taskrunner dependencies it needs), so this
// file can drive record/finish with tight, deterministic timing to stress
// Subscribe's backfill-vs-live race specifically -- see Subscribe's doc
// comment in registry.go for the invariant under test here.
func registerTestRun(reg *Registry, id string) *run {
	r := &run{
		id:          id,
		status:      StatusRunning,
		startedAt:   time.Now(),
		cancel:      func() {},
		subscribers: make(map[int]*subQueue),
	}
	reg.mu.Lock()
	reg.runs[id] = r
	reg.mu.Unlock()
	return r
}

// TestRegistry_Subscribe_ConcurrentBackfillLiveRace runs one producer
// appending a long, strictly increasing sequence of events while many
// subscribers Subscribe at staggered, overlapping times -- some before
// production starts, some mid-stream, some after it (and after finish).
// Every subscriber's received sequence must be a gapless, duplicate-free
// run ending at the final event, regardless of exactly when it joined:
// that's the property Subscribe's doc comment claims holding r.mu across
// the backfill-push and the live-registration guarantees. Run with -race
// to also catch any actual data race in record/Subscribe/finish.
func TestRegistry_Subscribe_ConcurrentBackfillLiveRace(t *testing.T) {
	t.Parallel()
	reg := New()
	r := registerTestRun(reg, "run-1")

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
			events, unsubscribe, err := reg.Subscribe("run-1")
			if err != nil {
				errs[i] = err
				return
			}
			defer unsubscribe()

			var got []int
			deadline := time.After(10 * time.Second)
			for {
				select {
				case e, ok := <-events:
					if !ok {
						results[i] = got
						return
					}
					n, convErr := strconv.Atoi(e.Text)
					if convErr != nil {
						errs[i] = convErr
						return
					}
					got = append(got, n)
				case <-deadline:
					errs[i] = errTimeout
					results[i] = got
					return
				}
			}
		}(i)
	}

	for n := 0; n < total; n++ {
		reg.record(r, Event{Type: taskrunner.EventTypeText, Text: strconv.Itoa(n)})
	}
	reg.finish(r, nil)

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("subscriber %d: %v (got %d events)", i, err, len(results[i]))
		}
	}
	for i, got := range results {
		if len(got) == 0 {
			t.Fatalf("subscriber %d: received no events at all, want at least the final one", i)
		}
		for j := 1; j < len(got); j++ {
			if got[j] != got[j-1]+1 {
				t.Fatalf("subscriber %d: gap or duplicate between position %d (%d) and %d (%d): %v", i, j-1, got[j-1], j, got[j], got)
			}
		}
		if got[len(got)-1] != total-1 {
			t.Fatalf("subscriber %d: last event received = %d, want %d (missed the tail)", i, got[len(got)-1], total-1)
		}
	}
}

var errTimeout = &timeoutError{}

type timeoutError struct{}

func (*timeoutError) Error() string { return "timed out waiting for events" }
