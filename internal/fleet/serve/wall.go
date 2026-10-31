package serve

import (
	"errors"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

// defaultWallEvery is the wall-sweep period when every <= 0.
const defaultWallEvery = 10 * time.Second

// SweepWall stops every live remote-origin run that has outlived its
// origin's wall budget (ADR-0048 S17) and stamps why. It reads the store,
// not timers, so the limit survives a daemon restart. A run whose grant
// cannot be resolved (host revoked, inbound link off) is skipped, not
// killed, and one whose stop failed is left for the next sweep: only a
// stop that succeeded is stamped and audited.
func SweepWall(ledger Ledger, auth Authorizer, audit AuditSink, stop func(runID string) error, now time.Time) (int, error) {
	runs, err := ledger.ListLiveOriginRuns("")
	if err != nil {
		return 0, err
	}
	stopped := 0
	for _, run := range runs {
		g, err := auth.Authorize(run.OriginHost, registry.OpRead, 0)
		if err != nil {
			continue
		}
		if now.Sub(run.StartedAt) <= time.Duration(g.MaxWallSeconds)*time.Second {
			continue
		}
		if err := stop(run.ID); err != nil {
			continue
		}
		// The stamp is why-after-the-fact: a run somehow still live was
		// not stopped, so there is nothing to stamp yet.
		if err := ledger.MarkRunWallLimit(run.ID); err != nil && !errors.Is(err, store.ErrFleetNotFound) {
			return stopped, err
		}
		if _, err := audit.AppendFleetAudit(store.FleetAuditRow{
			TS: now.UTC(), Direction: "in", HostID: run.OriginHost,
			Method: "wall_limit", RunID: run.ID, Outcome: "ok", Code: wire.StopReasonWallLimit,
		}); err != nil {
			return stopped, err
		}
		stopped++
	}
	return stopped, nil
}

// StartWallSweeper sweeps once now and then every period (10 s when
// period <= 0) until the returned stop is called; stop is idempotent and
// waits for the goroutine, like StartAuditSweeper.
func StartWallSweeper(ledger Ledger, auth Authorizer, audit AuditSink, stop func(runID string) error, period time.Duration, nowFn func() time.Time) (stopSweeper func()) {
	if period <= 0 {
		period = defaultWallEvery
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		sweep := func() { _, _ = SweepWall(ledger, auth, audit, stop, nowFn()) }
		sweep()
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				sweep()
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-exited
	}
}
