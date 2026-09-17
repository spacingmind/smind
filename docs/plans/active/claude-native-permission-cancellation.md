# claude-native permission requests can vanish without a resolution event

## Context

Live-tested via a real `claude-native` run against a real Anthropic
account (2026-09-17, see `docs/plans/active/visual-identity-console.md`'s
Validation section for the session that found this): a tool call gated
behind `ApprovalPolicyManual` resolved to denial within ~5-8 seconds
instead of staying pending for `defaultPermissionTimeout` (5 minutes,
`internal/runs/registry.go:44`) — well before a human (or this session's
own screenshot pass, with a browser already subscribed to the task) ever
had a real chance to see or answer it.

Root-caused via direct code reading (this session, not guessed):
`runPermissionDecider.Decide` (`internal/runs/registry.go:432-535`)
selects on four cases — a human answering, the 5-minute timer, the
provider's own per-request `ctx`, and the run's own `d.r.ctx` (cancelled
by `Registry.Stop`). Three of the four record a distinguishing
`EventTypePermissionResolved` (`PermissionResolvedByHuman`/`AutoSafe`/
`Timeout`) so the timeline shows why a request resolved. **The
`case <-ctx.Done()` branch (registry.go:521-524) does not** — it just
calls `d.abandon(requestID)` and returns the raw `ctx.Err()`.

The doc comment on the `run` struct (`registry.go:267-270`) says this is
safe because "ACP's own session/request_permission dispatch runs with
`context.Background()`" — i.e. for ACP providers (GLM/Kimi/Codex) that
per-request `ctx` can never actually fire on its own, so the branch is
dead code there. **That assumption does not hold for `claude-native`.**
Tracing `claude-agent-sdk-go@v0.3.2` (the actual dependency, not
speculation): `dispatchControlRequest` derives each control request's
handler `ctx` from `context.WithCancel(c.baseCtx)`
(`engine.go:246-270`) and registers its cancel func keyed by request ID;
`cancelInflightHandler` (`engine.go:272-280`) fires that cancel when the
**real `claude` CLI subprocess itself** sends a `control_cancel_request`
for that ID (`engine.go:106-107`) — a message the CLI decides to send on
its own. That `ctx` is exactly what's threaded through
`c.permissionPolicy.Decide(ctx, ...)` → `claudeDeciderAdapter.Decide`
(`internal/taskrunner/permission.go:293-330`) →
`runPermissionDecider.Decide`'s `ctx` parameter. Confirmed independently
by inspecting the real `claude` CLI binary: it has its own internal,
much-shorter-than-5-minutes auto-deny fallback for an unanswered
permission dialog (a `setTimeout`-driven denial, logged internally as
`tengu_auto_mode_denial_dialog_auto_denied`) — completely independent of
and invisible to smind's own timeout. The exact response text observed
("The command requires approval and wasn't run") traces to the CLI's own
`bashMissKind:"no-rule-match"` passthrough-deny reason string, not
anything smind emits (smind's own fixed string is `"denied by human
reviewer"`, `permission.go:281` — different wording, confirming the CLI
resolved it, not smind's own decider logic).

**Not a policy misconfiguration**: default `ApprovalPolicy` is `manual`
(`registry.go:347-348`, matching the composer's "Manual approval"), and
`curl` isn't in `policy.go`'s `safeCommandPrefixes` allowlist regardless,
so the identical manual/5-minute path would have been taken either way.

**Prior art that missed this**: `docs/plans/completed/task-permission-ux.md`
logs the identical symptom text more than once (2026-09-12/13) but every
occurrence is framed as a nuisance hitting *the plan-authoring agent's own
outer Claude Code harness* while writing the feature — never diagnosed as
a defect in the shipped feature itself, never turned into a fix or an
ADR.

## Acceptance Criteria

- **Item 1 — record the missing resolution event (required, no
  dependency on Item 2's outcome).** `runPermissionDecider.Decide`'s
  `case <-ctx.Done()` branch (`registry.go:521-524`) records an
  `EventTypePermissionResolved` event, tagged with a new
  `taskrunner.PermissionResolvedByProviderCancellation` constant
  (`internal/taskrunner/event.go`, alongside the existing `..ByHuman`/
  `..ByAutoSafe`/`..ByTimeout`), before returning — mirroring exactly how
  the timeout branch above it does this, just with the new reason and no
  synthesized reject option (there may not be one available/relevant;
  the resolution records that the request was cancelled out from under
  the decider, not that a reject option was chosen). The `d.r.ctx.Done()`
  branch below it is unaffected — that one is a run-level stop, already
  has its own lifecycle events elsewhere, and is out of scope here.
- Frontend: `web/packages/ui/src/components/timeline/permission-reason.ts`'s
  `PERMISSION_REASON_LABEL` gets a `provider_cancellation` entry (label
  along the lines of "Cancelled by provider", `StatusBadgeStatus`
  `"warning"` — this is "worth a second look," same tier as `timeout`,
  per that file's own doc-comment convention) so the reason is visible
  instead of silently falling through to "no badge" (which is safe, per
  that file's design, but not informative).
- **Item 2 — investigate whether the underlying constraint can be
  narrowed at all**, i.e. whether `claude-agent-sdk-go` (or the `claude`
  CLI it wraps) exposes any documented way to raise, disable, or
  configure its own internal auto-deny fallback for a headless/
  `can_use_tool`-driven session (env var, CLI flag, SDK option). This is
  a research task with an honest binary outcome:
  - If such a mechanism exists: wire it (scoped to `claude-native` runs
    only) so smind's real 5-minute manual-approval window can actually
    be honored end-to-end, matching how ACP providers already behave.
    Add a regression test using a scripted fake `claude` CLI (extending
    `internal/taskrunner`'s existing `runFakeClaudeCLI` test double, per
    `taskrunner_test.go`) that sends a late `control_cancel_request` and
    confirms the run still gets a real chance to have a human answer
    first if configured to.
  - If no such mechanism exists (this is the more likely outcome per
    this session's research — no evidence surfaced of one): document the
    constraint explicitly (a short ADR under `docs/decisions/`, since
    this materially limits a safety-relevant feature — human-in-the-loop
    approval — for one specific provider in a way a future contributor
    could otherwise assume is a smind bug rather than an upstream CLI
    behavior) and close this item as "documented limitation," not
    "fixed." Do not attempt to route around the CLI's own timer by e.g.
    intercepting/suppressing its `control_cancel_request` — that would be
    fighting the CLI's own safety mechanism, a materially different (and
    riskier) kind of change than this plan's scope.
- Regardless of Item 2's outcome: `internal/runs/runs_test.go` gets a new
  test modeling this exact race — a decider whose `ctx` argument is
  independently cancelled mid-`Decide` (not `d.r.ctx`, not the timeout
  timer) — asserting the new `PermissionResolvedByProviderCancellation`
  event is recorded and is distinguishable in the run's event history
  from `..ByHuman`/`..ByAutoSafe`/`..ByTimeout`. This is the scenario
  `runs_test.go`'s existing coverage (`..._AppearsInHistoryBlocksThen
  RespondPermissionUnblocks`, `..._TimesOutAutoResolvesToDeny
  DistinguishableFromHuman`, `..._Stop_WhilePermissionPending_Unblocks`)
  never modeled — confirmed by reading that file directly before writing
  this plan, not assumed.

## Test Scenarios

- `internal/runs`: the new ctx-cancellation race test above (Go, direct
  unit test against `runPermissionDecider.Decide`, not requiring a real
  `claude` binary).
- `internal/taskrunner`: if Item 2 finds and wires a real fix, a
  scripted-fake-CLI test sending a late `control_cancel_request` mid
  permission-wait, confirming the configured behavior (either the fix
  actually holds the window open, or — if undocumented/unfixable — this
  test scenario is dropped and replaced by the ADR from Item 2's
  documented-limitation path).
- Web: a `permission-reason.test.ts`-style assertion (check whether that
  test file already exists before creating a new one) that
  `provider_cancellation` resolves to the expected label/status tuple,
  and that a still-unrecognized reason continues to resolve to `undefined`
  (no crash) — mirroring the existing three-reason coverage this file's
  doc comment implies should already exist somewhere.
- Manual: not required for this plan — the Go-level event test is what
  actually proves the fix; the visual/timeline rendering of a
  `provider_cancellation`-tagged permission entry is a straightforward,
  already-proven rendering path (same component that already renders
  `timeout`) and doesn't need its own live smoke test.

## Decisions

Item 2 landed as a real fix, not a documented limitation: the research
found the knob. The `claude` CLI (confirmed in binary 2.1.263) reads env
var `CLAUDE_CODE_USER_DIALOG_TIMEOUT_MS` (milliseconds, clamped to a
300000 minimum when set; default 300000) as its internal auto-deny
deadline for unanswered permission dialogs; the SDK exposes it on the
subprocess via `claudecode.WithEnv`. smind wires it to 3600000 (60
minutes) — comfortably above `defaultPermissionTimeout` (5 minutes,
`internal/runs/registry.go`) so smind's own timeout always fires first
and records its distinguishable `PermissionResolvedByTimeout` event,
mirroring how ACP providers already behave. Scoped to `runClaudeNative`'s
decider branch only (Runner-level policy defaults answer
programmatically and never wait on a dialog). A value already present in
the environment is forwarded as-is — a deployment-wide escape hatch.
Unlike paseo (refs/paseo), which only handles the abort signal, smind
extends the CLI's deadline so its own 5-minute window is the real
deadline. The constant lives in taskrunner (not imported from
internal/runs) because runs imports taskrunner.

## Progress

- [x] Item 1 — record `PermissionResolvedByProviderCancellation` on the
      `ctx.Done()` branch (Go) + frontend label entry (commit eb42d75)
- [x] Item 2 — research: the CLI reads
      `CLAUDE_CODE_USER_DIALOG_TIMEOUT_MS`; the SDK exposes `WithEnv`
- [x] Item 2 outcome — wired fix + regression test (fake CLI dumps the
      env var; a decider-wired run sets it, a pre-set user value wins)
- [x] `internal/runs` ctx-cancellation race regression test (Item 1,
      commit eb42d75)
- [ ] `task test` / `task lint` green

## Validation

- `go test ./internal/taskrunner/ ./internal/runs/` — green (2026-09-17),
  including the new
  `TestRunner_RunPrompt_ClaudeNative_DialogTimeoutEnv`.
- `task lint` (`go vet ./...` + gofmt check) — green (2026-09-17).
- Web tests unaffected: no web files touched by Item 2 (Item 1's label
  test landed with eb42d75).
