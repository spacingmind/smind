package wsapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

type sysFixture struct {
	wm     *workspace.Manager
	db     *store.Store
	api    *API
	srv    *httptest.Server
	runner *taskrunner.Runner
	ws     store.Workspace
	user   store.Task
	sys    store.Task
	sysCh  store.Chat
	done   chan runs.RunFinished
	seen   map[string]bool
}

// newSysFixture builds a full wsapi stack with one workspace, one user task
// and one hidden system task. Cleanup order follows WORKSPACE_PROTOCOL's
// flake rules: api.Close is registered right after New, before the server.
func newSysFixture(t *testing.T) *sysFixture {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	api, err := New(wm, newTestAccountsRegistry(t), runner, db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)

	repo := newTestRepo(t)
	w, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	user, err := wm.CreateTask(w.ID, nil, "user task")
	if err != nil {
		t.Fatal(err)
	}
	sys, ch, err := wm.CreateSystemTask(w.ID, "learning", repo)
	if err != nil {
		t.Fatal(err)
	}
	f := &sysFixture{wm: wm, db: db, api: api, srv: srv, runner: runner, ws: w, user: user, sys: sys, sysCh: ch, done: make(chan runs.RunFinished, 8), seen: map[string]bool{}}
	api.Runs.AddObserver(chanObserver(f.done))
	return f
}

type chanObserver chan runs.RunFinished

func (c chanObserver) OnRunFinished(f runs.RunFinished) { c <- f }

func (f *sysFixture) startRun(t *testing.T, taskID, chatID int64, opts ...runs.StartOption) string {
	t.Helper()
	id, err := f.api.Runs.Start(context.Background(), f.wm, f.runner, taskID, chatID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", opts...)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return id
}

func (f *sysFixture) waitFinished(t *testing.T, runID string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !f.seen[runID] {
		select {
		case fin := <-f.done:
			f.seen[fin.RunID] = true
		case <-deadline:
			t.Fatalf("run %s did not finish", runID)
		}
	}
}

func (f *sysFixture) subscribe(topics ...string) *subscriber {
	s := newSubscriber()
	s.subscribe(topics)
	f.api.bus.register(s)
	return s
}

func queued(s *subscriber) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.queue...)
}

func callResult(t *testing.T, f *sysFixture, method string, params map[string]any, out any) {
	t.Helper()
	ws := dialWS(t, f.srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })
	sendRequest(t, ws, "c", method, params)
	resp := readEnvelopeFor(t, ws, "c", 10*time.Second)
	if resp.Error != nil {
		t.Fatalf("%s error = %v", method, resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		t.Fatalf("decode %s: %v", method, err)
	}
}

func TestSystemTask_HiddenFromTaskList(t *testing.T) {
	t.Parallel()
	f := newSysFixture(t)

	tasks, err := f.wm.ListTasks(f.ws.ID)
	if err != nil || len(tasks) != 1 || tasks[0].ID != f.user.ID {
		t.Fatalf("Manager.ListTasks = %+v, %v", tasks, err)
	}
	var listed []store.Task
	callResult(t, f, "task.list", map[string]any{"workspaceId": f.ws.ID}, &listed)
	if len(listed) != 1 || listed[0].ID != f.user.ID {
		t.Fatalf("task.list = %+v", listed)
	}
	var tree treeResult
	callResult(t, f, "task.tree", map[string]any{"workspaceId": f.ws.ID}, &tree)
	if len(tree.Nodes) != 1 || tree.Nodes[0].ID != f.user.ID {
		t.Fatalf("task.tree = %+v", tree.Nodes)
	}
}

func TestSystemTask_NoNotifierEvents(t *testing.T) {
	t.Parallel()
	f := newSysFixture(t)
	sub := f.subscribe(TopicTaskStatus, TopicTaskCreated, TopicTaskUpdated, TopicTaskArchived, TopicTaskDeleted,
		TopicChatCreated, TopicChatUpdated, TopicChatArchived, TopicChatQueueUpdated)

	// Creating a system task fires nothing.
	if _, _, err := f.wm.CreateSystemTask(f.ws.ID, "learning 2", f.ws.Path); err != nil {
		t.Fatal(err)
	}
	if got := queued(sub); len(got) != 0 {
		t.Fatalf("CreateSystemTask published %+v", got)
	}

	// Adapters drop every notification about the system task/chat...
	wn := busWorkspaceNotifier{bus: f.api.bus, db: f.db}
	wn.NotifyTaskStatus(f.sys.ID, "done")
	wn.NotifyTaskCreated(f.sys)
	wn.NotifyTaskUpdated(f.sys)
	wn.NotifyTaskArchived(f.sys)
	wn.NotifyTaskDeleted(f.sys.ID, f.ws.ID, nil)
	wn.NotifyChatCreated(f.sysCh)
	wn.NotifyChatUpdated(f.sysCh)
	wn.NotifyChatArchived(f.sysCh)
	rn := busRunNotifier{bus: f.api.bus, db: f.db}
	rn.NotifyChatQueueUpdated(f.sysCh.ID, nil)
	if got := queued(sub); len(got) != 0 {
		t.Fatalf("system task notifications leaked: %+v", got)
	}

	// ...while the same calls for the user task go through (positive control).
	userChats, _ := f.db.ListChatsByTask(f.user.ID, true)
	wn.NotifyTaskStatus(f.user.ID, "running")
	wn.NotifyTaskCreated(f.user)
	wn.NotifyChatCreated(userChats[0])
	rn.NotifyChatQueueUpdated(userChats[0].ID, nil)
	if got := queued(sub); len(got) != 4 {
		t.Fatalf("user task notifications = %d, want 4", len(got))
	}
}

func TestSystemRun_NotNotifiedOrListed(t *testing.T) {
	t.Parallel()
	f := newSysFixture(t)
	sub := f.subscribe(TopicRunStatus, TopicPermissionPending, TopicPermissionResolved)

	sysRun := f.startRun(t, f.sys.ID, f.sysCh.ID, runs.WithPurpose("reflect"))
	userRun := f.startRun(t, f.user.ID, 0)
	f.waitFinished(t, sysRun)
	f.waitFinished(t, userRun)

	for _, e := range queued(sub) {
		p, ok := e.Payload.(runStatusPayload)
		if !ok {
			t.Fatalf("unexpected event %+v", e)
		}
		if p.RunID == sysRun {
			t.Fatalf("system run published run.status: %+v", p)
		}
	}
	var seenUser bool
	for _, e := range queued(sub) {
		if e.Payload.(runStatusPayload).RunID == userRun {
			seenUser = true
		}
	}
	if !seenUser {
		t.Fatal("user run published no run.status (positive control)")
	}

	var list []runs.RunSummary
	callResult(t, f, "run.list", map[string]any{}, &list)
	if len(list) != 1 || list[0].ID != userRun {
		t.Fatalf("run.list = %+v, want only the user run", list)
	}
	callResult(t, f, "run.list", map[string]any{"taskId": f.sys.ID}, &list)
	if len(list) != 0 {
		t.Fatalf("run.list(system task) = %+v", list)
	}
}

func TestUsage_LearningBucketSplit(t *testing.T) {
	t.Parallel()
	f := newSysFixture(t)
	sysRun := f.startRun(t, f.sys.ID, f.sysCh.ID, runs.WithPurpose("reflect"))
	userRun := f.startRun(t, f.user.ID, 0)
	f.waitFinished(t, sysRun)
	f.waitFinished(t, userRun)

	// Overwrite whatever the fake agent reported with known numbers.
	if err := f.db.UpsertRunUsage(store.RunUsage{RunID: sysRun, TaskID: f.sys.ID, Provider: "glm", Source: "t",
		InputTokens: ptr(int64(1000)), OutputTokens: ptr(int64(200)), CostUSD: ptr(0.5)}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.UpsertRunUsage(store.RunUsage{RunID: userRun, TaskID: f.user.ID, Provider: "glm", Source: "t",
		InputTokens: ptr(int64(10)), OutputTokens: ptr(int64(2)), CostUSD: ptr(0.25)}); err != nil {
		t.Fatal(err)
	}

	var summary []usageSummaryEntry
	callResult(t, f, "usage.summary", map[string]any{"groupBy": "workspace", "scope": "runs"}, &summary)
	if len(summary) != 1 || summary[0].Count != 1 || summary[0].InputTokens != 10 {
		t.Fatalf("usage.summary = %+v, want only the user run", summary)
	}
	callResult(t, f, "usage.summary", map[string]any{"groupBy": "task", "scope": "runs"}, &summary)
	if len(summary) != 1 {
		t.Fatalf("usage.summary by task = %+v, want only the user task", summary)
	}

	var learning learningUsageResult
	callResult(t, f, "usage.learning", map[string]any{}, &learning)
	if learning.Runs != 1 || learning.InputTokens != 1000 || learning.OutputTokens != 200 || learning.CostUSD != 0.5 {
		t.Fatalf("usage.learning = %+v", learning)
	}

	var tree treeResult
	callResult(t, f, "task.tree", map[string]any{"workspaceId": f.ws.ID}, &tree)
	if len(tree.Nodes) != 1 || tree.Nodes[0].Usage.Own.Runs != 1 || *tree.Nodes[0].Usage.Own.InputTokens != 10 {
		t.Fatalf("task.tree usage = %+v", tree.Nodes)
	}
}
