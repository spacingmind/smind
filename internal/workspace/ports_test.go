package workspace

import (
	"errors"
	"net"
	"strconv"
	"sync"
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

// freeRange finds a run of n consecutive loopback-free ports.
func freeRange(t *testing.T, n int) int {
	t.Helper()
	for base := 41000; base < 60000; base += 50 {
		ok := true
		for p := base; p < base+n; p++ {
			if !probePort(p) {
				ok = false
				break
			}
		}
		if ok {
			return base
		}
	}
	t.Fatal("no free port range")
	return 0
}

func TestPorts_AllocatesDistinctFreePorts(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	base := freeRange(t, 3)
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
	base := freeRange(t, 1)
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
	base := freeRange(t, 2)
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base)))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base + 1})
	if p, err := m.claimPort(portTestTasks(t, m, 1)[0]); err != nil || p != base+1 {
		t.Fatalf("p=%d err=%v, want %d", p, err, base+1)
	}
}

func TestPorts_ExhaustionFails(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	base := freeRange(t, 1)
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
	base := freeRange(t, n)
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
