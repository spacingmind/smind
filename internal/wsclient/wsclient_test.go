package wsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// fakeACPAgentPath is the compiled internal/taskrunner/fakeagent binary --
// same rationale as internal/wsapi's own test suite (see that package's
// TestMain): it lets these tests drive a real streaming ACP turn through
// wsapi.Handler without depending on npx/network access.
var fakeACPAgentPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "smind-wsclient-test-")
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

	os.Exit(m.Run())
}

// testDaemon is a running wsapi.Handler-backed httptest server plus one
// task ready to drive a turn against, standing in for a real smind daemon.
type testDaemon struct {
	srv   *httptest.Server
	token string
	task  store.Task
}

func newTestDaemon(t *testing.T, scenario string) *testDaemon {
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
	wm := workspace.New(s)

	repo := t.TempDir()
	runGitT(t, repo, "init")
	runGitT(t, repo, "config", "user.email", "test@example.com")
	runGitT(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitT(t, repo, "add", "README.md")
	runGitT(t, repo, "commit", "-m", "initial commit")

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

	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	token := "tok"
	handler, err := wsapi.Handler(wm, runner, s, token)
	if err != nil {
		t.Fatalf("wsapi.Handler() error = %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &testDaemon{srv: srv, token: token, task: task}
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

func (d *testDaemon) addr() string {
	return d.srv.Listener.Addr().String()
}

func dialTestClient(t *testing.T, d *testDaemon) *Client {
	t.Helper()
	c, err := Dial(context.Background(), d.addr(), d.token)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestClient_Call_RoundTrip proves the basic non-streaming request/response
// path: a real workspace.list call round-trips through a live connection.
func TestClient_Call_RoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, "")
	c := dialTestClient(t, d)

	var task store.Task
	if err := c.Call(context.Background(), "task.get", map[string]any{"id": d.task.ID}, &task); err != nil {
		t.Fatalf("Call(task.get) error = %v", err)
	}
	if task.ID != d.task.ID {
		t.Fatalf("task.get id = %d, want %d", task.ID, d.task.ID)
	}
}

// TestClient_Call_ServerError proves a terminal server-side error response
// surfaces as an *RPCError, not a transport error.
func TestClient_Call_ServerError(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, "")
	c := dialTestClient(t, d)

	err := c.Call(context.Background(), "task.get", map[string]any{"id": 999999}, nil)
	if err == nil {
		t.Fatal("Call(task.get) with an unknown id: error = nil, want an error")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("Call() error = %v (%T), want an *RPCError", err, err)
	}
}

// chunkParams mirrors wsapi's taskChunkParams -- the wire shape of a
// run.attach/task.prompt "chunk" event.
type chunkParams struct {
	Text string `json:"text"`
}

// TestClient_CallStream_DeliversEventsBeforeTerminal proves CallStream
// delivers events to its callback as they arrive off the wire, not
// buffered until the terminal response -- gated the same way as
// internal/wsapi's own TestServer_TaskPromptStreamsIncrementally: the fake
// agent's "hang" scenario sends exactly one chunk and then blocks for an
// hour, so observing that chunk within this test's timeout is only
// possible if CallStream delivered it as its own message rather than
// queuing it behind a terminal response that will not arrive in time.
func TestClient_CallStream_DeliversEventsBeforeTerminal(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, "hang")
	c := dialTestClient(t, d)

	var runID string
	if err := c.Call(context.Background(), "run.start", map[string]any{
		"taskId": d.task.ID, "provider": "glm", "prompt": "hi",
	}, &struct {
		RunID *string `json:"runId"`
	}{&runID}); err != nil {
		t.Fatalf("Call(run.start) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan string, 8)
	done := make(chan error, 1)
	go func() {
		done <- c.CallStream(ctx, "run.attach", map[string]any{"runId": runID}, func(event string, params json.RawMessage) {
			if event != "chunk" {
				return
			}
			var p chunkParams
			_ = json.Unmarshal(params, &p)
			events <- p.Text
		}, nil)
	}()

	select {
	case text := <-events:
		if text != "before hang" {
			t.Fatalf("chunk text = %q, want %q", text, "before hang")
		}
	case err := <-done:
		t.Fatalf("CallStream(run.attach) finished (err=%v) before delivering the hang scenario's chunk", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the chunk event")
	}

	// Detach: the run itself keeps hanging server-side, we just stop
	// watching it.
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallStream(run.attach) after ctx cancel: err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for CallStream to return after ctx cancel")
	}

	// Cleanup: stop the run so the fake agent subprocess doesn't outlive
	// this test.
	if err := c.Call(context.Background(), "run.stop", map[string]any{"runId": runID}, nil); err != nil {
		t.Fatalf("Call(run.stop) error = %v", err)
	}
}

// TestClient_CallStream_CtxCancel_DetachesPromptlyWithoutStoppingRun proves
// the Ctrl+C-detach path a CLI foreground `task send` relies on: cancelling
// CallStream's ctx returns promptly (well under the fake agent's 1h hang)
// and only detaches the one in-flight run.attach request -- the run itself
// is left running, observable via a completely separate connection, exactly
// like the server-side behavior TestServer_RunAttach_DetachDoesNotStopRun
// already proves at the wsapi level.
func TestClient_CallStream_CtxCancel_DetachesPromptlyWithoutStoppingRun(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, "hang")
	starter := dialTestClient(t, d)

	var runID string
	if err := starter.Call(context.Background(), "run.start", map[string]any{
		"taskId": d.task.ID, "provider": "glm", "prompt": "hi",
	}, &struct {
		RunID *string `json:"runId"`
	}{&runID}); err != nil {
		t.Fatalf("Call(run.start) error = %v", err)
	}

	gotChunk := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- starter.CallStream(ctx, "run.attach", map[string]any{"runId": runID}, func(event string, params json.RawMessage) {
			if event == "chunk" {
				close(gotChunk)
			}
		}, nil)
	}()

	select {
	case <-gotChunk:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the chunk event")
	}

	start := time.Now()
	cancel()
	select {
	case err := <-streamDone:
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("detach took %s, want well under the fake agent's 1h hang", elapsed)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallStream after ctx cancel: err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for CallStream to return after ctx cancel")
	}

	// A brand new connection, unrelated to the one that started or watched
	// the run, must still see it running.
	checker, err := Dial(context.Background(), d.addr(), d.token)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer checker.Close()

	var status struct {
		Status string `json:"status"`
	}
	if err := checker.Call(context.Background(), "run.logs", map[string]any{"runId": runID}, &status); err != nil {
		t.Fatalf("Call(run.logs) error = %v", err)
	}
	if status.Status != "running" {
		t.Fatalf("run.logs status = %q after detaching, want %q", status.Status, "running")
	}

	if err := checker.Call(context.Background(), "run.stop", map[string]any{"runId": runID}, nil); err != nil {
		t.Fatalf("Call(run.stop) error = %v", err)
	}
}

// TestClient_CallStream_SlowScenario_ObservesRealGapsBetweenEvents is a
// second, independent proof of incremental delivery (distinct from the
// hang-scenario's "one chunk before an hour-long block" gate above): the
// fake agent's "slow" scenario streams five chunks with a real 300ms sleep
// between each, so if CallStream buffered events until the terminal
// response instead of delivering them as received, all five timestamps
// would land together at the very end instead of being spread out.
func TestClient_CallStream_SlowScenario_ObservesRealGapsBetweenEvents(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, "slow")
	c := dialTestClient(t, d)

	var runID string
	if err := c.Call(context.Background(), "run.start", map[string]any{
		"taskId": d.task.ID, "provider": "glm", "prompt": "hi",
	}, &struct {
		RunID *string `json:"runId"`
	}{&runID}); err != nil {
		t.Fatalf("Call(run.start) error = %v", err)
	}

	var timestamps []time.Time
	err := c.CallStream(context.Background(), "run.attach", map[string]any{"runId": runID}, func(event string, params json.RawMessage) {
		if event == "chunk" {
			timestamps = append(timestamps, time.Now())
		}
	}, nil)
	if err != nil {
		t.Fatalf("CallStream(run.attach) error = %v", err)
	}
	if len(timestamps) != 5 {
		t.Fatalf("got %d chunk events, want 5", len(timestamps))
	}
	first, last := timestamps[0], timestamps[len(timestamps)-1]
	if elapsed := last.Sub(first); elapsed < 900*time.Millisecond {
		t.Fatalf("first-to-last chunk elapsed = %s, want >= ~1.2s (4 gaps of ~300ms) -- events look buffered, not streamed", elapsed)
	}
}
