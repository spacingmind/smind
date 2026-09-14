package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Tests for ADR 0009's eight lifecycle topics: each mutation path emits
// exactly one event with the documented full-entity (or id+scope, for
// deletes) payload, and a second client subscribed to the same topic
// receives it independently -- the actual gap (cross-client staleness,
// gap-matrix.md item 8) this is fixing.

func TestEvents_WorkspaceCreatedSubscribeAndReceive(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicWorkspaceCreated)

	repo := newTestRepo(t)
	sendRequest(t, ec.ws, "create", "workspace.create", map[string]any{
		"path": repo, "title": "W", "routingPolicy": "hard",
	})
	resp := ec.nextResponse("create", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("workspace.create error = %v", resp.Error.Message)
	}
	var created store.Workspace
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		t.Fatalf("decode workspace.create result: %v", err)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicWorkspaceCreated || ev.Seq != 1 {
		t.Fatalf("event = %+v, want topic %q seq 1", ev, TopicWorkspaceCreated)
	}
	var p workspaceCreatedPayload
	decodePayload(t, ev, &p)
	if p.Workspace.ID != created.ID || p.Workspace.Path != repo || p.Workspace.Title != "W" {
		t.Fatalf("workspace.created payload = %+v, want the created workspace %+v", p.Workspace, created)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

func TestEvents_WorkspaceCreatedReachesASecondClient(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	// ecA mutates; ecB is the "second browser tab" that must see it without
	// any refetch, and without itself ever having called workspace.create.
	ecA := newEventConn(t, dialWS(t, srv, "tok"))
	ecB := newEventConn(t, dialWS(t, srv, "tok"))
	ecB.subscribe("sub", TopicWorkspaceCreated)

	repo := newTestRepo(t)
	sendRequest(t, ecA.ws, "create", "workspace.create", map[string]any{
		"path": repo, "title": "W", "routingPolicy": "hard",
	})
	if resp := ecA.nextResponse("create", 5*time.Second); resp.Error != nil {
		t.Fatalf("workspace.create error = %v", resp.Error.Message)
	}

	ev := ecB.nextEvent(5 * time.Second)
	if ev.Topic != TopicWorkspaceCreated {
		t.Fatalf("ecB event topic = %q, want %q", ev.Topic, TopicWorkspaceCreated)
	}
	var p workspaceCreatedPayload
	decodePayload(t, ev, &p)
	if p.Workspace.Path != repo {
		t.Fatalf("workspace.created payload path = %q, want %q", p.Workspace.Path, repo)
	}
}

func TestEvents_WorkspaceDeletedIsRootOnlyCascade(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicWorkspaceDeleted, TopicSpaceDeleted, TopicTaskDeleted)

	task := newTestTask(t, wm, "")
	sp, err := wm.CreateSpace(task.WorkspaceID, "S", "")
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	if _, err := wm.CreateTask(task.WorkspaceID, &sp.ID, "Task2"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	sendRequest(t, ec.ws, "del", "workspace.delete", map[string]any{"id": task.WorkspaceID})
	if resp := ec.nextResponse("del", 5*time.Second); resp.Error != nil {
		t.Fatalf("workspace.delete error = %v", resp.Error.Message)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicWorkspaceDeleted {
		t.Fatalf("event topic = %q, want %q (cascaded space/task deletes must not publish their own events)", ev.Topic, TopicWorkspaceDeleted)
	}
	var p workspaceDeletedPayload
	decodePayload(t, ev, &p)
	if p.ID != task.WorkspaceID {
		t.Fatalf("workspace.deleted payload id = %d, want %d", p.ID, task.WorkspaceID)
	}
	// Only the one root event: no space.deleted/task.deleted followed it.
	ec.expectNoEvent(300 * time.Millisecond)
}

func TestEvents_SpaceCreatedSubscribeAndReceive(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicSpaceCreated)

	task := newTestTask(t, wm, "")
	sendRequest(t, ec.ws, "create", "space.create", map[string]any{
		"workspaceId": task.WorkspaceID, "title": "S", "envData": "",
	})
	resp := ec.nextResponse("create", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("space.create error = %v", resp.Error.Message)
	}
	var created store.Space
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		t.Fatalf("decode space.create result: %v", err)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicSpaceCreated {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicSpaceCreated)
	}
	var p spaceCreatedPayload
	decodePayload(t, ev, &p)
	if p.Space.ID != created.ID || p.Space.WorkspaceID != task.WorkspaceID || p.Space.Title != "S" {
		t.Fatalf("space.created payload = %+v, want the created space %+v", p.Space, created)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

func TestEvents_SpaceDeletedCascadesWithoutPerTaskEvents(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicSpaceDeleted, TopicTaskDeleted)

	task := newTestTask(t, wm, "")
	sp, err := wm.CreateSpace(task.WorkspaceID, "S", "")
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	if _, err := wm.CreateTask(task.WorkspaceID, &sp.ID, "Task2"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	sendRequest(t, ec.ws, "del", "space.delete", map[string]any{"id": sp.ID})
	if resp := ec.nextResponse("del", 5*time.Second); resp.Error != nil {
		t.Fatalf("space.delete error = %v", resp.Error.Message)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicSpaceDeleted {
		t.Fatalf("event topic = %q, want %q (cascaded task delete must not publish its own event)", ev.Topic, TopicSpaceDeleted)
	}
	var p spaceDeletedPayload
	decodePayload(t, ev, &p)
	if p.ID != sp.ID || p.WorkspaceID != task.WorkspaceID {
		t.Fatalf("space.deleted payload = %+v, want id %d workspaceId %d", p, sp.ID, task.WorkspaceID)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

func TestEvents_TaskCreatedSubscribeAndReceive(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicTaskCreated)

	repo := newTestRepo(t)
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	sendRequest(t, ec.ws, "create", "task.create", map[string]any{
		"workspaceId": ws.ID, "title": "Task",
	})
	resp := ec.nextResponse("create", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.create error = %v", resp.Error.Message)
	}
	var created store.Task
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		t.Fatalf("decode task.create result: %v", err)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicTaskCreated {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicTaskCreated)
	}
	var p taskCreatedPayload
	decodePayload(t, ev, &p)
	if p.Task.ID != created.ID || p.Task.WorkspaceID != ws.ID || p.Task.Status != "created" {
		t.Fatalf("task.created payload = %+v, want the created task %+v", p.Task, created)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

func TestEvents_TaskArchivedSubscribeAndReceive(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	task := newTestTask(t, wm, "")
	ec.subscribe("sub", TopicTaskArchived)

	sendRequest(t, ec.ws, "arch", "task.archive", map[string]any{"id": task.ID})
	if resp := ec.nextResponse("arch", 5*time.Second); resp.Error != nil {
		t.Fatalf("task.archive error = %v", resp.Error.Message)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicTaskArchived {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicTaskArchived)
	}
	var p taskArchivedPayload
	decodePayload(t, ev, &p)
	if p.Task.ID != task.ID || p.Task.Status != "archived" || p.Task.ArchivedAt == nil {
		t.Fatalf("task.archived payload = %+v, want task %d archived with ArchivedAt set", p.Task, task.ID)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

// TestEvents_TaskArchivedPrecedesTaskStatus pins ADR 0009's ordering
// clause: a client subscribed to both a lifecycle topic and task.status
// sees the lifecycle event first, so the row already exists in the tree
// by the time the status update arrives.
func TestEvents_TaskArchivedPrecedesTaskStatus(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	task := newTestTask(t, wm, "")
	ec.subscribe("sub", TopicTaskArchived, TopicTaskStatus)

	sendRequest(t, ec.ws, "arch", "task.archive", map[string]any{"id": task.ID})
	if resp := ec.nextResponse("arch", 5*time.Second); resp.Error != nil {
		t.Fatalf("task.archive error = %v", resp.Error.Message)
	}

	first := ec.nextEvent(5 * time.Second)
	second := ec.nextEvent(5 * time.Second)
	if first.Topic != TopicTaskArchived || second.Topic != TopicTaskStatus {
		t.Fatalf("event order = [%s, %s], want [%s, %s]", first.Topic, second.Topic, TopicTaskArchived, TopicTaskStatus)
	}
}

// TestEvents_TaskUpdatedOnRunTask exercises RunTask's task.updated event
// by calling Manager.RunTask directly rather than through a wsapi RPC:
// RunTask has no wsapi method wired to it (task.prompt/run.start never
// call it -- see ADR 0009's consequences), so there is no request to drive
// this over the wire yet, but the Notifier contract is still exercised
// end to end through the real event bus a wsapi client is subscribed to.
func TestEvents_TaskUpdatedOnRunTask(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicTaskUpdated)

	task := newTestTask(t, wm, "")
	updated, err := wm.RunTask(task.ID)
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicTaskUpdated {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicTaskUpdated)
	}
	var p taskUpdatedPayload
	decodePayload(t, ev, &p)
	if p.Task.ID != updated.ID || p.Task.Status != "running" {
		t.Fatalf("task.updated payload = %+v, want task %d status running", p.Task, task.ID)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

// TestEvents_TaskDeletedOnManagerDeleteTask is DeleteTask's counterpart to
// TestEvents_TaskUpdatedOnRunTask: task.delete has no wsapi RPC either
// (only the cascading workspace.delete/space.delete paths are wired), so
// this drives Manager.DeleteTask directly and asserts the event still
// reaches a subscribed connection through the real bus.
func TestEvents_TaskDeletedOnManagerDeleteTask(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicTaskDeleted)

	task := newTestTask(t, wm, "")
	if _, err := wm.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicTaskDeleted {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicTaskDeleted)
	}
	var p taskDeletedPayload
	decodePayload(t, ev, &p)
	if p.ID != task.ID || p.WorkspaceID != task.WorkspaceID || p.SpaceID != nil {
		t.Fatalf("task.deleted payload = %+v, want id %d workspaceId %d spaceId nil", p, task.ID, task.WorkspaceID)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

// TestEvents_LifecycleTopicsAreOptIn confirms an unrelated subscriber
// (task.status only, the pre-ADR-0009 default) sees none of the new
// topics -- the additive-wire-change guarantee (ADR 0009's whole premise:
// existing clients are unaffected until they opt in).
func TestEvents_LifecycleTopicsAreOptIn(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicTaskStatus)

	repo := newTestRepo(t)
	if _, err := wm.CreateWorkspace(repo, "W", "hard", nil); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}
