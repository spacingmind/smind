package wsapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeACPAgentPath is the compiled internal/taskrunner/fakeagent binary,
// reused here (rather than inventing a second scripted agent) so
// task.prompt tests can drive Runner's GLM path against a real ACP
// subprocess without depending on npx/network access.
var fakeACPAgentPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "smind-wsapi-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fakeACPAgentPath = filepath.Join(dir, "fakeagent")
	build := exec.Command("go", "build", "-o", fakeACPAgentPath, "../taskrunner/fakeagent")
	build.Dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build fakeagent: %v: %s", err, out))
	}

	smindHome, err := os.MkdirTemp("", "smind-wsapi-home-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(smindHome)
	os.Setenv("SMIND_HOME", smindHome)

	// terminal.* tests spawn a real shell via internal/terminal's
	// resolveShell, which honors $SHELL -- force it to a plain /bin/bash
	// (when available) rather than the dev machine's own interactive
	// shell (zsh with dotfiles/oh-my-zsh/etc., in this sandbox), so
	// terminal output assertions aren't at the mercy of one developer's
	// shell config/startup chatter/slower startup under parallel load.
	// Same rationale as internal/terminal's own tests (see that package's
	// forceTestShell).
	if _, err := os.Stat("/bin/bash"); err == nil {
		os.Setenv("SHELL", "/bin/bash")
	}

	os.Exit(m.Run())
}

// newTestWorkspaceManager also returns the underlying store: newTestWSServer
// needs it to back the /ws API's runs.Registry persistence with the same
// database wm's tasks live in (runs.task_id references tasks(id), enforced
// via the foreign_keys pragma -- see store.sqliteDSN).
func newTestWorkspaceManager(t *testing.T) (*workspace.Manager, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	return workspace.New(s), s
}

// newTestRepo creates a real git repository in a temp dir with one commit,
// so worktree creation (via workspace.Manager.CreateTask) has a commit to
// branch from.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGitT(t, dir, "init")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitT(t, dir, "add", "README.md")
	runGitT(t, dir, "commit", "-m", "initial commit")

	return dir
}

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
}

// newTestTask creates a workspace and a task with a real git worktree, and
// (if scenario is non-empty) writes it as the "scenario" file inside the
// worktree for the fake agent to read -- see internal/taskrunner/fakeagent.
func newTestTask(t *testing.T, wm *workspace.Manager, scenario string) store.Task {
	t.Helper()

	repo := newTestRepo(t)
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := wm.CreateTask(ws.ID, nil, "Task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if scenario != "" {
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
			t.Fatalf("write scenario file: %v", err)
		}
	}

	return task
}

func newTestRunner(wm *workspace.Manager) *taskrunner.Runner {
	return taskrunner.New(wm, taskrunner.WithACPCommand([]string{fakeACPAgentPath}))
}

func newTestWSServer(t *testing.T, wm *workspace.Manager, runner *taskrunner.Runner, db *store.Store, token string) *httptest.Server {
	t.Helper()
	handler, err := Handler(wm, runner, db, token)
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func dialWS(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	u := "ws" + srv.URL[len("http"):] + "/ws?token=" + token
	ws, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", u, err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func sendRequest(t *testing.T, ws *websocket.Conn, id, method string, params any) {
	t.Helper()
	raw := marshalOrNull(params)
	data, err := json.Marshal(envelope{ID: id, Method: method, Params: raw})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
}

func sendCancel(t *testing.T, ws *websocket.Conn, id string) {
	t.Helper()
	raw := marshalOrNull(cancelParams{ID: id})
	data, err := json.Marshal(envelope{Method: "task.cancel", Params: raw})
	if err != nil {
		t.Fatalf("marshal cancel: %v", err)
	}
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
}

// readEnvelopeFor reads messages off ws, discarding any not addressed to
// id, until one addressed to id arrives or timeout elapses. Tests use this
// instead of assuming the very next message on the wire is theirs, since a
// single connection can have other requests' responses/events interleaved
// on it.
func readEnvelopeFor(t *testing.T, ws *websocket.Conn, id string, timeout time.Duration) envelope {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for a message with id %q", id)
		}
		if err := ws.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline() error = %v", err)
		}
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage() error = %v (waiting for id %q)", err, id)
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v: %s", err, data)
		}
		if env.ID == id {
			return env
		}
	}
}

// expectNoMoreMessages asserts that no message for id arrives on ws within
// timeout, proving a request that already got its terminal response
// doesn't also emit a later, duplicate one.
func expectNoMoreMessages(t *testing.T, ws *websocket.Conn, id string, timeout time.Duration) {
	t.Helper()
	if err := ws.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	_, data, err := ws.ReadMessage()
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return
	}
	if err != nil {
		t.Fatalf("ReadMessage() error = %v, want a read timeout", err)
	}
	var env envelope
	_ = json.Unmarshal(data, &env)
	if env.ID == id {
		t.Fatalf("received an unexpected extra message for id %q: %s", id, data)
	}
}

func TestServer_WorkspaceSpaceTaskCRUDRoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	repo := newTestRepo(t)

	sendRequest(t, ws, "1", "workspace.create", map[string]any{
		"path": repo, "title": "W1", "routingPolicy": "hard", "accountIds": []int64{},
	})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("workspace.create error = %v", resp.Error.Message)
	}
	var createdWorkspace store.Workspace
	if err := json.Unmarshal(resp.Result, &createdWorkspace); err != nil {
		t.Fatalf("decode workspace.create result: %v", err)
	}
	if createdWorkspace.Title != "W1" {
		t.Fatalf("workspace.Title = %q, want %q", createdWorkspace.Title, "W1")
	}

	sendRequest(t, ws, "2", "workspace.list", nil)
	resp = readEnvelopeFor(t, ws, "2", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("workspace.list error = %v", resp.Error.Message)
	}
	var workspaces []store.Workspace
	if err := json.Unmarshal(resp.Result, &workspaces); err != nil {
		t.Fatalf("decode workspace.list result: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("got %d workspaces, want 1", len(workspaces))
	}

	sendRequest(t, ws, "3", "workspace.get", map[string]any{"id": createdWorkspace.ID})
	resp = readEnvelopeFor(t, ws, "3", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("workspace.get error = %v", resp.Error.Message)
	}
	var gotWorkspace store.Workspace
	if err := json.Unmarshal(resp.Result, &gotWorkspace); err != nil {
		t.Fatalf("decode workspace.get result: %v", err)
	}
	if gotWorkspace.ID != createdWorkspace.ID {
		t.Fatalf("workspace.get id = %d, want %d", gotWorkspace.ID, createdWorkspace.ID)
	}

	sendRequest(t, ws, "4", "space.create", map[string]any{
		"workspaceId": createdWorkspace.ID, "title": "S1", "envData": "{}",
	})
	resp = readEnvelopeFor(t, ws, "4", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("space.create error = %v", resp.Error.Message)
	}
	var createdSpace store.Space
	if err := json.Unmarshal(resp.Result, &createdSpace); err != nil {
		t.Fatalf("decode space.create result: %v", err)
	}

	sendRequest(t, ws, "5", "space.list", map[string]any{"workspaceId": createdWorkspace.ID})
	resp = readEnvelopeFor(t, ws, "5", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("space.list error = %v", resp.Error.Message)
	}
	var spaces []store.Space
	if err := json.Unmarshal(resp.Result, &spaces); err != nil {
		t.Fatalf("decode space.list result: %v", err)
	}
	if len(spaces) != 1 {
		t.Fatalf("got %d spaces, want 1", len(spaces))
	}

	sendRequest(t, ws, "6", "space.get", map[string]any{"id": createdSpace.ID})
	resp = readEnvelopeFor(t, ws, "6", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("space.get error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "7", "task.create", map[string]any{
		"workspaceId": createdWorkspace.ID, "title": "T1",
	})
	resp = readEnvelopeFor(t, ws, "7", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.create error = %v", resp.Error.Message)
	}
	var createdTask store.Task
	if err := json.Unmarshal(resp.Result, &createdTask); err != nil {
		t.Fatalf("decode task.create result: %v", err)
	}

	sendRequest(t, ws, "8", "task.list", map[string]any{"workspaceId": createdWorkspace.ID})
	resp = readEnvelopeFor(t, ws, "8", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.list error = %v", resp.Error.Message)
	}
	var tasks []store.Task
	if err := json.Unmarshal(resp.Result, &tasks); err != nil {
		t.Fatalf("decode task.list result: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}

	sendRequest(t, ws, "9", "task.get", map[string]any{"id": createdTask.ID})
	resp = readEnvelopeFor(t, ws, "9", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.get error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "10", "task.archive", map[string]any{"id": createdTask.ID})
	resp = readEnvelopeFor(t, ws, "10", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.archive error = %v", resp.Error.Message)
	}
	var archivedTask store.Task
	if err := json.Unmarshal(resp.Result, &archivedTask); err != nil {
		t.Fatalf("decode task.archive result: %v", err)
	}
	if archivedTask.ArchivedAt == nil {
		t.Fatal("task.archive: ArchivedAt is nil, want set")
	}
}

func TestServer_AuthRejection(t *testing.T) {
	t.Parallel()
	_, db := newTestWorkspaceManager(t)
	srv := newTestWSServer(t, nil, nil, db, "correct-token")

	tests := []struct {
		name  string
		token string
	}{
		{name: "missing token", token: ""},
		{name: "wrong token", token: "wrong"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := "ws" + srv.URL[len("http"):] + "/ws"
			if tt.token != "" {
				u += "?token=" + tt.token
			}
			_, resp, err := websocket.DefaultDialer.Dial(u, nil)
			if err == nil {
				t.Fatal("Dial() error = nil, want a rejected upgrade")
			}
			if resp == nil {
				t.Fatalf("Dial() error = %v, want an HTTP response", err)
			}
			if resp.StatusCode != 401 {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

func TestServer_ConcurrentInFlightRequests(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "prompt", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})

	chunk := readEnvelopeFor(t, ws, "prompt", 5*time.Second)
	if chunk.Event != "chunk" {
		t.Fatalf("first message for prompt = %+v, want a chunk event", chunk)
	}
	var chunkParams taskChunkParams
	if err := json.Unmarshal(chunk.Params, &chunkParams); err != nil {
		t.Fatalf("decode chunk params: %v", err)
	}
	if chunkParams.Text != "before hang" {
		t.Fatalf("chunk text = %q, want %q", chunkParams.Text, "before hang")
	}

	// task.prompt is now mid-turn and will not produce anything else for an
	// hour (the fake agent's "hang" scenario). workspace.list on the same
	// connection must still be answered promptly, proving one connection
	// truly multiplexes concurrent in-flight requests rather than
	// serializing behind whichever request arrived first.
	sendRequest(t, ws, "list", "workspace.list", nil)
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("workspace.list error = %v", resp.Error.Message)
	}

	sendCancel(t, ws, "prompt")
	term := readEnvelopeFor(t, ws, "prompt", 5*time.Second)
	if term.Error == nil {
		t.Fatal("task.prompt after cancel: error = nil, want an error")
	}
}

// TestServer_TaskPromptStreamsIncrementally proves task.prompt forwards
// each chunk as it arrives rather than buffering until the turn completes:
// the fake agent's "hang" scenario sends one chunk and then blocks for an
// hour, so receiving that chunk within the test's read deadline is only
// possible if it was delivered as its own message, not queued behind a
// response the agent will never send in time.
func TestServer_TaskPromptStreamsIncrementally(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})

	chunk := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if chunk.Event != "chunk" {
		t.Fatalf("got %+v, want a chunk event", chunk)
	}
	var chunkParams taskChunkParams
	if err := json.Unmarshal(chunk.Params, &chunkParams); err != nil {
		t.Fatalf("decode chunk params: %v", err)
	}
	if chunkParams.Text != "before hang" {
		t.Fatalf("chunk text = %q, want %q", chunkParams.Text, "before hang")
	}

	sendCancel(t, ws, "1")
	term := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if term.Error == nil {
		t.Fatal("task.prompt after cancel: error = nil, want an error")
	}
}

// TestServer_TaskCancel_StopsRunningTurn proves task.cancel both stops an
// in-progress turn promptly (rather than the client having to wait out the
// fake agent's hour-long hang) and produces exactly one terminal message
// for the cancelled request's id, no duplicate or late second one.
func TestServer_TaskCancel_StopsRunningTurn(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})

	chunk := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if chunk.Event != "chunk" {
		t.Fatalf("got %+v, want a chunk event", chunk)
	}

	start := time.Now()
	sendCancel(t, ws, "1")
	term := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancel took %s to take effect, want well under the fake agent's 1h hang", elapsed)
	}
	if term.Event != "" {
		t.Fatalf("got another event %+v after cancel, want the terminal response", term)
	}
	if term.Error == nil {
		t.Fatal("task.prompt after cancel: error = nil, want an error")
	}

	expectNoMoreMessages(t, ws, "1", time.Second)
}
