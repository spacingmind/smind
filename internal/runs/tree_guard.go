package runs

import "fmt"

// defaultMaxConcurrentRunsPerTree is the per-tree concurrency limit when
// SetMaxConcurrentRunsPerTree has not been called (or was called with a
// non-positive value), mirroring how maxDepth's unwired default behaves.
const defaultMaxConcurrentRunsPerTree = 4

// TreeBusyError is the per-tree concurrency guard's rejection: at most
// maxConcurrentRunsPerTree runs may be StatusRunning in one task tree
// (a root task plus all its descendants). Its message is model-readable,
// telling an agent the two ways to free a slot.
type TreeBusyError struct {
	RootTaskID int64
	Limit      int
	Running    int
}

func (e *TreeBusyError) Error() string {
	return fmt.Sprintf("runs: start: task tree concurrency limit reached (%d of %d runs already running in task tree #%d): wait for one to finish (task_wait) or stop one (task_stop), then try again", e.Running, e.Limit, e.RootTaskID)
}

// SetMaxConcurrentRunsPerTree sets the per-tree running-run cap. n <= 0
// restores the default. Guarded by reg.mu.
func (reg *Registry) SetMaxConcurrentRunsPerTree(n int) {
	reg.mu.Lock()
	reg.maxConcurrentRunsPerTree = n
	reg.mu.Unlock()
}

// maxConcurrentRunsPerTreeLocked returns the effective limit; callers must
// hold reg.mu.
func (reg *Registry) maxConcurrentRunsPerTreeLocked() int {
	if reg.maxConcurrentRunsPerTree > 0 {
		return reg.maxConcurrentRunsPerTree
	}
	return defaultMaxConcurrentRunsPerTree
}

// treeRootWalkHopLimit bounds parent_task_id traversal in treeRootOf, so
// corrupt data (a cycle or an unreasonably deep chain) can never loop or
// spiral.
const treeRootWalkHopLimit = 32

// treeRootOf resolves the root of taskID's tree by walking parent_task_id
// links through the store. It must be called BEFORE reg.mu is taken
// (parent links are immutable while a run exists, so resolving early is
// safe; the store call must not happen under the registry lock). The walk
// is bounded to 32 hops and cycle-safe: a corrupt cycle resolves to the
// lowest task id seen on it (or the task itself when it has no parent),
// never loops.
func (reg *Registry) treeRootOf(taskID int64) (int64, error) {
	root := taskID
	lowest := taskID
	seen := map[int64]bool{taskID: true}
	cur := taskID
	for i := 0; i < treeRootWalkHopLimit; i++ {
		if reg.treeRootStoreRead != nil {
			reg.treeRootStoreRead()
		}
		t, err := reg.st.GetTask(cur)
		if err != nil {
			return 0, fmt.Errorf("runs: start: resolve tree root of task %d: %w", taskID, err)
		}
		if t.ParentTaskID == nil || *t.ParentTaskID == 0 {
			return root, nil
		}
		parent := *t.ParentTaskID
		if seen[parent] {
			// Corrupt cycle: resolve to the lowest id seen so the same
			// cycle always maps to one deterministic root.
			return lowest, nil
		}
		seen[parent] = true
		if parent < lowest {
			lowest = parent
		}
		root = parent
		cur = parent
	}
	// Chain deeper than the hop limit: treat it like a cycle, deterministically.
	return lowest, nil
}

// runningInTreeLocked counts StatusRunning runs whose treeRoot equals
// rootTaskID; callers must hold reg.mu. It locks each run's own mu
// briefly (never two at once).
func (reg *Registry) runningInTreeLocked(rootTaskID int64) int {
	n := 0
	for _, existing := range reg.runs {
		if existing.treeRoot != rootTaskID {
			continue
		}
		existing.mu.Lock()
		running := existing.status == StatusRunning
		existing.mu.Unlock()
		if running {
			n++
		}
	}
	return n
}
