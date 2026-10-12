package workspace

import (
	"sync"
	"testing"
)

// TestTaskStamp_StrictlyIncreasingUnderConcurrency: every caller gets a
// distinct stamp even when the wall clock hasn't moved between calls (it
// moves in ~0.5-15.6 ms steps on Windows).
func TestTaskStamp_StrictlyIncreasingUnderConcurrency(t *testing.T) {
	t.Parallel()
	const goroutines, perG = 16, 1000

	var mu sync.Mutex
	seen := make(map[int64]bool, goroutines*perG)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]int64, 0, perG)
			prev := int64(0)
			for i := 0; i < perG; i++ {
				s := taskStamp()
				if s <= prev {
					t.Errorf("taskStamp() = %d after %d in the same goroutine, want strictly increasing", s, prev)
				}
				prev = s
				local = append(local, s)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, s := range local {
				if seen[s] {
					t.Errorf("taskStamp() returned %d twice", s)
				}
				seen[s] = true
			}
		}()
	}
	wg.Wait()
}

// TestCreateTask_ConcurrentSameTitle_UniqueNames is the parity plan's AC5
// scenario: tasks with one title created concurrently in one workspace all
// get distinct worktrees and branches. Before taskStamp they collided on
// Windows ("git worktree add ... already exists").
func TestCreateTask_ConcurrentSameTitle_UniqueNames(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w, err := m.CreateWorkspace(newTestRepo(t), "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	const n = 20
	paths := make([]string, n)
	branches := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := m.CreateTask(w.ID, nil, "same title")
			if err != nil {
				t.Errorf("CreateTask() #%d error = %v", i, err)
				return
			}
			paths[i], branches[i] = *task.WorktreePath, *task.Branch
		}()
	}
	wg.Wait()

	for name, values := range map[string][]string{"worktree path": paths, "branch": branches} {
		seen := map[string]bool{}
		for _, v := range values {
			if v == "" {
				continue // its CreateTask already reported an error
			}
			if seen[v] {
				t.Errorf("duplicate %s %q", name, v)
			}
			seen[v] = true
		}
	}
}
