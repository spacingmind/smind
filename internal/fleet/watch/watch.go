// Package watch is the durable watcher of remote fleet runs (ADR-0048 F3b):
// one goroutine per open fleet_watches row long-polls fleet.run.wait on the
// remote host and, when the run settles, hands the facts to the notifier,
// whose store transaction closes the row. All state lives in the row, so a
// daemon restart simply resumes from the stored cursor.
package watch

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// Observer is the slice of the fleet client a watch needs.
type Observer interface {
	Wait(ctx context.Context, ref string, o client.CallOpts, p wire.RunWaitParams) (client.Host, wire.RunWaitResult, error)
	RunStatus(ctx context.Context, ref string, o client.CallOpts, runID string, taskID int64) (wire.RunStatus, error)
	Activity(ctx context.Context, ref string, o client.CallOpts, taskID int64, runID string, cursor int64, limit int) (wire.Activity, error)
}

// Store is the slice of the store a watch needs.
type Store interface {
	ListOpenFleetWatches() ([]store.FleetWatch, error)
	GetFleetWatch(id int64) (store.FleetWatch, error)
	GetFleetRemoteTask(hostID, remoteTaskID string) (store.FleetRemoteTask, error)
	UpdateFleetWatch(id int64, cursor string, failures int) error
	CloseFleetWatchIfOpen(id int64, reason string) (bool, error)
}

// Notifier delivers a watch's facts to the parent chat and closes the row;
// a nil return means the row is closed, an error means retry.
type Notifier interface {
	NotifyRemoteChild(w store.FleetWatch, f runs.RemoteChildFacts) error
}

// Hosts resolves a host id to its alias; ok=false means the host was
// removed or is unknown.
type Hosts interface {
	Alias(hostID string) (alias string, ok bool, err error)
}

// Options configures a Watcher; zero durations and limits take defaults.
type Options struct {
	Store    Store
	Observer Observer
	Notifier Notifier
	Hosts    Hosts

	BackoffMin, BackoffMax time.Duration // defaults 1s, 60s
	PollMin                time.Duration // minimum pause after a non-terminal wait, default 1s
	WaitTimeout            time.Duration // per fleet.run.wait long poll, default 20s
	FailureLimit           int           // default 3

	// Sleep is replaced by tests; the default returns early on ctx done.
	Sleep func(ctx context.Context, d time.Duration) error
}

const (
	refusalLimit  = 3
	activityLimit = 200
	activityPages = 10
)

// Watcher runs one goroutine per open watch.
type Watcher struct {
	o Options

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	running map[int64]struct{}
	wg      sync.WaitGroup
}

// New returns a Watcher with defaults applied.
func New(o Options) *Watcher {
	if o.BackoffMin <= 0 {
		o.BackoffMin = time.Second
	}
	if o.BackoffMax <= 0 {
		o.BackoffMax = 60 * time.Second
	}
	if o.PollMin <= 0 {
		o.PollMin = time.Second
	}
	if o.WaitTimeout <= 0 {
		o.WaitTimeout = 20 * time.Second
	}
	if o.FailureLimit <= 0 {
		o.FailureLimit = 3
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	return &Watcher{o: o, running: map[int64]struct{}{}}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Start lists the open watches and runs one goroutine per row. A second
// Start keeps the first one's context and only adds rows not yet running.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(ctx)
	}
	w.mu.Unlock()

	open, err := w.o.Store.ListOpenFleetWatches()
	if err != nil {
		return err
	}
	for _, fw := range open {
		w.spawn(fw)
	}
	return nil
}

// Add runs the watch with this id if it is open and not already running.
func (w *Watcher) Add(id int64) {
	fw, err := w.o.Store.GetFleetWatch(id)
	if err != nil || fw.State != store.FleetWatchOpen {
		return
	}
	w.spawn(fw)
}

// Stop cancels every goroutine and waits for them. It never writes or
// deletes a row: an interrupted watch stays open for the next start.
func (w *Watcher) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.wg.Wait()
}

func (w *Watcher) spawn(fw store.FleetWatch) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ctx == nil || w.ctx.Err() != nil {
		return
	}
	if _, ok := w.running[fw.ID]; ok {
		return
	}
	w.running[fw.ID] = struct{}{}
	w.wg.Add(1)
	ctx := w.ctx
	go func() {
		defer w.wg.Done()
		defer func() {
			w.mu.Lock()
			delete(w.running, fw.ID)
			w.mu.Unlock()
		}()
		w.run(ctx, fw)
	}()
}

func (w *Watcher) backoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := w.o.BackoffMin
	for i := 1; i < n; i++ {
		d *= 2
		if d >= w.o.BackoffMax {
			return w.o.BackoffMax
		}
	}
	return min(d, w.o.BackoffMax)
}

func opts(fw store.FleetWatch) client.CallOpts {
	var o client.CallOpts
	if fw.LocalTaskID != 0 {
		o.OriginTask = strconv.FormatInt(fw.LocalTaskID, 10)
	}
	return o
}

func (w *Watcher) run(ctx context.Context, fw store.FleetWatch) {
	failures, storedFailures, refusals := fw.Failures, fw.Failures, 0
	cursor, _ := strconv.ParseInt(fw.Cursor, 10, 64)
	o := opts(fw)

	for ctx.Err() == nil {
		alias, ok, err := w.o.Hosts.Alias(fw.HostID)
		if err == nil && !ok {
			w.deliverUnavailable(ctx, fw)
			return
		}
		var res wire.RunWaitResult
		if err == nil {
			_, res, err = w.o.Observer.Wait(ctx, fw.HostID, o, wire.RunWaitParams{
				RunID: fw.RemoteRunID, Cursor: cursor, TimeoutSeconds: int(w.o.WaitTimeout.Seconds()),
			})
		}
		if ctx.Err() != nil {
			// Stop or shutdown cancelled the call: not a failed observation.
			return
		}

		if err != nil {
			switch {
			case client.IsConnecting(err):
				if w.o.Sleep(ctx, w.o.BackoffMin) != nil {
					return
				}
			case client.IsCode(err, client.CodeUnknownHost):
				w.deliverUnavailable(ctx, fw)
				return
			case client.CountsAsFailure(err):
				failures++
				storedFailures = failures
				_ = w.o.Store.UpdateFleetWatch(fw.ID, strconv.FormatInt(cursor, 10), failures)
				if failures >= w.o.FailureLimit {
					w.deliver(ctx, fw, runs.RemoteChildFacts{Kind: runs.RemoteStatusUnknown, Failures: failures}, alias)
					return
				}
				if w.o.Sleep(ctx, w.backoff(failures)) != nil {
					return
				}
			case client.IsCode(err, wire.CodeRateLimited):
				if w.o.Sleep(ctx, w.o.BackoffMin) != nil {
					return
				}
			default:
				refusals++
				if refusals >= refusalLimit {
					w.deliver(ctx, fw, runs.RemoteChildFacts{Kind: runs.RemoteStatusUnknown, Failures: refusals}, alias)
					return
				}
				if w.o.Sleep(ctx, w.backoff(refusals)) != nil {
					return
				}
			}
			continue
		}

		failures, refusals = 0, 0
		if res.NextCursor != cursor || storedFailures != 0 {
			cursor = res.NextCursor
			if w.o.Store.UpdateFleetWatch(fw.ID, strconv.FormatInt(cursor, 10), 0) == nil {
				storedFailures = 0
			}
		}
		if !res.Done {
			if w.o.Sleep(ctx, w.o.PollMin) != nil {
				return
			}
			continue
		}
		w.deliverFinished(ctx, fw, alias, o, res)
		return
	}
}

func (w *Watcher) mirrorTitle(fw store.FleetWatch) string {
	t, err := w.o.Store.GetFleetRemoteTask(fw.HostID, fw.RemoteTaskID)
	if err != nil {
		return ""
	}
	return t.Title
}

func (w *Watcher) deliverUnavailable(ctx context.Context, fw store.FleetWatch) {
	alias, ok, err := w.o.Hosts.Alias(fw.HostID)
	if err != nil || !ok || alias == "" {
		alias = fw.HostID[:min(8, len(fw.HostID))]
	}
	w.deliver(ctx, fw, runs.RemoteChildFacts{Kind: runs.RemoteHostUnavailable}, alias)
}

// deliver fills the identity facts shared by every kind and hands them to
// the notifier; a watch that does not notify is closed here instead.
func (w *Watcher) deliver(ctx context.Context, fw store.FleetWatch, f runs.RemoteChildFacts, alias string) {
	f.Host, f.RemoteTaskID, f.RunID = alias, fw.RemoteTaskID, fw.RemoteRunID
	if f.Title == "" {
		f.Title = w.mirrorTitle(fw)
	}
	if !fw.Notify {
		reason := string(f.Kind)
		if f.Kind == runs.RemoteFinished {
			reason = "finished_silent"
		}
		_, _ = w.o.Store.CloseFleetWatchIfOpen(fw.ID, reason)
		return
	}
	for ctx.Err() == nil {
		if w.o.Notifier.NotifyRemoteChild(fw, f) == nil {
			return
		}
		// E.g. the parent queue is full: the row is still open, so retry
		// the notification without polling the remote again.
		if w.o.Sleep(ctx, w.o.BackoffMin) != nil {
			return
		}
	}
}

func (w *Watcher) deliverFinished(ctx context.Context, fw store.FleetWatch, alias string, o client.CallOpts, res wire.RunWaitResult) {
	f := runs.RemoteChildFacts{Kind: runs.RemoteFinished, State: res.State, StopReason: res.StopReason}
	// Enrichment is best effort: a finish is reported even if the extras fail.
	if st, err := w.o.Observer.RunStatus(ctx, fw.HostID, o, fw.RemoteRunID, 0); err == nil {
		f.FailureKind = st.FailureKind
		if st.CostUSD != nil {
			c := *st.CostUSD
			f.CostUSD = &c
		}
		if f.StopReason == "" {
			f.StopReason = st.StopReason
		}
	}
	f.LastText = w.lastText(ctx, fw, o)
	w.deliver(ctx, fw, f, alias)
}

func (w *Watcher) lastText(ctx context.Context, fw store.FleetWatch, o client.CallOpts) string {
	var events []wire.Event
	var cursor int64
	for range activityPages {
		act, err := w.o.Observer.Activity(ctx, fw.HostID, o, 0, fw.RemoteRunID, cursor, activityLimit)
		if err != nil {
			break
		}
		events = append(events, act.Events...)
		if !act.HasMore || act.NextCursor == cursor {
			break
		}
		cursor = act.NextCursor
	}
	var parts []string
	i := len(events) - 1
	for i >= 0 && events[i].Type != "text" {
		i--
	}
	for ; i >= 0; i-- {
		switch events[i].Type {
		case "text":
			parts = append(parts, events[i].Text)
			continue
		case "done", "usage":
			continue
		}
		break
	}
	for l, r := 0, len(parts)-1; l < r; l, r = l+1, r-1 {
		parts[l], parts[r] = parts[r], parts[l]
	}
	return strings.Join(parts, "")
}
