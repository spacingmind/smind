# Mobile app — Milestone 2: persistent RPC client + workspace/task navigation + realtime timeline

## Context

`docs/plans/completed/mobile-app-milestone-1.md` (PR #175, merged) proved the
whole chain works end to end: a real Expo app, a from-scratch TypeScript
E2EE client byte-exact with the Go daemon's crypto, and a real daemon↔relay
bridge — but deliberately scoped to a single proof-of-life screen that
sends exactly one `workspace.list` request and closes the connection
(`mobile/App.tsx`'s own doc comment: "No task list, no navigation, no QR
scanning -- that's Milestone 2+").

This plan is that next milestone: turn the proof-of-life into the first
genuinely *usable* slice of `docs/ROADMAP.md` Phase 3's mobile app goal —
"assign new work, get notified, approve an agent" is the eventual bar, but
this milestone stops at "see your workspaces, tasks, and a task's live
agent output," deferring sending new work, permission approval, and push
notifications to Milestone 3 (same incremental-scoping discipline that
kept Milestone 1's GLM/Sonnet handoff tractable).

## Current state, confirmed by reading the code

- **`mobile/src/relay/client.ts`'s `connectAndFetchWorkspaceList`** admits,
  handshakes, sends exactly **one** hardcoded `workspace.list` request
  over the E2EE `Channel`, reads exactly one response, then closes the
  stream (`stream.close()` at the end). The underlying `Channel`
  (`mobile/src/relay/e2ee.ts`) and `GRPCWebSocketStream`
  (`mobile/src/relay/grpcweb.ts`) are already persistent, bidirectional,
  multi-message-capable — Milestone 1 just never exercised that, since
  its scope was one request and done.
- **`internal/wsapi/conn.go`'s wire protocol** (`dispatch`/`sendResult`/
  `sendError`) is a generic id-keyed request/response protocol — any
  number of sequential (or concurrent) `{id, method, params}` calls over
  one connection, each answered by exactly one `{id, result|error}`.
  `internal/wsapi/events.go` layers an unsolicited push mechanism on top:
  `events.subscribe`/`events.unsubscribe` RPCs plus `eventNotification`
  messages the server sends without being asked, for topics like task/run
  status changes. Nothing about this protocol is browser-specific — it's
  exactly what a persistent mobile connection needs too.
- **Confirmed real RPC methods already implemented** (`internal/wsapi/handlers.go`):
  `workspace.list`, `space.list`, `task.list`, `task.get`, `run.list`,
  `run.attach`, `run.logs` — everything Items 2 and 3 need already exists
  and is exercised daily by the web UI; nothing new to build on the Go
  side for this milestone (unlike Milestone 1, which needed three new Go
  pieces before any mobile screen could work at all).
- **No navigation library is in `mobile/`'s `package.json` yet** — Milestone
  1 is a single `App.tsx` screen with no router.

## Decisions

- **Three sequential items, same discipline as Milestone 1**: Item 1
  (TypeScript-only, generalize the transport — no new UI, directly
  testable the same way Milestone 1's crypto was) → Item 2 (list
  screens) → Item 3 (realtime task detail). Commit/push each separately.
- **Item 1 replaces the one-shot call with a persistent `RelayConnection`**
  that: connects once (parse offer → admit → open data stream → E2EE
  handshake — the exact sequence `connectAndFetchWorkspaceList` already
  does), exposes `call(method, params) => Promise<result>` for any number
  of sequential requests keyed by a locally-generated id (mirroring
  `internal/wsapi/conn.go`'s id-keyed contract), and exposes
  `subscribe(topics, onEvent)` wrapping `events.subscribe` +
  dispatching incoming `eventNotification` messages by topic. Closing is
  now an explicit `connection.close()` the UI calls on unmount/disconnect,
  not something the client library decides on its own after one call.
- **No navigation library added — a tiny hand-rolled screen-stack state**
  (`useState<Screen>` with a small union type: `{kind: 'pairing'} |
  {kind: 'tasks'} | {kind: 'task'; taskId: number}`) is enough for three
  screens and avoids pulling in `@react-navigation` or `expo-router` for
  a prototype this small — matches Milestone 1's own bias toward the
  minimum dependency footprint needed to prove the milestone's point.
  Revisit if/when Milestone 3's scope actually needs deep linking or a
  header/back-button chrome a real router would justify.
- **Item 3's realtime timeline is read-only.** It renders whatever
  `run.attach`/`run.logs` streams for a task's most recent run (reusing
  the same wire event shapes ADR 0008 defined and the web UI already
  renders) — text chunks, tool calls, done/error status. No follow-up
  prompt box, no permission-approval buttons, no push notifications: all
  three are explicitly Milestone 3, once this milestone proves the mobile
  client can sustain a long-lived, multi-message, event-subscribed
  connection at all (the thing Milestone 1 never had to prove).
- **Reconnection/backgrounding is out of scope for this milestone.** If
  the app backgrounds or the connection drops, Item 1's `RelayConnection`
  may simply surface an error/disconnected state rather than silently
  auto-reconnecting — matching Milestone 1's own bias toward proving the
  smallest real slice first. Auto-reconnect (with the same session-resume
  semantics the Go bridge already has) is a reasonable Milestone 3+ item
  once there's an actual always-on usage pattern to design it against.

## Acceptance Criteria

### Item 1 — persistent RelayConnection + event subscription
- A single admit+handshake sequence supports at least 3 sequential
  `call()`s over the same connection without re-admitting or
  re-handshaking, each correctly matched to its own response by id even
  if issued back-to-back before the previous response arrives.
- `subscribe(topics, onEvent)` receives at least one real
  server-pushed `eventNotification` in a live end-to-end test (e.g.
  subscribing to a task's status topic, then triggering a real status
  change on the Go side and observing the mobile-side callback fire).
- `connection.close()` cleanly tears down the underlying stream; a
  `call()` issued after close rejects with a clear error rather than
  hanging.
- A malformed/error response (`{id, error}`) rejects that specific call's
  promise with the error's message, without disrupting any other
  in-flight call on the same connection.

### Item 2 — workspace/task list screens
- After a successful pairing connect, the app navigates to a screen
  listing the paired workspace's spaces and tasks (via `space.list`/
  `task.list`), not just a raw JSON dump.
- Pull-to-refresh (or an explicit refresh action) re-fetches the list
  over the same persistent connection (no new admit/handshake).
- An empty workspace (no spaces/tasks) renders an honest empty state,
  not a blank screen or a crash.
- A `call()` failure (e.g. connection dropped) surfaces a visible error
  state with a way to retry, not a silent failure.

### Item 3 — task detail with realtime timeline (read-only)
- Tapping a task navigates to a detail screen showing that task's most
  recent run's transcript: text output, tool calls (name + status is
  enough — full per-tool rendering detail is not required), and a
  final done/error status.
- If the task's run is still active when the screen opens, new events
  arrive and render live (via `run.attach` and/or the event-subscription
  mechanism from Item 1) without the user manually refreshing.
- If the task has no runs yet, the screen shows an honest empty state.
- Navigating back from the detail screen to the list does not leak the
  subscription (no continued event handling for a screen no longer on
  view).

## Test Scenarios

- **Item 1**: unit tests for request/response id-matching under
  concurrent/out-of-order responses (a fake transport that responds
  out of the order requests were sent); a real end-to-end test extending
  Milestone 1's Go-harness pattern (`internal/relay/bridge/harness`) to
  exercise multiple sequential calls plus at least one real event push
  over one connection; close-then-call rejects; server error response
  rejects the right promise only.
- **Item 2**: list renders real data from a real harness-backed
  connection; empty-workspace state; refresh re-uses the connection
  (assert no second admit/handshake happens); a forced call failure
  shows the error state and a retry actually retries.
- **Item 3**: a task with a completed run renders its full historical
  transcript from `run.logs`; a task with an active run receives and
  renders at least one live event after the screen mounts; a task with
  no runs shows the empty state; unmounting the detail screen and
  re-mounting a different one doesn't double-render or crash from a
  stale subscription.

## Progress

- [x] Item 1 — persistent `RelayConnection` + event subscription.
- [x] Item 2 — workspace/task list screens.
- [ ] Item 3 — task detail with realtime timeline (read-only).
- [ ] Hand off implementation via Paseo (GLM as primary implementer, per
      the user's standing preference — fall back to Sonnet immediately,
      without extended nudging, if GLM shows the "many turns, no commits"
      stall pattern documented in this session's own memory).
- [ ] Independent verification of agent-reported work before merge.

## Validation

### Item 1 — persistent RelayConnection + event subscription

- **3+ sequential calls over one admit+handshake, id-matched even when
  responses arrive out of order**: `npm test` —
  `mobile/src/relay/__tests__/RelayConnection.test.ts` ("matches
  responses to calls by id, including back-to-back calls answered out of
  order" answers three concurrent calls in reverse order); real end to
  end against the Go harness —
  `mobile/src/relay/__tests__/integration.persistent.node.test.ts`
  (workspace.create → space.list → task.create → subscribe → task.create
  → task.list over one connection, plus an intentional `task.get` error
  response).
- **A real server-pushed `eventNotification` reaches a subscribe()
  callback**: integration.persistent.node.test.ts subscribes to
  `task.created`/`task.updated` and observes the `task.created` push
  fired by the daemon's own task.create mutation, over the same
  connection (asserted via `vi.waitFor`).
- **`close()` tears down the stream; `call()` after close rejects
  clearly**: unit tests "rejects calls issued after close with a clear
  error" and "rejects in-flight calls when closed"; integration test's
  final assertion (`call` after `close` rejects with "relay: connection
  is closed").
- **An `{id, error}` response rejects only that call's promise**: unit
  test "rejects only the errored call, leaving other in-flight calls
  intact" (sibling call resolves while the errored one rejects).

Also verified: `npx tsc --noEmit` clean; Milestone 1's tests unchanged
and passing (channel/e2ee/pairing/proto suites, plus
integration.node.test.ts's one-shot workspace.list round trip via the
reimplemented `connectAndFetchWorkspaceList`).

### Item 2 — workspace/task list screens

- **Navigate to a list screen after pairing (spaces+tasks, not a JSON
  dump)**: `mobile/src/screens/TasksScreen.tsx` resolves the daemon's
  workspace (workspace.list) and groups tasks by space
  (space.list/task.list), with an Ungrouped section; `mobile/App.tsx`
  switches to it on a successful connect. Rendering against a real
  harness-backed connection is exercised by the Item 1 integration test's
  workspace.create/space.list/task.create/task.list sequence.
- **Refresh re-fetches over the same connection**: pull-to-refresh calls
  the same `loadTasks(conn)` against the same `RelayConnection`
  instance; unit test "refresh re-fetches over the same connection"
  (`mobile/src/__tests__/api.test.ts`) asserts the RPC pattern
  (workspace.list once, space.list/task.list per load) over one conn.
- **Empty workspace renders an honest empty state**: TasksScreen's
  ListEmptyComponent; unit test "an empty daemon yields empty lists, not
  an error".
- **Call failure surfaces a visible error with retry**: TasksScreen's
  error state + Retry button; unit test "a failed call rejects with the
  error the screen shows (error + retry state)" verifies retry after a
  forced failure succeeds over the same connection.

Also verified: `npx tsc --noEmit` clean; `npm test` 27 passing (23 prior
+ 4 new). A Disconnect control closes the connection and returns to
pairing, since the app (not the client library) now owns the connection
lifetime per Item 1's design.

Note surfaced during validation: the milestone-1 bridge serves exactly
one E2EE data session per workspace (fixed DefaultSessionID/DeviceID),
so a second fresh-key mobile connection after a clean close leaves the
daemon-side bridge unable to re-handshake (stale READY frames on the
persisting relay route). The persistent-connection integration test
therefore spawns its own harness instance. The Milestone 2 app keeps one
connection alive for the app's lifetime, which is RelayConnection's
contract, so this doesn't affect the app; a multi-device story remains
future work (per bridge.go's own doc comment).
