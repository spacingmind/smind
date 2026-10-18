package store

import (
	"testing"
	"time"
)

var collectBase = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

type collectFixture struct {
	s    *Store
	ws   Workspace
	task Task
	chat Chat
}

func newCollectFixture(t *testing.T, mode string) collectFixture {
	t.Helper()
	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		if err := s.SetWorkspaceLearningMode(ws.ID, mode); err != nil {
			t.Fatal(err)
		}
	}
	task, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := s.CreateChat(task.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	return collectFixture{s: s, ws: ws, task: task, chat: chat}
}

func (f collectFixture) run(t *testing.T, id string, chatID, taskID int64, started time.Time, finished *time.Time, status, purpose string, n int) []int64 {
	t.Helper()
	if _, err := f.s.CreateRun(Run{ID: id, TaskID: taskID, ChatID: chatID, Provider: "claude-native", Prompt: "p-" + id,
		Status: status, StartedAt: started, FinishedAt: finished, Purpose: purpose}); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := 0; i < n; i++ {
		e, err := f.s.AppendRunEvent(id, int64(i), `{"type":"text"}`)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	return ids
}

func tp(t time.Time) *time.Time { return &t }

func TestChatLearnCounters(t *testing.T) {
	t.Parallel()
	f := newCollectFixture(t, "propose")
	n, err := f.s.AddChatLearnPending(f.chat.ID, 7)
	if err != nil || n != 7 {
		t.Fatalf("AddChatLearnPending = %d, %v", n, err)
	}
	if n, _ = f.s.AddChatLearnPending(f.chat.ID, 5); n != 12 {
		t.Fatalf("second add = %d, want 12", n)
	}
	if _, err := f.s.AddChatLearnPending(99999, 1); err == nil {
		t.Fatal("unknown chat: want error")
	}
	if err := f.s.SetChatLearnState(f.chat.ID, 40, 0); err != nil {
		t.Fatal(err)
	}
	c, _ := f.s.GetChat(f.chat.ID)
	if c.LearnWatermark != 40 || c.LearnPending != 0 {
		t.Fatalf("state = %d/%d", c.LearnWatermark, c.LearnPending)
	}
	// The watermark never moves backwards; pending is set exactly.
	if err := f.s.SetChatLearnState(f.chat.ID, 10, 3); err != nil {
		t.Fatal(err)
	}
	c, _ = f.s.GetChat(f.chat.ID)
	if c.LearnWatermark != 40 || c.LearnPending != 3 {
		t.Fatalf("state after backwards set = %d/%d", c.LearnWatermark, c.LearnPending)
	}
	if err := f.s.SetChatLearnState(99999, 1, 0); err == nil {
		t.Fatal("unknown chat: want error")
	}
}

func TestListChatRunsWithEventsAfter(t *testing.T) {
	t.Parallel()
	f := newCollectFixture(t, "propose")
	r1 := f.run(t, "r1", f.chat.ID, f.task.ID, collectBase, tp(collectBase.Add(time.Minute)), "done", "user", 3)
	r2 := f.run(t, "r2", f.chat.ID, f.task.ID, collectBase.Add(time.Hour), tp(collectBase.Add(2*time.Hour)), "done", "user", 2)

	// Another chat of the same task, a reflect-purpose run in this chat's
	// task, and a system task's run must never leak in.
	other, _ := f.s.CreateChat(f.task.ID, "other")
	f.run(t, "other", other.ID, f.task.ID, collectBase, nil, "done", "user", 2)
	f.run(t, "refl", f.chat.ID, f.task.ID, collectBase, nil, "done", "reflect", 2)
	sys, sysChat, err := f.s.CreateSystemTask(f.ws.ID, "learning", "/cwd")
	if err != nil {
		t.Fatal(err)
	}
	f.run(t, "sysrun", sysChat.ID, sys.ID, collectBase, nil, "done", "user", 2)

	got, err := f.s.ListChatRunsWithEventsAfter(f.chat.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Run.ID != "r1" || got[1].Run.ID != "r2" {
		t.Fatalf("runs = %+v", got)
	}
	if !got[0].PromptInWindow || !got[1].PromptInWindow || len(got[0].Events) != 3 || len(got[1].Events) != 2 {
		t.Fatalf("full window wrong: %+v", got)
	}
	for _, w := range got {
		for i, e := range w.Events {
			if e.RunID != w.Run.ID || e.Seq != int64(i) {
				t.Fatalf("event %+v not in run %s order", e, w.Run.ID)
			}
		}
	}

	// Watermark in the middle of r1: r1 keeps only its tail and its prompt is
	// no longer in the window; r2 is untouched.
	got, err = f.s.ListChatRunsWithEventsAfter(f.chat.ID, r1[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].Events) != 2 || got[0].Events[0].ID != r1[1] || got[0].PromptInWindow || !got[1].PromptInWindow {
		t.Fatalf("mid watermark: %+v", got)
	}
	// Watermark at r1's last event drops r1 entirely.
	got, _ = f.s.ListChatRunsWithEventsAfter(f.chat.ID, r1[2])
	if len(got) != 1 || got[0].Run.ID != "r2" {
		t.Fatalf("after r1: %+v", got)
	}
	got, _ = f.s.ListChatRunsWithEventsAfter(f.chat.ID, r2[1])
	if len(got) != 0 {
		t.Fatalf("after all events: %+v", got)
	}
	if got, _ := f.s.ListChatRunsWithEventsAfter(sysChat.ID, 0); len(got) != 0 {
		t.Fatalf("system chat leaked runs: %+v", got)
	}
}

func TestListTailCandidates(t *testing.T) {
	t.Parallel()
	f := newCollectFixture(t, "propose")
	idleBefore := collectBase.Add(time.Hour)

	mk := func(ws Workspace, task Task, title string, pending int64, runStatus string, finished time.Time) Chat {
		c, err := f.s.CreateChat(task.ID, title)
		if err != nil {
			t.Fatal(err)
		}
		if pending > 0 {
			if _, err := f.s.AddChatLearnPending(c.ID, pending); err != nil {
				t.Fatal(err)
			}
		}
		fin := &finished
		if runStatus == "running" {
			fin = nil
		}
		f.run(t, "run-"+title, c.ID, task.ID, finished.Add(-time.Minute), fin, runStatus, "user", 1)
		return c
	}
	idle := mk(f.ws, f.task, "idle", 12, "done", idleBefore.Add(-time.Minute))
	mk(f.ws, f.task, "recent", 12, "done", idleBefore.Add(time.Minute))
	mk(f.ws, f.task, "few", 9, "done", idleBefore.Add(-time.Hour))
	mk(f.ws, f.task, "running", 12, "running", idleBefore.Add(-time.Hour))
	arch := mk(f.ws, f.task, "archived", 10, "done", idleBefore.Add(time.Hour))
	if _, err := f.s.ArchiveChat(arch.ID); err != nil {
		t.Fatal(err)
	}

	// A chat in an 'off' workspace and a system task's chat are never candidates.
	offWS, _ := f.s.CreateWorkspace(Workspace{Path: "/off", Title: "off", RoutingPolicy: "hard"})
	offTask, _ := f.s.CreateTask(Task{WorkspaceID: offWS.ID, Title: "t", Status: "created"})
	mk(offWS, offTask, "off", 50, "done", idleBefore.Add(-time.Hour))
	sys, sysChat, _ := f.s.CreateSystemTask(f.ws.ID, "learning", "/cwd")
	_ = sys
	if _, err := f.s.AddChatLearnPending(sysChat.ID, 50); err != nil {
		t.Fatal(err)
	}

	got, err := f.s.ListTailCandidates(10, idleBefore)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	for _, c := range got {
		ids[c.ID] = true
	}
	if len(got) != 2 || !ids[idle.ID] || !ids[arch.ID] {
		t.Fatalf("candidates = %+v, want idle + archived only", got)
	}
}

func TestSumLearningUsageForWorkspace(t *testing.T) {
	t.Parallel()
	f := newCollectFixture(t, "propose")
	sys, sysChat, err := f.s.CreateSystemTask(f.ws.ID, "learning", "/cwd")
	if err != nil {
		t.Fatal(err)
	}
	now := collectBase
	since := now.Add(-24 * time.Hour)
	f.run(t, "reported", sysChat.ID, sys.ID, now.Add(-time.Hour), tp(now), "done", "reflect", 0)
	f.run(t, "nousage", sysChat.ID, sys.ID, now.Add(-time.Hour), tp(now), "done", "reflect", 0)
	f.run(t, "nullcost", sysChat.ID, sys.ID, now.Add(-time.Hour), tp(now), "done", "curate", 0)
	f.run(t, "old", sysChat.ID, sys.ID, now.Add(-48*time.Hour), tp(now), "done", "reflect", 0)
	f.run(t, "userrun", f.chat.ID, f.task.ID, now.Add(-time.Hour), tp(now), "done", "user", 0)
	for _, u := range []RunUsage{
		{RunID: "reported", TaskID: sys.ID, Provider: "claude-native", Source: "x", InputTokens: ptr(int64(100)), OutputTokens: ptr(int64(20)), CostUSD: ptr(0.05)},
		{RunID: "nullcost", TaskID: sys.ID, Provider: "claude-native", Source: "x", InputTokens: ptr(int64(7)), OutputTokens: ptr(int64(3))},
		{RunID: "old", TaskID: sys.ID, Provider: "claude-native", Source: "x", InputTokens: ptr(int64(1000)), OutputTokens: ptr(int64(1000)), CostUSD: ptr(9.0)},
		{RunID: "userrun", TaskID: f.task.ID, Provider: "claude-native", Source: "x", InputTokens: ptr(int64(5000)), OutputTokens: ptr(int64(5000)), CostUSD: ptr(9.0)},
	} {
		if err := f.s.UpsertRunUsage(u); err != nil {
			t.Fatal(err)
		}
	}
	cost, tokens, unreported, err := f.s.SumLearningUsageForWorkspace(f.ws.ID, since)
	if err != nil {
		t.Fatal(err)
	}
	if cost != 0.05 || tokens != 130 || unreported != 2 {
		t.Fatalf("sum = cost %v tokens %d unreported %d, want 0.05/130/2", cost, tokens, unreported)
	}
	// Another workspace sees nothing.
	other, _ := f.s.CreateWorkspace(Workspace{Path: "/o", Title: "o", RoutingPolicy: "hard"})
	if cost, tokens, unreported, _ = f.s.SumLearningUsageForWorkspace(other.ID, since); cost != 0 || tokens != 0 || unreported != 0 {
		t.Fatalf("other workspace leaked: %v %d %d", cost, tokens, unreported)
	}
}

func TestLastLearningJobAt(t *testing.T) {
	t.Parallel()
	f := newCollectFixture(t, "propose")
	if _, ok, err := f.s.LastLearningJobAt(f.ws.ID, "reflect"); ok || err != nil {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	mk := func(kind, trigger string, at time.Time) {
		t.Helper()
		if _, err := f.s.CreateLearningJob(LearningJob{WorkspaceID: f.ws.ID, Kind: kind, TriggerKind: trigger, Status: "empty", CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	mk("reflect", "cadence", collectBase)
	mk("reflect", "tail", collectBase.Add(time.Minute))
	mk("reflect", "explicit", collectBase.Add(time.Hour)) // never counts
	mk("curate", "schedule", collectBase.Add(2*time.Hour))
	at, ok, err := f.s.LastLearningJobAt(f.ws.ID, "reflect")
	if err != nil || !ok || !at.Equal(collectBase.Add(time.Minute)) {
		t.Fatalf("reflect = %v %v %v", at, ok, err)
	}
	if at, ok, _ := f.s.LastLearningJobAt(f.ws.ID, "curate"); !ok || !at.Equal(collectBase.Add(2*time.Hour)) {
		t.Fatalf("curate = %v %v", at, ok)
	}
}

func TestFailInterruptedLearningJobs(t *testing.T) {
	t.Parallel()
	f := newCollectFixture(t, "propose")
	if err := f.s.SetChatLearnState(f.chat.ID, 55, 8); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, st := range []string{"queued", "running", "landed"} {
		j, err := f.s.CreateLearningJob(LearningJob{WorkspaceID: f.ws.ID, ChatID: &f.chat.ID, Kind: "reflect", TriggerKind: "cadence", Status: st})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	n, err := f.s.FailInterruptedLearningJobs("interrupted by restart")
	if err != nil || n != 2 {
		t.Fatalf("n = %d, %v", n, err)
	}
	for i, want := range []string{"failed", "failed", "landed"} {
		j, _ := f.s.GetLearningJob(ids[i])
		if j.Status != want {
			t.Fatalf("job %d status = %s, want %s", i, j.Status, want)
		}
		if want == "failed" && (j.Error != "interrupted by restart" || j.FinishedAt == nil) {
			t.Fatalf("job %d = %+v", i, j)
		}
	}
	if active, _ := f.s.HasActiveLearningJob(f.ws.ID); active {
		t.Fatal("still an active job")
	}
	c, _ := f.s.GetChat(f.chat.ID)
	if c.LearnWatermark != 55 || c.LearnPending != 8 {
		t.Fatalf("chat state touched: %d/%d", c.LearnWatermark, c.LearnPending)
	}
}

func TestGetWorkspaceByChat(t *testing.T) {
	t.Parallel()
	f := newCollectFixture(t, "propose")
	if _, err := f.s.BindChatProvider(f.chat.ID, "glm"); err != nil {
		t.Fatal(err)
	}
	cw, err := f.s.GetWorkspaceByChat(f.chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cw.WorkspaceID != f.ws.ID || cw.WorkspacePath != "/repo" || cw.TaskID != f.task.ID || cw.TaskKind != TaskKindTask ||
		cw.LearningMode != "propose" || cw.Provider != "glm" {
		t.Fatalf("cw = %+v", cw)
	}
	_, sysChat, _ := f.s.CreateSystemTask(f.ws.ID, "learning", "/cwd")
	if cw, _ := f.s.GetWorkspaceByChat(sysChat.ID); cw.TaskKind != TaskKindSystem {
		t.Fatalf("system chat kind = %q", cw.TaskKind)
	}
	if _, err := f.s.GetWorkspaceByChat(99999); err == nil {
		t.Fatal("unknown chat: want error")
	}
}
