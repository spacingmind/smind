package taskrunner

import (
	"context"
	"strings"
	"testing"
)

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
