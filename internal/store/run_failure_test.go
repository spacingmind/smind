package store

import (
	"strings"
	"testing"
)

func TestRunColumnsAs(t *testing.T) {
	t.Parallel()

	got := runColumnsAs("r")
	if !strings.HasPrefix(got, "r.id, r.task_id") {
		t.Fatalf("runColumnsAs(r) = %q, want prefix %q", got, "r.id, r.task_id")
	}
	if strings.Count(got, ",") != strings.Count(runColumns, ",") {
		t.Fatalf("runColumnsAs(r) has %d commas, runColumns has %d", strings.Count(got, ","), strings.Count(runColumns, ","))
	}
}
