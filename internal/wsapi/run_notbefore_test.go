package wsapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// queueOf returns the queued items of the env task's chats.
func (e *attachEnv) queueOf(t *testing.T) []store.ChatQueueItem {
	t.Helper()
	chats, err := e.db.ListChatsByTask(e.task.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	var all []store.ChatQueueItem
	for _, c := range chats {
		items, err := e.db.ListChatQueue(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, items...)
	}
	return all
}

func TestRunStart_NotBefore_QueuesOnIdleChat(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", false)
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	env := e.start(t, map[string]any{"notBefore": at.Format(time.RFC3339)})
	if env.Error != nil {
		t.Fatalf("run.start error = %+v", env.Error)
	}
	var res runQueuedResult
	if err := json.Unmarshal(env.Result, &res); err != nil || !res.Queued || res.QueueItemID == 0 {
		t.Fatalf("result = %s, %v, want queued with an item id", env.Result, err)
	}
	if n := e.runCount(t); n != 0 {
		t.Fatalf("runs = %d, want 0", n)
	}
	items := e.queueOf(t)
	if len(items) != 1 || items[0].ID != res.QueueItemID || items[0].NotBefore == nil || !items[0].NotBefore.Equal(at) {
		t.Fatalf("queue = %+v, want one item %d held until %v", items, res.QueueItemID, at)
	}
}

func TestRunStart_NotBefore_RefusedForAgentCaller(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", false)
	raw, _ := json.Marshal(map[string]any{
		"taskId": e.task.ID, "provider": "glm", "prompt": "x",
		"notBefore": time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	_, err := e.api.hs["run.start"](context.Background(), &requestContext{runScoped: true}, raw)
	if err == nil || err.Error() != "run.start: notBefore is not available to agent callers" {
		t.Fatalf("error = %v, want the agent-caller refusal", err)
	}
	if items := e.queueOf(t); len(items) != 0 {
		t.Fatalf("queue = %+v, want empty", items)
	}
	if n := e.runCount(t); n != 0 {
		t.Fatalf("runs = %d, want 0", n)
	}
}

func TestRunStart_NotBefore_Invalid(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", false)
	cases := []struct {
		name, value, want string
	}{
		{"garbage", "tomorrow-ish", "run.start: invalid notBefore"},
		{"past", time.Now().Add(-time.Minute).Format(time.RFC3339), "run.start: notBefore must be in the future"},
		{"8 days", time.Now().Add(8 * 24 * time.Hour).Format(time.RFC3339), "run.start: notBefore is more than 7 days ahead"},
	}
	for _, tc := range cases {
		env := e.start(t, map[string]any{"notBefore": tc.value, "attachments": []any{fileReq("a.txt", "hi")}})
		if env.Error == nil || env.Error.Message != tc.want {
			t.Fatalf("%s: error = %+v, want %q", tc.name, env.Error, tc.want)
		}
		if items := e.queueOf(t); len(items) != 0 {
			t.Fatalf("%s: queue = %+v, want empty", tc.name, items)
		}
	}
	if n := e.runCount(t); n != 0 {
		t.Fatalf("runs = %d, want 0", n)
	}
}

func TestRunStart_NoNotBefore_Unchanged(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", false)
	id := e.startOK(t, nil)
	e.waitRunDone(t, id)
	if items := e.queueOf(t); len(items) != 0 {
		t.Fatalf("queue = %+v, want empty", items)
	}
}
