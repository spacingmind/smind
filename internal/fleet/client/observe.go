package client

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacingmind/smind/internal/fleet/wire"
)

// Observation is the outcome of one observation of a remote run. Three failed
// observations in a row mean "status unknown", never "finished" (ADR-0048 D4).
type Observation struct {
	Status   *wire.RunStatus // non-nil only on success
	Unknown  bool            // FailureLimit consecutive failures reached
	Failures int
}

type observer struct {
	mu       sync.Mutex
	failures map[string]int
}

// bump adds delta to the counter of key (reset when delta is 0) and returns it.
func (ob *observer) bump(key string, delta int) int {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	if ob.failures == nil {
		ob.failures = map[string]int{}
	}
	if delta == 0 {
		delete(ob.failures, key)
		return 0
	}
	ob.failures[key] += delta
	return ob.failures[key]
}

func (ob *observer) get(key string) int {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	return ob.failures[key]
}

// Observe asks a peer for the status of a run and counts consecutive failures.
// It only relays wire.RunStatus; it never reports "finished" by itself.
func (c *Client) Observe(ctx context.Context, ref string, o CallOpts, runID string, taskID int64) (Observation, error) {
	var out wire.RunStatus
	h, err := c.call(ctx, ref, o, wire.MethodRunStatus, func(p wire.Prov) any {
		return wire.RunStatusParams{Prov: p, RunID: runID, TaskID: taskID}
	}, &out)
	key := fmt.Sprintf("%s|%s|%d", h.HostID, runID, taskID)
	if err == nil {
		c.ob.bump(key, 0)
		return Observation{Status: &out}, nil
	}
	var e *Error
	if errors.As(err, &e) {
		switch {
		case e.Code == CodeHostUnavailable && e.connecting:
			return Observation{Failures: c.ob.get(key)}, err
		case e.Code == CodeHostUnavailable, e.Code == CodeTimeout:
			// counted below
		default:
			return Observation{Failures: c.ob.get(key)}, err
		}
	}
	n := c.ob.bump(key, 1)
	if n >= c.o.FailureLimit {
		return Observation{Unknown: true, Failures: n}, nil
	}
	return Observation{Failures: n}, err
}
