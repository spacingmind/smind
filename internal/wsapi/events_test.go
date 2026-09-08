package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// eventConn wraps one test WebSocket connection with a buffered reader
// that classifies each inbound message as either an RPC response (queued
// by id) or an ADR-0005 event notification (queued in arrival order).
// The two kinds interleave arbitrarily on the wire (the pump goroutine
// writes notifications concurrently with handler responses), so tests
// must never consume-and-discard one kind while waiting for the other --
// hence the queues.
type eventConn struct {
	t         *testing.T
	ws        *websocket.Conn
	events    []eventNotification
	responses map[string]envelope
	lastErr   error
}

func newEventConn(t *testing.T, ws *websocket.Conn) *eventConn {
	return &eventConn{t: t, ws: ws, responses: make(map[string]envelope)}
}

// readOne reads a single message and files it in the matching queue.
func (c *eventConn) readOne(deadline time.Time) bool {
	c.t.Helper()
	if err := c.ws.SetReadDeadline(deadline); err != nil {
		c.t.Fatalf("SetReadDeadline() error = %v", err)
	}
	_, data, err := c.ws.ReadMessage()
	if err != nil {
		c.lastErr = err
		return false
	}
	var msg struct {
		ID    string             `json:"id"`
		Event *eventNotification `json:"event"`
		Error *rpcError          `json:"error"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		c.t.Fatalf("unmarshal message: %v: %s", err, data)
	}
	switch {
	case msg.ID == "" && msg.Event != nil:
		c.events = append(c.events, *msg.Event)
	case msg.ID != "":
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			c.t.Fatalf("unmarshal response: %v: %s", err, data)
		}
		c.responses[msg.ID] = env
	default:
		c.t.Fatalf("unclassifiable message: %s", data)
	}
	return true
}

// nextResponse drains until id's terminal response is queued.
func (c *eventConn) nextResponse(id string, timeout time.Duration) envelope {
	c.t.Helper()
	if env, ok := c.responses[id]; ok {
		delete(c.responses, id)
		return env
	}
	deadline := time.Now().Add(timeout)
	for c.readOne(deadline) {
		if env, ok := c.responses[id]; ok {
			delete(c.responses, id)
			return env
		}
	}
	c.t.Fatalf("timed out waiting for a response with id %q (last read error: %v)", id, c.lastErr)
	return envelope{}
}

// nextEvent drains until an event notification is queued.
func (c *eventConn) nextEvent(timeout time.Duration) eventNotification {
	c.t.Helper()
	if len(c.events) > 0 {
		e := c.events[0]
		c.events = c.events[1:]
		return e
	}
	deadline := time.Now().Add(timeout)
	for c.readOne(deadline) {
		if len(c.events) > 0 {
			e := c.events[0]
			c.events = c.events[1:]
			return e
		}
	}
	c.t.Fatal("timed out waiting for an event notification")
	return eventNotification{}
}

// expectNoEvent asserts no event notification arrives (or is already
// queued) within timeout.
func (c *eventConn) expectNoEvent(timeout time.Duration) {
	c.t.Helper()
	if len(c.events) > 0 {
		c.t.Fatalf("an event notification was already queued: %+v", c.events[0])
	}
	c.readOne(time.Now().Add(timeout))
	if len(c.events) > 0 {
		c.t.Fatalf("received an unexpected event notification: %+v", c.events[0])
	}
}

// subscribe sends events.subscribe and waits for its response, returning
// once the subscription is guaranteed effective.
func (c *eventConn) subscribe(id string, topics ...string) {
	c.t.Helper()
	sendRequest(c.t, c.ws, id, "events.subscribe", map[string]any{"topics": topics})
	resp := c.nextResponse(id, 5*time.Second)
	if resp.Error != nil {
		c.t.Fatalf("events.subscribe error = %v", resp.Error.Message)
	}
	var got subscribeResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		c.t.Fatalf("decode events.subscribe result: %v", err)
	}
	if len(got.Topics) != len(topics) {
		c.t.Fatalf("events.subscribe effective topics = %v, want %v", got.Topics, topics)
	}
}

func (c *eventConn) unsubscribe(id string, topics ...string) {
	c.t.Helper()
	sendRequest(c.t, c.ws, id, "events.unsubscribe", map[string]any{"topics": topics})
	resp := c.nextResponse(id, 5*time.Second)
	if resp.Error != nil {
		c.t.Fatalf("events.unsubscribe error = %v", resp.Error.Message)
	}
}

func decodePayload(t *testing.T, e eventNotification, v any) {
	t.Helper()
	data, err := json.Marshal(e.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s payload: %v", e.Topic, err)
	}
}

func TestEvents_TaskStatusSubscribeAndReceive(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	ec.subscribe("sub", TopicTaskStatus)

	task := newTestTask(t, wm, "")

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicTaskStatus || ev.Seq != 1 {
		t.Fatalf("first event = %+v, want topic %q seq 1", ev, TopicTaskStatus)
	}
	var p taskStatusPayload
	decodePayload(t, ev, &p)
	if p.TaskID != task.ID || p.Status != "created" {
		t.Fatalf("task.status payload = %+v, want taskId %d status created", p, task.ID)
	}

	sendRequest(t, ec.ws, "arch", "task.archive", map[string]any{"id": task.ID})
	if resp := ec.nextResponse("arch", 5*time.Second); resp.Error != nil {
		t.Fatalf("task.archive error = %v", resp.Error.Message)
	}

	ev = ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicTaskStatus || ev.Seq != 2 {
		t.Fatalf("second event = %+v, want topic %q seq 2", ev, TopicTaskStatus)
	}
	decodePayload(t, ev, &p)
	if p.TaskID != task.ID || p.Status != "archived" {
		t.Fatalf("task.status payload = %+v, want taskId %d status archived", p, task.ID)
	}
}

func TestEvents_UnsubscribeStopsDelivery(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	ec.subscribe("sub", TopicTaskStatus)
	ec.unsubscribe("unsub", TopicTaskStatus)

	task := newTestTask(t, wm, "")
	if _, err := wm.ArchiveTask(task.ID); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

func TestEvents_NoCrossDeliveryBetweenTopics(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ecTask := newEventConn(t, dialWS(t, srv, "tok"))
	ecRun := newEventConn(t, dialWS(t, srv, "tok"))

	ecTask.subscribe("sub1", TopicTaskStatus)
	ecRun.subscribe("sub2", TopicRunStatus)

	// A task transition must reach only the task.status subscriber.
	// (ecRun's no-delivery assertion is deferred to the end: after a read
	// deadline fires, gorilla may return the stale deadline error on the
	// next read, so a connection that has "timed out" once must not be
	// read again in the same test.)
	task := newTestTask(t, wm, "")
	if ev := ecTask.nextEvent(5 * time.Second); ev.Topic != TopicTaskStatus {
		t.Fatalf("ecTask event topic = %q, want %q", ev.Topic, TopicTaskStatus)
	}

	// A real run (fake agent default scenario) must reach only the
	// run.status subscriber: "running" on start, then "done" on finish.
	sendRequest(t, ecRun.ws, "start", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	resp := ecRun.nextResponse("start", 15*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start error = %v", resp.Error.Message)
	}
	var started runStartResult
	if err := json.Unmarshal(resp.Result, &started); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}

	ev := ecRun.nextEvent(15 * time.Second)
	if ev.Topic != TopicRunStatus {
		t.Fatalf("ecRun event topic = %q, want %q", ev.Topic, TopicRunStatus)
	}
	var p runStatusPayload
	decodePayload(t, ev, &p)
	if p.RunID != started.RunID || p.TaskID != task.ID || p.Status != "running" {
		t.Fatalf("run.status payload = %+v, want runId %s taskId %d status running", p, started.RunID, task.ID)
	}

	ev = ecRun.nextEvent(15 * time.Second)
	if ev.Topic != TopicRunStatus {
		t.Fatalf("ecRun event topic = %q, want %q", ev.Topic, TopicRunStatus)
	}
	decodePayload(t, ev, &p)
	if p.RunID != started.RunID || p.Status != "done" {
		t.Fatalf("run.status payload = %+v, want runId %s status done", p, started.RunID)
	}

	// Neither connection saw the other's topic: the run produced no
	// task.status traffic for ecTask beyond the "created" event already
	// consumed, and the task transition produced no run.status traffic for
	// ecRun beyond the events already consumed.
	ecTask.expectNoEvent(300 * time.Millisecond)
	ecRun.expectNoEvent(300 * time.Millisecond)
}

func TestEvents_UnknownTopicIsError(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	sendRequest(t, ec.ws, "sub", "events.subscribe", map[string]any{"topics": []string{"nope.topic"}})
	resp := ec.nextResponse("sub", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("events.subscribe with unknown topic: error = nil, want an error")
	}
}
