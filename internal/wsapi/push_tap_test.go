package wsapi

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/push"
	"github.com/spacingmind/smind/internal/runs"
)

// pushKinds is the default kind list the push RPC tests compare against.
var pushKinds = push.AllKinds

func TestPushInputMapping(t *testing.T) {
	t.Parallel()
	rs := func(status, disp, errText string) Event {
		return Event{Topic: TopicRunStatus, Payload: runStatusPayload{RunID: "r1", TaskID: 4, ChatID: 5, Status: status, Disposition: disp, Err: errText}}
	}
	cases := []struct {
		name string
		ev   Event
		ok   bool
		want push.Input
	}{
		{"permission", Event{Topic: TopicPermissionPending, Payload: permissionPendingPayload{RunID: "r1", TaskID: 4, ChatID: 5, RequestID: "q1", Summary: "s"}}, true,
			push.Input{Kind: push.KindPermission, Key: "q1", TaskID: 4, ChatID: 5, RunID: "r1", Summary: "s"}},
		{"done", rs("done", "", ""), true, push.Input{Kind: push.KindDone, Key: "r1:done", TaskID: 4, ChatID: 5, RunID: "r1"}},
		{"needs continue", rs("done", runs.DispositionNeedsContinue, ""), true, push.Input{Kind: push.KindNeedsContinue, Key: "r1:needs_continue", TaskID: 4, ChatID: 5, RunID: "r1"}},
		{"error", rs("error", "", "bad"), true, push.Input{Kind: push.KindError, Key: "r1:error", TaskID: 4, ChatID: 5, RunID: "r1", Err: "bad"}},
		{"stopped", rs("stopped", "", ""), false, push.Input{}},
		{"running", rs("running", "", ""), false, push.Input{}},
		{"interrupted", rs("interrupted", "", ""), false, push.Input{}},
		{"task.status", Event{Topic: TopicTaskStatus, Payload: taskStatusPayload{TaskID: 4, Status: "done"}}, false, push.Input{}},
		{"wrong payload", Event{Topic: TopicRunStatus, Payload: "nope"}, false, push.Input{}},
		{"wrong payload permission", Event{Topic: TopicPermissionPending, Payload: runStatusPayload{}}, false, push.Input{}},
	}
	for _, tc := range cases {
		got, ok := pushInput(tc.ev)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got (%+v, %v), want (%+v, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

type tapSender struct {
	mu     sync.Mutex
	msgs   []push.Message
	called chan struct{}
	block  bool
}

func (s *tapSender) Send(ctx context.Context, msgs []push.Message) ([]push.Result, error) {
	s.mu.Lock()
	s.msgs = append(s.msgs, msgs...)
	s.mu.Unlock()
	select {
	case s.called <- struct{}{}:
	default:
	}
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	out := make([]push.Result, len(msgs))
	for i, m := range msgs {
		out[i] = push.Result{Token: m.Token, OK: true}
	}
	return out, nil
}

func (s *tapSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.msgs)
}

func newTapAPI(t *testing.T, enabled, block bool) (*API, *tapSender, *push.Notifier) {
	t.Helper()
	reg, err := push.OpenRegistry(filepath.Join(t.TempDir(), "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Put(push.Registration{
		DeviceID: "dev-a", Provider: push.ProviderExpo, Token: "ExponentPushToken[tap]", Platform: push.PlatformAndroid,
		Key: pushKeyBytes(32), Kinds: push.AllKinds, RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	s := &tapSender{called: make(chan struct{}, 1), block: block}
	n := push.NewNotifier(push.Deps{
		Registry: reg,
		Sender:   s,
		Task:     func(int64) (push.TaskInfo, bool) { return push.TaskInfo{Title: "t", Root: true}, true },
	})
	t.Cleanup(n.Close)
	api, _ := newRelayDeviceAPI(t, newFakeAdmin())
	api.SetPush(PushOptions{Enabled: enabled, Registry: reg, Notifier: n})
	return api, s, n
}

func permEvent(i int) Event {
	return Event{Topic: TopicPermissionPending, Payload: permissionPendingPayload{TaskID: 1, RequestID: fmt.Sprintf("q%d", i), Summary: "s"}}
}

func TestBusTapReceivesPublishedEvents(t *testing.T) {
	t.Parallel()
	api, s, _ := newTapAPI(t, true, false)
	api.bus.Publish(permEvent(1))
	select {
	case <-s.called:
	case <-time.After(2 * time.Second):
		t.Fatal("sender not called")
	}
	if s.count() != 1 {
		t.Fatalf("messages = %d", s.count())
	}
}

func TestBusTapDoesNotBlockPublishers(t *testing.T) {
	t.Parallel()
	api, _, _ := newTapAPI(t, true, true)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		api.bus.Publish(permEvent(i))
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("publishing took %v", d)
	}
}

func TestSetPushDisabledInstallsNoTap(t *testing.T) {
	t.Parallel()
	api, s, n := newTapAPI(t, false, false)
	api.bus.Publish(permEvent(1))
	time.Sleep(100 * time.Millisecond)
	if s.count() != 0 {
		t.Fatal("disabled push sent a message")
	}
	api.bus.mu.Lock()
	tap := api.bus.tap
	api.bus.mu.Unlock()
	if tap != nil {
		t.Fatal("tap installed")
	}
	// Not started: an enqueue sits in the queue and is never delivered.
	n.Enqueue(push.Input{Kind: push.KindPermission, Key: "x", TaskID: 1})
	time.Sleep(100 * time.Millisecond)
	if s.count() != 0 {
		t.Fatal("notifier was started")
	}
}

func TestPushInputStalled(t *testing.T) {
	t.Parallel()
	ev := Event{Topic: TopicTaskStalled, Payload: stalledPayload{TaskID: 4, Title: "lead", Minutes: 15, Since: "2026-10-25T01:00:00Z"}}
	got, ok := pushInput(ev)
	want := push.Input{Kind: push.KindStalled, Key: "stall:4:1792890000", TaskID: 4, Minutes: 15}
	if !ok || got != want {
		t.Fatalf("pushInput = (%+v, %v), want (%+v, true)", got, ok, want)
	}
	for name, bad := range map[string]Event{
		"bad since":     {Topic: TopicTaskStalled, Payload: stalledPayload{TaskID: 4, Since: "nope"}},
		"wrong payload": {Topic: TopicTaskStalled, Payload: "nope"},
	} {
		if _, ok := pushInput(bad); ok {
			t.Errorf("%s: pushInput ok = true, want false", name)
		}
	}
}

func TestStalledBusEventEmitted(t *testing.T) {
	t.Parallel()
	if !knownTopics[TopicTaskStalled] || TopicTaskStalled != "task.stalled" {
		t.Fatal("task.stalled is not a known topic")
	}
	wm, db := newTestWorkspaceManager(t)
	api, srv := newGitTestAPI(t, wm, db, nil)
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicTaskStalled)

	since := time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)
	api.PublishTaskStalled(9, "lead", 15, since)
	n := ec.nextEvent(5 * time.Second)
	if n.Topic != TopicTaskStalled {
		t.Fatalf("topic = %q, want task.stalled", n.Topic)
	}
	var raw map[string]any
	decodePayload(t, n, &raw)
	if raw["taskId"] != 9.0 || raw["title"] != "lead" || raw["minutes"] != 15.0 || raw["since"] != "2026-10-25T01:00:00Z" || len(raw) != 4 {
		t.Fatalf("payload = %v", raw)
	}
}
