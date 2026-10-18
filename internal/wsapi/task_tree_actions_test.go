package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// taFixture is one task.archiveFinished / task.stopTree test setup: its
// own workspace, repo, store and server, so tests can run in parallel.
// Tests that swap the package-level seams must NOT be parallel (see
// swapSeams) and restore them via t.Cleanup.
type taFixture struct {
	t    *testing.T
	wm   *workspace.Manager
	db   *store.Store
	ws   *websocket.Conn
	wsID int64
	dial func() *websocket.Conn
}

func newTAFixture(t *testing.T) *taFixture {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	f := &taFixture{t: t, wm: wm, db: db}
	f.dial = func() *websocket.Conn { return dialWS(t, srv, "tok") }
	f.ws = f.dial()
	w, err := wm.CreateWorkspace(newTestRepo(t), "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	f.wsID = w.ID
	t.Cleanup(f.drain)
	return f
}

// drain stops every still-running run and waits until none is left, so
// no fake-agent subprocess outlives the test or its TempDir.
func (f *taFixture) drain() {
	deadline := time.Now().Add(10 * time.Second)
	ws := f.dial()
	for time.Now().Before(deadline) {
		var list []runs.RunSummary
		sendRequest(f.t, ws, "dl", "run.list", nil)
		resp := readEnvelopeFor(f.t, ws, "dl", 5*time.Second)
		if err := json.Unmarshal(resp.Result, &list); err != nil {
			f.t.Errorf("drain: decode run.list: %v", err)
			return
		}
		running := 0
		for _, r := range list {
			if r.Status == runs.StatusRunning {
				running++
				sendRequest(f.t, ws, "ds"+r.ID, "run.stop", map[string]any{"runId": r.ID})
				readEnvelopeFor(f.t, ws, "ds"+r.ID, 5*time.Second)
			}
		}
		if running == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Errorf("drain: runs still running after 10s")
}

func (f *taFixture) task(title string, parent int64, scenario string) int64 {
	f.t.Helper()
	var opts []workspace.CreateTaskOption
	if parent != 0 {
		opts = append(opts, workspace.WithParentTask(parent))
	}
	task, err := f.wm.CreateTask(f.wsID, nil, title, opts...)
	if err != nil {
		f.t.Fatalf("CreateTask(%q) error = %v", title, err)
	}
	if scenario != "" {
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
			f.t.Fatalf("write scenario: %v", err)
		}
	}
	return task.ID
}

// startRun starts a run on taskID over ws and returns its id.
func (f *taFixture) startRun(ws *websocket.Conn, taskID int64) string {
	f.t.Helper()
	sendRequest(f.t, ws, "rs", "run.start", map[string]any{"taskId": taskID, "provider": "glm", "prompt": "hi"})
	resp := readEnvelopeFor(f.t, ws, "rs", 5*time.Second)
	if resp.Error != nil {
		f.t.Fatalf("run.start(task %d) error = %v", taskID, resp.Error.Message)
	}
	var r runStartResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		f.t.Fatalf("decode run.start: %v", err)
	}
	return r.RunID
}

func (f *taFixture) tree(taskID int64) map[int64]treeNode {
	f.t.Helper()
	return f.treeVia(f.ws, taskID)
}

func (f *taFixture) treeVia(ws *websocket.Conn, taskID int64) map[int64]treeNode {
	f.t.Helper()
	res, errObj := callTaskTree(f.t, ws, "tr", map[string]any{"taskId": taskID})
	if errObj != nil {
		f.t.Fatalf("task.tree error = %v", errObj.Message)
	}
	out := map[int64]treeNode{}
	for _, n := range res.Nodes {
		out[n.ID] = n
	}
	return out
}

func (f *taFixture) waitState(taskID, treeRoot int64, state string) {
	f.t.Helper()
	f.waitStateVia(f.ws, taskID, treeRoot, state)
}

func (f *taFixture) waitStateVia(ws *websocket.Conn, taskID, treeRoot int64, state string) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if n, ok := f.treeVia(ws, treeRoot)[taskID]; ok && n.Run.State == state {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("task %d never reached state %q", taskID, state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// finish makes taskID done (a "reply" run that completes by itself).
func (f *taFixture) finishDone(taskID, treeRoot int64) {
	f.t.Helper()
	f.startRun(f.ws, taskID)
	f.waitState(taskID, treeRoot, "done")
}

// finishStopped makes taskID stopped (a hanging run, stopped by run.stop).
func (f *taFixture) finishStopped(taskID, treeRoot int64) {
	f.t.Helper()
	runID := f.startRun(f.ws, taskID)
	f.waitState(taskID, treeRoot, "running")
	sendRequest(f.t, f.ws, "st", "run.stop", map[string]any{"runId": runID})
	readEnvelopeFor(f.t, f.ws, "st", 5*time.Second)
	f.waitState(taskID, treeRoot, "stopped")
}

func (f *taFixture) archiveFinished(taskID int64, dry bool) archiveFinishedResult {
	f.t.Helper()
	res, errObj := f.callArchiveFinished(taskID, dry)
	if errObj != nil {
		f.t.Fatalf("task.archiveFinished error = %v", errObj.Message)
	}
	return res
}

func (f *taFixture) callArchiveFinished(taskID int64, dry bool) (archiveFinishedResult, *rpcError) {
	f.t.Helper()
	sendRequest(f.t, f.ws, "af", "task.archiveFinished", map[string]any{"taskId": taskID, "dryRun": dry})
	resp := readEnvelopeFor(f.t, f.ws, "af", 20*time.Second)
	if resp.Error != nil {
		return archiveFinishedResult{}, resp.Error
	}
	var res archiveFinishedResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		f.t.Fatalf("decode archiveFinished: %v", err)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(resp.Result, &raw)
	for _, k := range []string{"eligible", "archived", "skipped", "failed"} {
		if string(raw[k]) == "null" || raw[k] == nil {
			f.t.Fatalf("archiveFinished.%s = %s, want a non-null array", k, raw[k])
		}
	}
	return res, nil
}

func (f *taFixture) stopTree(taskID int64, dry bool) stopTreeResult {
	f.t.Helper()
	res, errObj := f.callStopTree(taskID, dry)
	if errObj != nil {
		f.t.Fatalf("task.stopTree error = %v", errObj.Message)
	}
	return res
}

func (f *taFixture) callStopTree(taskID int64, dry bool) (stopTreeResult, *rpcError) {
	f.t.Helper()
	sendRequest(f.t, f.ws, "stp", "task.stopTree", map[string]any{"taskId": taskID, "dryRun": dry})
	resp := readEnvelopeFor(f.t, f.ws, "stp", 30*time.Second)
	if resp.Error != nil {
		return stopTreeResult{}, resp.Error
	}
	var res stopTreeResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		f.t.Fatalf("decode stopTree: %v", err)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(resp.Result, &raw)
	for _, k := range []string{"runs", "stopped", "failed"} {
		if string(raw[k]) == "null" || raw[k] == nil {
			f.t.Fatalf("stopTree.%s = %s, want a non-null array", k, raw[k])
		}
	}
	return res, nil
}

func (f *taFixture) isArchived(taskID int64) bool {
	f.t.Helper()
	got, err := f.wm.GetTask(taskID)
	if err != nil {
		f.t.Fatalf("GetTask(%d) error = %v", taskID, err)
	}
	return got.ArchivedAt != nil
}

func ids(items []treeItem) []int64 {
	out := []int64{}
	for _, i := range items {
		out = append(out, i.ID)
	}
	return out
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func skippedReason(res archiveFinishedResult, id int64) (string, bool) {
	for _, s := range res.Skipped {
		if s.ID == id {
			return s.Reason, true
		}
	}
	return "", false
}

// swapSeams replaces the package-level seams for one (non-parallel) test
// and restores them afterwards.
func swapSeams(t *testing.T) {
	t.Helper()
	cap0, dl0, arch0, await0, hook0 := treeArchiveCap, treeStopDeadline, treeArchiveTask, treeAwaitRun, treeAfterSelection
	t.Cleanup(func() {
		treeArchiveCap, treeStopDeadline, treeArchiveTask, treeAwaitRun, treeAfterSelection = cap0, dl0, arch0, await0, hook0
	})
}

func TestArchiveFinished_ArchivesOnlyFinishedDescendants(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	done := f.task("done", root, "reply")
	stopped := f.task("stopped", root, "hang")
	running := f.task("running", root, "hang")
	idle := f.task("idle", root, "")
	f.finishDone(done, root)
	f.finishStopped(stopped, root)
	f.startRun(f.ws, running)
	f.waitState(running, root, "running")

	res := f.archiveFinished(root, false)
	got := map[int64]bool{}
	for _, a := range res.Archived {
		got[a.ID] = true
	}
	if len(got) != 2 || !got[done] || !got[stopped] {
		t.Fatalf("archived = %+v, want exactly done %d and stopped %d", res.Archived, done, stopped)
	}
	for _, id := range []int64{root, running, idle} {
		if f.isArchived(id) {
			t.Fatalf("task %d was archived, want it untouched", id)
		}
	}
	if !f.isArchived(done) || !f.isArchived(stopped) {
		t.Fatal("finished children are not archived in the store")
	}
	if len(res.Failed) != 0 || res.Remaining != 0 || res.DryRun {
		t.Fatalf("result = %+v, want no failures, nothing remaining, dryRun false", res)
	}
}

func TestArchiveFinished_DeepTreeArchivesChildrenBeforeParents(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	a := f.task("a", root, "reply")
	b := f.task("b", a, "reply")
	d := f.task("d", a, "reply")
	for _, id := range []int64{a, b, d} {
		f.finishDone(id, root)
	}

	res := f.archiveFinished(root, false)
	if want := []int64{d, b, a}; !sameIDs(ids(res.Archived), want) {
		t.Fatalf("archived order = %v, want deepest first %v", ids(res.Archived), want)
	}
	if f.isArchived(root) {
		t.Fatal("the root was archived")
	}
}

func TestArchiveFinished_SkipsFinishedParentWithUnfinishedDescendant(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	parent := f.task("parent", root, "reply")
	child := f.task("child", parent, "hang")
	f.finishDone(parent, root)
	f.startRun(f.ws, child)
	f.waitState(child, root, "running")

	res := f.archiveFinished(root, false)
	if len(res.Archived) != 0 {
		t.Fatalf("archived = %+v, want nothing", res.Archived)
	}
	if reason, ok := skippedReason(res, parent); !ok || reason != "has unfinished descendants" {
		t.Fatalf("skipped = %+v, want parent %d with reason %q", res.Skipped, parent, "has unfinished descendants")
	}
	if f.isArchived(parent) || f.isArchived(child) {
		t.Fatal("parent or running child archived")
	}
}

func TestArchiveFinished_FailedChildKeepsItsAncestors(t *testing.T) {
	swapSeams(t)
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	parent := f.task("parent", root, "reply")
	bad := f.task("bad", parent, "reply")
	good := f.task("good", root, "reply")
	for _, id := range []int64{parent, bad, good} {
		f.finishDone(id, root)
	}
	treeArchiveTask = func(wm *workspace.Manager, id int64) error {
		if id == bad {
			return errors.New("boom")
		}
		_, err := wm.ArchiveTask(id)
		return err
	}

	res := f.archiveFinished(root, false)
	if len(res.Failed) != 1 || res.Failed[0].ID != bad || !strings.Contains(res.Failed[0].Error, "boom") {
		t.Fatalf("failed = %+v, want %d with the injected error", res.Failed, bad)
	}
	if reason, ok := skippedReason(res, parent); !ok || reason != "descendant failed to archive" {
		t.Fatalf("skipped = %+v, want parent %d with reason %q", res.Skipped, parent, "descendant failed to archive")
	}
	if want := []int64{good}; !sameIDs(ids(res.Archived), want) {
		t.Fatalf("archived = %v, want only the unrelated %v", ids(res.Archived), want)
	}
	if f.isArchived(parent) || f.isArchived(bad) {
		t.Fatal("parent or failed child archived despite the failure")
	}
}

func TestArchiveFinished_QueuedPromptMakesTaskNotFinished(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	queued := f.task("queued", root, "reply")
	plain := f.task("plain", root, "reply")
	f.finishDone(queued, root)
	f.finishDone(plain, root)
	chats, err := f.wm.ListChats(queued, false)
	if err != nil || len(chats) == 0 {
		t.Fatalf("ListChats() = %+v, %v", chats, err)
	}
	if _, err := f.db.EnqueueChatPrompt(chats[0].ID, "waiting", "{}", "human", 0, 0, 0); err != nil {
		t.Fatalf("EnqueueChatPrompt() error = %v", err)
	}

	res := f.archiveFinished(root, false)
	if want := []int64{plain}; !sameIDs(ids(res.Archived), want) {
		t.Fatalf("archived = %v, want only %v (a queued prompt is not finished)", ids(res.Archived), want)
	}
	if f.isArchived(queued) {
		t.Fatal("task with a queued prompt was archived")
	}
}

func TestArchiveFinished_RunStartingMidOperationIsSkippedNotArchived(t *testing.T) {
	swapSeams(t)
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	victim := f.task("victim", root, "reply")
	other := f.task("other", root, "reply")
	f.finishDone(victim, root)
	f.finishDone(other, root)
	treeAfterSelection = func() {
		// The selection holds both; now a long run starts on victim.
		vt, err := f.wm.GetTask(victim)
		if err != nil {
			t.Errorf("GetTask() error = %v", err)
			return
		}
		if err := os.WriteFile(filepath.Join(*vt.WorktreePath, "scenario"), []byte("hang"), 0o644); err != nil {
			t.Errorf("write scenario: %v", err)
		}
		f.startRun(f.dial(), victim)
	}

	res := f.archiveFinished(root, false)
	if want := []int64{other}; !sameIDs(ids(res.Archived), want) {
		t.Fatalf("archived = %v, want only %v", ids(res.Archived), want)
	}
	if _, ok := skippedReason(res, victim); !ok {
		t.Fatalf("skipped = %+v, want victim %d reported skipped", res.Skipped, victim)
	}
	got, err := f.wm.GetTask(victim)
	if err != nil || got.ArchivedAt != nil {
		t.Fatalf("victim = %+v, %v, want it live", got, err)
	}
	if got.WorktreePath == nil {
		t.Fatal("victim worktree was removed")
	}
	if _, err := os.Stat(*got.WorktreePath); err != nil {
		t.Fatalf("victim worktree gone: %v", err)
	}
}

func TestArchiveFinished_IdempotentSecondCallArchivesNothing(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	a := f.task("a", root, "reply")
	f.finishDone(a, root)

	if first := f.archiveFinished(root, false); len(first.Archived) != 1 {
		t.Fatalf("first call archived = %+v, want 1", first.Archived)
	}
	second := f.archiveFinished(root, false)
	if len(second.Archived) != 0 || len(second.Eligible) != 0 || len(second.Failed) != 0 {
		t.Fatalf("second call = %+v, want nothing to do", second)
	}
}

func TestArchiveFinished_DryRunChangesNothing(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	a := f.task("a", root, "reply")
	b := f.task("b", a, "reply")
	f.finishDone(a, root)
	f.finishDone(b, root)

	res := f.archiveFinished(root, true)
	if !res.DryRun || !sameIDs(ids(res.Eligible), []int64{b, a}) {
		t.Fatalf("dry run = %+v, want dryRun and eligible [%d %d]", res, b, a)
	}
	if len(res.Archived) != 0 || len(res.Failed) != 0 {
		t.Fatalf("dry run archived/failed = %+v / %+v, want empty", res.Archived, res.Failed)
	}
	if f.isArchived(a) || f.isArchived(b) {
		t.Fatal("dry run archived a task")
	}
}

func TestArchiveFinished_CapsAtHundredAndReportsRemaining(t *testing.T) {
	swapSeams(t)
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	for i := 0; i < 5; i++ {
		id := f.task("c", root, "reply")
		f.finishDone(id, root)
	}
	treeArchiveCap = 2

	res := f.archiveFinished(root, false)
	if len(res.Archived) != 2 || res.Remaining != 3 || len(res.Eligible) != 2 {
		t.Fatalf("result = %+v, want 2 archived/eligible and remaining 3", res)
	}
	// Repeating drains the rest.
	res = f.archiveFinished(root, false)
	res2 := f.archiveFinished(root, false)
	if len(res.Archived)+len(res2.Archived) != 3 || res2.Remaining != 0 {
		t.Fatalf("follow-ups = %+v / %+v, want the remaining 3 archived", res, res2)
	}
}

func TestArchiveFinished_OtherTreesUntouched(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	mine := f.task("mine", root, "reply")
	other := f.task("otherRoot", 0, "")
	theirs := f.task("theirs", other, "reply")
	sibling := f.task("sibling", 0, "reply")
	f.finishDone(mine, root)
	f.finishDone(theirs, other)
	f.finishDone(sibling, sibling)

	res := f.archiveFinished(root, false)
	if want := []int64{mine}; !sameIDs(ids(res.Archived), want) {
		t.Fatalf("archived = %v, want %v", ids(res.Archived), want)
	}
	if f.isArchived(theirs) || f.isArchived(sibling) || f.isArchived(other) {
		t.Fatal("a task outside the tree was archived")
	}
}

func TestArchiveFinished_UnknownArchivedAndMissingTaskErrors(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	gone := f.task("gone", root, "")
	if _, err := f.wm.ArchiveTask(gone); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	for name, id := range map[string]int64{"unknown": 999999, "archived": gone, "missing": 0} {
		if _, errObj := f.callArchiveFinished(id, false); errObj == nil {
			t.Fatalf("%s task: error = nil, want an error", name)
		}
	}
	sendRequest(t, f.ws, "bad", "task.archiveFinished", "nope")
	if resp := readEnvelopeFor(t, f.ws, "bad", 5*time.Second); resp.Error == nil {
		t.Fatal("bad params: error = nil, want an error")
	}
}

func TestArchiveFinished_ArchiveEventsEmittedPerTask(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	a := f.task("a", root, "reply")
	b := f.task("b", root, "reply")
	f.finishDone(a, root)
	f.finishDone(b, root)
	ec := newEventConn(t, f.dial())
	ec.subscribe("sub", TopicTaskArchived)

	f.archiveFinished(root, false)
	got := map[int64]bool{}
	for i := 0; i < 2; i++ {
		ev := ec.nextEvent(5 * time.Second)
		var p taskArchivedPayload
		decodePayload(t, ev, &p)
		got[p.Task.ID] = true
	}
	if len(got) != 2 || !got[a] || !got[b] {
		t.Fatalf("task.archived events for %v, want exactly %d and %d", got, a, b)
	}
}

func TestStopTree_StopsEveryRunInSubtreeIncludingRoot(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	child := f.task("child", root, "hang")
	grand := f.task("grand", child, "hang")
	want := map[int64]string{}
	for _, id := range []int64{root, child, grand} {
		want[id] = f.startRun(f.ws, id)
	}
	for _, id := range []int64{root, child, grand} {
		f.waitState(id, root, "running")
	}

	res := f.stopTree(root, false)
	if len(res.Stopped) != 3 || len(res.Failed) != 0 {
		t.Fatalf("result = %+v, want 3 stopped, none failed", res)
	}
	for _, s := range res.Stopped {
		if want[s.ID] != s.RunID {
			t.Fatalf("stopped %+v, want run %q", s, want[s.ID])
		}
	}
	for id, n := range f.tree(root) {
		if n.Run.State != "stopped" {
			t.Fatalf("task %d state = %q, want stopped", id, n.Run.State)
		}
	}
}

func TestStopTree_OnlyTouchesTheSubtree(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	sub := f.task("sub", root, "hang")
	sibling := f.task("sibling", root, "hang")
	other := f.task("other", 0, "hang")
	for _, id := range []int64{sub, sibling, other} {
		f.startRun(f.ws, id)
	}
	for _, id := range []int64{sub, sibling} {
		f.waitState(id, root, "running")
	}
	f.waitState(other, other, "running")

	res := f.stopTree(sub, false)
	if len(res.Stopped) != 1 || res.Stopped[0].ID != sub {
		t.Fatalf("stopped = %+v, want only %d", res.Stopped, sub)
	}
	if s := f.tree(root)[sibling].Run.State; s != "running" {
		t.Fatalf("sibling subtree state = %q, want running", s)
	}
	if s := f.tree(other)[other].Run.State; s != "running" {
		t.Fatalf("other tree state = %q, want running", s)
	}
}

func TestStopTree_CancelsQueuedPromptsBeforeStopping(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	runID := f.startRun(f.ws, root)
	f.waitState(root, root, "running")
	chats, err := f.wm.ListChats(root, false)
	if err != nil || len(chats) == 0 {
		t.Fatalf("ListChats() = %+v, %v", chats, err)
	}
	item, err := f.db.EnqueueChatPrompt(chats[0].ID, "next", "{}", "human", 0, 0, 0)
	if err != nil {
		t.Fatalf("EnqueueChatPrompt() error = %v", err)
	}

	res := f.stopTree(root, false)
	if res.QueuedCancelled != 1 || len(res.Stopped) != 1 || res.Stopped[0].RunID != runID {
		t.Fatalf("result = %+v, want 1 queued cancelled and the run stopped", res)
	}
	got, err := f.db.GetChatQueueItem(item.ID)
	if err != nil {
		t.Fatalf("GetChatQueueItem() error = %v", err)
	}
	if got.Status != store.ChatQueueStatusCancelled || got.CancelKind != store.ChatQueueCancelUser {
		t.Fatalf("queue item = status %q kind %q, want cancelled/%q", got.Status, got.CancelKind, store.ChatQueueCancelUser)
	}
	// Nothing was delivered after the stop.
	time.Sleep(200 * time.Millisecond)
	if s := f.tree(root)[root].Run.State; s != "stopped" {
		t.Fatalf("state after = %q, want stopped (no queued item delivered)", s)
	}
}

func TestStopTree_FreesSlotsUnderTheTreeGuard(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	var kids []int64
	f.startRun(f.ws, root)
	for i := 0; i < 3; i++ { // the default per-tree limit is 4
		id := f.task("k", root, "hang")
		kids = append(kids, id)
		f.startRun(f.ws, id)
	}
	fresh := f.task("fresh", root, "hang")
	sendRequest(t, f.ws, "full", "run.start", map[string]any{"taskId": fresh, "provider": "glm", "prompt": "x"})
	if resp := readEnvelopeFor(t, f.ws, "full", 5*time.Second); resp.Error == nil {
		t.Fatal("run.start at the tree limit: error = nil, want the guard to refuse (premise of the test)")
	}
	chats, err := f.wm.ListChats(kids[0], false)
	if err != nil || len(chats) == 0 {
		t.Fatalf("ListChats() = %+v, %v", chats, err)
	}
	if _, err := f.db.EnqueueChatPrompt(chats[0].ID, "queued", "{}", "human", 0, 0, 0); err != nil {
		t.Fatalf("EnqueueChatPrompt() error = %v", err)
	}

	res := f.stopTree(root, false)
	if len(res.Stopped) != 4 || res.QueuedCancelled != 1 {
		t.Fatalf("result = %+v, want 4 stopped and 1 queued cancelled", res)
	}
	f.startRun(f.ws, fresh) // fails the test on a guard refusal
	f.waitState(fresh, root, "running")
	// The cancelled prompt did not take the slot: only the fresh run runs.
	running := 0
	for _, n := range f.tree(root) {
		running += len(n.Run.RunningRunIDs)
	}
	if running != 1 {
		t.Fatalf("running runs = %d, want only the fresh one", running)
	}
}

func TestStopTree_RunFinishingMidOperationIsNotAnError(t *testing.T) {
	swapSeams(t)
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	child := f.task("child", root, "hang")
	f.startRun(f.ws, root)
	childRun := f.startRun(f.ws, child)
	f.waitState(root, root, "running")
	f.waitState(child, root, "running")
	treeAfterSelection = func() {
		// child's run ends by itself between selection and stop.
		// A separate connection: the main one is busy with the RPC itself.
		ws := f.dial()
		sendRequest(t, ws, "x", "run.stop", map[string]any{"runId": childRun})
		readEnvelopeFor(t, ws, "x", 5*time.Second)
		f.waitStateVia(ws, child, root, "stopped")
	}

	res := f.stopTree(root, false)
	if len(res.Failed) != 0 {
		t.Fatalf("failed = %+v, want none (a run finishing on its own is not an error)", res.Failed)
	}
	if s := f.tree(root)[root].Run.State; s != "stopped" {
		t.Fatalf("root state = %q, want stopped", s)
	}
}

func TestStopTree_RunStartedDuringStopIsStoppedBySecondPass(t *testing.T) {
	swapSeams(t)
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	late := f.task("late", root, "hang")
	f.startRun(f.ws, root)
	f.waitState(root, root, "running")
	var lateRun string
	treeAfterSelection = func() {
		lateRun = f.startRun(f.dial(), late) // a live parent spawns work mid-stop
	}

	res := f.stopTree(root, false)
	found := false
	for _, s := range res.Stopped {
		if s.RunID == lateRun && lateRun != "" {
			found = true
		}
	}
	if !found || len(res.Failed) != 0 {
		t.Fatalf("result = %+v, want the late run %q stopped by a later pass", res, lateRun)
	}
	if s := f.tree(root)[late].Run.State; s != "stopped" {
		t.Fatalf("late state = %q, want stopped", s)
	}
}

func TestStopTree_StuckRunIsReportedFailed(t *testing.T) {
	swapSeams(t)
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	stuck := f.task("stuck", root, "hang")
	f.startRun(f.ws, root)
	stuckRun := f.startRun(f.ws, stuck)
	f.waitState(root, root, "running")
	f.waitState(stuck, root, "running")
	treeStopDeadline = 300 * time.Millisecond
	treeAwaitRun = func(ctx context.Context, reg *runs.Registry, runID string) error {
		if runID == stuckRun {
			<-ctx.Done() // simulates a run that never reaches a terminal state
			return ctx.Err()
		}
		return reg.StopAndWait(ctx, runID)
	}

	res := f.stopTree(root, false)
	if len(res.Failed) != 1 || res.Failed[0].RunID != stuckRun || res.Failed[0].Error == "" {
		t.Fatalf("failed = %+v, want the stuck run %q with an error", res.Failed, stuckRun)
	}
	if len(res.Stopped) != 1 {
		t.Fatalf("stopped = %+v, want the one healthy run", res.Stopped)
	}
}

func TestStopTree_DryRunStopsNothing(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	child := f.task("child", root, "hang")
	f.startRun(f.ws, root)
	f.startRun(f.ws, child)
	f.waitState(root, root, "running")
	f.waitState(child, root, "running")
	chats, err := f.wm.ListChats(child, false)
	if err != nil || len(chats) == 0 {
		t.Fatalf("ListChats() = %+v, %v", chats, err)
	}
	item, err := f.db.EnqueueChatPrompt(chats[0].ID, "q", "{}", "human", 0, 0, 0)
	if err != nil {
		t.Fatalf("EnqueueChatPrompt() error = %v", err)
	}

	res := f.stopTree(root, true)
	if !res.DryRun || len(res.Runs) != 2 || res.QueuedPrompts != 1 {
		t.Fatalf("dry run = %+v, want 2 runs and 1 queued prompt", res)
	}
	if len(res.Stopped) != 0 || len(res.Failed) != 0 || res.QueuedCancelled != 0 {
		t.Fatalf("dry run outcome = %+v, want empty", res)
	}
	for id, n := range f.tree(root) {
		if n.Run.State != "running" {
			t.Fatalf("task %d state = %q, want running", id, n.Run.State)
		}
	}
	if got, err := f.db.GetChatQueueItem(item.ID); err != nil || got.Status != store.ChatQueueStatusQueued {
		t.Fatalf("queue item = %+v, %v, want still queued", got, err)
	}
}

func TestStopTree_IdempotentSecondCallStopsNothing(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "hang")
	f.startRun(f.ws, root)
	f.waitState(root, root, "running")

	if first := f.stopTree(root, false); len(first.Stopped) != 1 {
		t.Fatalf("first call = %+v, want 1 stopped", first)
	}
	second := f.stopTree(root, false)
	if len(second.Stopped) != 0 || len(second.Failed) != 0 || second.QueuedCancelled != 0 {
		t.Fatalf("second call = %+v, want nothing to do", second)
	}
}

func TestStopTree_UnknownAndArchivedTaskErrors(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	root := f.task("root", 0, "")
	gone := f.task("gone", root, "")
	if _, err := f.wm.ArchiveTask(gone); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	for name, id := range map[string]int64{"unknown": 999999, "archived": gone, "missing": 0} {
		if _, errObj := f.callStopTree(id, false); errObj == nil {
			t.Fatalf("%s task: error = nil, want an error", name)
		}
	}
}

// TestTreeActions_NotExposedOverMCP pins that an agent cannot cascade-
// archive or kill a tree: neither RPC may be reachable from the MCP
// server (cmd/smind), whose tools are its only wsapi call sites.
func TestTreeActions_NotExposedOverMCP(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "cmd", "smind")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", dir, err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", e.Name(), err)
		}
		checked++
		for _, name := range []string{"task.archiveFinished", "task.stopTree", "archiveFinished", "stopTree"} {
			if strings.Contains(string(data), name) {
				t.Fatalf("cmd/smind/%s mentions %q: tree actions must not be exposed over MCP", e.Name(), name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no cmd/smind sources scanned")
	}
}
