package mirror

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

const (
	defaultInterval   = 10 * time.Second
	defaultMaxPerPass = 32

	stateRunning           = "running"
	stateWaitingPermission = "waiting_permission"
)

// Observer asks a host for the latest status of one of its runs.
type Observer interface {
	RunStatus(ctx context.Context, ref string, o client.CallOpts, runID string, taskID int64) (wire.RunStatus, error)
}

// Store is the slice of the store the refresher needs.
type Store interface {
	ListFleetRemoteTasks(hostID string) ([]store.FleetRemoteTask, error)
	PutFleetRemoteTaskAndWatch(t store.FleetRemoteTask, w *store.FleetWatch) (int64, error)
}

// Options configure a Refresher.
type Options struct {
	Store      Store
	Observer   Observer
	Interval   time.Duration
	MaxPerPass int
	Now        func() time.Time
}

// Refresher keeps the state and cost cache of remote-task mirrors fresh
// by asking each mirror's owner.
type Refresher struct {
	o Options
}

// New returns a Refresher with defaults filled in.
func New(o Options) *Refresher {
	if o.Interval <= 0 {
		o.Interval = defaultInterval
	}
	if o.MaxPerPass <= 0 {
		o.MaxPerPass = defaultMaxPerPass
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Refresher{o: o}
}

func terminal(state string) bool {
	return state == "done" || state == "error" || state == "stopped"
}

// Pass refreshes up to MaxPerPass non-terminal mirrors, oldest first, and
// returns how many rows it wrote. Any error leaves the row untouched: a
// stale cache, never a guess.
func (r *Refresher) Pass(ctx context.Context) (updated int) {
	rows, err := r.o.Store.ListFleetRemoteTasks("")
	if err != nil {
		return 0
	}
	type cand struct {
		row    store.FleetRemoteTask
		taskID int64
	}
	var cands []cand
	for _, row := range rows {
		if terminal(row.State) {
			continue
		}
		id, err := strconv.ParseInt(row.RemoteTaskID, 10, 64)
		if err != nil {
			continue
		}
		cands = append(cands, cand{row, id})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].row.UpdatedAt.Before(cands[j].row.UpdatedAt) })
	if len(cands) > r.o.MaxPerPass {
		cands = cands[:r.o.MaxPerPass]
	}

	for _, c := range cands {
		if ctx.Err() != nil {
			return updated
		}
		row := c.row
		var co client.CallOpts
		if row.LocalTaskID != 0 {
			co.OriginTask = strconv.FormatInt(row.LocalTaskID, 10)
		}
		st, err := r.o.Observer.RunStatus(ctx, row.HostID, co, "", c.taskID)
		if err != nil {
			continue
		}
		// The owner is the row's host id; a response can never retarget a
		// write: we put a copy of the row itself, changing only cache fields.
		next := row
		if st.State != "" {
			next.State = st.State
		}
		if st.WaitingPermission && next.State == stateRunning {
			next.State = stateWaitingPermission
		}
		if st.CostUSD != nil {
			v := *st.CostUSD
			next.CostUSD = &v
		}
		if next.State == row.State && sameCost(next.CostUSD, row.CostUSD) {
			continue
		}
		next.UpdatedAt = r.o.Now().UTC()
		if _, err := r.o.Store.PutFleetRemoteTaskAndWatch(next, nil); err != nil {
			continue
		}
		updated++
	}
	return updated
}

func sameCost(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Start runs Pass at once and then every Interval until ctx is done. The
// returned func cancels the loop and waits for it to exit.
func (r *Refresher) Start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.Pass(ctx)
		t := time.NewTicker(r.o.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.Pass(ctx)
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}
