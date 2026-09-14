package taskrunner

import (
	"encoding/json"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
)

// TestClaudeEvents covers every claudecode content block claudeEvents can
// see, per-message-kind -- the provider-native half of
// docs/decisions/0008-structured-run-events.md. The end-to-end fake-CLI
// test (TestRunner_RunPrompt_ClaudeNative_ToolCallEvents) proves the
// plumbing; this proves the translation table itself, including the block
// kinds that scenario doesn't produce: a tool_result arriving on an
// *assistant* message (claudecode.ToolResultBlock's own doc comment says
// it appears on both), and the server-side tool blocks WebSearch/WebFetch
// produce.
func TestClaudeEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  claudecode.Message
		want []Event
	}{
		{
			name: "assistant text and thinking",
			msg: claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
				claudecode.ThinkingBlock{Thinking: "hmm"},
				claudecode.TextBlock{Text: "hello"},
			}},
			want: []Event{
				{Type: EventTypeThinking, Text: "hmm"},
				{Type: EventTypeText, Text: "hello"},
			},
		},
		{
			name: "assistant tool_use starts a running call",
			msg: claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
				claudecode.ToolUseBlock{ID: "tu-1", Name: "Bash", Input: map[string]any{"command": "echo hi"}},
			}},
			want: []Event{{
				Type:       EventTypeToolCall,
				ToolCallID: "tu-1",
				ToolName:   "Bash",
				ToolStatus: ToolStatusRunning,
				ToolInput:  json.RawMessage(`{"command":"echo hi"}`),
			}},
		},
		{
			name: "user tool_result completes the matching call",
			msg: claudecode.UserMessage{Content: []claudecode.ContentBlock{
				claudecode.ToolResultBlock{ToolUseID: "tu-1", Content: json.RawMessage(`"hi"`)},
				claudecode.ToolResultBlock{ToolUseID: "tu-2", Content: json.RawMessage(`"boom"`), IsError: true},
			}},
			want: []Event{
				{Type: EventTypeToolCall, ToolCallID: "tu-1", ToolStatus: ToolStatusSuccess, ToolResult: json.RawMessage(`"hi"`)},
				{Type: EventTypeToolCall, ToolCallID: "tu-2", ToolStatus: ToolStatusFailure, ToolResult: json.RawMessage(`"boom"`)},
			},
		},
		{
			// A user turn's text is either the human's own prompt echoed
			// back or the envelope around tool results -- never new prose
			// to render. See claudeEvents' doc comment.
			name: "user text blocks are dropped, not turned into events",
			msg: claudecode.UserMessage{Content: []claudecode.ContentBlock{
				claudecode.TextBlock{Text: "the original prompt"},
			}},
			want: nil,
		},
		{
			// Regression: ToolResultBlock can arrive on an assistant
			// message too. Dropping it there left the card that the
			// matching ToolUseBlock opened stuck on "running" forever.
			name: "assistant tool_result also completes its call",
			msg: claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
				claudecode.ToolResultBlock{ToolUseID: "tu-9", Content: json.RawMessage(`"ok"`)},
			}},
			want: []Event{
				{Type: EventTypeToolCall, ToolCallID: "tu-9", ToolStatus: ToolStatusSuccess, ToolResult: json.RawMessage(`"ok"`)},
			},
		},
		{
			// Regression: WebSearch/WebFetch are server-side tools, so
			// they arrive as server_tool_use/advisor_tool_result rather
			// than tool_use/tool_result. Dropping them meant a run that
			// searched the web produced no tool-call events at all.
			name: "server-side tool blocks produce a call and its result",
			msg: claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
				claudecode.ServerToolUseBlock{ID: "srv-1", Name: "WebSearch", Input: map[string]any{"query": "acp"}},
				claudecode.ServerToolResultBlock{ToolUseID: "srv-1", Content: json.RawMessage(`["a"]`)},
			}},
			want: []Event{
				{
					Type:       EventTypeToolCall,
					ToolCallID: "srv-1",
					ToolName:   "WebSearch",
					ToolStatus: ToolStatusRunning,
					ToolInput:  json.RawMessage(`{"query":"acp"}`),
				},
				{Type: EventTypeToolCall, ToolCallID: "srv-1", ToolStatus: ToolStatusSuccess, ToolResult: json.RawMessage(`["a"]`)},
			},
		},
		{
			name: "unmodeled block kinds and non-content messages yield nothing",
			msg: claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
				claudecode.RawBlock{Type: "future_block", Raw: json.RawMessage(`{}`)},
			}},
			want: nil,
		},
		{
			name: "system messages yield nothing",
			msg:  claudecode.SystemMessage{Subtype: "init"},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := claudeEvents(tt.msg)
			assertEvents(t, got, tt.want)
		})
	}
}

// TestACPEvent covers acpEvent's translation of every ACP session update
// kind this package forwards, plus the ones it deliberately drops.
func TestACPEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		update acp.SessionUpdate
		want   Event
		wantOK bool
	}{
		{
			name:   "agent message chunk",
			update: acp.SessionUpdate{Type: acp.SessionUpdateAgentMessageChunk, Content: json.RawMessage(`{"type":"text","text":"hi"}`)},
			want:   Event{Type: EventTypeText, Text: "hi"},
			wantOK: true,
		},
		{
			name:   "agent thought chunk",
			update: acp.SessionUpdate{Type: acp.SessionUpdateAgentThoughtChunk, Content: json.RawMessage(`{"type":"text","text":"hm"}`)},
			want:   Event{Type: EventTypeThinking, Text: "hm"},
			wantOK: true,
		},
		{
			name:   "user message chunk",
			update: acp.SessionUpdate{Type: acp.SessionUpdateUserMessageChunk, Content: json.RawMessage(`{"type":"text","text":"you"}`)},
			want:   Event{Type: EventTypeUserMessage, Text: "you"},
			wantOK: true,
		},
		{
			name: "tool_call with in_progress status",
			update: acp.SessionUpdate{
				Type: acp.SessionUpdateToolCall, ToolCallID: "tc-1", Title: "Run tests",
				Kind: "execute", Status: "in_progress", RawInput: json.RawMessage(`{"command":"go test"}`),
			},
			want: Event{
				Type: EventTypeToolCall, ToolCallID: "tc-1", ToolName: "execute",
				ToolTitle: "Run tests", ToolStatus: ToolStatusRunning, ToolInput: json.RawMessage(`{"command":"go test"}`),
			},
			wantOK: true,
		},
		{
			// Regression: ACP's ToolCall.status is optional with a
			// "pending" default, and an initial tool_call by definition
			// hasn't completed -- so an omitted status must still render
			// as a running card, not a card with no status at all.
			name:   "tool_call with omitted status is still running",
			update: acp.SessionUpdate{Type: acp.SessionUpdateToolCall, ToolCallID: "tc-2", Kind: "read"},
			want: Event{
				Type: EventTypeToolCall, ToolCallID: "tc-2", ToolName: "read", ToolStatus: ToolStatusRunning,
			},
			wantOK: true,
		},
		{
			name:   "tool_call with a status this mapper doesn't know is still running",
			update: acp.SessionUpdate{Type: acp.SessionUpdateToolCall, ToolCallID: "tc-3", Status: "queued_in_a_future_acp_revision"},
			want:   Event{Type: EventTypeToolCall, ToolCallID: "tc-3", ToolStatus: ToolStatusRunning},
			wantOK: true,
		},
		{
			// A partial update: no kind/title/rawInput repeated. Absent
			// really does mean "unchanged" here, so those stay empty and
			// the consumer merges by id.
			name: "tool_call_update completing a call carries only what changed",
			update: acp.SessionUpdate{
				Type: acp.SessionUpdateToolCallUpdate, ToolCallID: "tc-1", Status: "completed",
				Content: json.RawMessage(`[{"type":"content"}]`),
			},
			want: Event{
				Type: EventTypeToolCall, ToolCallID: "tc-1", ToolStatus: ToolStatusSuccess,
				ToolResult: json.RawMessage(`[{"type":"content"}]`),
			},
			wantOK: true,
		},
		{
			name:   "tool_call_update reporting failure",
			update: acp.SessionUpdate{Type: acp.SessionUpdateToolCallUpdate, ToolCallID: "tc-1", Status: "failed"},
			want:   Event{Type: EventTypeToolCall, ToolCallID: "tc-1", ToolStatus: ToolStatusFailure},
			wantOK: true,
		},
		{
			// An unchanged-status partial update keeps ToolStatus empty:
			// unlike an initial tool_call, there is nothing to default to.
			name:   "tool_call_update with no status reports no status change",
			update: acp.SessionUpdate{Type: acp.SessionUpdateToolCallUpdate, ToolCallID: "tc-1", Title: "renamed"},
			want:   Event{Type: EventTypeToolCall, ToolCallID: "tc-1", ToolTitle: "renamed"},
			wantOK: true,
		},
		{
			name:   "plan updates are not forwarded",
			update: acp.SessionUpdate{Type: acp.SessionUpdatePlan},
		},
		{
			name:   "a non-text content block on a chunk is not forwarded",
			update: acp.SessionUpdate{Type: acp.SessionUpdateAgentMessageChunk, Content: json.RawMessage(`{"type":"image"}`)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := acpEvent(tt.update)
			if ok != tt.wantOK {
				t.Fatalf("acpEvent() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			assertEvents(t, []Event{got}, []Event{tt.want})
		})
	}
}

// assertEvents compares got against want on every field the wire carries,
// ignoring Raw (backend-native debug data, never serialized).
func assertEvents(t *testing.T, got, want []Event) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Type != w.Type || g.Text != w.Text || g.StopReason != w.StopReason ||
			g.ToolCallID != w.ToolCallID || g.ToolName != w.ToolName ||
			g.ToolTitle != w.ToolTitle || g.ToolStatus != w.ToolStatus ||
			string(g.ToolInput) != string(w.ToolInput) || string(g.ToolResult) != string(w.ToolResult) {
			t.Errorf("event[%d]:\n got %+v\nwant %+v", i, g, w)
		}
	}
}
