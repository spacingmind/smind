package store

import (
	"testing"
	"time"
)

func TestListWorkspaceUserEvents(t *testing.T) {
	t.Parallel()
	f := newCollectFixture(t, "propose")
	f.run(t, "u1", f.chat.ID, f.task.ID, collectBase, nil, "completed", "user", 3)
	f.run(t, "refl", f.chat.ID, f.task.ID, collectBase, nil, "completed", "reflect", 2)

	sys, err := f.s.CreateTask(Task{WorkspaceID: f.ws.ID, Title: "sys", Status: "created", Kind: "system"})
	if err != nil {
		t.Fatal(err)
	}
	f.run(t, "sys1", 0, sys.ID, collectBase, nil, "completed", "user", 2)

	other, err := f.s.CreateWorkspace(Workspace{Path: "/other", Title: "o", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	ot, err := f.s.CreateTask(Task{WorkspaceID: other.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	f.run(t, "o1", 0, ot.ID, collectBase, nil, "completed", "user", 4)

	rows, trunc, err := f.s.ListWorkspaceUserEvents(f.ws.ID, time.Now().Add(-time.Hour), 100)
	if err != nil || trunc {
		t.Fatalf("rows=%d trunc=%v err=%v", len(rows), trunc, err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want only the 3 user-run events", len(rows))
	}
	for _, r := range rows {
		if r.RunID != "u1" || r.ChatID != f.chat.ID {
			t.Fatalf("unexpected row %+v", r)
		}
	}
	if rows[0].ID < rows[1].ID {
		t.Fatal("rows must be newest first")
	}

	rows, trunc, err = f.s.ListWorkspaceUserEvents(f.ws.ID, time.Now().Add(-time.Hour), 2)
	if err != nil || !trunc || len(rows) != 2 {
		t.Fatalf("limit: rows=%d trunc=%v err=%v", len(rows), trunc, err)
	}

	// since filters on created_at; a run without a chat reports chat 0.
	if _, err := f.s.db.Exec(`UPDATE run_events SET created_at = ? WHERE run_id = 'u1' AND seq = 0`, time.Now().Add(-48*time.Hour).UTC()); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = f.s.ListWorkspaceUserEvents(f.ws.ID, time.Now().Add(-24*time.Hour), 100)
	if len(rows) != 2 {
		t.Fatalf("since: got %d rows, want 2", len(rows))
	}
	rows, _, _ = f.s.ListWorkspaceUserEvents(other.ID, time.Now().Add(-time.Hour), 100)
	if len(rows) != 4 || rows[0].ChatID != 0 {
		t.Fatalf("other workspace: %d rows, chat %d", len(rows), rows[0].ChatID)
	}
}
