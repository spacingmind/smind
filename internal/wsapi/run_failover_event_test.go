package wsapi

import (
	"encoding/json"
	"testing"
)

func TestRunFailoverEventOnBus(t *testing.T) {
	t.Parallel()
	bus := newEventBus()
	sub := newSubscriber()
	sub.subscribe([]string{TopicRunFailover})
	bus.register(sub)
	busRunNotifier{bus: bus}.NotifyRunFailover("run-a", "run-b", 7, 9)

	sub.mu.Lock()
	queue := append([]Event(nil), sub.queue...)
	sub.mu.Unlock()
	if len(queue) != 1 || queue[0].Topic != "run.failover" {
		t.Fatalf("queue = %+v, want one run.failover event", queue)
	}
	b, err := json.Marshal(queue[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 4 || m["fromRunId"] != "run-a" || m["toRunId"] != "run-b" || m["taskId"] != float64(7) || m["toChatId"] != float64(9) {
		t.Fatalf("payload = %v, want {fromRunId, toRunId, taskId, toChatId}", m)
	}
	if !knownTopics[TopicRunFailover] {
		t.Fatal("run.failover is not a known topic")
	}
}
