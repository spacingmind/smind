// Package storetest holds test helpers over internal/store for other packages'
// tests.
package storetest

import (
	"fmt"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// PrunedRun is a seeded run whose detailed events retention has deleted.
type PrunedRun struct {
	RunID, TaskID, FinalText string
	TaskRowID, WorkspaceID   int64
}

// SeedPrunedRun creates a workspace, an archived task and a finished run of it
// 60 days old with `events` stored text events, then prunes it the way the
// daemon does (phase 1 mark with finalText, phase 2 delete). The run row, usage
// and finalText stay.
func SeedPrunedRun(t testing.TB, st *store.Store, runID, finalText string, events int) PrunedRun {
	t.Helper()
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/pruned/" + runID, Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "archived", Kind: "task", ArchivedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	fin := now.Add(-60 * 24 * time.Hour)
	if _, err := st.CreateRun(store.Run{ID: runID, TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "done", Purpose: "user", StartedAt: fin.Add(-time.Minute), FinishedAt: &fin}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < events; i++ {
		if _, err := st.AppendRunEvent(runID, int64(i), fmt.Sprintf(`{"type":0,"text":"e%d"}`, i)); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := now.Add(-30 * 24 * time.Hour)
	if ok, err := st.MarkRunPruned(runID, finalText, now, cutoff); err != nil || !ok {
		t.Fatalf("mark pruned: %v %v", ok, err)
	}
	for {
		n, err := st.DeleteRunEventsBatch(runID, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	return PrunedRun{RunID: runID, FinalText: finalText, TaskRowID: task.ID, WorkspaceID: ws.ID}
}
