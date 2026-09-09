# Live UI on wsapi event subscriptions

Consumes ADR 0005's `events.subscribe` from the web UI, replacing
refresh/reconnect-driven updates with live push. This is the natural
follow-up to #54 (the subscription RPC) and #53 (refresh-driven
attention badges), and the first real validation of the event pump
end-to-end against a browser client.

## Acceptance Criteria

- **Client notification support** (`web/packages/ui/src/lib/ws-client.ts`):
  `WireEnvelope`/`handleMessage` currently ignore subscription
  notifications (`{event: {topic, seq, payload}}`, no `id` — the
  envelope's `event` field is an object there, a string for
  request-stream events; see internal/wsapi/events.go). Add a way to
  register a notification listener on WsClient (e.g. `onNotification`
  or a subscribe-style API) that fires for every such message, without
  disturbing the existing request/response/stream-event machinery.
- **A small `useDaemonEvents(client)` hook** (new, hooks/): on connect
  (client change) subscribes to `task.status`, `run.status`,
  `permission.pending` via `events.subscribe`; on disconnect/reconnect
  the subscription is re-established automatically (live-only per ADR —
  a reconnect triggers consumers' own refetch, which the app already
  does via lib/reconnect). Exposes the raw event stream as a stable
  callback-registration surface for consumers.
- **Live attention badges**: `useTaskAttention` consumes
  `run.status`/`permission.pending` events to update attention state
  live (a run finishing while you watch another task badges its task
  immediately; a permission request badges immediately). The existing
  run.list/run.logs refresh becomes the reconnect-resync path only
  (still needed after reconnect since delivery is live-only). The
  run.logs-based unresolved-permission scan can be replaced by
  permission.pending events for the live path, but keep it for the
  post-reconnect resync.
- **Live task status in the sidebar**: task rows show current Status
  driven by `task.status` events (in-memory patch over the fetched
  tree; refetch on reconnect stays).
- **Live diff refresh**: DiffViewerPane refetches `task.diff` when a
  `run.status` terminal event (done/error/stopped) arrives for the
  viewed task — the agent just stopped changing files. (No per-write
  diff events exist; terminal run status is the right trigger.)
- Unsubscribe cleanly on unmount; no listener leaks across task
  switches; two components listening to the same event stream must not
  double-subscribe the daemon (one events.subscribe per connection).

## Test Scenarios

- ws-client unit tests (FakeSocket pattern): a notification message
  `{event:{topic,seq,payload}}` fires the listener and is NOT routed
  into the inflight request machinery; a request-stream event with a
  string `event` field still routes to callStream's onEvent; malformed
  notification payloads don't throw.
- useDaemonEvents: with a fake client, issues `events.subscribe` with
  the three topics once per connection; listener registration/
  unregistration; reconnect path re-subscribes.
- useTaskAttention: run.status(done) event for an unseen task sets the
  badge without any refetch; permission.pending event sets it;
  selecting the task still clears; post-reconnect resync unchanged.
- App-level: task status text in sidebar updates on a task.status
  event; DiffViewerPane refetches task.diff on terminal run.status for
  the same task.
- `bunx tsc -b` clean, `bun run test` passes, `task build` succeeds;
  `.gitkeep` restored if wiped.

## Decisions

(To be filled by the implementer: notification listener API shape,
 event fan-out to multiple hooks, where the single subscription lives,
 how DiffViewerPane subscribes without prop-drilling the stream
 everywhere.)

## Progress

- [ ] ws-client notification support + tests
- [ ] useDaemonEvents hook (subscribe/resubscribe) + tests
- [ ] Live attention badges
- [ ] Live task status in sidebar
- [ ] Live diff refresh on terminal run.status
- [ ] Verification (typecheck/tests/build)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
