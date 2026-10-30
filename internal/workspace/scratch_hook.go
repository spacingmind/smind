package workspace

import "sync"

// ScratchCleaner removes the scratch dir of a closed task; best effort.
type ScratchCleaner func(taskID int64)

// The cleaner is registered per Manager in a package-level table rather
// than a Manager field: the Manager struct is declared in workspace.go,
// which this change does not touch. Access is mutex-guarded, matching the
// notifier's SetNotifier/getNotifier shape.
var (
	scratchCleanerMu sync.Mutex
	scratchCleaners  = map[*Manager]ScratchCleaner{}
)

// SetScratchCleaner registers fn, called with a task's id once its row is
// archived or deleted -- never before the store write, so a failed close
// keeps the scratch dir. Nil-safe like SetNotifier.
func (m *Manager) SetScratchCleaner(fn ScratchCleaner) {
	if m == nil {
		return
	}
	scratchCleanerMu.Lock()
	defer scratchCleanerMu.Unlock()
	if fn == nil {
		delete(scratchCleaners, m)
		return
	}
	scratchCleaners[m] = fn
}

func (m *Manager) getScratchCleaner() ScratchCleaner {
	if m == nil {
		return nil
	}
	scratchCleanerMu.Lock()
	defer scratchCleanerMu.Unlock()
	return scratchCleaners[m]
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
