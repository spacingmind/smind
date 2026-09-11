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

- **Notification listener API shape**: `WsClient.onNotification(fn)`
  returns an unregister function (same shape as React cleanup).
  `DaemonNotification = {topic, seq, payload}`. `handleMessage`
  detects the notification shape (no id + object-valued `event` with a
  string topic) *before* the id-routed path via a module-level
  `parseNotification` that returns null for anything malformed — a bad
  push can never throw off the RPC engine. Each listener is
  try/catch-isolated (mirrors `failAll`'s closeWaiters isolation).
- **Single subscription lives in `useDaemonEvents`** (App.tsx mounts it
  once): one `events.subscribe` with all three topics per connection,
  issued in a client-keyed effect; the fire-and-forget call's failure is
  swallowed (no live events ≠ an error state — refetch remains the
  source of truth). Consumers get a stable `{subscribe(topic, fn)}`
  surface filtered by topic; topic listeners are also try/catch
  isolated. Client change tears down the old `onNotification` and
  re-subscribes on the new client.
- **Event fan-out to multiple hooks**: `useDaemonEvents` holds a
  ref-based `Map<topic, Set<listener>>`; consumers register/unregister
  locally, never touching the daemon subscription. The hook returns a
  new surface object per client (memoized on client), which doubles as
  the effect key consumers use — listener registration naturally
  follows the connection lifecycle.
- **No prop-drilling of the stream**: `events` is an optional prop on
  AppSidebar and DiffViewerPane (`DaemonEvents | null | undefined`),
  threaded from App.tsx. Optional so existing tests/mounts without it
  render exactly as before; null-ness doubles as "no live path, rely on
  refetch".
- **useTaskAttention live path**: run.status notifications patch a
  ref-held `RunLike[]` (`Pick<RunSummary, "ID"|"TaskID"|"Status">`) in
  place, then `recompute()` derives badges from it — the same rederive
  function the resync path uses, so live and resync can't diverge in
  shape. A terminal run for the *selected* task is marked seen directly
  (the user is watching it); for any other task it stays unseen and
  badges. `status: "running"` un-terminalizes (clears seen marker +
  stale permission badge). permission.pending adds the task to the
  pending set directly. The run.list/run.logs pass is unchanged and
  remains the post-reconnect resync (live-only delivery).
- **Sidebar status overrides**: `useStatusOverrides` inside
  app-sidebar.tsx keeps `Map<taskId, string>`, cleared on client change
  so a reconnect never pairs stale overrides with the fresh tree;
  dedupes no-op status repeats. Applied at render
  (`overrides.get(id) ?? task.Status`).
- **DiffViewerPane**: subscribes to run.status on the `events` surface
  in an effect keyed on `[events, fetchDiff, task.ID]` — `fetchDiff`'s
  own client/task.ID deps make task switches re-register cleanly.
  Terminal statuses only (done/error/stopped), same-task only.

## Progress

- [x] ws-client notification support + tests
- [x] useDaemonEvents hook (subscribe/resubscribe) + tests
- [x] Live attention badges
- [x] Live task status in sidebar
- [x] Live diff refresh on terminal run.status
- [x] Verification (typecheck/tests/build)

## Validation

- **ws-client**: 4 new tests — notification fires listener while a call
  stays pending (and the pending call still resolves normally); id +
  string-event still routes to callStream onEvent with no notification
  fan-out; malformed `{event:{seq}}` (no topic) ignored without
  throwing; unregister stops delivery. (ws-client.test.ts: 14 total.)
- **useDaemonEvents**: 5 tests — exactly one events.subscribe with the
  three topics per connection regardless of consumer count and
  re-renders; topic-filtered delivery + unregistration stops delivery;
  throwing listener isolated; client change re-subscribes once per
  connection and drops old-connection listeners; failed subscribe never
  throws into render.
- **useTaskAttention (live path)**: covered at App level — run.status
  done for an unseen task badges with `socket.sent.length` unchanged
  (no refetch); permission.pending badges live; pre-existing resync
  tests (errored run via run.list, unresolved permission via run.logs,
  selection clears) still pass unchanged.
- **Sidebar live status**: App test — task.status event flips the row's
  status text from "active" to "done" with no task.list refetch.
- **DiffViewerPane**: 2 tests — terminal run.status for the viewed task
  refetches (non-terminal and other-task events don't), and the new
  diff renders.
- **Toolchain**: `bunx tsc -b` clean; `bun run test` 94/94 across 10
  files; `task test` (Go) ok; `task lint` clean; `task build` succeeds
  (UI + Go binary); `internal/server/dist/.gitkeep` present after
  build.
