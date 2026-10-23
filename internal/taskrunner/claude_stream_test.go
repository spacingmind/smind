package taskrunner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

func streamDelta(kind, text string) claudecode.StreamEvent {
	key := "text"
	if kind == "thinking_delta" {
		key = "thinking"
	}
	raw, _ := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": kind, key: text},
	})
	return claudecode.StreamEvent{Event: raw}
}

func streamMessageStart() claudecode.StreamEvent {
	return claudecode.StreamEvent{Event: json.RawMessage(`{"type":"message_start","message":{"id":"m1"}}`)}
}

func assistantText(text string) claudecode.AssistantMessage {
	return claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.TextBlock{Text: text}}}
}

// collect runs msgs through a streamer the way forwardClaudeMessages does and
// returns every emitted event, flushing at the end.
func collect(msgs ...claudecode.Message) []Event {
	var s claudeStreamer
	var out []Event
	for _, m := range msgs {
		out = append(out, s.handle(m)...)
	}
	return append(out, s.flush()...)
}

func texts(events []Event, typ EventType) string {
	var b strings.Builder
	for _, e := range events {
		if e.Type == typ {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func TestClaudeStreamer_StreamedTextIsNotRepeatedByTheCompleteMessage(t *testing.T) {
	t.Parallel()
	got := collect(
		streamMessageStart(),
		streamDelta("text_delta", "Hel"),
		streamDelta("text_delta", "lo "),
		streamDelta("text_delta", "world"),
		assistantText("Hello world"),
	)
	if len(got) != 1 || got[0].Type != EventTypeText || got[0].Text != "Hello world" {
		t.Fatalf("events = %+v, want one text event %q", got, "Hello world")
	}
}

func TestClaudeStreamer_ToolUseAfterStreamedTextStillArrives(t *testing.T) {
	t.Parallel()
	msg := claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
		claudecode.TextBlock{Text: "Running it."},
		claudecode.ToolUseBlock{ID: "t1", Name: "Bash", Input: map[string]any{"command": "ls"}},
	}}
	got := collect(streamMessageStart(), streamDelta("text_delta", "Running it."), msg)
	if texts(got, EventTypeText) != "Running it." {
		t.Fatalf("text = %q, want it once", texts(got, EventTypeText))
	}
	var tools int
	for _, e := range got {
		if e.Type == EventTypeToolCall {
			tools++
		}
	}
	if tools != 1 {
		t.Fatalf("tool events = %d, want 1 (%+v)", tools, got)
	}
}

func TestClaudeStreamer_NoDeltasMeansTheCompleteMessageCarriesTheText(t *testing.T) {
	t.Parallel()
	got := collect(streamMessageStart(), assistantText("whole"))
	if texts(got, EventTypeText) != "whole" {
		t.Fatalf("text = %q, want %q", texts(got, EventTypeText), "whole")
	}
}

func TestClaudeStreamer_ALostTailIsRecoveredFromTheCompleteMessage(t *testing.T) {
	t.Parallel()
	got := collect(streamMessageStart(), streamDelta("text_delta", "Hel"), assistantText("Hello"))
	if texts(got, EventTypeText) != "Hello" {
		t.Fatalf("text = %q, want %q (streamed prefix + recovered tail)", texts(got, EventTypeText), "Hello")
	}
}

func TestClaudeStreamer_ADisagreeingStreamTrustsTheCompleteMessage(t *testing.T) {
	t.Parallel()
	got := collect(streamMessageStart(), streamDelta("text_delta", "draft"), assistantText("final"))
	if texts(got, EventTypeText) != "draftfinal" {
		// The partial draft was already shown; the complete text follows it.
		t.Fatalf("text = %q, want %q", texts(got, EventTypeText), "draftfinal")
	}
}

func TestClaudeStreamer_ThinkingAndTextKeepTheirOrder(t *testing.T) {
	t.Parallel()
	got := collect(
		streamMessageStart(),
		streamDelta("thinking_delta", "hmm "),
		streamDelta("thinking_delta", "ok"),
		streamDelta("text_delta", "Answer"),
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ThinkingBlock{Thinking: "hmm ok"}}},
		assistantText("Answer"),
	)
	if len(got) != 2 || got[0].Type != EventTypeThinking || got[0].Text != "hmm ok" || got[1].Type != EventTypeText || got[1].Text != "Answer" {
		t.Fatalf("events = %+v, want thinking %q then text %q", got, "hmm ok", "Answer")
	}
}

func TestClaudeStreamer_ASubAgentsDeltasAreNotShown(t *testing.T) {
	t.Parallel()
	d := streamDelta("text_delta", "inner")
	d.ParentToolUseID = "toolu_1"
	if got := collect(d); len(got) != 0 {
		t.Fatalf("events = %+v, want none for a sub-agent delta", got)
	}
}

func TestForwardClaudeMessages_CoalescesDeltasIntoFewEvents(t *testing.T) {
	t.Parallel()
	updates := make(chan claudecode.Message)
	events := make(chan Event, 256)
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardClaudeMessages(context.Background(), updates, events, nil)
	}()
	updates <- streamMessageStart()
	for i := 0; i < 200; i++ {
		updates <- streamDelta("text_delta", "x")
	}
	updates <- assistantText(strings.Repeat("x", 200))
	close(updates)
	<-done
	close(events)

	var n int
	var all strings.Builder
	for e := range events {
		n++
		all.WriteString(e.Text)
	}
	if all.String() != strings.Repeat("x", 200) {
		t.Fatalf("text = %q (%d runes), want 200 x exactly once", all.String(), len(all.String()))
	}
	if n >= 50 {
		t.Fatalf("%d events for 200 deltas, want them merged", n)
	}
}

func TestForwardClaudeMessages_DeltasFlushOnTheTimerWhileTheTurnRuns(t *testing.T) {
	t.Parallel()
	updates := make(chan claudecode.Message)
	events := make(chan Event, 16)
	go forwardClaudeMessages(context.Background(), updates, events, nil)
	updates <- streamMessageStart()
	updates <- streamDelta("text_delta", "live")
	select {
	case e := <-events:
		if e.Text != "live" {
			t.Fatalf("event text = %q, want %q", e.Text, "live")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a delta was not emitted while the turn was still running")
	}
	close(updates)
}

// A Stop cancels the turn's context; what was streamed so far must still
// reach the transcript.
func TestForwardClaudeMessages_KeepsTheStreamedTextAfterTheContextIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan claudecode.Message)
	events := make(chan Event, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardClaudeMessages(ctx, updates, events, nil)
	}()
	updates <- streamMessageStart()
	updates <- streamDelta("text_delta", "partial ans")
	updates <- streamDelta("text_delta", "wer")
	cancel()
	close(updates) // what the SDK's send path does when the turn is cancelled
	<-done
	close(events)

	var got strings.Builder
	for e := range events {
		got.WriteString(e.Text)
	}
	if got.String() != "partial answer" {
		t.Fatalf("kept text = %q, want %q", got.String(), "partial answer")
	}
}
