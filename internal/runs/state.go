package runs

// State returns runID's current status without copying its event history
// (a cheap check for per-request liveness gates). ErrNotFound if unknown.
func (reg *Registry) State(runID string) (RunStatus, error) {
	r, err := reg.get(runID)
	if err != nil {
		return RunStatus{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked(), nil
}
