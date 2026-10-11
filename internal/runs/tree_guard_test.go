package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// treeGuardEnv is one tree-guard test setup: a workspace manager, store,
// fake-agent runner, registry with the starter wired, and a root task
// with child tasks (plus optionally a grandchild) so runs can spread
// across a real tree. Every task in the tree gets the "hang" scenario
// written into its own worktree, so every run stays running until
// stopped.
type treeGuardEnv struct {
	wm     *workspace.Manager
	st     *store.Store
	runner *taskrunner.Runner
	reg    *Registry
	root   store.Task
	tasks  []store.Task
}

// writeScenario writes the fake-agent scenario file into taskID's
// worktree.
func (e *treeGuardEnv) writeScenario(t *testing.T, taskID int64, scenario string) {
	t.Helper()
	task, err := e.wm.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask(%d) error = %v", taskID, err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}
}

func newTreeGuardEnv(t *testing.T, children int, grandchild bool) *treeGuardEnv {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	root := newTestTask(t, wm, "hang")
	runner := taskrunner.New(wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	reg := newTestRegistry(t, st)
	reg.SetStarter(wm, runner)
	e := &treeGuardEnv{wm: wm, st: st, runner: runner, reg: reg, root: root}
	for i := 0; i < children; i++ {
		child, err := wm.CreateTask(root.WorkspaceID, nil, "child", workspace.WithParentTask(root.ID))
		if err != nil {
			t.Fatalf("CreateTask(child) error = %v", err)
		}
		e.writeScenario(t, child.ID, "hang")
		e.tasks = append(e.tasks, child)
	}
	if grandchild && len(e.tasks) > 0 {
		parent := e.tasks[0].ID
		gc, err := wm.CreateTask(root.WorkspaceID, nil, "grandchild", workspace.WithParentTask(parent))
		if err != nil {
			t.Fatalf("CreateTask(grandchild) error = %v", err)
		}
		e.writeScenario(t, gc.ID, "hang")
		e.tasks = append(e.tasks, gc)
	}
	return e
}

// newChild adds a child of the root with the given scenario.
func (e *treeGuardEnv) newChild(t *testing.T, title, scenario string) store.Task {
	t.Helper()
	child, err := e.wm.CreateTask(e.root.WorkspaceID, nil, title, workspace.WithParentTask(e.root.ID))
	if err != nil {
		t.Fatalf("CreateTask(%q) error = %v", title, err)
	}
	e.writeScenario(t, child.ID, scenario)
	e.tasks = append(e.tasks, child)
	return child
}

// startOn starts a run on taskID's default chat, failing on any error.
func (e *treeGuardEnv) startOn(t *testing.T, taskID int64) string {
	t.Helper()
	runID, err := e.reg.Start(context.Background(), e.wm, e.runner, taskID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start(task %d) error = %v", taskID, err)
	}
	return runID
}

// startOnNewChat creates a fresh chat on taskID and starts a run there,
// so the per-chat busy guard can never be the reason for a rejection.
func (e *treeGuardEnv) startOnNewChat(t *testing.T, taskID int64) string {
	t.Helper()
	chat, err := e.wm.CreateChat(taskID, "chat")
	if err != nil {
		t.Fatalf("CreateChat(task %d) error = %v", taskID, err)
	}
	runID, err := e.reg.Start(context.Background(), e.wm, e.runner, taskID, chat.ID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start(task %d, chat %d) error = %v", taskID, chat.ID, err)
	}
	return runID
}

func asTreeBusy(err error) (*TreeBusyError, bool) {
	var tb *TreeBusyError
	if errors.As(err, &tb) {
		return tb, true
	}
	return nil, false
}

// TestTreeGuard_FifthConcurrentRunRejected pins G-AC3: a tree (root + 2
// children + 1 grandchild) with 4 running runs refuses a 5th Start on any
// task of the tree with *TreeBusyError.
func TestTreeGuard_FifthConcurrentRunRejected(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 2, true)

	e.startOn(t, e.root.ID)
	e.startOn(t, e.tasks[0].ID)
	e.startOn(t, e.tasks[1].ID)
	e.startOn(t, e.tasks[2].ID) // the grandchild

	// A 5th on each task of the tree, each on a fresh chat so only the
	// tree guard can reject.
	all := []int64{e.root.ID}
	for _, tsk := range e.tasks {
		all = append(all, tsk.ID)
	}
	for _, taskID := range all {
		chat, err := e.wm.CreateChat(taskID, "extra")
		if err != nil {
			t.Fatalf("CreateChat(task %d) error = %v", taskID, err)
		}
		_, err = e.reg.Start(context.Background(), e.wm, e.runner, taskID, chat.ID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
		tb, ok := asTreeBusy(err)
		if !ok {
			t.Fatalf("Start(task %d) error = %v, want *TreeBusyError", taskID, err)
		}
		if tb.RootTaskID != e.root.ID || tb.Limit != 4 || tb.Running != 4 {
			t.Fatalf("TreeBusyError = {root %d, limit %d, running %d}, want {root %d, limit 4, running 4}", tb.RootTaskID, tb.Limit, tb.Running, e.root.ID)
		}
	}
}

// TestTreeGuard_SlotFreesWhenRunEnds: stopping one of the 4 running runs
// frees its tree slot and the previously rejected start succeeds.
func TestTreeGuard_SlotFreesWhenRunEnds(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 1, false)
	var ids []string
	ids = append(ids, e.startOnNewChat(t, e.root.ID))
	ids = append(ids, e.startOnNewChat(t, e.root.ID))
	ids = append(ids, e.startOnNewChat(t, e.tasks[0].ID))
	ids = append(ids, e.startOnNewChat(t, e.tasks[0].ID))

	if _, err := e.reg.Start(context.Background(), e.wm, e.runner, e.root.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, ""); err == nil {
		t.Fatal("5th Start = nil error, want *TreeBusyError")
	}

	if err := e.reg.Stop(ids[0]); err != nil {
		t.Fatalf("Stop(%q) error = %v", ids[0], err)
	}
	waitForStatus(t, e.reg, ids[0], StatusStopped, 5*time.Second)

	// The tree now has room again.
	runID, err := e.reg.Start(context.Background(), e.wm, e.runner, e.tasks[0].ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start after slot freed: error = %v, want nil", err)
	}
	waitForStatus(t, e.reg, runID, StatusRunning, 5*time.Second)
}

// TestTreeGuard_SeparateTreesDoNotShareBudget: tree A at its limit does
// not block a lone task in tree B.
func TestTreeGuard_SeparateTreesDoNotShareBudget(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 1, false)
	e.reg.SetMaxConcurrentRunsPerTree(2)
	e.startOnNewChat(t, e.root.ID)
	e.startOnNewChat(t, e.tasks[0].ID)
	if _, err := e.reg.Start(context.Background(), e.wm, e.runner, e.root.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, ""); err == nil {
		t.Fatal("3rd Start in tree A = nil error, want *TreeBusyError")
	}

	// A lone task is a tree of one with its own budget.
	lone := newTestTask(t, e.wm, "hang")
	if _, err := e.reg.Start(context.Background(), e.wm, e.runner, lone.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, ""); err != nil {
		t.Fatalf("Start in tree B error = %v, want nil", err)
	}
}

// TestTreeGuard_ConcurrentStartsAdmitExactlyLimit: 32 concurrent Starts
// (distinct chats, so only the tree guard can reject) admit exactly 4.
func TestTreeGuard_ConcurrentStartsAdmitExactlyLimit(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 8, false)

	const n = 32
	chats := make([]int64, n)
	for i := 0; i < n; i++ {
		chat, err := e.wm.CreateChat(e.root.ID, "chat")
		if err != nil {
			t.Fatalf("CreateChat() error = %v", err)
		}
		chats[i] = chat.ID
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var succeeded, busy int
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(chatID int64) {
			defer wg.Done()
			_, err := e.reg.Start(context.Background(), e.wm, e.runner, e.root.ID, chatID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			default:
				if _, ok := asTreeBusy(err); ok {
					busy++
				} else {
					t.Errorf("Start error = %v, want nil or *TreeBusyError", err)
				}
			}
		}(chats[i])
	}
	wg.Wait()
	if succeeded != 4 || busy != n-4 {
		t.Fatalf("concurrent Starts: %d succeeded, %d TreeBusyError, want exactly 4 and %d", succeeded, busy, n-4)
	}
}

// TestTreeGuard_ConcurrentStartWhenBusyAdmitsExactlyLimit: the same
// atomicity proof through StartWhenBusy.
func TestTreeGuard_ConcurrentStartWhenBusyAdmitsExactlyLimit(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 8, false)

	const n = 32
	chats := make([]int64, n)
	for i := 0; i < n; i++ {
		chat, err := e.wm.CreateChat(e.root.ID, "chat")
		if err != nil {
			t.Fatalf("CreateChat() error = %v", err)
		}
		chats[i] = chat.ID
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var succeeded, busy int
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(chatID int64) {
			defer wg.Done()
			_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.root.ID, chatID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			default:
				if _, ok := asTreeBusy(err); ok {
					busy++
				} else {
					t.Errorf("StartWhenBusy error = %v, want nil or *TreeBusyError", err)
				}
			}
		}(chats[i])
	}
	wg.Wait()
	if succeeded != 4 || busy != n-4 {
		t.Fatalf("concurrent StartWhenBusy: %d succeeded, %d TreeBusyError, want exactly 4 and %d", succeeded, busy, n-4)
	}
}

// TestTreeGuard_WaitingPermissionRunStillCountsAsSlot: a run blocked on a
// pending permission request is still StatusRunning, so it occupies a
// tree slot.
func TestTreeGuard_WaitingPermissionRunStillCountsAsSlot(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 3, false)

	// A child of the root whose scenario blocks the run on a pending
	// human answer -- part of the same tree, so it must count.
	permTask := e.newChild(t, "perm", "permission")
	permRun := e.startOn(t, permTask.ID)
	waitForPermissionRequest(t, e.reg, permRun, 5*time.Second)

	member := e.tasks[0].ID
	e.startOnNewChat(t, member)
	e.startOnNewChat(t, member)
	e.startOnNewChat(t, e.tasks[1].ID)

	_, err := e.reg.Start(context.Background(), e.wm, e.runner, member, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	tb, ok := asTreeBusy(err)
	if !ok {
		t.Fatalf("5th Start error = %v, want *TreeBusyError (permission-waiting run must count)", err)
	}
	if tb.Running != 4 {
		t.Fatalf("TreeBusyError.Running = %d, want 4 (the permission-waiting run counted)", tb.Running)
	}
}

// TestTreeGuard_QueuedPromptStaysQueuedAndDeliversWhenSlotFrees pins the
// queue half of the guard: delivery that hits a full tree leaves the item
// QUEUED (never cancelled), and it starts once a run in another chat of
// the tree ends.
func TestTreeGuard_QueuedPromptStaysQueuedAndDeliversWhenSlotFrees(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 2, false)
	e.reg.SetMaxConcurrentRunsPerTree(2)

	r1 := e.startOnNewChat(t, e.root.ID)
	e.startOnNewChat(t, e.tasks[0].ID)

	// An item for an idle chat of the tree, inserted directly: its
	// delivery attempt must hit the tree guard.
	idleChat, err := e.wm.CreateChat(e.tasks[1].ID, "idle")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	item, err := e.st.EnqueueChatPrompt(idleChat.ID, "queued", `{"provider":"glm"}`, store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatalf("EnqueueChatPrompt() error = %v", err)
	}
	e.reg.DeliverQueued()

	// The tree is full (r1, r2 hang), so delivery must have been refused
	// with the item still queued -- not cancelled, no run id.
	got, err := e.st.GetChatQueueItem(item.ID)
	if err != nil {
		t.Fatalf("GetChatQueueItem() error = %v", err)
	}
	if got.Status != store.ChatQueueStatusQueued {
		t.Fatalf("queue item %d status %q (cancel reason %q), want still queued", item.ID, got.Status, got.CancelReason)
	}
	if got.RunID != nil && *got.RunID != "" {
		t.Fatalf("queue item %d has run id %q, want none while the tree is full", item.ID, *got.RunID)
	}

	// A run in ANOTHER chat of the tree ends -> its finish retries
	// delivery for every queued chat and the item starts.
	if err := e.reg.Stop(r1); err != nil {
		t.Fatalf("Stop(r1) error = %v", err)
	}
	waitForStatus(t, e.reg, r1, StatusStopped, 5*time.Second)
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err = e.st.GetChatQueueItem(item.ID)
		if err != nil {
			t.Fatalf("GetChatQueueItem() error = %v", err)
		}
		if got.Status == store.ChatQueueStatusDelivered {
			if got.RunID == nil || *got.RunID == "" {
				t.Fatal("delivered item has no run id")
			}
			_, status, err := e.reg.History(*got.RunID)
			if err != nil {
				t.Fatalf("delivered run %q unknown to registry: %v", *got.RunID, err)
			}
			if status.Status != StatusRunning {
				t.Fatalf("delivered run %q status %q, want running", *got.RunID, status.Status)
			}
			return
		}
		if got.Status == store.ChatQueueStatusCancelled {
			t.Fatalf("queue item %d cancelled (%q), want delivered", item.ID, got.CancelReason)
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue item %d still %q, want delivered", item.ID, got.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestTreeGuard_ConfiguredLimitHonoured: SetMaxConcurrentRunsPerTree(2)
// caps the tree at 2.
func TestTreeGuard_ConfiguredLimitHonoured(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 1, false)
	e.reg.SetMaxConcurrentRunsPerTree(2)

	e.startOnNewChat(t, e.root.ID)
	e.startOnNewChat(t, e.tasks[0].ID)

	chat, err := e.wm.CreateChat(e.root.ID, "third")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	_, err = e.reg.Start(context.Background(), e.wm, e.runner, e.root.ID, chat.ID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	tb, ok := asTreeBusy(err)
	if !ok {
		t.Fatalf("3rd Start error = %v, want *TreeBusyError", err)
	}
	if tb.Limit != 2 || tb.Running != 2 {
		t.Fatalf("TreeBusyError = {limit %d, running %d}, want {2, 2}", tb.Limit, tb.Running)
	}

	// Restoring the default lifts the cap again.
	e.reg.SetMaxConcurrentRunsPerTree(0)
	_, err = e.reg.Start(context.Background(), e.wm, e.runner, e.root.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start after restoring default limit: error = %v, want nil", err)
	}
}
