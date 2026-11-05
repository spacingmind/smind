package store

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func seedRunEvents(t *testing.T, s *Store, runID string, n int) {
	t.Helper()
	ws, err := s.CreateWorkspace(Workspace{Path: "/p/" + runID, Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(Run{ID: runID, TaskID: tk.ID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		data := fmt.Sprintf(`{"type":1,"text":"e%d"}`, i)
		if i%4 == 1 {
			data = fmt.Sprintf(`{"type":1,"toolCallId":"c%d","toolInput":{"command":"echo %d"}}`, i%3, i)
		}
		if _, err := s.AppendRunEvent(runID, int64(i), data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStreamRunEventsMatchesList(t *testing.T) {
	s := newTestStore(t)
	seedRunEvents(t, s, "r1", 40)
	want, err := s.ListRunEvents("r1")
	if err != nil {
		t.Fatal(err)
	}
	var got []RunEvent
	if err := s.StreamRunEvents("r1", func(e RunEvent) error { got = append(got, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("streamed %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Seq != want[i].Seq || got[i].EventData != want[i].EventData {
			t.Fatalf("row %d differs", i)
		}
	}
	stop := fmt.Errorf("stop")
	n := 0
	if err := s.StreamRunEvents("r1", func(RunEvent) error { n++; return stop }); err != stop || n != 1 {
		t.Fatalf("callback error: err %v after %d rows, want stop after 1", err, n)
	}
}

func TestTailRunEvents(t *testing.T) {
	s := newTestStore(t)
	seedRunEvents(t, s, "r1", 10)
	got, err := s.TailRunEvents("r1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Seq != 7 || got[2].Seq != 9 {
		t.Fatalf("tail = %+v, want seq 7..9 ascending", got)
	}
	if all, _ := s.TailRunEvents("r1", 99); len(all) != 10 || all[0].Seq != 0 {
		t.Fatalf("tail beyond the run = %d rows", len(all))
	}
	if _, err := s.TailRunEvents("r1", 0); err == nil {
		t.Fatal("n=0 must be rejected")
	}
}

func TestToolCallInputEvents(t *testing.T) {
	s := newTestStore(t)
	seedRunEvents(t, s, "r1", 12)
	got, err := s.ToolCallInputEvents("r1", []string{"c1"}, 9)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for _, e := range got {
		seqs = append(seqs, e.Seq)
		if !strings.Contains(e.EventData, `"toolCallId":"c1"`) || e.Seq >= 9 {
			t.Fatalf("unexpected row %+v", e)
		}
	}
	if fmt.Sprint(seqs) != "[1]" { // input-bearing rows are i%4==1 (ids c1,c2,c0 for 1,5,9): only seq 1 is c1 below 9
		t.Fatalf("seqs = %v, want [1]", seqs)
	}
	if none, err := s.ToolCallInputEvents("r1", nil, 9); err != nil || none != nil {
		t.Fatalf("no ids = %v, %v", none, err)
	}
}

// TestRunEventsQueryPlans: the lazy-history reads walk the (run_id, seq) unique
// index: an index search, no table scan, no sort.
func TestRunEventsQueryPlans(t *testing.T) {
	s := newTestStore(t)
	for _, q := range []string{
		streamRunEventsSQL,
		tailRunEventsSQL,
		fmt.Sprintf(toolCallInputSQL, "?"),
	} {
		args := []any{"r"}
		for i := 1; i < strings.Count(q, "?"); i++ {
			args = append(args, 1)
		}
		rows, err := s.db.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		joined := strings.Join(plan, "; ")
		t.Logf("plan: %s", joined)
		if !strings.Contains(joined, "SEARCH run_events USING INDEX sqlite_autoindex_run_events_1 (run_id=?") || strings.Contains(joined, "TEMP B-TREE") || strings.Contains(joined, "SCAN") {
			t.Fatalf("query plan = %q: want an index search on (run_id, seq), no scan, no temp b-tree", joined)
		}
	}
}
