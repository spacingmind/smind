package probesvc

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spacingmind/smind/internal/learning/probe/arm"
	"github.com/spacingmind/smind/internal/learning/projection"
)

const (
	janitorEvery = time.Hour
	orphanAge    = time.Hour
)

// Recover runs once at daemon start, when no probe can be live: probes left
// queued or running are failed, leftover arm and launch directories and
// projections are removed, and decided probes past retention are purged.
func (s *Service) Recover(_ context.Context) {
	st := s.d.Store
	if n, err := st.FailInterruptedProbes("interrupted by restart"); err != nil {
		slog.Warn("learning probe: fail interrupted probes", "err", err)
	} else if n > 0 {
		slog.Info("learning probe: failed interrupted probes", "count", n)
	}
	if _, err := arm.SweepOrphans(s.armsRoot(), 0, s.d.Now()); err != nil {
		slog.Warn("learning probe: sweep arm dirs", "err", err)
	}
	sweepChildren(s.launchRoot(), 0, s.d.Now())
	s.removeProjections()
	s.purgeNow(s.d.Now())
}

// Janitor purges decided probes past retention and sweeps abandoned
// directories every hour until ctx is done.
func (s *Service) Janitor(ctx context.Context) {
	every := s.janitorEvery
	if every <= 0 {
		every = janitorEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.sweepStale()
			s.purgeNow(s.d.Now())
		}
	}
}

// sweepStale removes arm and launch directories older than an hour that no
// running probe could own.
func (s *Service) sweepStale() {
	now := s.d.Now()
	if _, err := arm.SweepOrphans(s.armsRoot(), orphanAge, now); err != nil {
		slog.Warn("learning probe: sweep arm dirs", "err", err)
	}
	sweepChildren(s.launchRoot(), orphanAge, now)
}

// purgeNow deletes the rows, runs and events of every probe whose proposal
// was decided more than RetentionDays ago. A pending proposal keeps its
// probe.
func (s *Service) purgeNow(now time.Time) {
	days := s.d.Config().Probe.RetentionDays
	if days <= 0 {
		return
	}
	n, _, err := s.d.Store.PurgeDecidedProbes(now.AddDate(0, 0, -days))
	if err != nil {
		slog.Warn("learning probe: purge decided probes", "err", err)
		return
	}
	if n > 0 {
		slog.Info("learning probe: purged decided probes", "count", n)
	}
}

// sweepChildren removes direct children of root older than minAge.
func sweepChildren(root string, minAge time.Duration, now time.Time) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range ents {
		p := filepath.Join(root, e.Name())
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() || now.Sub(fi.ModTime()) < minAge {
			continue
		}
		removeAll(p)
	}
}

// removeProjections deletes every workspace's probe projection tree.
func (s *Service) removeProjections() {
	root := filepath.Join(s.d.Home, "learning", "probe-projections")
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range ents {
		if id, err := strconv.ParseInt(e.Name(), 10, 64); err == nil && id > 0 {
			if err := projection.Remove(root, id); err != nil {
				slog.Warn("learning probe: remove projections", "workspace", id, "err", err)
			}
		}
	}
}
