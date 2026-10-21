package store

import "testing"

func TestProbeSchemaExists(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	for _, table := range []string{"learning_probes", "learning_probe_arms"} {
		if n := countRows(t, s, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table); n != 1 {
			t.Fatalf("table %s missing", table)
		}
	}
}
