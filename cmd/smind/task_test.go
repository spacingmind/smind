package main

import (
	"encoding/json"
	"testing"
)

// TestRunLogsResult_DecodesStructuredEvents pins the wire keys
// docs/decisions/0008-structured-run-events.md specifies for a run.logs
// response against the CLI's own local structs. The tool-call fields reach
// runLogEvent through an embedded, unexported toolCallEventParams, which
// only works because encoding/json promotes an embedded unexported
// *struct*'s exported fields -- a rename or a re-shaping into a named
// field would silently decode every tool_call entry as zero values with no
// error, which nothing else in the repo would catch.
func TestRunLogsResult_DecodesStructuredEvents(t *testing.T) {
	t.Parallel()

	// Byte-for-byte the shape internal/wsapi's runLogsResult marshals.
	const payload = `{
		"runId": "run-1",
		"status": "done",
		"stopReason": "end_turn",
		"events": [
			{"type": "raw", "kind": "plan", "payload": {"sessionUpdate": "plan"}},
			{"type": "thinking", "text": "let me check"},
			{"type": "user_message", "text": "a synthesized turn"},
			{"type": "tool_call", "toolCallId": "tc-1", "toolName": "execute", "title": "Run tests", "status": "running", "input": {"command": "go test ./..."}},
			{"type": "tool_call", "toolCallId": "tc-1", "status": "success", "result": [{"type": "content"}]},
			{"type": "chunk", "text": "done"},
			{"type": "done", "stopReason": "end_turn"}
		]
	}`

	var got runLogsResult
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	if len(got.Events) != 7 {
		t.Fatalf("got %d events, want 7", len(got.Events))
	}

	// docs/decisions/0010-preserve-unknown-acp-event-kinds.md: the CLI's
	// runLogEvent must decode a "raw" entry's kind/payload -- same
	// embedded-unexported-struct promotion caveat this test's own doc
	// comment describes for toolCallEventParams.
	if raw := got.Events[0]; raw.Kind != "plan" || string(raw.Payload) != `{"sessionUpdate": "plan"}` {
		t.Fatalf("raw entry = %+v, want kind %q carrying its payload", raw, "plan")
	}

	start := got.Events[3]
	if start.ToolCallID != "tc-1" || start.ToolName != "execute" || start.Title != "Run tests" || start.Status != "running" {
		t.Fatalf("tool_call start = %+v, want tc-1/execute/%q/running", start, "Run tests")
	}
	if string(start.Input) != `{"command": "go test ./..."}` {
		t.Fatalf("tool_call start input = %s, want the raw arguments object", start.Input)
	}
	if done := got.Events[4]; done.Status != "success" || string(done.Result) != `[{"type": "content"}]` {
		t.Fatalf("tool_call completion = %+v, want success carrying the raw result", done)
	}
}

// TestRenderRaw proves a "raw" event -- an ACP session-update kind the
// daemon's normalizer doesn't recognize -- prints its kind and payload
// rather than being silently skipped by `task attach`/`task logs`. See
// docs/decisions/0010-preserve-unknown-acp-event-kinds.md.
func TestRenderRaw(t *testing.T) {
	t.Parallel()

	got := renderRaw(rawEventParams{Kind: "plan", Payload: json.RawMessage(`{"sessionUpdate":"plan"}`)})
	want := "[raw] plan: {\"sessionUpdate\":\"plan\"}\n"
	if got != want {
		t.Fatalf("renderRaw() = %q, want %q", got, want)
	}
}

// TestToolCallNames_Render proves `task attach`/`task logs` renders a tool
// call's completion under the name the call opened with. Neither provider
// repeats the name on the completing event (Claude Code never carries one;
// ACP's tool_call_update only sends what changed), so without merging by
// toolCallId the completion line would show a raw wire id.
func TestToolCallNames_Render(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		events []toolCallEventParams
		want   []string
	}{
		{
			name: "claude: name from the start event carries to the completion",
			events: []toolCallEventParams{
				{ToolCallID: "toolu_01", ToolName: "Bash", Status: "running", Input: json.RawMessage(`{"command":"echo hi"}`)},
				{ToolCallID: "toolu_01", Status: "success", Result: json.RawMessage(`"hi"`)},
			},
			want: []string{
				"[tool] Bash: {\"command\":\"echo hi\"}\n",
				"[tool] Bash: done\n",
			},
		},
		{
			name: "acp: title wins over kind, and carries to a partial update",
			events: []toolCallEventParams{
				{ToolCallID: "tc-1", ToolName: "execute", Title: "Run tests", Status: "running"},
				{ToolCallID: "tc-1", Status: "failure"},
			},
			want: []string{
				"[tool] Run tests\n",
				"[tool] Run tests: failed\n",
			},
		},
		{
			name: "interleaved calls keep their own names",
			events: []toolCallEventParams{
				{ToolCallID: "a", ToolName: "Read", Status: "running"},
				{ToolCallID: "b", ToolName: "Write", Status: "running"},
				{ToolCallID: "a", Status: "success"},
				{ToolCallID: "b", Status: "failure"},
			},
			want: []string{
				"[tool] Read\n", "[tool] Write\n",
				"[tool] Read: done\n", "[tool] Write: failed\n",
			},
		},
		{
			name:   "an event with no name anywhere falls back to its id",
			events: []toolCallEventParams{{ToolCallID: "orphan", Status: "success"}},
			want:   []string{"[tool] orphan: done\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			names := toolCallNames{}
			for i, e := range tt.events {
				if got := names.render(e); got != tt.want[i] {
					t.Errorf("render(%+v) = %q, want %q", e, got, tt.want[i])
				}
			}
		})
	}
}
