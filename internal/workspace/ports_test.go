package workspace

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func portTestTasks(t *testing.T, m *Manager, n int) []int64 {
	t.Helper()
	ws, err := m.store.CreateWorkspace(wsFixture())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, n)
	for i := range ids {
		task, err := m.store.CreateTask(taskFixture(ws.ID))
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = task.ID
	}
	return ids
}

// portBase is where fakePorts starts handing out port numbers. The numbers
// are never bound: allocation tests use a stub probe, so they cannot collide
// with a real listener (ephemeral ports on Linux span 32768-60999, which
// includes any fixed range a test could pick and probe).
const portBase = 41000

var portBlock atomic.Int64

// fakePorts gives m a probe that reports every port free except base+offset
// for each busyOffsets entry, and returns base, the first port number the
// test may use (a 100-number block of its own).
func fakePorts(m *Manager, busyOffsets ...int) int {
	base := portBase + int(portBlock.Add(1))*100
	taken := map[int]bool{}
	for _, off := range busyOffsets {
		taken[base+off] = true
	}
	m.portProbe = func(p int) bool { return !taken[p] }
	return base
}

func TestPorts_AllocatesDistinctFreePorts(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	base := fakePorts(m)
	m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base + 2})
	seen := map[int]bool{}
	for i, id := range portTestTasks(t, m, 3) {
		p, err := m.claimPort(id)
		if err != nil {
			t.Fatal(err)
		}
		if seen[p] || p != base+i {
			t.Fatalf("port %d (i=%d) seen=%v", p, i, seen)
		}
		seen[p] = true
	}
}

func TestPorts_ReleasedOnArchive(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	base := fakePorts(m)
	m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base})
	ids := portTestTasks(t, m, 2)
	if _, err := m.claimPort(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := m.claimPort(ids[1]); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("err = %v, want exhausted", err)
	}
	if _, err := m.ArchiveTask(ids[0]); err != nil {
		t.Fatal(err)
	}
	if p, err := m.claimPort(ids[1]); err != nil || p != base {
		t.Fatalf("after archive: p=%d err=%v", p, err)
	}
}

func TestPorts_SkipsPortInUse(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	base := fakePorts(m, 0)
	m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base + 1})
	if p, err := m.claimPort(portTestTasks(t, m, 1)[0]); err != nil || p != base+1 {
		t.Fatalf("p=%d err=%v, want %d", p, err, base+1)
	}
}

// TestProbePort_ReportsBoundPortBusy exercises the real probe: a port this
// test itself holds open must read as busy. (The inverse -- a closed port
// reads free -- is not asserted: another process may take it first.)
func TestProbePort_ReportsBoundPortBusy(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if probePort(l.Addr().(*net.TCPAddr).Port) {
		t.Fatal("probePort reported a port held open by this test as free")
	}
}

func TestPorts_ExhaustionFails(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	base := fakePorts(m)
	m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base})
	ids := portTestTasks(t, m, 2)
	if _, err := m.claimPort(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := m.claimPort(ids[1]); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("err = %v", err)
	}
}

func TestPorts_NoRangeNoPort(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	if p, err := m.claimPort(portTestTasks(t, m, 1)[0]); err != nil || p != 0 {
		t.Fatalf("p=%d err=%v", p, err)
	}
}

func TestPorts_ConcurrentCreatesUnique(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	const n = 8
	base := fakePorts(m)
	m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base + n - 1})
	ids := portTestTasks(t, m, n)
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[int]bool{}
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := m.claimPort(id)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if got[p] {
				t.Errorf("duplicate port %d", p)
			}
			got[p] = true
		}()
	}
	wg.Wait()
	if len(got) != n {
		t.Fatalf("got %d distinct ports", len(got))
	}
}

func wsFixture() store.Workspace {
	return store.Workspace{Path: "/nonexistent", Title: "w", RoutingPolicy: "hard"}
}

func taskFixture(wsID int64) store.Task {
	return store.Task{WorkspaceID: wsID, Title: "t", Status: "created"}
}

func TestCreateTask_PortExhaustionLeavesNothing(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	base := fakePorts(m)
	m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base})
	w := setupWorkspace(t, m, "")
	first, err := m.CreateTask(w.ID, nil, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateTask(w.ID, nil, "second"); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("err = %v, want exhausted", err)
	}
	tasks, _ := m.store.ListTasksByWorkspace(w.ID)
	if len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Fatalf("tasks = %+v, want only the first", tasks)
	}
	// The worktree dir root is shared by parallel tests, so look only for
	// this test's own discarded task by its slug.
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(*first.WorktreePath), "second-*")); len(left) != 0 {
		t.Fatalf("discarded worktree dirs left behind: %v", left)
	}
	out, err := runGitOutput(w.Path, "branch", "--list", "smind/task-*")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "task-second-") {
		t.Fatalf("discarded task's branch left behind: %q", out)
	}
}

// TestDiscardTask_DoesNotDeletePreExistingBranch: a branch CreateTask did not
// create (createdBranch == "") must survive discardTask.
func TestDiscardTask_DoesNotDeletePreExistingBranch(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, "")
	runGitT(t, w.Path, "branch", "user/feature")
	wt := filepath.Join(t.TempDir(), "wt")
	runGitT(t, w.Path, "worktree", "add", wt, "user/feature")
	branch := "user/feature"
	task, err := m.store.CreateTask(store.Task{WorkspaceID: w.ID, Title: "t", Status: "created", WorktreePath: &wt, Branch: &branch})
	if err != nil {
		t.Fatal(err)
	}
	m.discardTask(w, task, "")
	if out, _ := runGitOutput(w.Path, "branch", "--list", "user/feature"); !strings.Contains(out, "user/feature") {
		t.Fatalf("pre-existing branch deleted; branches = %q", out)
	}
	if dirExists(wt) {
		t.Fatal("worktree not removed")
	}
	if _, err := m.store.GetTask(task.ID); err == nil {
		t.Fatal("task row not removed")
	}
}

// TestCreateTask_ExistingBranchPortExhaustionKeepsUserBranch: a task created
// from an existing (user-owned) branch whose port claim fails is discarded
// without ever deleting that branch.
func TestCreateTask_ExistingBranchPortExhaustionKeepsUserBranch(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	base := fakePorts(m)
	m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base})
	w := setupWorkspace(t, m, "")
	runGitT(t, w.Path, "branch", "user/feature")
	if _, err := m.CreateTask(w.ID, nil, "keepbranch-first"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateTask(w.ID, nil, "keepbranch-second", WithExistingBranch("user/feature")); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("err = %v, want exhausted", err)
	}
	if out, _ := runGitOutput(w.Path, "branch", "--list", "user/feature"); !strings.Contains(out, "user/feature") {
		t.Fatalf("user's branch was deleted; branches = %q", out)
	}
}
