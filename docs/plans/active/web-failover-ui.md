# Web + mobile failover UI (ADR-0041 AC17 / AC20, web and mobile halves)

Size: L (6) — scope 2, PRs 2, risk 1, decisions 0, verify 1

Follow-up to [`in-smind-orchestration.md`](../completed/in-smind-orchestration.md) "Remaining (parked)". The daemon already classifies a rate-limited run
(`failureKind`, `failureWindow`, `failureResetAt`), can fail over (`profile.onRateLimit = fallback`, `run.failover` event, `runs.failover_of`) and can hold a
retry (`onRateLimit = wait`, `chat_queue.not_before`); `run.logs` carries `fallbackProfileId`. The UI shows only "Rate limited (5h window) · resets 17:35" + Retry.

## Scope

- **PR 1 (daemon, small):** `run.start` takes a human-only `notBefore` (RFC 3339). Non-empty => the prompt is always queued (even on an idle chat) held until
  that instant (`chat_queue.not_before`); the result is the existing `{queued, queueItemId}`. Refused for run-scoped (identified agent) callers, for a past/invalid
  value and for more than 7 days ahead.
- **PR 2 (web + mobile UI):** (1) rate-limited footer actions "Retry on <fallback profile>" (new chat of the same task, fallback profile's provider/mode/model,
  sends `profileId`) and "Wait until HH:MM" (same chat, `notBefore = resetAt + 30 s`); (2) "continued on <provider> in chat #N" badge on the failed run, link to that chat;
  "continues run …" hint on the successor; (3) mobile `runFooter.ts` equivalents.
- Out of scope: profile editor/CLI (Item B), `mobile/app/new-task.tsx`, `mobile/src/newTask*`, auto-failover itself, any change to the daemon's failover.

## Acceptance criteria

1. AC-D1 `run.start` with a future `notBefore` queues the prompt with `not_before = notBefore` even when the chat is idle, and it starts when the time arrives.
2. AC-D2 `notBefore` is refused (clear error, nothing queued) for a run-scoped caller, an unparsable value, a past value, and > 7 days ahead.
3. AC-D3 A request without `notBefore` behaves exactly as before.
4. AC-W1 A rate-limited latest run whose `fallbackProfileId` names a profile shows "Retry on <name>"; clicking creates a NEW chat on the task and starts the same prompt with the fallback profile's provider/permissionMode/autoAccept/model/thinking and `profileId`.
5. AC-W2 With a reset time in the future, "Wait until HH:MM" shows (weekday prefix on another day); clicking calls `run.start` on the SAME chat with `notBefore = resetAt + 30 s`, the button then reads as queued (no double submit), and the queue row shows the hold.
6. AC-W3 Without a fallback / without a future reset the respective button is absent; plain Retry/Continue still works.
7. AC-W4 A failed run with a successor (`failoverOf == run.id`) shows "continued on <provider> in chat #N"; click opens that chat. The successor shows "continues run …".
8. AC-W5 Light + dark screenshots and transition frames (button -> pending -> queued/new chat; badge appearing).
9. AC-M1 Mobile `describeRunFooter` carries a Wait label/state when a held retry is queued and the "continued on" text, with unit tests.

## Test scenarios (names)

Daemon: `TestRunStart_NotBefore_QueuesOnIdleChat`, `TestRunStart_NotBefore_RefusedForAgentCaller`, `TestRunStart_NotBefore_Invalid` (unparsable/past/too far), `TestRunStart_NoNotBefore_Unchanged`.
Web: `rate-limit.test.ts` (`waitUntilLabel`, `retryNotBefore`), `run-footer-rate-limit.test.tsx` (`shows Retry on fallback`, `hides without fallback`, `Wait until label + click`, `continued-on badge links`, `successor hint`), hook test for `failoverOf` mapping + successor index.
Mobile: `runFooter.test.ts` (`wait state`, `continued on`).

## Decisions

- Route for the hold: `run.start.notBefore` (not a new RPC): smallest surface, reuses `chat_queue.not_before`.
- No ADR: no architecture change (ADR-0041 already decides failover and not_before).

## Progress

- [ ] PR 1 daemon  - [ ] PR 2 web  - [ ] PR 2 mobile

## Validation

(filled in as candidates are accepted)
