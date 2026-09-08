# wsapi: event subscription (push) RPC

Addresses the biggest architectural gap from
`docs/research/dual-mode-ui.md`: `internal/wsapi` has no subscribe/push
RPC — events only flow inside a request's own stream
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
  store, real taskrunner-driven state change — follow existing test
  harness patterns).
- Go: reconnect semantics test if the ADR chooses snapshot+tail
  (subscribe with a since/cursor if designed; otherwise document that
  live-only was chosen and why).
- `go build ./...` / `gofmt -l .` / `go vet ./...` / `go test -race ./...`
  clean; `task test`, `task lint` pass.

## Decisions

(To be filled: everything the ADR requires — this plan defers the
 protocol shape to the ADR, which must exist before code.)

## Progress

- [ ] ADR 0005 (protocol shape) — before implementation
- [ ] Subscription RPC + event push plumbing
- [ ] Topic emission points (task/run/permission)
- [ ] Wire tests
- [ ] Verification (race/tests/lint)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
