package acp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestClient(t *testing.T, opts ...Option) (*Client, string) {
	t.Helper()

	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "hello.txt"), []byte("file-content"), 0o644); err != nil {
		t.Fatalf("write hello.txt: %v", err)
	}

	c, err := New([]string{fakeAgentPath}, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	return c, cwd
}

// TestClient_HandshakeAndStreamingPrompt drives a full initialize ->
// session/new -> session/prompt turn against the fake agent, and proves
// that session/update notifications are forwarded onto Prompt's updates
// channel incrementally rather than buffered until the turn ends: the fake
// agent sends one chunk, then blocks on a "_test/release" notification
// before sending anything else (including the final PromptResponse). If
// Client buffered updates internally, the first channel read below would
// block until Prompt returns, which can't happen until release is sent —
// so reading it successfully within the timeout, before release, is the
// proof, mirroring the gating approach in
// TestProxy_StreamingPassthrough (internal/server/proxy_test.go) rather
// than a fragile short-timeout race.
func TestClient_HandshakeAndStreamingPrompt(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessionID, err := c.NewSession(ctx, cwd)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	updates := make(chan SessionUpdate)
	type promptResult struct {
		stopReason string
		err        error
	}
	resultCh := make(chan promptResult, 1)
	go func() {
		stopReason, err := c.Prompt(ctx, sessionID, "hi", updates)
		resultCh <- promptResult{stopReason, err}
	}()

	first := readUpdate(t, updates)
	text, ok := first.Text()
	if !ok || text != "Hello, " {
		t.Fatalf("first update = %+v, want text chunk %q", first, "Hello, ")
	}

	// Release the fake agent now that incremental delivery is proven; it
	// will exercise fs/read_text_file and session/request_permission
	// before finishing the turn.
	if err := c.conn.notify("_test/release", nil); err != nil {
		t.Fatalf("notify(_test/release) error = %v", err)
	}

	var texts []string
	for u := range updates {
		text, ok := u.Text()
		if !ok {
			t.Fatalf("update not a text chunk: %+v", u)
		}
		texts = append(texts, text)
	}

	want := []string{"read:file-content", "permission:allow-1", "world!"}
	if len(texts) != len(want) {
		t.Fatalf("updates after release = %v, want %v", texts, want)
	}
	for i, w := range want {
		if texts[i] != w {
			t.Fatalf("updates[%d] = %q, want %q", i, texts[i], w)
		}
	}

	res := <-resultCh
	if res.err != nil {
		t.Fatalf("Prompt() error = %v", res.err)
	}
	if res.stopReason != "end_turn" {
		t.Fatalf("Prompt() stopReason = %q, want %q", res.stopReason, "end_turn")
	}
}

func readUpdate(t *testing.T, updates <-chan SessionUpdate) SessionUpdate {
	t.Helper()
	select {
	case u, ok := <-updates:
		if !ok {
			t.Fatal("updates channel closed before any update arrived")
		}
		return u
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for update")
		return SessionUpdate{}
	}
}

// TestClient_RequestPermissionAutoDeny wires an AutoDenyPolicy client
// against the fake agent to prove session/request_permission round-trips
// correctly with a non-default policy too: the fake agent always offers
// both an allow_once and a reject_once option, so the selected optionId
// it echoes back distinguishes which policy decided.
func TestClient_RequestPermissionAutoDeny(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t, WithPermissionPolicy(AutoDenyPolicy{}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessionID, err := c.NewSession(ctx, cwd)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	updates := make(chan SessionUpdate)
	go func() { _, _ = c.Prompt(ctx, sessionID, "hi", updates) }()

	readUpdate(t, updates) // "Hello, "
	if err := c.conn.notify("_test/release", nil); err != nil {
		t.Fatalf("notify(_test/release) error = %v", err)
	}
	readUpdate(t, updates) // "read:file-content"

	got := readUpdate(t, updates)
	text, ok := got.Text()
	if !ok || text != "permission:reject-1" {
		t.Fatalf("permission update = %+v, want text %q", got, "permission:reject-1")
	}

	for range updates {
	}
}

func TestClient_Close(t *testing.T) {
	t.Parallel()
	c, err := New([]string{fakeAgentPath})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if c.conn.cmd.ProcessState == nil || !c.conn.cmd.ProcessState.Exited() {
		t.Fatalf("Close() did not wait for the subprocess to exit: state = %+v", c.conn.cmd.ProcessState)
	}
}

func TestGLMCommand(t *testing.T) {
	got := GLMCommand()
	want := []string{"npx", "-y", "glm-acp-agent@1.3.0"}
	if len(got) != len(want) {
		t.Fatalf("GLMCommand() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GLMCommand() = %v, want %v", got, want)
		}
	}
}
