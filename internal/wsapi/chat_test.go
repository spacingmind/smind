package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Tests for ADR-0016 P1's wire surface: chat.create/list/get/rename/archive,
// their lifecycle events, task.prompt/run.start's optional chatId, and
// run.list's chatId filter.

func TestChat_CreateListGetRenameArchive_RoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "list0", "chat.list", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "list0", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.list error = %v", resp.Error.Message)
	}
	var initial []store.Chat
	if err := json.Unmarshal(resp.Result, &initial); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	if len(initial) != 1 || initial[0].Title != "Chat" {
		t.Fatalf("chat.list = %+v, want exactly the task's default \"Chat\"", initial)
	}

	sendRequest(t, ws, "create", "chat.create", map[string]any{"taskId": task.ID, "title": "Second chat"})
	resp = readEnvelopeFor(t, ws, "create", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.create error = %v", resp.Error.Message)
	}
	var created store.Chat
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		t.Fatalf("decode chat.create result: %v", err)
	}
	if created.Title != "Second chat" || created.TaskID != task.ID {
		t.Fatalf("chat.create result = %+v, want title %q under task %d", created, "Second chat", task.ID)
	}
	if created.Provider != nil {
		t.Fatalf("chat.create result Provider = %v, want nil", created.Provider)
	}

	sendRequest(t, ws, "get", "chat.get", map[string]any{"id": created.ID})
	resp = readEnvelopeFor(t, ws, "get", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.get error = %v", resp.Error.Message)
	}
	var got store.Chat
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("decode chat.get result: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("chat.get result = %+v, want id %d", got, created.ID)
	}

	sendRequest(t, ws, "rename", "chat.rename", map[string]any{"id": created.ID, "title": "Renamed"})
	resp = readEnvelopeFor(t, ws, "rename", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.rename error = %v", resp.Error.Message)
	}
	var renamed store.Chat
	if err := json.Unmarshal(resp.Result, &renamed); err != nil {
		t.Fatalf("decode chat.rename result: %v", err)
	}
	if renamed.Title != "Renamed" {
		t.Fatalf("chat.rename result Title = %q, want %q", renamed.Title, "Renamed")
	}

	sendRequest(t, ws, "archive", "chat.archive", map[string]any{"id": created.ID})
	resp = readEnvelopeFor(t, ws, "archive", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.archive error = %v", resp.Error.Message)
	}
	var archived store.Chat
	if err := json.Unmarshal(resp.Result, &archived); err != nil {
		t.Fatalf("decode chat.archive result: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatalf("chat.archive result ArchivedAt = nil, want set")
	}

	sendRequest(t, ws, "list1", "chat.list", map[string]any{"taskId": task.ID})
	resp = readEnvelopeFor(t, ws, "list1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.list error = %v", resp.Error.Message)
	}
	var active []store.Chat
	if err := json.Unmarshal(resp.Result, &active); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("chat.list (active only) = %+v, want just the default chat, archived one excluded", active)
	}

	sendRequest(t, ws, "list2", "chat.list", map[string]any{"taskId": task.ID, "includeArchived": true})
	resp = readEnvelopeFor(t, ws, "list2", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.list(includeArchived) error = %v", resp.Error.Message)
	}
	var all []store.Chat
	if err := json.Unmarshal(resp.Result, &all); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("chat.list(includeArchived=true) = %+v, want both chats", all)
	}
}

func TestChat_UnknownID_IsClearNotFoundError(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	cases := []struct {
		method string
		params map[string]any
	}{
		{"chat.get", map[string]any{"id": 999999}},
		{"chat.rename", map[string]any{"id": 999999, "title": "x"}},
		{"chat.archive", map[string]any{"id": 999999}},
	}
	for _, tc := range cases {
		sendRequest(t, ws, tc.method, tc.method, tc.params)
		resp := readEnvelopeFor(t, ws, tc.method, 5*time.Second)
		if resp.Error == nil {
			t.Fatalf("%s(unknown id): error = nil, want a clear not-found error", tc.method)
		}
	}
}

func TestChat_Archive_RefusedWhileRunning(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "hang")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "list", "chat.list", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var chats []store.Chat
	if err := json.Unmarshal(resp.Result, &chats); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	chatID := chats[0].ID

	sendRequest(t, ws, "start", "run.start", map[string]any{
		"taskId": task.ID, "chatId": chatID, "provider": "glm", "prompt": "hi",
	})
	resp = readEnvelopeFor(t, ws, "start", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start error = %v", resp.Error.Message)
	}
	var started runStartResult
	if err := json.Unmarshal(resp.Result, &started); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}
	t.Cleanup(func() {
		sendRequest(t, ws, "cleanup-stop", "run.stop", map[string]any{"runId": started.RunID})
		readEnvelopeFor(t, ws, "cleanup-stop", 5*time.Second)
	})

	sendRequest(t, ws, "archive-busy", "chat.archive", map[string]any{"id": chatID})
	resp = readEnvelopeFor(t, ws, "archive-busy", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("chat.archive while a run is running: error = nil, want a clear error")
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	if resp := readEnvelopeFor(t, ws, "stop", 5*time.Second); resp.Error != nil {
		t.Fatalf("run.stop error = %v", resp.Error.Message)
	}

	// run.stop only requests cancellation; the drive goroutine transitions
	// the run out of "running" asynchronously, so chat.archive's own
	// reg.List-based check can still see it as running for a brief window
	// right after run.stop returns. Poll chat.archive until that window
	// closes, rather than asserting success on the very next call.
	deadline := time.Now().Add(5 * time.Second)
	for {
		sendRequest(t, ws, "archive-ok", "chat.archive", map[string]any{"id": chatID})
		resp = readEnvelopeFor(t, ws, "archive-ok", 5*time.Second)
		if resp.Error == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("chat.archive after stop error = %v, want success", resp.Error.Message)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestChat_LifecycleEvents(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicChatCreated, TopicChatUpdated, TopicChatArchived)

	sendRequest(t, ec.ws, "create", "chat.create", map[string]any{"taskId": task.ID, "title": "New"})
	resp := ec.nextResponse("create", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.create error = %v", resp.Error.Message)
	}
	var created store.Chat
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		t.Fatalf("decode chat.create result: %v", err)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicChatCreated {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicChatCreated)
	}
	var createdPayload chatCreatedPayload
	decodePayload(t, ev, &createdPayload)
	if createdPayload.Chat.ID != created.ID {
		t.Fatalf("chat.created payload = %+v, want chat %d", createdPayload.Chat, created.ID)
	}

	sendRequest(t, ec.ws, "rename", "chat.rename", map[string]any{"id": created.ID, "title": "Renamed"})
	if resp := ec.nextResponse("rename", 5*time.Second); resp.Error != nil {
		t.Fatalf("chat.rename error = %v", resp.Error.Message)
	}
	ev = ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicChatUpdated {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicChatUpdated)
	}
	var updatedPayload chatUpdatedPayload
	decodePayload(t, ev, &updatedPayload)
	if updatedPayload.Chat.Title != "Renamed" {
		t.Fatalf("chat.updated payload Title = %q, want %q", updatedPayload.Chat.Title, "Renamed")
	}

	sendRequest(t, ec.ws, "archive", "chat.archive", map[string]any{"id": created.ID})
	if resp := ec.nextResponse("archive", 5*time.Second); resp.Error != nil {
		t.Fatalf("chat.archive error = %v", resp.Error.Message)
	}
	ev = ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicChatArchived {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicChatArchived)
	}
	var archivedPayload chatArchivedPayload
	decodePayload(t, ev, &archivedPayload)
	if archivedPayload.Chat.ArchivedAt == nil {
		t.Fatalf("chat.archived payload ArchivedAt = nil, want set")
	}
}

// TestTaskPrompt_OmittedChatId_LandsOnDefaultChat proves the ADR-0016
// P1.6 compatibility path end to end over the wire: a task.prompt call
// carrying no chatId at all -- exactly what every pre-chats client sends --
// still lands its run on the task's one (default) chat.
func TestTaskPrompt_OmittedChatId_LandsOnDefaultChat(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "list", "chat.list", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var chats []store.Chat
	if err := json.Unmarshal(resp.Result, &chats); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	defaultChatID := chats[0].ID

	sendRequest(t, ws, "prompt", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	if resp := readEnvelopeFor(t, ws, "prompt", 5*time.Second); resp.Error != nil {
		t.Fatalf("task.prompt error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "filtered", "run.list", map[string]any{"chatId": defaultChatID})
	resp = readEnvelopeFor(t, ws, "filtered", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.list error = %v", resp.Error.Message)
	}
	var runs []runStatusForTest
	if err := json.Unmarshal(resp.Result, &runs); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("run.list(chatId=%d) = %+v, want exactly the task.prompt run", defaultChatID, runs)
	}
}

// TestTaskPrompt_ExplicitChatId_LandsOnThatChat proves task.prompt with an
// explicit chatId lands its run there, not the task's default chat, and
// run.list's chatId filter separates the two.
func TestTaskPrompt_ExplicitChatId_LandsOnThatChat(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "create", "chat.create", map[string]any{"taskId": task.ID, "title": "Second"})
	resp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	var second store.Chat
	if err := json.Unmarshal(resp.Result, &second); err != nil {
		t.Fatalf("decode chat.create result: %v", err)
	}

	sendRequest(t, ws, "prompt", "task.prompt", map[string]any{
		"taskId": task.ID, "chatId": second.ID, "provider": "glm", "prompt": "hi",
	})
	if resp := readEnvelopeFor(t, ws, "prompt", 5*time.Second); resp.Error != nil {
		t.Fatalf("task.prompt error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "list", "chat.list", map[string]any{"taskId": task.ID})
	resp = readEnvelopeFor(t, ws, "list", 5*time.Second)
	var chats []store.Chat
	if err := json.Unmarshal(resp.Result, &chats); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	var defaultChatID int64
	for _, c := range chats {
		if c.ID != second.ID {
			defaultChatID = c.ID
		}
	}

	sendRequest(t, ws, "default-runs", "run.list", map[string]any{"chatId": defaultChatID})
	resp = readEnvelopeFor(t, ws, "default-runs", 5*time.Second)
	var defaultRuns []runStatusForTest
	if err := json.Unmarshal(resp.Result, &defaultRuns); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(defaultRuns) != 0 {
		t.Fatalf("run.list(chatId=default %d) = %+v, want none (the run went to the second chat)", defaultChatID, defaultRuns)
	}

	sendRequest(t, ws, "second-runs", "run.list", map[string]any{"chatId": second.ID})
	resp = readEnvelopeFor(t, ws, "second-runs", 5*time.Second)
	var secondRuns []runStatusForTest
	if err := json.Unmarshal(resp.Result, &secondRuns); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(secondRuns) != 1 {
		t.Fatalf("run.list(chatId=%d) = %+v, want exactly the task.prompt run", second.ID, secondRuns)
	}
}

// TestEvents_RunStatusCarriesChatID proves run.status events carry chatId
// (ADR-0016 P1.4), alongside taskId, matching whichever chat the run
// actually landed on.
func TestEvents_RunStatusCarriesChatID(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "list", "chat.list", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var chats []store.Chat
	if err := json.Unmarshal(resp.Result, &chats); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	defaultChatID := chats[0].ID

	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicRunStatus)

	sendRequest(t, ws, "start", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	if resp := readEnvelopeFor(t, ws, "start", 5*time.Second); resp.Error != nil {
		t.Fatalf("run.start error = %v", resp.Error.Message)
	}

	sawChatID := false
	for i := 0; i < 10; i++ {
		ev := ec.nextEvent(5 * time.Second)
		if ev.Topic != TopicRunStatus {
			continue
		}
		var p runStatusPayload
		decodePayload(t, ev, &p)
		if p.TaskID != task.ID {
			continue
		}
		if p.ChatID != defaultChatID {
			t.Fatalf("run.status payload ChatID = %d, want %d", p.ChatID, defaultChatID)
		}
		sawChatID = true
		if p.Status == "done" {
			break
		}
	}
	if !sawChatID {
		t.Fatal("never observed a run.status event for this run")
	}
}

// runStatusForTest decodes just the fields these tests check off a
// run.list result -- run.list returns runs.RunSummary (an alias of
// runs.RunStatus), whose Go field names are the wire keys verbatim (no
// json tags), same convention as store's own no-tag structs.
type runStatusForTest struct {
	ID     string
	TaskID int64
	ChatID int64
	Status string
}
