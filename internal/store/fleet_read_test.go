package store

import (
	"fmt"
	"testing"
	"time"
)

func TestListRunEventsAfterSeq(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(Run{ID: "run-1", TaskID: task.ID, Provider: "glm", Prompt: "hi", Status: "done", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	for seq := int64(1); seq <= 5; seq++ {
		if _, err := s.AppendRunEvent("run-1", seq, fmt.Sprintf(`{"n":%d}`, seq)); err != nil {
			t.Fatal(err)
		}
	}

	seqs := func(after int64, limit int) []int64 {
		t.Helper()
		evs, err := s.ListRunEventsAfterSeq("run-1", after, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, e := range evs {
			out = append(out, e.Seq)
		}
		return out
	}
	eq := func(got []int64, want ...int64) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	eq(seqs(0, 2), 1, 2)
	eq(seqs(2, 10), 3, 4, 5)
	eq(seqs(5, 10))
	eq(seqs(0, 0), 1, 2, 3, 4, 5)
	eq(seqs(0, -1), 1, 2, 3, 4, 5)
}
