# Mobile app — Milestone 3: send a follow-up prompt + approve/deny permissions

## Context

`docs/plans/completed/mobile-app-milestone-2.md` (PR #176, merged) shipped
the first genuinely usable slice: `RelayConnection` (persistent, id-keyed
`call()`/`subscribe()`, and a `CancellablePromise` whose `cancel()` sends
`conn.go`'s `task.cancel` shape for a clean `run.attach` detach),
`TasksScreen` (workspace/space/task list), and `TaskDetailScreen`
(read-only: a task's most recent run's transcript, live-tailed via
`run.attach` if still active, rendered through `runTimeline.ts`).

`runTimeline.ts` already renders `permission_request`/`permission_resolved`
events as plain text lines ("permission requested: ...", "permission
resolved") — Milestone 2 explicitly scoped this as read-only. This
milestone is the next increment `docs/ROADMAP.md`'s Phase 3 bar needs
("assign new work, get notified, approve an agent"): two of those three —
sending a follow-up prompt, and actually approving/denying a live
permission request, not just reading about it after the fact. The third
(push notifications) is a materially different technical domain (see
Decisions) and is explicitly deferred to a later Milestone 4.

## Current state, confirmed by reading the code

- **`run.start`** (`internal/wsapi/handlers.go:756`): `{taskId, provider,
  approvalPolicy?, prompt, thinkingLevel?} -> {runId}`. Starts a run
  without implicitly attaching — unlike `task.prompt`
  (`internal/wsapi/handlers.go:705`), which does `run.start` + attach in
  one call and behaves like an implicit `run.attach` including honoring
  the *request's own* cancellation as a run-stop (see its doc comment at
  handlers.go:775-780). Mobile should use `run.start` then its own
  `call('run.attach', {runId}, {onEvent})` — the exact same attach/detach
  path `TaskDetailScreen`'s Item 3 already established for viewing an
  existing run — rather than adopt `task.prompt`'s different
  cancel-means-stop semantics for a second, inconsistent code path.
- **`run.respondPermission`** (`internal/wsapi/handlers.go:1044`):
  `{runId, requestId, optionId} -> {}`. Resolves one pending permission
  request; no return payload beyond success/error.
- **`permission_request` event/log-entry params**
  (`internal/wsapi/handlers.go:672-677`, `permissionRequestParams`):
  `{requestId: string, summary: string, options: [{id: string, label:
  string, kind: string}]}`.
- **`permission_resolved` event/log-entry params**
  (`internal/wsapi/handlers.go:679-687`, `permissionResolvedParams`):
  `{requestId: string, optionId: string, reason: string}` — `reason` is
  `"human" | "auto_safe" | "timeout"` (see `task-permission-ux.md` Item 2
  for why the web UI already distinguishes these; mobile doesn't need to
  render the distinction for v1, just needs `requestId` to know *which*
  request resolved).
- **No provider is stored on a `Task` itself** — `mobile/src/api.ts`'s
  `Task` interface (line 28) has no `Provider` field; provider only
  exists per-run (`RunSummary.Provider`, api.ts line 41). The only
  candidate default for a follow-up prompt's provider is the task's most
  recent run — already fetched by `TaskDetailScreen` via
  `listRunsForTask` (api.ts line 66) for Milestone 2's Item 3. A task
  with zero prior runs has no provider to default to and no picker UI
  exists anywhere in this codebase's mobile app; see Decisions for the
  scope cut this implies.
- `runTimeline.ts`'s `TimelineLine` (Milestone 2) has no concept of "this
  line is actionable" — it's a flat list of `{key, role, text}`. Item 2
  needs a way to carry a live, unresolved permission request's
  `requestId` + `options` through to the rendered UI, not just its
  `summary` text.

## Decisions

- **Push notifications are their own later milestone (Milestone 4), not
  part of this one.** Real OS push requires Expo push token registration,
  a permission-grant flow, and — the actually new part — teaching the
  relay/daemon to dispatch through Expo's push service, which doesn't
  exist anywhere in this codebase today (no token registry, no outbound
  push integration at all). That's a materially bigger and more novel
  technical domain than "call two RPCs the web UI already calls," and
  this session's own experience orchestrating Milestones 1-2 via Paseo
  found GLM reliably stalls in proportion to a task's size/novelty (see
  this repo's Paseo-agent model-choice memory) — bundling push in here
  would risk exactly that failure mode for no benefit, since it doesn't
  share any code with Items 1-2 anyway. Milestone 3 is scoped to exactly
  the two items that reuse 100% already-proven mobile patterns.
- **Item 1's compose affordance only appears once a task has at least one
  prior run to infer a provider from.** No provider-picker UI is being
  built for this milestone (matching Milestone 2's "smallest real slice"
  bias and Milestone 1's minimum-dependency-footprint bias). A
  zero-runs task keeps Milestone 2's existing empty state ("No runs yet
  for this task. Start one from the daemon or web UI.") — starting a
  task's very first run from mobile is out of scope here, not silently
  broken. `provider.list` exists on the wire if a future milestone wants
  a real picker; not used by this one.
- **Item 1 sends the follow-up via `run.start` + `run.attach`, not
  `task.prompt`.** `task.prompt` ties the request's own connection
  lifetime to the run (closing/cancelling that request stops the run,
  per its doc comment) — the opposite of the detach-not-stop contract
  `TaskDetailScreen`'s `CancellablePromise.cancel()` already established
  for viewing a run. Using `run.start` + a separate `run.attach` keeps
  exactly one attach/detach code path for both "view an existing run" and
  "view the run I just started," rather than introducing a second,
  differently-behaved one.
- **Item 1 omits approvalPolicy and thinkingLevel pickers.** Both fields
  are optional on `run.start`; omitting them preserves the daemon's
  existing defaults, same as `task.prompt`'s behavior when a caller
  doesn't set them. A picker for either is future scope if mobile usage
  shows it's needed, not assumed up front.
- **Item 2 resolves optimistically.** Tapping an option immediately marks
  that permission request as resolved-by-this-tap in the UI (buttons
  disabled, showing the chosen option) rather than waiting for
  `run.respondPermission`'s response or for a matching
  `permission_resolved` event to arrive — matching how a native chat app
  treats a sent action as done in the foreground UI. If the RPC call
  itself rejects (connection dropped mid-tap, daemon returns an error),
  the request's buttons re-enable and an inline error shows, so a real
  failure doesn't get silently swallowed. If the real
  `permission_resolved` event later disagrees (a race where the request
  resolved a different way first — e.g. an auto-safe timeout fired before
  the tap's RPC landed), the event is authoritative and overwrites the
  optimistic state.
- **A request resolved by any path while the screen is open updates
  live.** Whether resolution comes from this device's own tap, a
  concurrent approval from the web UI, or an auto-safe/timeout
  auto-resolution, the same `permission_resolved` event handling path
  updates the UI — there is exactly one source of truth (the event
  stream), not a separate "I just tapped this" local-only state that
  could drift from what the daemon actually recorded.
- Continues Milestone 2's established biases without re-litigating them:
  no navigation library (hand-rolled screen-stack state), read-only
  wherever not explicitly listed as interactive here.

## Acceptance Criteria

### Item 1 — send a follow-up prompt
- `TaskDetailScreen` shows a compose affordance (text input + send
  button) when the task has at least one prior run; a task with zero
  runs keeps Milestone 2's existing empty state instead, with no compose
  box shown.
- Sending a prompt calls `run.start` with the most recent run's
  `Provider` and the typed text, then attaches to the new run via
  `call('run.attach', {runId}, {onEvent})` — the same attach/detach path
  Item 3 of Milestone 2 uses for viewing an existing run.
- The sent prompt's own text appears in the timeline immediately (as a
  `user` line) without waiting for any server round trip; the new run's
  streamed events (chunks, tool calls, done) append live to the same
  timeline in place — no separate reload or screen transition.
- Leaving the screen (navigating back) detaches the new run's attach the
  same way Item 3 already does for an existing run's — no leaked
  handler, the run itself keeps going server-side.
- A `run.start` failure (e.g. connection dropped) surfaces a visible
  error near the compose box, and the typed text is not silently lost —
  the user can retry without retyping.

### Item 2 — approve/deny a live permission request
- A `permission_request` event/log-entry for the run currently on screen,
  while still unresolved (no matching `permission_resolved` for that
  `requestId` has arrived), renders its `summary` plus a tappable button
  per option (using each option's `label`), not just plain text.
- Tapping an option immediately shows that request as resolved with the
  chosen option (optimistic), disables its buttons, and calls
  `run.respondPermission({runId, requestId, optionId})`.
- If `run.respondPermission` rejects, the request's buttons re-enable and
  an inline error shows — the optimistic resolution is rolled back, not
  left in a stuck or misleading state.
- A `permission_resolved` event for a request this screen has open —
  whether triggered by this device's own tap, the web UI, or an
  auto-safe/timeout auto-resolution — updates that request's rendering to
  resolved (and is authoritative over any optimistic local state).
- A `permission_request` that arrives already resolved by the time
  `run.logs`' history is fetched (i.e. both events are in the historical
  log, not live) renders directly in its resolved state — no flash of
  interactive buttons for a request that's already closed.

## Test Scenarios

- **Item 1**: sending a prompt on a task with a prior run calls
  `run.start` with that run's provider and the typed text, then
  `run.attach` on the returned `runId` (assert both calls and their
  params against a fake/harness-backed connection, following
  `runTimeline.test.ts`/`attachLifecycle.test.ts`'s established style of
  testing the logic layer rather than full RN component rendering); the
  user's own message renders immediately, before any server response;
  streamed events for the new run append to the existing timeline, not a
  fresh one; unmounting/navigating back cancels the attach (reuse
  `attachLifecycle.test.ts`'s cancel-detach assertions against the new
  run); a `run.start` rejection surfaces an error and preserves the
  typed input.
- **Item 2**: a `permission_request` with 2+ options renders that many
  buttons with the right labels; tapping one calls
  `run.respondPermission` with the right `{runId, requestId, optionId}`
  and immediately shows it resolved; a forced `run.respondPermission`
  rejection re-enables the buttons and shows an error; a
  `permission_resolved` event for an unresolved request updates it to
  resolved even when no local tap caused it (simulating a concurrent
  auto-safe resolution); a `permission_request`+`permission_resolved`
  pair that both arrive from `run.logs` history at once renders directly
  resolved, never interactive.

## Progress

- [ ] Item 1 — send a follow-up prompt.
- [ ] Item 2 — approve/deny a live permission request.
- [ ] Hand off implementation via Paseo (GLM as primary implementer, per
      the user's standing preference — nudge with a direct, specific
      `deny` + `send_agent_prompt` redirect if it shows the "many turns,
      no commits" stall pattern before escalating to a full Sonnet
      replacement; this session found a direct nudge alone recovered a
      stalling GLM agent mid-Milestone-2 without needing a full swap).
- [ ] Independent verification of agent-reported work before merge.
- [ ] (Not started, future milestone) Milestone 4 — real OS push
      notifications: Expo push token registration + permission grant +
      the new relay/daemon-side dispatch integration through Expo's push
      service. Deliberately out of scope here; see Decisions.

## Validation

To be filled in as each item lands, mapping back to each Acceptance
Criterion with the specific test or manual check that confirmed it.
