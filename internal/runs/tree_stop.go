package runs

import (
	"context"
	"fmt"
)

// StopAndWait is Stop plus determinism: it cancels runID's background
// context and then blocks until the run actually reached its terminal
// state (its closedCh fired -- subprocess gone, subscribers notified) or
// ctx is done, whichever first. Already-terminal and unknown-to-memory
// runs return nil (the caller's intent, "not still running", is
// satisfied); a run still not terminal when ctx expires returns the
// context's error. The tree-stop RPC (task.stopTree) uses this under one
// shared deadline so a slow run can't wedge the whole-tree stop.
func (reg *Registry) StopAndWait(ctx context.Context, runID string) error {
	r, err := reg.get(runID)
	if err != nil {
		return nil
	}
	r.mu.Lock()
	terminal := r.status != StatusRunning
	closedCh := r.closedCh
	r.mu.Unlock()
	if terminal {
		return nil
	}
	if err := reg.Stop(runID); err != nil {
		return fmt.Errorf("stop and wait: %w", err)
	}
	select {
	case <-closedCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
