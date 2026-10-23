package taskrunner

import (
	"context"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

func rateLimitMsg(status string, resetsAt int64) claudecode.RateLimitEvent {
	return claudecode.RateLimitEvent{RateLimitInfo: claudecode.RateLimitInfo{
		Status: status, ResetsAt: resetsAt, RateLimitType: "five_hour",
	}}
}

func TestClaudeEvents_RateLimitRejectedYieldsSignal(t *testing.T) {
	t.Parallel()
	got := claudeEvents(rateLimitMsg("rejected", 1790000000))
	if len(got) != 1 {
		t.Fatalf("events = %+v, want exactly one", got)
	}
	e := got[0]
	if e.Type != EventTypeRateLimit || e.RateLimit == nil {
		t.Fatalf("event = %+v", e)
	}
	if e.RateLimit.Type != "five_hour" || e.RateLimit.Status != "rejected" || e.RateLimit.ResetsAt.Unix() != 1790000000 {
		t.Fatalf("signal = %+v", *e.RateLimit)
	}
}

func TestClaudeEvents_RateLimitWarningYieldsNothing(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"allowed_warning", "allowed"} {
		if got := claudeEvents(rateLimitMsg(status, 1790000000)); len(got) != 0 {
			t.Errorf("status %q: events = %+v, want none", status, got)
		}
	}
}

func TestClaudeEvents_RateLimitRejectedWithoutResetHasZeroTime(t *testing.T) {
	t.Parallel()
	got := claudeEvents(rateLimitMsg("rejected", 0))
	if len(got) != 1 || got[0].RateLimit == nil {
		t.Fatalf("events = %+v", got)
	}
	if !got[0].RateLimit.ResetsAt.IsZero() {
		t.Fatalf("ResetsAt = %v, want zero", got[0].RateLimit.ResetsAt)
	}
}

func TestClaudeStreamer_RateLimitIsOrderedAfterBufferedText(t *testing.T) {
	t.Parallel()
	got := collect(
		streamMessageStart(),
		streamDelta("text_delta", "partial"),
		rateLimitMsg("rejected", 1790000000),
	)
	if len(got) != 2 || got[0].Type != EventTypeText || got[0].Text != "partial" || got[1].Type != EventTypeRateLimit {
		t.Fatalf("events = %+v, want text then rate limit", got)
	}
}

func TestRunPrompt_ClaudeRateLimitEventReachesEventsChannel(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "rate-limit-rejected")
	r := claudeNativeRunner(t, wm)
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	rl, done := -1, -1
	for i, e := range got {
		switch e.Type {
		case EventTypeRateLimit:
			if rl != -1 {
				t.Fatalf("more than one rate limit event: %+v", got)
			}
			rl = i
			if e.RateLimit == nil || e.RateLimit.Type != "five_hour" || e.RateLimit.ResetsAt.Unix() != 1790000000 {
				t.Fatalf("signal = %+v", e.RateLimit)
			}
		case EventTypeDone:
			done = i
		}
	}
	if rl == -1 || done == -1 || rl > done {
		t.Fatalf("want one rate limit event before Done, got %+v", got)
	}
}
