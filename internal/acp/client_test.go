package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	sessionID, _, err := c.NewSession(ctx, cwd)
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
	sessionID, _, err := c.NewSession(ctx, cwd)
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

// TestClient_PromptCancelledWhileUpdateInFlight is a regression test for a
// close/send race: cancelling Prompt's context makes call return as soon as
// ctx.Done() fires, independent of whatever the read loop goroutine happens
// to be doing at that exact moment. If Prompt's deferred cleanup closed
// updates without first confirming no handleSessionUpdate call was mid-send
// on it, this could panic ("send on closed channel"). Repeated iterations
// (see the -count driven by the caller/CI) are what actually catch the
// race; a single run passing doesn't prove much on its own, but the fix
// makes it safe regardless of the exact interleaving.
func TestClient_PromptCancelledWhileUpdateInFlight(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sessionID, _, err := c.NewSession(ctx, cwd)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	updates := make(chan SessionUpdate)
	done := make(chan error, 1)
	go func() {
		_, err := c.Prompt(ctx, sessionID, "hi", updates)
		done <- err
	}()

	// Read the first chunk (proves the turn is genuinely underway), then
	// cancel immediately without releasing the fake agent's gate -- the
	// read loop may or may not still be mid-delivery of that same update
	// when cancellation is observed, which is exactly the window this test
	// exercises.
	readUpdate(t, updates)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Prompt() did not return within 5s after cancellation")
	}

	// Drain to confirm updates was actually closed (not leaked open).
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("received an unexpected update after Prompt() returned")
		}
	case <-time.After(time.Second):
		t.Fatal("updates channel was not closed after Prompt() returned")
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

// TestClient_SetSessionConfigOptionRequestShape proves the client sends a
// well-formed session/set_config_option request: the fake agent decodes the
// request with ACP's real wire field names (sessionId, configId, and the
// flattened type/value of SessionConfigOptionValue) and echoes them back,
// so the assertions below fail if the client marshals any field under a
// different name or nests the value instead of flattening it.
func TestClient_SetSessionConfigOptionRequestShape(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessionID, _, err := c.NewSession(ctx, cwd)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	opts, err := c.SetSessionConfigOption(ctx, sessionID, "thinking-level", "low")
	if err != nil {
		t.Fatalf("SetSessionConfigOption() error = %v", err)
	}

	if len(opts) != 1 {
		t.Fatalf("echoed configOptions = %+v, want exactly 1 entry", opts)
	}
	got := opts[0]
	if got.ConfigID != "thinking-level" {
		t.Errorf("echoed configId = %q, want %q (client sent the wrong wire field name?)", got.ConfigID, "thinking-level")
	}
	// The fake agent packs the echoed sessionId and value type into the
	// option's description; empty means the client didn't send them.
	if got.Description != sessionID+" id" {
		t.Errorf("echoed sessionId/type = %q, want %q (client sent the wrong wire field names?)", got.Description, sessionID+" id")
	}
	if string(got.CurrentValue) != `{"type":"id","value":"low"}` {
		t.Errorf("echoed currentValue = %s, want the value %q under type %q", got.CurrentValue, "low", "id")
	}
}

// TestClient_SetSessionConfigOptionSuccess proves a success
// acknowledgement surfaces correctly: the agent's post-change
// SetSessionConfigOptionResponse (the full configOptions list with current
// values) round-trips through the method's return value.
func TestClient_SetSessionConfigOptionSuccess(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessionID, _, err := c.NewSession(ctx, cwd)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	opts, err := c.SetSessionConfigOption(ctx, sessionID, "thinking-level", "low")
	if err != nil {
		t.Fatalf("SetSessionConfigOption() error = %v", err)
	}
	if len(opts) != 1 {
		t.Fatalf("configOptions = %+v, want 1 entry", opts)
	}
	if opts[0].ConfigID != "thinking-level" || string(opts[0].CurrentValue) != `{"type":"id","value":"low"}` {
		t.Fatalf("configOptions[0] = %+v, want configId %q with currentValue {\"type\":\"id\",\"value\":\"low\"}", opts[0], "thinking-level")
	}
}

// TestClient_SetSessionConfigOptionAgentError proves an agent-side
// JSON-RPC error response (the fake agent rejects the sentinel config id
// with invalid params) becomes a Go error rather than being swallowed
// into a silent no-op.
func TestClient_SetSessionConfigOptionAgentError(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessionID, _, err := c.NewSession(ctx, cwd)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	opts, err := c.SetSessionConfigOption(ctx, sessionID, "no-such-option", "low")
	if err == nil {
		t.Fatalf("SetSessionConfigOption() error = nil, want the agent's rejection as a Go error (opts = %+v)", opts)
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != ErrCodeInvalidParams {
		t.Fatalf("SetSessionConfigOption() error = %v, want an *RPCError with code %d", err, ErrCodeInvalidParams)
	}
	if !strings.Contains(err.Error(), "unknown config option") {
		t.Fatalf("SetSessionConfigOption() error = %v, want it to carry the agent's message", err)
	}
}
