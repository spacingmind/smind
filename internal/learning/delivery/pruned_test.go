package delivery

import (
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/store/storetest"
)

// Counting skill loads for a run whose events retention deleted is a no-op, not an error.
func TestCountLoads_PrunedRunIsANoOp(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := storetest.SeedPrunedRun(t, st, "pruned-1", "final", 5)
	s := &Service{d: Deps{Store: st}}
	if err := s.countLoads(runs.RunFinished{RunID: p.RunID}, p.WorkspaceID); err != nil {
		t.Fatalf("countLoads on a pruned run: %v", err)
	}
}
