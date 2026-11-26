# Continue on resumable failed runs (J1)

Size: M (4) — scope 2, PRs 0, risk 1, decisions 1, verify 1 (daemon + web + mobile + CLI; no ADR, ADR-0027 section 4 note).

## Problem

After #619 a claude CLI that dies without a result is `failure_kind=cli_exited`
and its error text says "Send a follow-up prompt to resume the session", but
no surface offered Continue on an `error` run: the footer gated it on the
`needs-continue` disposition, which existed only for done-at-cap and
`daemon_restart` runs.

## Decision

Resumability lives in the **daemon's derived disposition** (ADR-0027 section 4:
derived, never stored): `runs.DispositionOf` takes the failure kind, the reset
time, whether the run's chat has a stored agent session, and the clock. One
source of truth for web, mobile, CLI and MCP, because the session is a daemon
fact no client can see (`sessionId` is not on the wire) and the rate-limit rule
depends on the clock at read time. The wire is unchanged (the existing additive
`disposition` field); status stays `error`.

The session fact is computed once at the run's terminal state (and at
rehydrate, and for evicted rows at read) and cached on the run, so the hot
status path never queries the store.

Continue sends through the existing path (web `resubmit`, mobile
`continueRun`): the run's own provider/model/mode, `whenBusy: queue` (ADR-0024),
nothing pinned. Prompt for an error run (one line):
`Continue from where you left off (the previous run ended unexpectedly: <label>).`
The needs-continue prompt for capped runs stays `Continue`.

## Table: failure -> Continue

| Run ended as | Continue | Why |
|---|---|---|
| done + `max_turn_requests` / `max_tokens` / `budget_exceeded` | yes (as before) | cut off at a cap; the session resumes it |
| stopped/interrupted + `daemon_restart` | yes (as before) | the daemon's restart cut it off and the chat has a session |
| stopped/interrupted + `daemon_restart_fresh` | no | no stored session |
| stopped by the user (`cancelled`) | no | the human's own choice |
| error + `cli_exited`, chat has a session | **yes** | the CLI died without a result; its session was stored at init |
| error + `cli_exited`, no session | no | nothing to resume |
| error + `rate_limited`, reset **in the future** | no | the existing wait/failover/Retry machinery owns it; the footer shows the reset text |
| error + `rate_limited`, reset **unknown** | no | the daemon never guesses a time; Retry/wait stay |
| error + `rate_limited`, reset **passed**, session | **yes** | the quota is back; resume the session |
| error + `rate_limited`, reset passed, no session | no | nothing to resume |
| error + permission denied / auto-deny timeout (no kind) | no | resuming repeats the denial |
| error + auth / invalid model / provider / config (any other kind) | no | resuming repeats the failure |
| error + `refused`, admission refusal (budget), unknown/empty kind | no | same |

## Surfaces

- **Web**: the footer's single action slot is Continue when the disposition is
  `needs-continue` on an error run, else Retry as before; the error prompt comes
  from `continuePrompt`. `task-tree-actions` keeps an error node finished even
  with the disposition (aligned with `isFinishedNode`).
- **Mobile**: already had a Continue banner (done-at-cap, and rate_limited at
  all times). It now follows the same rule: a rate-limited run without the
  disposition shows the sentence only, `cli_exited` with the disposition gets the
  banner. No Continue for `daemon_restart` existed on mobile (the footer is
  `done` only) and none was added.
- **CLI**: `task runs` STATUS shows `(needs-continue)` on an error run that has it.
- **MCP**: `task_wait` / `task_status` / child events carry the disposition on
  error runs; `task_wait` adds a resume message. No new write path: resuming is
  the existing `task_send`, under its existing subtree admission.

## Acceptance criteria and test scenarios

1. `TestDispositionOf` (Go): every table row. Mutations: every error resumable
   => the no-kind/auth/refused rows fail; drop the session requirement => the
   no-session rows fail.
2. `TestFinishCLIExitedDispositionNeedsSession`: live status, `TaskRunFacts`,
   a rehydrated registry and `summaryFromRow` agree, with and without a session.
3. Web `run-footer-model.test.ts` / `task-detail.test.tsx`: Continue for
   error+disposition only; one click sends the error prompt once, queue, the
   run's own model, none pinned (mutation: pin a model => the arguments tests
   fail); rate_limited before reset has Retry and the reset text, after reset
   with the disposition has Continue.
4. Mobile `runFooter.test.ts` / `followUpPrompt.test.ts`: same rule and prompt.
5. Frames (real app, isolated daemon, fake agent): evidence in
   `/tmp/j1-frames/out`.

## Progress / Validation

See the PR body. Known limits: a rate-limited footer that is open when the
reset passes gains Continue on the next refresh (the disposition is derived at
read time, no event fires at the reset); Continue replaces Retry in the single
action slot; the old footer's chips shift left when its Continue is superseded
by the new run (same as any superseded action button).
