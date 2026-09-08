# 0005: wsapi event subscription shape

## Status

Accepted

## Decision

`/ws` grows subscription RPCs and server-pushed notification messages,
using the existing request/response envelope:

- **Subscribe/unsubscribe**: `events.subscribe` with
  `{"topics": ["task.status", "run.status", "permission.pending"]}` adds
  those topics on the calling connection and returns
  `{"topics": [...]}` (the effective set). `events.unsubscribe` with the
  same params shape removes them, same result shape. Subscriptions are
  per-connection and additive; unknown topic names are an error;
  subscribe/unsubscribe of a topic not subscribed is idempotent.
- **Event envelope**: pushed events are notification messages
  distinguishable from RPC responses by shape —
  `{"event": {"topic": "...", "seq": N, "payload": {...}}}` with no `id`,
  versus a response's `{"id": ..., "result"}`/`{"error"}`. `seq` is a
  per-connection, per-subscription monotonic counter (starts at 1) so a
  client can detect gaps. `payload` is topic-specific
  (`task.status`: `{taskId, status}`; `run.status`:
  `{runId, taskId, status, stopReason, err}`; `permission.pending`:
  `{runId, taskId, requestId, summary, options}`).
- **Topic naming**: `domain.verb` dotted lowercase — the three initial
  topics above; new topics extend the set without protocol change.
- **Delivery semantics**: at-least-once per connection, live-only. No
  snapshot, no cursor, no replay: events that occur while a client is
  disconnected are simply missed; a reconnecting client re-derives
  current state via the existing RPCs (`task.list`, `run.list`) and then
  subscribes. Deliberate v1 simplification — the UI already refetches on
  reconnect, and a cursor would require a server-side event log.
- **Backpressure**: one bounded queue per connection (capacity 256). A
  full queue drops the oldest event and latches a drop flag; when the
  connection's writer next drains it, the flag produces a synthetic
  `event.dropped` notification (`payload: {count}`) so the client knows
  to refetch, and a connection that stays full for a sustained period is
  eventually closed by the server rather than buffering unboundedly.

## Alternatives considered

- **Per-resource `*.watch` subscriptions** (Paseo's
  `subscribe_checkout_diff_*` pairs). Finer-grained filtering, but three
  times the methods for the same wire surface and the initial consumers
  (sidebar rail, badges) always want whole-domain streams anyway.
- **One firehose per connection, no topic filter.** Simplest server, but
  couples every client to every event type and makes the topic set a
  compatibility liability.
- **Snapshot + tail with a replay cursor on reconnect.** Stronger
  guarantees, but needs a persisted event log and cursor semantics before
  any consumer needs them; live-only plus refetch-on-reconnect matches
  what the web UI already does.

## Rationale

The gap (`docs/research/dual-mode-ui.md`): permission events only reach a
client inside an active `run.attach`; nothing else is pushable. This shape
reuses the existing envelope and connection multiplexing
(`internal/wsapi/conn.go`), keeps the notification/response distinction
structural (no `id` + `event` object vs `id` + `result`), and locks in
only what v1 consumers need — topic streams over one shared connection —
while leaving the topic set open-ended.
