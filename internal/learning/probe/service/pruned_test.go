package probesvc

import (
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/store/storetest"
)

// The arm collector on a run whose events retention deleted: nothing, no panic.
func TestCollectEvents_PrunedRunHasNoEvents(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := storetest.SeedPrunedRun(t, st, "pruned-1", "final", 5)
	s := &Service{d: Deps{Store: st}}
	evs, final := s.collectEvents(p.RunID)
	if len(evs) != 0 || final != "" {
		t.Fatalf("collectEvents on a pruned run = %d events, %q; want none", len(evs), final)
	}
}
