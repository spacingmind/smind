package main

import (
	"context"
	"time"

	"github.com/spacingmind/smind/internal/stall"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsapi"
)

// stallTickPeriod is how often the stalled-tree detector runs: once a minute,
// but never slower than half the threshold (so a short threshold, e.g. 1
// minute in a live check, alerts within about N minutes) and never faster than
// every 5 seconds.
func stallTickPeriod(minutes int) time.Duration {
	p := time.Duration(minutes) * time.Minute / 2
	if p > time.Minute {
		p = time.Minute
	}
	if p < 5*time.Second {
		p = 5 * time.Second
	}
	return p
}

// wireStallAlert starts the stalled-tree detector when minutes > 0 (0 = off:
// nothing runs and task.tree never carries a stalled marker). Alerts go out as
// the task.stalled bus event, which the push tap and connected clients share;
// the same detector feeds task.tree's marker. The returned func stops it.
func wireStallAlert(ctx context.Context, db *store.Store, api *wsapi.API, minutes int) (stop func()) {
	if minutes <= 0 {
		return func() {}
	}
	det := stall.New(stall.Deps{
		Snapshot: db.StallSnapshot,
		Mark:     db.MarkStallAlerted,
		Alert: func(ep stall.Episode, minutes int) {
			api.PublishTaskStalled(ep.TaskID, ep.Title, minutes, ep.Since)
		},
		Now:     time.Now,
		Minutes: func() int { return minutes },
	})
	api.SetStalledSource(det.Current)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		det.Run(ctx, stallTickPeriod(minutes))
	}()
	return func() {
		cancel()
		<-done
	}
}
