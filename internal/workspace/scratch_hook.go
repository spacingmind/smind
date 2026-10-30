package workspace

// ScratchCleaner removes the scratch dir of a closed task; best effort.
type ScratchCleaner func(taskID int64)

// SetScratchCleaner registers fn, called with a task's id once its row is
// archived or deleted -- never before the store write, so a failed close
// keeps the scratch dir. Nil-safe like SetNotifier.
func (m *Manager) SetScratchCleaner(fn ScratchCleaner) {
	if m == nil {
		return
	}
	m.scratchMu.Lock()
	defer m.scratchMu.Unlock()
	if fn == nil {
		m.scratchCleaner = nil
		return
	}
	m.scratchCleaner = fn
}

func (m *Manager) getScratchCleaner() ScratchCleaner {
	if m == nil {
		return nil
	}
	m.scratchMu.Lock()
	defer m.scratchMu.Unlock()
	return m.scratchCleaner
}

// closedScratch calls the registered cleaner, if any, for each id.
func (m *Manager) closedScratch(ids ...int64) {
	fn := m.getScratchCleaner()
	if fn == nil {
		return
	}
	for _, id := range ids {
		fn(id)
	}
}
