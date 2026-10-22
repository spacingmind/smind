package taskrunner

import (
	"context"
	"strings"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// feed returns a recv function that serves each batch of messages as one
// ReceiveResponse call (closing the channel after the batch, as the real one
// does after a result), and a closed channel once the batches run out.
func feed(batches ...[]claudecode.Message) func(context.Context) <-chan claudecode.Message {
	i := 0
	return func(context.Context) <-chan claudecode.Message {
		ch := make(chan claudecode.Message, 16)
		if i < len(batches) {
			for _, m := range batches[i] {
				ch <- m
			}
			i++
		}
		close(ch)
		return ch
	}
}

var (
	notification = claudecode.TaskNotificationMessage{TaskID: "bg-1", Status: "stopped"}
	staleResult  = claudecode.ResultMessage{}
	realResult   = claudecode.ResultMessage{NumTurns: 1, StopReason: "end_turn", Result: "pong"}
	assistant    = claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.TextBlock{Text: "pong"}}}
)

func TestIsNotificationResult(t *testing.T) {
	t.Parallel()
	if !isNotificationResult(staleResult) {
		t.Error("the empty zero-turn result must match")
	}
	// Under the SDK's session the stale result repeats the previous turn's
	// modelUsage (observed live), so usage must not rule it out.
	withUsage := claudecode.ResultMessage{ModelUsage: map[string]claudecode.ModelUsage{"claude-haiku": {InputTokens: 1220, OutputTokens: 185}}}
	if !isNotificationResult(withUsage) {
		t.Error("a zero-turn empty result must match even when it carries a repeated modelUsage")
	}
	for name, r := range map[string]claudecode.ResultMessage{
		"a real result":        realResult,
		"an error":             {IsError: true},
		"has output":           {Result: "x"},
		"has a stop reason":    {StopReason: "end_turn"},
		"has turns but no out": {NumTurns: 1},
	} {
		if isNotificationResult(r) {
			t.Errorf("%s must not be taken for a notification result", name)
		}
	}
}

func TestForwardTurnSkippingStale_SkipsTheNotificationResult(t *testing.T) {
	t.Parallel()
	updates := make(chan claudecode.Message, 16)
	res, err := forwardTurnSkippingStale(context.Background(), feed(
		[]claudecode.Message{notification, staleResult},
		[]claudecode.Message{assistant, realResult},
	), updates)
	if err != nil || res.Result != "pong" || res.StopReason != "end_turn" {
		t.Fatalf("result = %+v, %v, want the real result after the skipped one", res, err)
	}
	if len(updates) != 2 { // the notification and the assistant message are forwarded
		t.Fatalf("forwarded %d messages, want 2 (notification, assistant)", len(updates))
	}
}

func TestForwardTurnSkippingStale_NoNotificationNoSkip(t *testing.T) {
	t.Parallel()
	res, err := forwardTurnSkippingStale(context.Background(), feed([]claudecode.Message{staleResult}), make(chan claudecode.Message, 4))
	if err != nil || !isNotificationResult(res) {
		t.Fatalf("result = %+v, %v, want the empty result returned as is (a genuinely empty turn must end)", res, err)
	}
}

func TestForwardTurnSkippingStale_EachNotificationSkipsOne(t *testing.T) {
	t.Parallel()
	res, err := forwardTurnSkippingStale(context.Background(), feed(
		[]claudecode.Message{notification, notification, staleResult},
		[]claudecode.Message{staleResult},
		[]claudecode.Message{realResult},
	), make(chan claudecode.Message, 8))
	if err != nil || res.Result != "pong" {
		t.Fatalf("two notifications: result = %+v, %v, want the real one", res, err)
	}
	// One notification, two empty results: only the first is skipped.
	res, err = forwardTurnSkippingStale(context.Background(), feed(
		[]claudecode.Message{notification, staleResult},
		[]claudecode.Message{staleResult},
	), make(chan claudecode.Message, 8))
	if err != nil || !isNotificationResult(res) {
		t.Fatalf("one notification: result = %+v, %v, want the second empty result returned", res, err)
	}
}

func TestForwardTurnSkippingStale_CLIExitDoesNotLoop(t *testing.T) {
	t.Parallel()
	done := make(chan error, 1)
	go func() {
		_, err := forwardTurnSkippingStale(context.Background(), feed(
			[]claudecode.Message{notification, staleResult}, // then the stream closes: the CLI exited
		), make(chan claudecode.Message, 4))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cli exited before sending a result") {
			t.Fatalf("error = %v, want the cli-exited error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forwardTurnSkippingStale looped after the CLI exited")
	}
}

func TestForwardTurnSkippingStale_ContextCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := forwardTurnSkippingStale(ctx, feed(), make(chan claudecode.Message, 1))
	if err == nil {
		t.Fatal("error = nil, want the context error")
	}
}

// runClaudeScenario drives one claude-native turn against the fake CLI's
// scenario and returns the events.
func runClaudeScenario(t *testing.T, scenario string) []Event {
	t.Helper()
	wm, task := newTestTask(t, scenario)
	r := claudeNativeRunner(t, wm)
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "ping", denyAllDecider{}, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	return got
}

// TestRunner_RunPrompt_ClaudeNative_SkipsStaleNotificationResult: a resumed
// session whose previous run left a background command behind answers the
// CLI's own task_notification with an empty result before the prompt's turn;
// the run must carry the real reply and stop reason, not end empty (the
// "empty second run" found while dogfooding).
func TestRunner_RunPrompt_ClaudeNative_SkipsStaleNotificationResult(t *testing.T) {
	t.Parallel()
	got := runClaudeScenario(t, "stale-notification")
	var text strings.Builder
	for _, e := range got {
		if e.Type == EventTypeText {
			text.WriteString(e.Text)
		}
	}
	if text.String() != "pong" {
		t.Fatalf("assistant text = %q, want the real reply \"pong\" (events %+v)", text.String(), got)
	}
	last := got[len(got)-1]
	if last.Type != EventTypeDone || last.StopReason != "end_turn" {
		t.Fatalf("last event = %+v, want Done with stop reason end_turn", last)
	}
}

// TestRunner_RunPrompt_ClaudeNative_EmptyResultWithoutNotificationEnds: a
// genuinely empty turn (no task_notification before it) still ends at its own
// result rather than waiting for another one.
func TestRunner_RunPrompt_ClaudeNative_EmptyResultWithoutNotificationEnds(t *testing.T) {
	t.Parallel()
	got := runClaudeScenario(t, "empty-result-no-notification")
	if len(got) == 0 || got[len(got)-1].Type != EventTypeDone {
		t.Fatalf("events = %+v, want a Done event", got)
	}
}
