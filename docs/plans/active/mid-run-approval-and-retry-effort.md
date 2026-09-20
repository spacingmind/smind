# Mid-run approval-policy switching + "retry with higher effort"

## Context

Follow-up from dogfooding PR #171 (task-move UI, per-provider `full-access`
approval tier, Claude/GLM thinking-level controls — merged to `develop`).
While researching comparable tools' UX (Claude Code, Cursor, Windsurf, Cline,
Zed, Codex CLI, Copilot — via pplx, 2026-09-20) two concrete gaps stood out
against smind's current design:

1. Every one of those tools lets a user change permission/approval mode
   **while a task is actively running**, not just at submission time.
   smind's `approvalPolicy` is chosen once in the composer and is otherwise
   immutable for the run's lifetime.
2. Several tools offer a **one-click "retry at higher effort"** affordance
   when a task fails or under-delivers, rather than making the user manually
   reopen the composer, reselect a thinking level, and retype the prompt.

## Current state, confirmed by reading the code

- **`internal/runs/registry.go`**: each live task run is a `*run` struct
  (`registry.go:244`) with a `sync.Mutex` (`mu`) already guarding several
  mutable fields (status, etc.) mid-run — this is the established pattern
  for "state a live run needs to change safely while other goroutines read
  it." `approvalPolicy` (`registry.go:259-262`) and `thinkingLevel`
  (`registry.go:264-270`) are currently **not** under that mutex — both are
  explicitly documented "set at Start and immutable thereafter."
- **`runPermissionDecider.Decide`** (`registry.go:451+`) is the only reader
  of `d.r.approvalPolicy`, checked per pending permission request
  (`registry.go:466`, the `ApprovalPolicyAutoSafe`+`AllowlistedCommand`
  branch). Because every request already round-trips through this one
  method, making `approvalPolicy` mutable-and-mutex-guarded here is a
  minimal, well-contained change — no changes needed in
  `internal/taskrunner`'s adapters themselves for the `manual`⟷`auto-safe`
  case.
- **`full-access` is architecturally different and out of scope for live
  switching in this pass**: per PR #171, `full-access` skips installing
  `runPermissionDecider` (or any decider) entirely and instead configures
  the provider client at construction time with its own native
  auto-approve mechanism (Claude's `bypassPermissions` CLI mode, Codex's
  `AutoApprovePolicy{}`, ACP's `AutoApprovePolicy{}` — see
  `internal/taskrunner/runner.go:256,417,586`). Switching into or out of
  `full-access` mid-run would require tearing down and respawning the
  provider's subprocess/session, which is a much larger change (loses
  in-flight turn state) and not what either of this plan's two items
  actually need. **Scope for this plan: live switching is
  `manual` ⟷ `auto-safe` only.** `full-access` remains composer-only,
  chosen before a run starts, same as today.
- **`config_options.go`'s `run.listConfigOptions`/`run.setConfigOption`
  RPC pair** (already shipped, GLM/Kimi thinking level) is the closest
  existing precedent for "a live control that mutates state on an
  in-flight run via a dedicated RPC" — same shape this plan's Item A needs
  (a `run.setApprovalPolicy` RPC alongside it), just for `approvalPolicy`
  instead of ACP config options.
- **Composer's approval-policy selector** (added in PR #171, wherever the
  provider/approval-policy dropdowns live in `web/packages/ui/src`)
  currently only applies to the *next* submission. There is no live control
  in the task/chat view for an in-flight run.
- **Claude's thinking-level composer selector** (PR #171): `Off` / `Standard`
  / `Extended`, threaded through `task.prompt`'s `thinkingLevel` field into
  `runClaudeNative`'s `claudecode.Option`. A failed or unsatisfying run today
  requires the user to manually reopen the composer, reselect a higher tier,
  and resend the same prompt text.
- **Run failure state**: (needs confirming during implementation, not yet
  read in this pass) — the task/chat view must already render some
  "failed"/"errored" status for a run (`Status` field referenced in
  `registry.go:400` — `StatusRunning` etc. imply a `Status` enum with a
  failure value). Item B hooks into whatever that existing failure
  affordance/rendering is.

## Decisions

- **Live approval switching is limited to `manual` ⟷ `auto-safe`.**
  `full-access` requires a provider-side respawn to enter or leave, which is
  a different (and much larger) problem than "let the decider look at an
  updated value" — explicitly deferred, not silently dropped: documented
  here as a known limitation, with the composer remaining the only way to
  start (or effectively "end", by starting a new run) a `full-access`
  session.
- **New RPC, not overloading `task.prompt`**: `run.setApprovalPolicy(runId,
  policy)` (mirroring `run.setConfigOption`'s shape), rejecting
  `full-access` as a target value with a clear error (not a silent no-op or
  a crash) rather than trying to support it.
- **"Retry with higher effort" is Claude-only and one tier at a time.**
  GLM/Kimi's thinking level is already live-adjustable mid-session via the
  shipped `run.listConfigOptions`/`run.setConfigOption` control, so a
  "retry" affordance would be redundant there — the existing live control
  already covers it. Codex has no reasoning-effort lever at all (per PR
  #171's research). No auto-escalation loop (e.g. retrying repeatedly until
  it succeeds) — one click, one tier up, capped at `Extended`; a failed
  `Extended` run gets no further "retry higher" option (nothing higher to
  offer).
- **Retry resubmits the same prompt text as a new run**, not a resume of the
  failed one — matches how every other retry/resend affordance in smind
  works today (no evidence of a "resume this exact failed turn" mechanism
  to hook into instead).

## Acceptance Criteria

### Item A — mid-run approval-policy switching (manual ⟷ auto-safe)
- While a task's run is active (`StatusRunning`) and its current
  `approvalPolicy` is `manual` or `auto-safe`, the task/chat view shows a
  live control (e.g. next to wherever run status/thinking-level-for-GLM
  controls already render) letting the user switch between the two.
- Switching takes effect for the *next* pending or future permission
  request in that run — a request already waiting on a human decision when
  the switch happens is not retroactively changed (that request keeps
  whatever behavior was already in flight; only requests raised after the
  switch see the new policy). Document this ordering explicitly, don't
  leave it implicit.
- Attempting to set `full-access` via this RPC (or selecting it from a
  control that shouldn't offer it) is rejected with a clear error, not
  silently ignored or crashing the run.
- Switching is a no-op (or hidden control) once the run has finished
  (`StatusCompleted`/`StatusFailed`/etc.) — no dangling mutation of a dead
  run's state.
- Existing `manual`/`auto-safe`/`full-access` *pre-run* selection behavior
  (composer) is unchanged — this item only adds a *mid-run* control, it
  doesn't change how a run starts.

### Item B — "Retry with higher effort" (Claude only)
- When a Claude-native run ends in a failure/error status AND its
  `thinkingLevel` was `Off` or `Standard` (i.e. there's a higher tier left
  to try), the task/chat view shows a "Retry with higher effort" affordance
  near the failure state.
- Clicking it submits a new run for the same task with the same prompt text
  and the next tier up (`Off`→`Standard`, `Standard`→`Extended`), same
  provider and approval policy as the failed run.
- A failed run already at `Extended`, or any non-Claude provider's failed
  run, does not show this affordance (nothing higher to offer / not
  applicable).
- A successful run never shows this affordance regardless of thinking level.

## Test Scenarios

- **Item A**: start a run with `manual`, verify a pending permission request
  behaves per `manual`; switch to `auto-safe` mid-run, verify a
  *subsequent* allowlisted command auto-resolves without a human prompt
  while the *already-pending* one from before the switch is unaffected;
  switch back to `manual`, verify auto-safe's allowlist stops applying to
  new requests; attempt to set `full-access` via the RPC and assert a clear
  error; attempt the RPC against a finished run and assert a clear error or
  documented no-op (pick one, be consistent, write the test either way);
  attempt the RPC against a nonexistent run id.
- **Item B**: failed Claude run at `Off` shows the retry affordance and
  clicking it resubmits at `Standard` with the same prompt; failed run at
  `Standard` resubmits at `Extended`; failed run at `Extended` shows no
  affordance; failed GLM/Codex run shows no affordance; successful Claude
  run at any tier shows no affordance.
- **Regression**: full existing Go (`internal/runs`, `internal/taskrunner`,
  `internal/wsapi`) and frontend (`web/packages/ui`) suites stay green;
  PR #171's own new tests (full-access decider-skip, thinking-level mapping)
  remain unaffected by this change (mutex addition around `approvalPolicy`
  must not change `full-access`'s spawn-time behavior at all).

## Progress

- [x] Item A backend — `Registry.SetApprovalPolicy` (mutex-guarded live
      `approvalPolicy` on `run`, `runPermissionDecider.Decide` reading it
      under the lock, full-access/finished-run/invalid-policy rejection)
      + `run.setApprovalPolicy` RPC in `internal/wsapi/handlers.go` +
      `ApprovalPolicy`/`ThinkingLevel` exposed on `RunStatus`/`RunSummary`
      so a client can read a run's current policy/tier back.
- [ ] Item A frontend — live control in the task/chat view.
- [ ] Item B — failure-state "Retry with higher effort" affordance for
      Claude-native runs, resubmitting one thinking tier up.
- [ ] Independent verification of agent-reported work (re-run Go tests +
      frontend typecheck/tests, read the actual diff) before merge.
- [ ] Rebuild, restart local daemon, dogfood in browser.

## Validation

To be filled in as each item lands — map back to each Acceptance Criterion
with the specific test or manual check that confirmed it, not just "tests
pass."
