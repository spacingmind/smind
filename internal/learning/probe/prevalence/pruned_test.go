package prevalence

import (
	"testing"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/store/storetest"
	"path/filepath"
)

// A run whose events retention deleted yields no events and no error.
func TestLoadRun_PrunedRunHasNoEvents(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := storetest.SeedPrunedRun(t, st, "pruned-1", "final", 5)
	s := New(Deps{Store: st, DecodeEvent: runs.DecodeStoredEvent})
	evs, err := s.loadRun(p.RunID)
	if err != nil || len(evs) != 0 {
		t.Fatalf("loadRun on a pruned run = %d events, %v; want none and no error", len(evs), err)
	}
}
