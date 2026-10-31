package wsapi

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

type countingFleetWorkspaces struct {
	list  []store.Workspace
	calls map[int64]int
}

func (c *countingFleetWorkspaces) ListWorkspaces() ([]store.Workspace, error) { return c.list, nil }

func (c *countingFleetWorkspaces) DefaultBase(id int64) (string, bool, error) {
	c.calls[id]++
	return "origin/main", true, nil
}

func TestFleetSourceCachesOriginLookups(t *testing.T) {
	ws := &countingFleetWorkspaces{
		list:  []store.Workspace{{ID: 1, Title: "a", Path: t.TempDir()}, {ID: 2, Title: "b", Path: t.TempDir()}},
		calls: map[int64]int{},
	}
	clock := time.Unix(1_000_000, 0)
	src := &fleetSource{ws: ws, now: func() time.Time { return clock }}

	for range 2 {
		got, err := src.ListWorkspaces()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].DefaultBranch != "main" {
			t.Fatalf("workspaces = %+v", got)
		}
		clock = clock.Add(10 * time.Second)
	}
	for id, n := range ws.calls {
		if n != 1 {
			t.Fatalf("workspace %d: DefaultBase called %d times within the TTL, want 1", id, n)
		}
	}

	clock = clock.Add(31 * time.Second)
	if _, err := src.ListWorkspaces(); err != nil {
		t.Fatal(err)
	}
	for id, n := range ws.calls {
		if n != 2 {
			t.Fatalf("workspace %d: DefaultBase called %d times after the TTL, want 2", id, n)
		}
	}
}
