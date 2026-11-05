package executor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/store/storetest"
)

// wait on a run whose events retention deleted returns at once with no error:
// the summary history is finite, so the subscription drains and closes.
func TestWait_PrunedRunReturns(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := storetest.SeedPrunedRun(t, st, "pruned-1", "final", 5)
	reg, err := runs.New(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.CloseAll)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := (&Executor{reg: reg}).wait(ctx, p.RunID); err != nil {
		t.Fatalf("wait on a pruned run: %v", err)
	}
}
