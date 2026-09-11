package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/gorilla/websocket"
)

// Event topics (ADR 0005): domain.verb, dotted lowercase. The set is
// open-ended -- new topics extend it without protocol change.
const (
	TopicTaskStatus        = "task.status"
	TopicRunStatus         = "run.status"
	TopicPermissionPending = "permission.pending"
)

// knownTopics is the set events.subscribe/events.unsubscribe accept;
// subscribing to an unknown topic name is an error per the ADR.
var knownTopics = map[string]bool{
	TopicTaskStatus:        true,
	TopicRunStatus:         true,
	TopicPermissionPending: true,
}

// subscriberQueueCap is the per-connection event queue bound (ADR 0005):
// a full queue drops the oldest event and latches a drop count; the pump
// turns that into a synthetic "event.dropped" notification.
const subscriberQueueCap = 256

// taskStatusPayload is the payload of task.status events: {taskId, status}.
type taskStatusPayload struct {
	TaskID int64  `json:"taskId"`
	Status string `json:"status"`
}

// runStatusPayload is the payload of run.status events:
// {runId, taskId, status, stopReason, err}.
type runStatusPayload struct {
	RunID      string `json:"runId"`
	TaskID     int64  `json:"taskId"`
	Status     string `json:"status"`
	StopReason string `json:"stopReason,omitempty"`
	Err        string `json:"err,omitempty"`
}

// permissionPendingPayload is the payload of permission.pending events:
// {runId, taskId, requestId, summary, options}.
type permissionPendingPayload struct {
	RunID     string                   `json:"runId"`
	TaskID    int64                    `json:"taskId"`
	RequestID string                   `json:"requestId"`
	Summary   string                   `json:"summary"`
	Options   []permissionOptionParams `json:"options"`
}

// Event is what the bus's publish sites hand it: a topic plus an
// already-wire-shaped payload.
type Event struct {
	Topic   string
	Payload any
}

// subscriber is one connection's registration on the event bus: the
// topics it currently receives, a bounded FIFO of pending events, and
// the drop accounting for the ADR's backpressure contract.
type subscriber struct {
	mu      sync.Mutex
	topics  map[string]bool
	queue   []Event
	dropped int
	closed  bool
	signal  chan struct{} // cap 1; nudged on push/close, never blocks
}

func newSubscriber() *subscriber {
	return &subscriber{
		topics: make(map[string]bool),
		signal: make(chan struct{}, 1),
	}
}

// subscribe adds topics (idempotent per topic, per the ADR) and returns
// the effective topic set, sorted for a stable wire shape.
func (s *subscriber) subscribe(topics []string) []string {
	s.mu.Lock()
	for _, t := range topics {
		s.topics[t] = true
	}
	out := make([]string, 0, len(s.topics))
	for t := range s.topics {
		out = append(out, t)
	}
	s.mu.Unlock()
	sort.Strings(out)
	return out
}

// unsubscribe removes topics (idempotent) and returns the effective set.
func (s *subscriber) unsubscribe(topics []string) []string {
	s.mu.Lock()
	for _, t := range topics {
		delete(s.topics, t)
	}
	out := make([]string, 0, len(s.topics))
	for t := range s.topics {
		out = append(out, t)
	}
	s.mu.Unlock()
	sort.Strings(out)
	return out
}

// offer enqueues e if s is subscribed to its topic, applying the
// drop-oldest policy when the queue is full. It never blocks: publish
// sites are state-transition code that must not stall on a slow client.
func (s *subscriber) offer(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.topics[e.Topic] {
		return
	}
	if len(s.queue) >= subscriberQueueCap {
		s.queue = s.queue[1:]
		s.dropped++
	}
	s.queue = append(s.queue, e)
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// next returns the next queued event plus the number of drops latched
// since the last drain (rendered by the pump as an "event.dropped"
// notification), or ok=false once closed and drained.
func (s *subscriber) next() (e Event, dropped int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped = s.dropped
	s.dropped = 0
	if len(s.queue) > 0 {
		e, s.queue = s.queue[0], s.queue[1:]
		return e, dropped, true
	}
	// Closed and drained: ok=false so pumpEvents falls into its wait(ctx)
	// branch instead of spinning -- returning s.closed here (true once
	// closed) made this look like "an event is ready" forever, so the
	// pump never blocked and never noticed ctx firing: a 100%-CPU
	// goroutine leaked per closed connection (2026-09-12 dogfood finding).
	return e, dropped, false
}

// close marks the subscriber done; the pump exits once drained.
func (s *subscriber) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// wait blocks until a nudge (push or close) or ctx fires; false means
// ctx fired and the pump should exit.
func (s *subscriber) wait(ctx context.Context) bool {
	select {
	case <-s.signal:
		return true
	case <-ctx.Done():
		return false
	}
}

// eventBus fans out published events to every subscriber registered for
// the event's topic. One bus is shared by all connections (owned by the
// wsapi server) and fed by the state-owning packages via their notifier
// hooks (workspace.Manager, runs.Registry), which the server adapts onto
// Publish in New.
type eventBus struct {
	mu   sync.Mutex
	subs map[*subscriber]bool
}

func newEventBus() *eventBus {
	return &eventBus{subs: make(map[*subscriber]bool)}
}

func (b *eventBus) register(s *subscriber) {
	b.mu.Lock()
	b.subs[s] = true
	b.mu.Unlock()
}

// unregister removes s; the caller also closes it so its pump exits.
func (b *eventBus) unregister(s *subscriber) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
}

// Publish offers e to every subscriber whose topic set matches. The
// snapshot-then-offer shape mirrors runs.Registry.record: per-subscriber
// locking happens inside offer, never under b.mu, so a full-queue
// subscriber can't hold up publishes to the others.
func (b *eventBus) Publish(e Event) {
	b.mu.Lock()
	subs := make([]*subscriber, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()
	for _, s := range subs {
		s.offer(e)
	}
}

// subscribeResult is the result of events.subscribe and
// events.unsubscribe: the connection's effective topic set.
type subscribeResult struct {
	Topics []string `json:"topics"`
}

// handleEventsSubscribe validates topic names (unknown => error, per the
// ADR), adds them to this connection's subscriber (idempotently), and
// returns the effective set.
func handleEventsSubscribe(sub *subscriber) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		topics, err := parseTopics(raw)
		if err != nil {
			return nil, fmt.Errorf("events.subscribe: %w", err)
		}
		return subscribeResult{Topics: sub.subscribe(topics)}, nil
	}
}

// handleEventsUnsubscribe is events.subscribe's removal twin: same
// params/result shape, idempotent per the ADR.
func handleEventsUnsubscribe(sub *subscriber) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		topics, err := parseTopics(raw)
		if err != nil {
			return nil, fmt.Errorf("events.unsubscribe: %w", err)
		}
		return subscribeResult{Topics: sub.unsubscribe(topics)}, nil
	}
}

// parseTopics decodes {"topics": [...]}, rejecting empty lists and
// unknown topic names.
func parseTopics(raw json.RawMessage) ([]string, error) {
	var p struct {
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if len(p.Topics) == 0 {
		return nil, fmt.Errorf("topics must be a non-empty list")
	}
	for _, t := range p.Topics {
		if !knownTopics[t] {
			return nil, fmt.Errorf("unknown topic %q", t)
		}
	}
	return p.Topics, nil
}

// eventNotification is the pushed-event wire shape per ADR 0005:
// {"event": {"topic": "...", "seq": N, "payload": {...}}} with no id --
// structurally distinguishable from any RPC response, which always
// carries an id.
type eventNotification struct {
	Topic   string `json:"topic"`
	Seq     int64  `json:"seq"`
	Payload any    `json:"payload"`
}

// eventDroppedPayload is the payload of the synthetic "event.dropped"
// notification delivered after a connection's queue overflowed, so the
// client knows to refetch state via task.list/run.list.
type eventDroppedPayload struct {
	Count int `json:"count"`
}

// pumpEvents drains sub for the connection's lifetime, writing each
// event as an ADR-0005 notification with a per-connection monotonic seq
// starting at 1 (sequenced in delivery order across all of the
// connection's topics, so a client detects any gap). Exits when ctx (the
// connection's own context) fires.
func (c *conn) pumpEvents(ctx context.Context, sub *subscriber) {
	seq := int64(0)
	for {
		e, dropped, ok := sub.next()
		if dropped > 0 {
			seq++
			c.writeEventNotification(eventNotification{
				Topic: "event.dropped", Seq: seq, Payload: eventDroppedPayload{Count: dropped},
			})
		}
		if !ok {
			if !sub.wait(ctx) {
				return
			}
			continue
		}
		seq++
		c.writeEventNotification(eventNotification{Topic: e.Topic, Seq: seq, Payload: e.Payload})
	}
}

// writeEventNotification marshals the ADR-0005 notification shape (which
// the shared envelope struct can't express -- its Event field is a
// string) and writes it under the same write lock as every other outbound
// message, preserving the one-writer-at-a-time invariant.
func (c *conn) writeEventNotification(n eventNotification) {
	data, err := json.Marshal(struct {
		Event eventNotification `json:"event"`
	}{Event: n})
	if err != nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.WriteMessage(websocket.TextMessage, data)
}
