package acp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	sessionID, _, err := c.NewSession(ctx, cwd, nil)
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
	sessionID, _, err := c.NewSession(ctx, cwd, nil)
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
	sessionID, _, err := c.NewSession(ctx, cwd, nil)
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

// TestACPClient_SupportsMcpServersOptOut proves WithoutMcpServers masks
// the agent's own advertised mcp capabilities: against the same fake
// agent advertising stdio+http+sse support, a default client reports all
// three supported while a client constructed with WithoutMcpServers
// reports none.
func TestACPClient_SupportsMcpServersOptOut(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opts []Option
	}{
		{"without option", nil},
		{"with WithoutMcpServers", []Option{WithoutMcpServers()}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := New([]string{fakeAgentPath, "mcp2=stdio,http,sse"}, tt.opts...)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			t.Cleanup(func() { _ = c.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Initialize(ctx); err != nil {
				t.Fatalf("Initialize() error = %v", err)
			}

			want := tt.opts == nil
			if got := c.SupportsMcpStdio(); got != want {
				t.Errorf("SupportsMcpStdio() = %v, want %v", got, want)
			}
			if got := c.SupportsMcpHttp(); got != want {
				t.Errorf("SupportsMcpHttp() = %v, want %v", got, want)
			}
			if got := c.SupportsMcpSSE(); got != want {
				t.Errorf("SupportsMcpSSE() = %v, want %v", got, want)
			}
		})
	}
}

func TestGLMCommand(t *testing.T) {
	got := GLMCommand()
	want := []string{"npx", "-y", "glm-acp-agent@1.14.0"}
	if len(got) != len(want) {
		t.Fatalf("GLMCommand() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GLMCommand() = %v, want %v", got, want)
		}
	}
}

// TestClient_NewSessionConfigOptions proves session/new's scripted
// configOptions (see fakeagent's own doc comment) round-trips through
// Client.NewSession intact and in order: a "select"-kind option's own
// enumerated Options list (id + label, e.g. GLM's real thinking-level
// tiers) is decoded, not silently dropped the way it was before
// ConfigOption gained the Options field, and a "boolean"-kind option
// (no choice list at all) carries none.
func TestClient_NewSessionConfigOptions(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, opts, err := c.NewSession(ctx, cwd, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if len(opts) != 2 {
		t.Fatalf("NewSession() configOptions = %+v, want 2 entries", opts)
	}

	thinking := opts[0]
	if thinking.ConfigID != "thinking-level" || thinking.Type != "select" || thinking.Category != "thought_level" {
		t.Fatalf("configOptions[0] = %+v, want the thinking-level select option", thinking)
	}
	if string(thinking.CurrentValue) != `"medium"` {
		t.Fatalf("configOptions[0].CurrentValue = %s, want %q", thinking.CurrentValue, `"medium"`)
	}
	wantChoices := []ConfigSelectOption{
		{Value: "minimal", Name: "Minimal"},
		{Value: "low", Name: "Low"},
		{Value: "medium", Name: "Medium"},
		{Value: "high", Name: "High", Description: "Reasons the longest"},
	}
	if len(thinking.Options) != len(wantChoices) {
		t.Fatalf("configOptions[0].Options = %+v, want %+v", thinking.Options, wantChoices)
	}
	for i, want := range wantChoices {
		if thinking.Options[i] != want {
			t.Errorf("configOptions[0].Options[%d] = %+v, want %+v", i, thinking.Options[i], want)
		}
	}

	webSearch := opts[1]
	if webSearch.ConfigID != "web-search" || webSearch.Type != "boolean" {
		t.Fatalf("configOptions[1] = %+v, want the web-search boolean option", webSearch)
	}
	if len(webSearch.Options) != 0 {
		t.Fatalf("configOptions[1].Options = %+v, want none for a boolean-kind option", webSearch.Options)
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
	sessionID, _, err := c.NewSession(ctx, cwd, nil)
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
	sessionID, _, err := c.NewSession(ctx, cwd, nil)
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
	sessionID, _, err := c.NewSession(ctx, cwd, nil)
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

// TestClient_SupportsLoadSessionAndResumeSession proves capabilityFlags
// decodes AgentCapabilities.loadSession and .sessionCapabilities.resume
// independently, without needing a live subprocess -- pure decode logic
// exercised directly against the raw JSON Initialize would have stored.
func TestClient_SupportsLoadSessionAndResumeSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		caps       string
		wantLoad   bool
		wantResume bool
	}{
		{"neither", `{}`, false, false},
		{"loadSession only", `{"loadSession":true}`, true, false},
		{"resume only", `{"sessionCapabilities":{"resume":{}}}`, false, true},
		{"both", `{"loadSession":true,"sessionCapabilities":{"resume":{}}}`, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{AgentCapabilities: json.RawMessage(tt.caps)}
			if got := c.SupportsLoadSession(); got != tt.wantLoad {
				t.Errorf("SupportsLoadSession() = %v, want %v", got, tt.wantLoad)
			}
			if got := c.SupportsResumeSession(); got != tt.wantResume {
				t.Errorf("SupportsResumeSession() = %v, want %v", got, tt.wantResume)
			}
		})
	}
}

// TestClient_LoadSessionAndResumeSession drives both resume RPCs against
// the fake agent (which only accepts requests for the session id
// session/new hands out), proving each succeeds for the known id, fails
// for an unknown one (the stale-session case a caller falls back to a
// fresh session/new on), and that both requests are always sent with a
// non-nil mcpServers (see loadSessionParams/resumeSessionParams' doc
// comments on why an omitted mcpServers can break some agents).
func TestClient_LoadSessionAndResumeSession(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sid, _, err := c.NewSession(ctx, cwd, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	if _, err := c.LoadSession(ctx, sid, cwd, nil); err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if _, err := c.ResumeSession(ctx, sid, cwd, nil); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}

	if _, err := c.LoadSession(ctx, "unknown-session", cwd, nil); err == nil {
		t.Fatal("LoadSession() with unknown session id error = nil, want error")
	}
	if _, err := c.ResumeSession(ctx, "unknown-session", cwd, nil); err == nil {
		t.Fatal("ResumeSession() with unknown session id error = nil, want error")
	}
}

// TestLoadSessionReplayCollectsUpdatesAndSendsNoPrompt drives
// LoadSessionReplay against a fake agent scripted (via FAKEAGENT_REPLAY)
// to replay four updates on session/load, proving the replay is forwarded
// onto the updates channel in order, the channel is closed once the call
// returns, and -- via the fake agent's FAKEAGENT_METHOD_LOG -- that the
// client sent only initialize and session/load: no session/new (the
// session is not re-created), no session/prompt (a replay prompts
// nothing), and none of the mode/config calls.
func TestLoadSessionReplayCollectsUpdatesAndSendsNoPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	replayPath := filepath.Join(dir, "replay.json")
	methodLog := filepath.Join(dir, "methods.log")
	replay := `[
		{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hi"}},
		{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}},
		{"sessionUpdate":"tool_call","toolCallId":"t1","kind":"execute","title":"ls","status":"completed"},
		{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}
	]`
	if err := os.WriteFile(replayPath, []byte(replay), 0o644); err != nil {
		t.Fatalf("write replay file: %v", err)
	}

	c, cwd := newTestClient(t, WithEnv(
		"FAKEAGENT_REPLAY="+replayPath,
		"FAKEAGENT_METHOD_LOG="+methodLog,
	))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updates := make(chan SessionUpdate)
	var got []SessionUpdate
	drained := make(chan struct{})
	go func() {
		for u := range updates {
			got = append(got, u)
		}
		close(drained)
	}()

	if _, err := c.LoadSessionReplay(ctx, "fake-session-1", cwd, updates); err != nil {
		t.Fatalf("LoadSessionReplay() error = %v", err)
	}
	<-drained // proves updates was closed, after the call returned

	if len(got) != 4 {
		t.Fatalf("collected %d updates, want 4: %+v", len(got), got)
	}
	if got[0].Type != SessionUpdateUserMessageChunk {
		t.Errorf("updates[0].Type = %q, want %q", got[0].Type, SessionUpdateUserMessageChunk)
	}
	if text, ok := got[0].Text(); !ok || text != "hi" {
		t.Errorf("updates[0].Text() = %q, %v; want %q, true", text, ok, "hi")
	}
	if got[1].Type != SessionUpdateAgentMessageChunk {
		t.Errorf("updates[1].Type = %q, want %q", got[1].Type, SessionUpdateAgentMessageChunk)
	}
	if text, ok := got[1].Text(); !ok || text != "hello" {
		t.Errorf("updates[1].Text() = %q, %v; want %q, true", text, ok, "hello")
	}
	if got[2].Type != SessionUpdateToolCall || got[2].ToolCallID != "t1" || got[2].Kind != "execute" || got[2].Title != "ls" {
		t.Errorf("updates[2] = %+v, want a tool_call t1 (execute, %q)", got[2], "ls")
	}
	if got[3].Type != SessionUpdateAgentMessageChunk {
		t.Errorf("updates[3].Type = %q, want %q", got[3].Type, SessionUpdateAgentMessageChunk)
	}
	if text, ok := got[3].Text(); !ok || text != "done" {
		t.Errorf("updates[3].Text() = %q, %v; want %q, true", text, ok, "done")
	}

	logBytes, err := os.ReadFile(methodLog)
	if err != nil {
		t.Fatalf("read method log: %v", err)
	}
	sent := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(logBytes)), "\n") {
		if line != "" {
			sent[line] = true
		}
	}
	for _, want := range []string{"initialize", "session/load"} {
		if !sent[want] {
			t.Errorf("method log missing %q: %v", want, sent)
		}
	}
	for _, banned := range []string{"session/prompt", "session/new", "session/set_mode", "session/set_config_option"} {
		if sent[banned] {
			t.Errorf("method log contains %q (a replay must not send it): %v", banned, sent)
		}
	}
}

// TestLoadSessionReplayUnknownSession proves an agent-side rejection of
// session/load (unknown session id) surfaces as a Go error from
// LoadSessionReplay while the updates channel is still closed cleanly.
func TestLoadSessionReplayUnknownSession(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updates := make(chan SessionUpdate)
	if _, err := c.LoadSessionReplay(ctx, "nope", cwd, updates); err == nil {
		t.Fatal("LoadSessionReplay() error = nil, want the agent's unknown-session rejection")
	}
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("received an unexpected update after LoadSessionReplay() returned")
		}
	case <-time.After(time.Second):
		t.Fatal("updates channel was not closed after LoadSessionReplay() returned")
	}
}

// TestClient_SessionModes proves session/new's modes (ACP's
// SessionModeState) are decoded and retained per session, and that
// SetSessionMode sends session/set_mode, updates CurrentModeID on success,
// and surfaces an agent rejection as an error without changing it.
func TestClient_SessionModes(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessionID, _, err := c.NewSession(ctx, cwd, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	modes, ok := c.SessionModes(sessionID)
	if !ok {
		t.Fatal("SessionModes() ok = false, want the advertised modes")
	}
	if modes.CurrentModeID != "default" || len(modes.AvailableModes) != 3 || modes.AvailableModes[1].ID != "accept_edits" || modes.AvailableModes[1].Description == "" {
		t.Fatalf("SessionModes() = %+v, want default current with 3 modes", modes)
	}

	if err := c.SetSessionMode(ctx, sessionID, "accept_edits"); err != nil {
		t.Fatalf("SetSessionMode(accept_edits) error = %v", err)
	}
	if m, _ := c.SessionModes(sessionID); m.CurrentModeID != "accept_edits" {
		t.Fatalf("CurrentModeID = %q after set, want accept_edits", m.CurrentModeID)
	}

	if err := c.SetSessionMode(ctx, sessionID, "no-such-mode"); err == nil {
		t.Fatal("SetSessionMode(no-such-mode) error = nil, want the agent's rejection")
	}
	if m, _ := c.SessionModes(sessionID); m.CurrentModeID != "accept_edits" {
		t.Fatalf("CurrentModeID = %q after rejected set, want unchanged accept_edits", m.CurrentModeID)
	}

	if _, ok := c.SessionModes("unknown-session"); ok {
		t.Fatal("SessionModes(unknown) ok = true, want false")
	}
}

func TestGLMCommand_PassesMaxTurns(t *testing.T) {
	got := GLMCommand(WithMaxTurns(40))
	want := []string{"npx", "-y", "glm-acp-agent@1.14.0", "--max-turns", "40"}
	if len(got) != len(want) {
		t.Fatalf("GLMCommand(WithMaxTurns(40)) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GLMCommand(WithMaxTurns(40)) = %v, want %v", got, want)
		}
	}
	if got := GLMCommand(WithMaxTurns(0)); len(got) != 3 {
		t.Fatalf("GLMCommand(WithMaxTurns(0)) = %v, want no --max-turns", got)
	}
}

// TestClient_WithCleanEnv_DoesNotInheritEnvironment proves WithCleanEnv:
// the agent subprocess gets ONLY the WithEnv entries, never the daemon's
// environment (which carries secrets), while a client without it inherits
// as before. The fake agent dumps its whole os.Environ to
// FAKEAGENT_ENV_DUMP's path at startup.
func TestClient_WithCleanEnv_DoesNotInheritEnvironment(t *testing.T) {
	t.Setenv("SMIND_SENTINEL", "leak-1")

	startAndDump := func(t *testing.T, opts ...Option) string {
		t.Helper()
		dump := filepath.Join(t.TempDir(), "env-dump")
		opts = append(opts, WithEnv("FAKEAGENT_ENV_DUMP="+dump, "PATH="+os.Getenv("PATH")))
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
		b, err := os.ReadFile(dump)
		if err != nil {
			t.Fatalf("read env dump: %v", err)
		}
		return string(b)
	}

	clean := startAndDump(t, WithCleanEnv())
	if !strings.Contains(clean, "FAKEAGENT_ENV_DUMP=") {
		t.Fatalf("clean env is missing the WithEnv entries:\n%s", clean)
	}
	if strings.Contains(clean, "SMIND_SENTINEL") {
		t.Fatalf("clean env inherited the daemon environment:\n%s", clean)
	}

	inherited := startAndDump(t)
	if !strings.Contains(inherited, "SMIND_SENTINEL") {
		t.Fatalf("a client without WithCleanEnv did not inherit the daemon environment:\n%s", inherited)
	}
}

// loadWithFSProbe loads a session while the fake agent tries to write and read a
// file through the client, with the session's cwd already registered so the fs
// boundary check would pass. It returns the agent's own report of both calls and
// whether the file exists afterwards.
func loadWithFSProbe(t *testing.T, opts ...Option) (report string, written bool) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "fs-report")
	c, cwd := newTestClient(t, append([]Option{WithEnv("FAKEAGENT_LOAD_FS=" + logPath)}, opts...)...)
	c.recordSession("fake-session-1", cwd, nil) // the boundary is registered: nothing but the option stops the agent

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updates := make(chan SessionUpdate)
	go func() {
		for range updates {
		}
	}()
	if _, err := c.LoadSessionReplay(ctx, "fake-session-1", cwd, updates); err != nil {
		t.Fatalf("LoadSessionReplay() error = %v", err)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the agent never reported: %v", err)
	}
	_, statErr := os.Stat(filepath.Join(cwd, "fs-probe.txt"))
	return string(b), statErr == nil
}

// WithoutFS: an agent loading its session can neither write nor read through the
// client, even with the session's cwd registered; without the option the same
// calls succeed (so the probe is sensitive).
func TestLoadSessionReplayWithoutFSRefusesEveryFSRequest(t *testing.T) {
	t.Parallel()
	report, written := loadWithFSProbe(t, WithoutFS())
	if report != "write:error\nread:error\n" || written {
		t.Fatalf("with WithoutFS: report %q, file written %v", report, written)
	}
}

func TestLoadSessionReplayFSProbeIsSensitive(t *testing.T) {
	t.Parallel()
	report, written := loadWithFSProbe(t)
	if report != "write:ok\nread:ok\n" || !written {
		t.Fatalf("without WithoutFS the probe should succeed: report %q, file written %v", report, written)
	}
}

// --- ACP elicitation (ADR-0055 section 4) ---

// recordingElicitPolicy is a test ElicitationPolicy that records the
// request the client decoded and answers with a canned response or error.
type recordingElicitPolicy struct {
	calls int
	got   *ElicitationRequest
	resp  ElicitationResponse
	err   error
}

func (p *recordingElicitPolicy) Decide(_ context.Context, req ElicitationRequest) (ElicitationResponse, error) {
	p.calls++
	got := req
	p.got = &got
	return p.resp, p.err
}

// newElicitClient starts the fake agent with its "elicit=<mode>" scenario
// argument (see fakeagent's runElicitationScript) plus any client options,
// mirroring newTestClient.
func newElicitClient(t *testing.T, mode string, opts ...Option) (*Client, string) {
	t.Helper()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "hello.txt"), []byte("file-content"), 0o644); err != nil {
		t.Fatalf("write hello.txt: %v", err)
	}
	c, err := New([]string{fakeAgentPath, "elicit=" + mode}, opts...)
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

// elicitTurn runs one full prompt turn against the elicit scenario and
// returns every text chunk the agent streamed, in order: the read and
// permission chunks, then "caps:<...>", "elicitation:<...>" and "world!".
func elicitTurn(t *testing.T, c *Client, cwd string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sessionID, _, err := c.NewSession(ctx, cwd, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	updates := make(chan SessionUpdate)
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Prompt(ctx, sessionID, "hi", updates)
		errCh <- err
	}()
	readUpdate(t, updates) // "Hello, ": release the gate only after it arrives
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
	if err := <-errCh; err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	return texts
}

// elicitChunk returns the one chunk of texts starting with prefix.
func elicitChunk(t *testing.T, texts []string, prefix string) string {
	t.Helper()
	for _, s := range texts {
		if strings.HasPrefix(s, prefix) {
			return s
		}
	}
	t.Fatalf("no %q chunk in %v", prefix, texts)
	return ""
}

// TestElicitation_NotAdvertisedWithoutPolicy proves the default client
// stays exactly as before ADR-0055: no elicitation capability is advertised
// at initialize, and an agent that asks anyway gets method-not-found.
func TestElicitation_NotAdvertisedWithoutPolicy(t *testing.T) {
	t.Parallel()
	c, cwd := newElicitClient(t, "form")
	texts := elicitTurn(t, c, cwd)
	if got := elicitChunk(t, texts, "caps:"); got != "caps:none" {
		t.Fatalf("caps chunk = %q, want %q", got, "caps:none")
	}
	if got := elicitChunk(t, texts, "elicitation:"); !strings.Contains(got, "method not found") {
		t.Fatalf("elicitation chunk = %q, want a method-not-found error", got)
	}
}

// TestElicitation_AdvertisedFormOnlyWithPolicy proves WithElicitation
// advertises exactly the form mode -- never url.
func TestElicitation_AdvertisedFormOnlyWithPolicy(t *testing.T) {
	t.Parallel()
	c, cwd := newElicitClient(t, "form", WithElicitation(&recordingElicitPolicy{}))
	caps := elicitChunk(t, elicitTurn(t, c, cwd), "caps:")
	if !strings.Contains(caps, `"form":{}`) {
		t.Fatalf("caps chunk = %q, want the form capability", caps)
	}
	if strings.Contains(caps, "url") {
		t.Fatalf("caps chunk = %q, url must never be advertised", caps)
	}
}

// TestElicitation_FormRoundTripAccept drives one full form elicitation
// through the decode: the policy sees the properties sorted by name with
// their choices and constraints intact, and its accept answer reaches the
// agent as the elicitation/create result.
func TestElicitation_FormRoundTripAccept(t *testing.T) {
	t.Parallel()
	p := &recordingElicitPolicy{resp: ElicitationResponse{
		Action:  ElicitationAccept,
		Content: map[string]any{"env": "prod", "tags": []string{"a", "c"}, "count": 3, "note": "hi", "ok": true},
	}}
	c, cwd := newElicitClient(t, "form", WithElicitation(p))
	texts := elicitTurn(t, c, cwd)

	if p.calls != 1 || p.got == nil {
		t.Fatalf("policy called %d times, got %+v", p.calls, p.got)
	}
	req := p.got
	if req.SessionID != "fake-session-1" || req.Message != "Pick a deploy target" || req.Mode != "form" {
		t.Fatalf("request header fields = %+v", req)
	}
	if len(req.Properties) != 5 {
		t.Fatalf("properties = %+v, want 5", req.Properties)
	}
	names := make([]string, len(req.Properties))
	for i, prop := range req.Properties {
		names[i] = prop.Name
	}
	if !reflect.DeepEqual(names, []string{"count", "env", "note", "ok", "tags"}) {
		t.Fatalf("property order = %v, want sorted by name", names)
	}
	byName := map[string]ElicitationProperty{}
	for _, prop := range req.Properties {
		byName[prop.Name] = prop
	}
	if env := byName["env"]; env.Type != "string" || !reflect.DeepEqual(env.Options, []ElicitationOption{{Value: "prod", Title: "Production"}, {Value: "stg", Title: "Staging"}}) {
		t.Fatalf("env property = %+v", env)
	}
	if tags := byName["tags"]; tags.Type != "array" || !reflect.DeepEqual(tags.Options, []ElicitationOption{{Value: "a"}, {Value: "b"}, {Value: "c"}}) {
		t.Fatalf("tags property = %+v", tags)
	}
	if count := byName["count"]; count.Type != "integer" || count.Minimum == nil || *count.Minimum != 1 || count.Maximum == nil || *count.Maximum != 5 {
		t.Fatalf("count property = %+v", count)
	}
	if note := byName["note"]; note.Type != "string" || note.MaxLength == nil || *note.MaxLength != 20 {
		t.Fatalf("note property = %+v", note)
	}
	if !reflect.DeepEqual(req.Required, []string{"env"}) {
		t.Fatalf("required = %v", req.Required)
	}

	want := `elicitation:{"action":"accept","content":{"count":3,"env":"prod","note":"hi","ok":true,"tags":["a","c"]}}`
	if got := elicitChunk(t, texts, "elicitation:"); got != want {
		t.Fatalf("elicitation chunk = %q, want %q", got, want)
	}
}

// TestElicitation_Decline and TestElicitation_Cancel prove the other two
// actions round-trip with no content attached.
func TestElicitation_Decline(t *testing.T) {
	t.Parallel()
	p := &recordingElicitPolicy{resp: ElicitationResponse{Action: ElicitationDecline}}
	c, cwd := newElicitClient(t, "form", WithElicitation(p))
	if got := elicitChunk(t, elicitTurn(t, c, cwd), "elicitation:"); got != `elicitation:{"action":"decline"}` {
		t.Fatalf("elicitation chunk = %q, want a bare decline", got)
	}
}

func TestElicitation_Cancel(t *testing.T) {
	t.Parallel()
	p := &recordingElicitPolicy{resp: ElicitationResponse{Action: ElicitationCancel}}
	c, cwd := newElicitClient(t, "form", WithElicitation(p))
	if got := elicitChunk(t, elicitTurn(t, c, cwd), "elicitation:"); got != `elicitation:{"action":"cancel"}` {
		t.Fatalf("elicitation chunk = %q, want a bare cancel", got)
	}
}

// TestElicitation_UrlModeDeclinedWithoutAskingThePolicy proves a url-mode
// request is refused before any policy runs: url was never advertised.
func TestElicitation_UrlModeDeclinedWithoutAskingThePolicy(t *testing.T) {
	t.Parallel()
	p := &recordingElicitPolicy{resp: ElicitationResponse{Action: ElicitationAccept, Content: map[string]any{"x": "y"}}}
	c, cwd := newElicitClient(t, "url", WithElicitation(p))
	texts := elicitTurn(t, c, cwd)
	if p.calls != 0 {
		t.Fatalf("the policy was asked %d times for a url request", p.calls)
	}
	if got := elicitChunk(t, texts, "elicitation:"); got != `elicitation:{"action":"decline"}` {
		t.Fatalf("elicitation chunk = %q, want a decline", got)
	}
}

// TestElicitation_UnsupportedPropertyIsFlagged proves a property whose
// schema this client cannot model decodes as Unsupported rather than being
// silently dropped or erroring the whole request.
func TestElicitation_UnsupportedPropertyIsFlagged(t *testing.T) {
	t.Parallel()
	p := &recordingElicitPolicy{resp: ElicitationResponse{Action: ElicitationDecline}}
	c, cwd := newElicitClient(t, "badtype", WithElicitation(p))
	elicitTurn(t, c, cwd)
	if p.got == nil || len(p.got.Properties) != 1 || !p.got.Properties[0].Unsupported || p.got.Properties[0].Name != "nested" {
		t.Fatalf("properties = %+v, want one unsupported \"nested\"", p.got)
	}
}

// TestElicitation_PolicyErrorIsInternalError proves a failing policy
// surfaces to the agent as a JSON-RPC error, never as a phantom answer.
func TestElicitation_PolicyErrorIsInternalError(t *testing.T) {
	t.Parallel()
	p := &recordingElicitPolicy{err: errors.New("boom")}
	c, cwd := newElicitClient(t, "form", WithElicitation(p))
	got := elicitChunk(t, elicitTurn(t, c, cwd), "elicitation:")
	if !strings.HasPrefix(got, "elicitation:error:") || !strings.Contains(got, "boom") {
		t.Fatalf("elicitation chunk = %q, want an error carrying the policy failure", got)
	}
}
