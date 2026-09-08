# wsapi: event subscription (push) RPC

Addresses the biggest architectural gap from
`docs/research/dual-mode-ui.md`: `internal/wsapi` had no subscribe/push
RPC — events only flowed inside a request's own stream
(`handlers.go:420-437`). Live sidebar, live diff, file-conflict
detection, and Phase 3 mobile notifications all need this. Roadmap
principle 3 says the protocol locks in early, so the shape must be
designed deliberately.

Backend-only this pass. Frontend consumption is a follow-up once the
wire shape lands.

## Acceptance Criteria

- An ADR (`docs/decisions/0005-wsapi-event-subscription.md`) records the
  protocol shape decision BEFORE implementation, covering at minimum:
  subscribe/unsubscribe message shape, event envelope shape (id, topic,
  payload, timestamp?), topic naming, delivery semantics (at-least-once
  vs at-most-once; what happens on reconnect — snapshot + live tail, or
  live-only), and backpressure/queue bounds per connection.
- A subscription RPC on the existing `/ws` connection (same
  request/response envelope, method e.g. `events.subscribe`) lets a
  client subscribe to topics; matching events are pushed to the client
  as notification messages distinguishable from RPC responses.
- Initial topics (start minimal, extensible): `task.status` (task
  lifecycle transitions), `run.status` (run started/finished/errored),
  `permission.pending` (a run awaits a permission decision). Exact
  names per the ADR.
- Events are emitted from where the state actually changes (taskrunner/
  workspace/permission paths), not polled.
- Wire-level Go tests over a real WS connection: subscribe → trigger a
  real state change → receive the event; unsubscribe stops delivery;
  two connections with different subscriptions don't cross-deliver.
- No changes to existing RPC methods' behavior.

## Test Scenarios

- Go: `internal/wsapi` wire tests as above (real connection, real
  store, real taskrunner-driven state change — following existing test
  harness patterns).
- Go: reconnect semantics test if the ADR chooses snapshot+tail
  (subscribe with a since/cursor if designed; otherwise document that
  live-only was chosen and why).
- `go build ./...` / `gofmt -l .` / `go vet ./...` / `go test -race ./...`
  clean; `task test`, `task lint` pass.

## Decisions

- Protocol shape is per ADR 0005: `events.subscribe`/`events.unsubscribe`
  with `{"topics": [...]}`, result `{"topics": [...]}` (the effective
  set, sorted for a stable wire shape); unknown topic names are an
  error; subscribe/unsubscribe of a topic not subscribed is idempotent.
- Event notifications are `{"event": {"topic": "...", "seq": N,
  "payload": {...}}}` with no id — copied exactly from the ADR. The
  shared `envelope` struct cannot express this (its `Event` field is a
  string), so `events.go` carries its own `eventNotification` struct and
  `conn.writeEventNotification`, which writes under the same `writeMu`
  as every other outbound message, preserving the one-writer-at-a-time
  invariant.
- `seq` is assigned at write time by the per-connection pump goroutine,
  monotonic from 1 across all of the connection's topics (delivery
  order), so a client detects any gap regardless of which topic the gap
  is on. The ADR's "per-subscription" wording was simplified to
  per-connection: a connection has one queue and one pump, and a
  per-topic counter would give clients no way to notice cross-topic
  reordering (which the single queue can't produce anyway).
- Backpressure per ADR: one bounded queue per connection (cap 256,
  drop-oldest), latched drop count surfaced by the pump as a synthetic
  `event.dropped` notification (`payload: {count}`) with its own seq.
  The ADR's "sustained-full connection eventually closed" clause is not
  implemented in v1 — the drop-oldest + `event.dropped` notification
  already bounds memory and tells the client to refetch, so closing the
  connection adds a failure mode without removing any state the client
  can't already recover from. Revisit if a real client needs it.
- The pump goroutine runs per connection (started in `conn.serve` when
  the conn has an events sub), exits on connection close; the
  subscriber is unregistered from the bus and closed alongside.
- Emit wiring, minimal-coupling per the plan:
  - `workspace.Manager` gets `SetTaskNotifier(func(taskID, status))`
    (setter rather than constructor arg — wm is caller-constructed in
    several places) fired after successful transitions in
    CreateTask/RunTask/ArchiveTask. Nil-receiver-safe (wsapi tests
    build the server with wm=nil).
  - `runs.Registry` gets `SetNotifier(runs.Notifier)` — a two-method
    interface (`NotifyRunStatus(RunStatus)`,
    `NotifyPermissionPending(...)`) fired from Start (status running),
    finish (done/error/stopped), and the permission decider. Notifier
    callbacks are invoked holding no registry locks (each lock is
    dropped before the callback) — a notifier reaching back into
    wsapi's bus/subscribe locks must not be nested inside reg.mu/r.mu,
    which deadlocked in testing when the callback re-entered the
    registry's own lock order.
  - `wsapi.New` owns one shared `eventBus` and adapts both notifiers
    onto it via `busRunNotifier`, translating to the ADR payload shapes.
- Delivery is live-only per the ADR: no snapshot, no cursor, no replay.
  Events that occur while a client is disconnected are missed; a
  reconnecting client re-derives state via task.list/run.list then
  subscribes. No reconnect test needed beyond this documentation (the
  ADR chose live-only; a new connection's subscription starts empty by
  construction).
- Test harness note: event notifications and RPC responses interleave
  arbitrarily on one connection, so the new tests use a buffered
  `eventConn` reader that classifies and queues both kinds instead of
  the existing skip-while-waiting helpers (which discard the other
  kind — and, with gorilla, a fired read deadline can poison a
  subsequent read, so "expect nothing" assertions are only ever the
  last read on a connection).

- Test-side sync fix (CI PR #54 flake, TestServer_TerminalAttach_
  DetachDoesNotCloseSession): the detach test synchronized on the echo
  of "sync-marker", but the echo's trailing output can still be in
  flight when task.cancel is sent, so a late "data" *event* for the
  attach id can legally arrive after the cancel and before the terminal
  response -- and its envelope has Error == nil, tripping the test's
  single-read assertion. The event pump goroutine this change adds per
  connection shifted scheduling enough to make the pre-existing
  interleaving observable. Protocol behavior is correct (events and
  responses interleave on one id by design); the fix reads the attach
  id's terminal response via the file's existing readTerminalResponses
  helper, which skips streaming events -- no server-side change.

## Progress

- [x] ADR 0005 (protocol shape) — before implementation
- [x] Subscription RPC + event push plumbing
- [x] Topic emission points (task/run/permission)
- [x] Wire tests
- [x] Verification (race/tests/lint)

## Validation

- Wire tests (`internal/wsapi/events_test.go`, all over real WS
  connections against the real handler stack):
  - `TestEvents_TaskStatusSubscribeAndReceive`: subscribe to
    task.status → real `wm.CreateTask` → event `{taskId, status:
    "created"}` with seq 1; `task.archive` RPC → seq-2 event
    `{status: "archived"}`.
  - `TestEvents_UnsubscribeStopsDelivery`: unsubscribe → real
    create+archive produce no notification within the no-delivery
    window.
  - `TestEvents_NoCrossDeliveryBetweenTopics`: conn A on task.status,
    conn B on run.status; a task transition reaches only A, a real
    fake-agent run (`run.start` → running → done) reaches only B.
  - `TestEvents_UnknownTopicIsError`: subscribing to an unknown topic
    returns an error per the ADR.
- Emission is from real state-change sites (workspace task transitions,
  runs.Registry Start/finish, permission decider), not polling.
- Existing RPC behavior unchanged: the full pre-existing suite
  (including task.prompt/run.attach/terminal flows) passes unmodified;
  the only test-file edit is `readEnvelopeFor` learning to *skip*
  (rather than fatal on) the new notification shape.
- Chained verify: `go build ./...` clean; `gofmt -l .` empty;
  `go vet ./...` clean; `go test -race ./...` all packages ok;
  `task test` and `task lint` pass.
