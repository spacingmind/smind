# Stalled-tree alert

Size: L (7) — scope 2, PRs 1, risk 1, decisions 1, verify 2.

Problem: a task tree can sit silent for hours (a Lead ended a turn on an
intention, a seat waits on nothing, a wake was lost) and nobody is told.
ADR-0041's plan parked "M2 push for a stuck tree"; this item delivers it.
Notification only: no auto-wake, no auto-archive, no recovery.

## Acceptance Criteria

Definitions (the detector's contract):

- **Seat**: a live (not archived, `kind='task'`) task that has live children,
  or whose latest run's profile has `OrchestrationTools` enabled (a
  Lead/Supervisor task).
- **Subtree activity time `A(seat)`**: the max, over the seat and every live
  descendant, of: `tasks.created_at`; each run's `started_at` and
  `finished_at`; each chat-queue item's `created_at`/`updated_at`, and its
  `not_before` while status is `queued`; and, when the seat's own latest run
  failed with `failure_kind='rate_limited'` under a profile with
  `on_rate_limit='wait'`, that run's `failure_reset_at`. `A` may be in the
  future (a held retry or a known reset) — then the seat is not stalled yet.
- **Busy**: any run in the subtree with `status='running'` (this includes a run
  waiting on a permission). A tree `budget_exceeded` refusal creates no run,
  so it is not busy and does not move `A`.
- **Done tree**: every live node of the seat's root tree is finished under the
  existing `isFinishedNode` rule (latest run terminal, not `needs-continue`)
  and has no queued prompt. A done tree never stalls.
- **Stalled** = seat, tree not done, not busy, `now - A(seat) >= N`, where
  `N = orchestration.stallAlertMinutes`. `stalledSince = A(seat)`.
- **Episode** = `(seat task id, stalledSince)`. Any run/queue activity in the
  subtree moves `A`, which ends the episode; a later stall has a new
  `stalledSince` and may alert again.

Criteria:

1. AC1 — an idle seat past N raises exactly one notification per episode.
2. AC2 — a daemon restart does not re-alert an already alerted episode (the
   episode marker is persisted).
3. AC3 — busy subtrees, queued items younger than N, rate-limited `wait`
   seats with a future reset, done trees, archived seats and system tasks
   never alert; `stallAlertMinutes: 0` turns the detector off.
4. AC4 — config key `orchestration.stallAlertMinutes` (default 15, 0 = off,
   negative rejected with a clear error), documented beside the other
   `orchestration.*` keys.
5. AC5 — delivery reuses the existing paths: ADR-0040 sealed push (new kind
   `stalled`, no plaintext title/body leaves the daemon) and a bus event the
   web client turns into the existing desktop/browser notification. Text:
   "Tree stalled: <seat title> has been idle for N min", with the task id
   deep-link the other notifications use.
6. AC6 — `task.tree` nodes carry `stalled` (bool) and `stalledSince` (RFC 3339
   UTC, omitted when not stalled); `smind task tree` prints a `stalled` word
   on the node line; MCP `task_tree` carries the same JSON.
7. AC7 — web shows a compact "stalled" marker on the tree node (shared
   status chip/icon registry `lib/icons.ts`, light + dark, tooltip "No run
   for N min") and nothing when not stalled.
8. AC8 — a tick costs O(seats): one store snapshot, no GitHub or provider
   calls, no per-node query.
9. AC9 — mobile read-only tree (#594) shows the marker only if the change is
   <= ~40 lines plus a test; otherwise it is a listed follow-up.

## Test Scenarios

Detector (`internal/stall`, fake clock, injected tick, no sleeps):

- `TestIdleSeatPastThresholdAlertsOnce`
- `TestSecondTickSameEpisodeDoesNotReAlert`
- `TestRunStartingInSubtreeClearsEpisodeAndLaterStallAlertsAgain`
- `TestRunningChildRunSuppresses`
- `TestQueuedItemSuppresses` (item younger than N; an item older than N does
  not suppress)
- `TestRateLimitedWaitSeatFutureResetDoesNotAlert`
- `TestRateLimitedWaitSeatAlertsNMinutesAfterReset`
- `TestStallAlertMinutesZeroNeverAlerts`
- `TestRestartDoesNotReAlertAlertedEpisode` (new detector over the same DB)
- `TestDoneTreeNeverAlerts`, `TestArchivedSeatNeverAlerts`,
  `TestSystemTaskNeverAlerts`
- `TestNestedSeatsAlertOnlyTopmost` (nested stalled seats: one alert for the
  topmost; every stalled seat keeps the marker)
- `TestBudgetExceededRefusalAloneIsNotBusy`
- `TestTickIsOSeatsNoExternalCalls` (count-asserting fakes: store snapshot
  queries counted, GitHub/provider fakes called 0 times)

Config (`internal/config`):

- `TestStallAlertMinutesDefaultAndZero`, `TestStallAlertMinutesNegativeRejected`

Delivery (`internal/push`, `internal/wsapi`):

- `TestNotifyStalledSealedNoPlaintext` (assert the bytes handed to the Sender
  contain neither title nor body text; opening with the device key yields
  "Tree stalled: … has been idle for N min" and the task id)
- `TestStalledDedupeByEpisodeKey`
- `TestStalledBusEventEmitted` (topic `task.stalled`, payload {taskId, title,
  minutes, since}; in `knownTopics`)
- `TestPushInputStalled` (bus event -> push.Input kind `stalled`)

Surfaces:

- `TestTaskTreeCarriesStalledAndSince`, `TestTaskTreeOmitsStalledWhenNot`
- CLI: `TestTaskTreeLinePrintsStalled`
- web (vitest): `renders the stalled marker with tooltip`, `omits the marker
  when not stalled`, `fires a notification on task.stalled`

## Decisions

Supervisor defaults (2026-10-25; the Human may veto):

- D1 N = 15 min, `orchestration.stallAlertMinutes`, 0 = off, negative rejected.
- D2 Notification only. No auto-wake, no auto-archive.
- D3 The stall clears when any run starts in the subtree; a later stall is a
  new episode and may alert again.
- D4 One notification per episode, persisted. Lead decision: the episode id is
  derived deterministically as `stalledSince` (the subtree activity time, a
  DB fact). The only persisted state is ONE new column,
  `tasks.stall_alerted_since TIMESTAMP` (additive migration `stall_alert`):
  alert iff `stall_alerted_since IS NOT value of stalledSince`; set it when
  the alert is dispatched. A restart rebuilds the same `stalledSince`, so it
  does not re-alert; new activity changes `stalledSince`, so it can. No clear
  step is needed.
- D5 A `wait` seat with a known future reset is "rate limited", not stalled;
  after the reset, N minutes count from the reset (it is part of `A`).
- D6 A done tree never stalls; archived seats never stall.

Lead decisions:

- L1 Done tree reuses `isFinishedNode` + "no queued prompt" over every live
  node, seat included. Consequence: a Lead that ended its last turn with every
  child finished is "done", not stalled (finished trees awaiting archive are
  routine; alerting on them would be noise). A node that is `idle` (never
  ran or its run state is unknown) or `needs-continue`, or has a queued prompt,
  keeps the tree not done; an errored or stopped node counts as finished (the
  existing rule). **Human may veto**: the
  alternative is alerting whenever the root seat's own latest run ended and
  nothing else is going on.
- L2 Reuse: `push.Notifier.Notify` (dedupe by device+key, sealing, registry,
  Approved, queue) with a new `KindStalled` added to `AllKinds`; the key is
  `"stall:<taskId>:<stalledSince unix>"`. Stalled is not a run kind, so the
  done/error rate limit does not apply; the per-episode dedupe does.
  Devices registered before this change hold an explicit kinds list without
  `stalled` and must re-register (no installed base; noted in the PR).
  Desktop: a new bus topic `task.stalled` (the existing run-status path is
  edge-triggered in the web client by attention reasons; the new topic feeds
  the same Notification code with reason `stalled`).
- L5 Nested stalled seats (a Lead under a Supervisor, same silent subtree)
  alert once, for the topmost; every stalled seat still shows the marker.
- L3 Tick = one goroutine in the daemon (period 1 min, injectable), disabled
  when N = 0. The same pure core (`stall.Detect`) feeds `task.tree` so the
  marker and the alert cannot disagree.
- L4 Schema change: one column, as authorised. No new table.

## Progress

- [x] Plan written (this file), reuse inventory done.
- [ ] Peer 1: backend (store column + snapshot query, `internal/stall`,
      config, notifier kind, bus topic + tap, `task.tree` fields, CLI word,
      MCP passthrough).
- [ ] Peer 2: web marker + `task.stalled` notification (+ mobile if small).
- [ ] Review, mutations, live check, frames, PR.

### Reuse inventory

- Push: `internal/push/notifier.go` (`Notify`, `admit` dedupe, `Seal`),
  `internal/wsapi/push.go` (`pushInput` bus tap, `AllKinds` kind
  validation).
- Desktop/browser: `web/packages/ui/src/hooks/use-attention-notifications.ts`
  (+ `use-task-attention.ts`), the bus in `internal/wsapi/events.go`.
- Tree: `internal/tasktree/tree.go` (`Node`), `internal/wsapi/task_tree.go`
  (`buildTaskTree`), `isFinishedNode` in `task_tree_actions.go`,
  `Store.SubtreeBusy` (child_event.go) as the busy/queued SQL pattern.
- Rate-limit wait: `internal/runs/failover.go` (`enqueueRateLimitRetry` holds
  an item with `not_before = resetAt + grace`).
- Config: `internal/config/config.go` `OrchestrationConfig`.

## Validation

(filled in at PR time: each AC mapped to the test or manual check that
confirmed it, mutation results, live check, frame paths, what was not
verified.)
