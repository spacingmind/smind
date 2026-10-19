package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestRuns_PurposeRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(Run{ID: "u1", TaskID: task.ID, Provider: "glm", Status: "done", StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(Run{ID: "r1", TaskID: task.ID, Provider: "glm", Status: "done", StartedAt: base.Add(time.Minute), Purpose: "reflect"}); err != nil {
		t.Fatal(err)
	}
	u, _ := s.GetRun("u1")
	r, _ := s.GetRun("r1")
	if u.Purpose != "user" || r.Purpose != "reflect" {
		t.Fatalf("purposes = %q, %q", u.Purpose, r.Purpose)
	}
	recent, err := s.ListRecentRuns(10)
	if err != nil || len(recent) != 1 || recent[0].ID != "u1" {
		t.Fatalf("ListRecentRuns = %+v, %v", recent, err)
	}
	byP, err := s.ListRunsByPurpose("reflect", 10)
	if err != nil || len(byP) != 1 || byP[0].ID != "r1" {
		t.Fatalf("ListRunsByPurpose = %+v, %v", byP, err)
	}
}

func TestTasks_SystemKindHiddenFromListsButDeletedWithWorkspace(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	user := newTestTaskForRuns(t, s)
	wsID := user.WorkspaceID
	sp, err := s.CreateSpace(Space{WorkspaceID: wsID, Title: "sp"})
	if err != nil {
		t.Fatal(err)
	}
	sys, chat, err := s.CreateSystemTask(wsID, "learning", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if sys.Kind != TaskKindSystem || sys.Branch != nil || sys.Port != nil || sys.WorktreePath == nil || *sys.WorktreePath != "/repo" || chat.TaskID != sys.ID {
		t.Fatalf("system task = %+v chat = %+v", sys, chat)
	}
	got, err := s.GetTask(sys.ID)
	if err != nil || got.Kind != TaskKindSystem {
		t.Fatalf("GetTask = %+v, %v", got, err)
	}
	if u, _ := s.GetTask(user.ID); u.Kind != TaskKindTask {
		t.Fatalf("user task kind = %q", u.Kind)
	}
	// A system task placed in a space and under a parent is hidden from those lists too.
	if _, err := s.db.Exec(`UPDATE tasks SET space_id = ?, parent_task_id = ? WHERE id = ?`, sp.ID, user.ID, sys.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.CreateRun(Run{ID: "sysrun", TaskID: sys.ID, ChatID: chat.ID, Provider: "glm", Status: "done", StartedAt: now, Purpose: "reflect"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(Run{ID: "urun", TaskID: user.ID, Provider: "glm", Status: "done", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"sysrun", "urun"} {
		taskID := sys.ID
		if id == "urun" {
			taskID = user.ID
		}
		if err := s.UpsertRunUsage(RunUsage{RunID: id, TaskID: taskID, Provider: "glm", Source: "x", InputTokens: ptr(int64(10)), OutputTokens: ptr(int64(5)), CostUSD: ptr(1.0)}); err != nil {
			t.Fatal(err)
		}
	}

	if l, _ := s.ListTasksByWorkspace(wsID); len(l) != 1 || l[0].ID != user.ID {
		t.Fatalf("ListTasksByWorkspace = %+v", l)
	}
	if l, _ := s.ListAllTasksByWorkspace(wsID); len(l) != 2 {
		t.Fatalf("ListAllTasksByWorkspace = %+v", l)
	}
	if l, _ := s.ListTasksBySpace(sp.ID); len(l) != 0 {
		t.Fatalf("ListTasksBySpace = %+v", l)
	}
	if l, _ := s.ListTasksByParent(user.ID); len(l) != 0 {
		t.Fatalf("ListTasksByParent = %+v", l)
	}
	if _, err := s.db.Exec(`UPDATE tasks SET space_id = NULL WHERE id = ?`, sys.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListUngroupedTasksByWorkspace(wsID); len(l) != 1 {
		t.Fatalf("ListUngroupedTasksByWorkspace = %+v", l)
	}
	if l, _ := s.ListRecentRuns(10); len(l) != 1 || l[0].ID != "urun" {
		t.Fatalf("ListRecentRuns = %+v", l)
	}
	sum, err := s.SummarizeRunUsage(nil, nil, "workspace")
	if err != nil || len(sum) != 1 || sum[0].Count != 1 || sum[0].InputTokens != 10 {
		t.Fatalf("SummarizeRunUsage = %+v, %v", sum, err)
	}
	by, err := s.SumRunUsageByTask(wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := by[sys.ID]; ok || by[user.ID].Runs != 1 {
		t.Fatalf("SumRunUsageByTask = %+v", by)
	}
	n, in, out, cost, err := s.SummarizeLearningUsage(now.Add(-time.Hour))
	if err != nil || n != 1 || in != 10 || out != 5 || cost != 1.0 {
		t.Fatalf("SummarizeLearningUsage = %d %d %d %v %v", n, in, out, cost, err)
	}

	if err := s.DeleteWorkspace(wsID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	if _, err := s.GetTask(sys.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("system task survived: %v", err)
	}
	if _, err := s.GetRun("sysrun"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("system run survived: %v", err)
	}
}

func TestChats_LearnFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	c, err := s.CreateChat(task.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE chats SET learn_watermark = 7, learn_pending = 3 WHERE id = ?`, c.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetChat(c.ID)
	if err != nil || got.LearnWatermark != 7 || got.LearnPending != 3 {
		t.Fatalf("GetChat = %+v, %v", got, err)
	}
	list, err := s.ListChatsByTask(task.ID, true)
	if err != nil || len(list) != 1 || list[0].LearnWatermark != 7 || list[0].LearnPending != 3 {
		t.Fatalf("ListChatsByTask = %+v, %v", list, err)
	}
}

func TestWorkspaces_LearningModeRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetWorkspace(ws.ID); got.LearningMode != "off" {
		t.Fatalf("default mode = %q", got.LearningMode)
	}
	if err := s.SetWorkspaceLearningMode(ws.ID, LearningModePropose); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetWorkspace(ws.ID)
	list, _ := s.ListWorkspaces()
	if got.LearningMode != "propose" || len(list) != 1 || list[0].LearningMode != "propose" {
		t.Fatalf("got %q list %+v", got.LearningMode, list)
	}
}

func TestGetSystemTask(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	user := newTestTaskForRuns(t, s)
	if _, ok, err := s.GetSystemTask(user.WorkspaceID); err != nil || ok {
		t.Fatalf("GetSystemTask before create = ok %v, err %v; want absent", ok, err)
	}
	sys, _, err := s.CreateSystemTask(user.WorkspaceID, "learning", "/cwd")
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetSystemTask(user.WorkspaceID)
	if err != nil || !ok || got.ID != sys.ID || got.Kind != TaskKindSystem {
		t.Fatalf("GetSystemTask = %+v, ok %v, err %v", got, ok, err)
	}
}
