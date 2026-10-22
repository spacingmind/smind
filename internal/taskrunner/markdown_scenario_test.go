package taskrunner

import (
	"context"
	"strings"
	"testing"
)

// TestRunner_RunPrompt_GLM_MarkdownPermissionScenario pins the fake agent's
// "markdown-permission" scenario (the mobile transcript smoke's run): markdown
// arrives as several text chunks, a tool call completes, and the run ends
// after the permission request is answered.
func TestRunner_RunPrompt_GLM_MarkdownPermissionScenario(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "markdown-permission")
	r := glmRunner(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", "", events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	var text strings.Builder
	tools := 0
	for _, e := range got {
		switch e.Type {
		case EventTypeText:
			text.WriteString(e.Text)
		case EventTypeToolCall:
			tools++
		}
	}
	for _, want := range []string{"## Plan", "**fix**", "`retry`", "- read the test", "```go"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("streamed text %q lacks %q", text.String(), want)
		}
	}
	if tools != 1 {
		t.Fatalf("got %d tool-call events, want 1: %+v", tools, got)
	}
	if last := got[len(got)-1]; last.Type != EventTypeDone || last.StopReason != "end_turn" {
		t.Fatalf("last event = %+v, want done/end_turn", last)
	}
}
