package push

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// ADR-0055 D7: a question push says an agent asked and never what. Even if a
// caller hands the notifier question text in Summary, the body stays fixed.
func TestPush_QuestionTitleOnly(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	r := h.add("dev-a", 1, nil)
	secret := "Which production database should I drop? https://evil.example/x"
	in := Input{Kind: KindQuestion, Key: "req-1", TaskID: 1, ChatID: 9, RunID: "run-x", Summary: secret}
	if h.n.Notify(context.Background(), in) != 1 {
		t.Fatal("a question push was not delivered")
	}
	msgs := h.sender.messages()
	ev := h.open(r, msgs[len(msgs)-1])
	if ev.Kind != KindQuestion || ev.Title != "Root task" || ev.Body != QuestionBody || ev.TaskID != 1 || ev.RunID != "run-x" || ev.ChatID != 9 {
		t.Fatalf("event = %+v", ev)
	}
	if strings.Contains(ev.Title+ev.Body, "production") || strings.Contains(ev.Title+ev.Body, "evil") {
		t.Fatalf("question text leaked into the push: %+v", ev)
	}
}

func TestPush_QuestionDedupedByRequestID(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	ctx := context.Background()
	in := Input{Kind: KindQuestion, Key: "req-1", TaskID: 1}
	if h.n.Notify(ctx, in) != 1 || h.n.Notify(ctx, in) != 0 {
		t.Fatal("the same request id must deliver once")
	}
	in.Key = "req-2"
	if h.n.Notify(ctx, in) != 1 {
		t.Fatal("a different request id must deliver")
	}
}

func TestPush_QuestionKindIsSubscribable(t *testing.T) {
	t.Parallel()
	if !slices.Contains(AllKinds, KindQuestion) {
		t.Fatal("question is not in AllKinds")
	}
	h := newNHarness(t)
	h.add("dev-a", 1, []string{KindPermission})
	h.add("dev-b", 2, []string{KindQuestion})
	if got := h.n.Notify(context.Background(), Input{Kind: KindQuestion, Key: "k", TaskID: 1}); got != 1 {
		t.Fatalf("delivered to %d devices, want only the one subscribed to question", got)
	}
	if m := h.sender.messages(); len(m) != 1 || m[0].Token != "ExponentPushToken[dev-btok]" {
		t.Fatalf("messages = %+v", m)
	}
	// A system task never pushes, a question included.
	if got := h.n.Notify(context.Background(), Input{Kind: KindQuestion, Key: "k2", TaskID: 3}); got != 0 {
		t.Fatalf("a system task pushed a question: %d", got)
	}
}
