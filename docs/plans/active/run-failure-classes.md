# Sharper run failure classes with a next action

Size: L (12) — scope 3 (classifier + store, inbox/MCP/CLI, web), PRs 3, risk 2 (the cooldown and failover paths change), decisions 3, verify 3 (UI frames).

A failed run's `failureKind` is only `rate_limited` or `cli_exited` today
(ADR-0041 B1). A 401, a model the plan does not include, a 429 that clears in
ten seconds and a weekly plan window all look alike to a Lead and a human:
one sentence of raw provider text, no instruction. This adds four classes, each
with a concrete next action that renders in the UI, the inbox, the MCP tools
and the CLI. ADR-0041 B1 already names `auth` and `overloaded` as "the next
obvious values"; this is that step.

Phase 1 (this PR) is the spec only. Nothing is implemented until the Steward
accepts it.

## Acceptance Criteria

Classes (all on an error run; status stays `error`, as ADR-0041 D13 requires):

- AC1 `failureKind` takes the values `auth_expired`, `plan_window_exhausted`,
  `model_not_in_plan`, `transient_429`, `cli_exited` (unchanged) and
  `rate_limited` (now only the residual, see Decision D1).
- AC2 `plan_window_exhausted`: the plan's usage window is spent. Carries
  `window` (when known) and `resetAt` (when known, RFC 3339 UTC). Any run the
  old classifier called `rate_limited` with a window, a reset time or a
  rejected `rate_limit_event` is this class.
- AC3 `transient_429`: a throttle that is not a plan window: a 429 / overloaded
  with a Retry-After, or an "overloaded" / "high concurrency" body, and no
  window or reset time. It never sets a provider cooldown and never reads as a
  plan problem anywhere.
- AC4 `auth_expired`: the provider refused the credential (401, "invalid API
  key", "please run /login", "token expired", GLM's token-expired codes).
- AC5 `model_not_in_plan`: the account's plan does not include the model the
  run asked for (404 model not found, "may not exist or you may not have
  access", GLM unknown-model code). When smind knows the models the plan allows
  they are named in the action.
- AC6 Precedence is deterministic: a declared window / reset time beats auth
  and model text; auth beats model; a bare "rate limit" word without a window
  and without a Retry-After stays `rate_limited` (residual), never silently
  `transient_429`.

Wire (additive, `omitempty`; Decision D2):

- AC7 Every surface that carries `failureKind` today also carries `action`
  `{kind, target?, label}` (and keeps `window`, `resetAt`, `resetInferred`,
  `fallbackProfileId`): wsapi run payloads and `run.finished` events,
  `run.list`, task tree, MCP `task_status` / `task_wait` / `run_list` /
  `task_tree`, the fleet wire (`internal/fleet/wire`) and the plugin hook
  input. Unknown kinds from a newer peer render as the raw text (no crash).
- AC8 The action is derived from (kind, provider, account, profile, now), not
  stored: no new column except the migration of old rows (AC9).
- AC9 Old rows: `rate_limited` rows with a window or a reset time become
  `plan_window_exhausted` in one store migration; the others stay
  `rate_limited` (no installed base; the break is noted in the ADR and the
  release notes).

Next actions (vocabulary, Decision D2):

| Class | action.kind | target | label (example) |
|---|---|---|---|
| `auth_expired` | `sign_in` | `settings:providers?account=<id>` (or `provider=<id>` when the run used an env/secret key) | "Sign in again to <account>" |
| `plan_window_exhausted` | `wait_until` / `switch_profile` | resetAt / `profile:<fallbackProfileId>` | "Resets 17:35 (in 2h 3m) · or switch to <profile>" |
| `model_not_in_plan` | `pick_model` | `models:[...]` allowed, when known | "Pick another model: <a>, <b>" |
| `transient_429` | `auto_retry` then `retry_now` | `{attempt, max, nextAt}` | "Retrying in 30 s (1 of 2)" |
| `rate_limited` (residual) | `retry_now` | none | "Retry" |
| `cli_exited` | `continue` | none | "Continue the run" |

- AC10 Each surface that shows a failure shows the label and a working control:
  run row, task header chip, timeline error card, inbox card, and (text) MCP and
  CLI. Locations in "Where it renders" below.
- AC11 A Lead can route on the class from MCP output alone: `failureKind` plus
  `action.kind` plus a one-line `message` that says what NOT to do (for example
  "do not retry and do not fall back: a human must sign in").

Routing, cooldown and failover (matrix below):

- AC12 `auth_expired` and `model_not_in_plan` never set a provider cooldown,
  never enqueue a `wait` retry and never start a `fallback` successor.
- AC13 `transient_429` never sets a provider cooldown; the daemon retries it on
  the same chat with a stated backoff (D3), at most `max` times, then it becomes
  an actionable failure (inbox item, `retry_now`).
- AC14 `plan_window_exhausted` behaves exactly as `rate_limited` does today:
  cooldown on a future provider-wide reset (ADR-0041 F1), `wait` / `fallback`
  profile policy (F2b), `weekly-<model>` windows never block the provider.
- AC15 No consumer keys on the literal `rate_limited` any more: they all use
  one helper (`IsLimitKind` for window/transient/residual, `IsHumanFix` for
  auth/model), so a fifth class cannot be missed.

Inbox (internal/inbox derive):

- AC16 A new item kind `run_failed` is derived for a task whose latest run is an
  error with an actionable class and no later run: `auth_expired` and
  `model_not_in_plan` (attention high), `plan_window_exhausted` (normal; high
  when no fallback and no wait retry is queued), `transient_429` only after its
  retries are spent, residual `rate_limited` (normal). `cli_exited` is not an
  inbox item (it has the footer's Continue; unchanged). Key
  `runfail:<runID>`; it resolves by itself when a later run exists, the task is
  archived or the cooldown is cleared.

Docs and frames:

- AC17 ADR amendment pointers (ADR-0041 B1, ADR-0028 routing section), a new
  short ADR if D1/D2 are accepted, `docs/cli.md` via `task docs:cli`, the MCP
  instructions table.
- AC18 UI frames, light and dark, at 1280 and 390, of every new card (frames
  plan below), each looked at by the Lead.

## Detection per harness

One pure classifier replaces `ratelimit.Classify`'s role: `internal/runs/failure`
(package renamed from `ratelimit`, which keeps the date/zone parsing and moves
under it). Input is the existing `ratelimit.Input` plus the provider spec's
harness, the HTTP status and Retry-After when the backend saw them, the
requested model, and the typed error from the runner. It is pure and has no
live calls. Order: (1) structured signal, (2) typed runner error, (3) harness
text patterns, (4) generic patterns. `registry.finish` (internal/runs/registry.go
~line 1597) still calls it once and stores kind/window/reset.

Signals found in the code today:

| Harness | What smind sees now | Source |
|---|---|---|
| claude-native | `rate_limit_event` with `status == "rejected"`, `type` five_hour / seven_day / seven_day_opus / seven_day_sonnet (not `overage`) = plan window with a `resetsAt`. Text: "usage limit reached\|<epoch>", "N-hour limit reached ... resets 3pm", "You've hit your session / weekly / Opus limit · resets 1pm (Asia/Saigon)". CLI died without a result = `CLIExitError` (stderr tail, exit code) | `internal/runs/ratelimit/ratelimit.go`, `internal/taskrunner/cli_exit.go`, `runner.go` `claudeRateLimitEvents` |
| codex-native | Text only: "You've hit your usage limit" (plan window, no parsed reset). The SDK's final error string is what reaches `errMsg` | `ratelimit.go` `reCodex` |
| ACP (GLM, kimi, opencode, ...) | JSON-RPC error chain `acp: ... rpc error -32603: Internal error` wrapping the vendor text. GLM: "429 ... usage limit reached for N hour ... reset at <UTC+8 wall clock>" | `ratelimit.go` `reGLM`, `web/.../lib/run-error.ts` |
| compatible providers (HTTP) | Same text path; the proxy's own errors are Anthropic/OpenAI-shaped: `not_found_error` / `model_not_found`, `overloaded_error` | `internal/server/proxy.go` (~644-685) |
| any | Generic: "usage limit reached", `rate_limit_error`, "429" plus rate words | `ratelimit.go` generic group |

In-code precedent for the transient/plan split: the quota passive observer
already separates a declared hard rejection (a cooldown) from a bare 429 with
`Retry-After` (a back-off of min(Retry-After, 60 s), reason `backoff`):
`internal/quota/passive_detect.go` and `passive_types.go`. The classifier reuses
that rule, so the routing view and the run view agree.

New patterns (all unproven until a fixture exists; PR1 must confirm each
against `refs/claude-code`, the codex SDK source and the recorded payloads before
it ships, and drop any pattern it cannot source):

| Class | claude-native | codex-native | ACP / GLM | compatible HTTP |
|---|---|---|---|---|
| `auth_expired` | `result` with `is_error` and text "Invalid API key · Please run /login", "OAuth token has expired", API 401 `authentication_error`; CLI stderr "Not logged in" (also seen in `learning/probe/sandbox/env.go`) | "401 Unauthorized", "token expired / refresh", "please sign in" | GLM codes for a missing / expired / incorrect token (1001, 1002, 1003) | HTTP 401/403 + `authentication_error` / `invalid_api_key` |
| `model_not_in_plan` | "There's an issue with the selected model (x). It may not exist or you may not have access to it", 404 `not_found_error` on `model` | "model ... is not supported when using Codex with a ChatGPT account" style text | GLM unknown-model / not-in-package codes (1211, 1220) | 404 `model_not_found`, 403 "does not have access to model" |
| `transient_429` | 429 / 529 `overloaded_error` / `rate_limit_error` with `retry-after`, no window | "rate limit exceeded, try again in Ns" | GLM concurrency / overload codes (1302, 1305) | 429 / 503 + `retry-after` |
| `plan_window_exhausted` | the existing rejected signal and the existing text patterns, unchanged | `reCodex` (reset unknown) | `reGLM` (window N h, reset UTC+8, inferred) | generic "usage limit reached" with a window or reset |

Ambiguity rule: when only the generic "429 + rate words" matched and there is no
window, no reset and no Retry-After, the result is the residual `rate_limited`
(today's behaviour), not a guess. An unsourced GLM code never classifies on the
code alone; it needs the text as well.

Typed runner errors beat text: PR1 adds an optional `ErrorClass` carried on the
runner's final error (`taskrunner.ClassedError{Class, Status, RetryAfter,
Message}`) so a harness that sees a structured status (an HTTP status in the
claude result, an ACP error code) says so and the text regex is only a
fallback. Harnesses that give no structure keep text-only detection.

### Fixtures plan (no live provider calls)

`internal/runs/failure/testdata/<harness>/<case>.json` (input) plus a golden
`want` per file; one table test reads the directory, so adding a payload is one
file. Real-looking payloads, copied from the recorded dogfood errors where we
have them (the GLM 429 of 2026-10-22, the Claude "session limit" text) and
written in each vendor's documented shape otherwise, each file carrying a
`"source"` field (`recorded` or `documented: <url or ref path>`). Cases per
harness: each of the four classes, a window with and without a reset, a
transient 429 with and without Retry-After, a bare "429 rate limit" (residual),
a model error that mentions "limit" (must stay `model_not_in_plan`), an auth
error that mentions "429" (must stay `auth_expired`), a non-error text that
contains the words (must not classify), and the redaction case (a key in the
error text never reaches the stored text; the existing `redact` pass is
asserted). Fake backends (`fakeclaude`, `fakeagent`) gain a scenario per class
so the runs-level tests drive `finish` end to end.

## Wire / schema (additive; D2)

```
failureKind  "auth_expired"|"plan_window_exhausted"|"model_not_in_plan"|"transient_429"|"rate_limited"|"cli_exited"
window       string   (plan_window_exhausted / rate_limited; unchanged)
resetAt      RFC3339  (unchanged name; the spec's "resetsAt" is this field)
action       {kind, target?, label, models?[], attempt?, max?, nextAt?}   omitempty
```

Go: `internal/store` keeps `failure_kind TEXT` (values only); the action is built
by `failure.NextAction(kind, ctx)` in the same package and attached where
`FailureKind` is copied today: `internal/wsapi/{events,handlers}.go`,
`internal/runs/{list,tree_view,registry}.go`, `internal/tasktree/tree.go`,
`internal/fleet/{wire,serve,watch}`, `internal/plugins/hooks.go` (redacted like
`FailureKind`), `cmd/smind/{task,task_tree,host_read,mcp_*}.go`. TS: the same
fields in `use-run-timeline.ts`, `use-task-attention.ts` and the wire types.
Unknown `action.kind` is always rendered as its `label`.

## Where it renders

- Run row / task header: the sidebar chip and `data-failure-kind` footer
  (`run-footer.tsx`, `run-footer-model.ts`, `app-sidebar.tsx` limit hint) show
  class, label and the control (Sign in / Pick model / Retry / Switch).
- Timeline error card (`run-error-box.tsx`, `lib/run-error.ts`): the one-line
  message becomes class-specific ("Signed out of <account>", "<model> is not in
  your plan", "Rate limited for a moment", the existing window sentence) with
  the control inline; Details keeps the raw text.
- Inbox card (`kind: run_failed`): title = task title, summary = the label,
  button = the action (deep link via the existing `smind:open-settings` event,
  `sectionId: "providers"`; the Usage section for window cases).
- MCP `task_status`, `task_wait`, `run_list`, `task_tree`: `failureKind` +
  `action` fields, and `message`: "Run r-4f2 failed: auth_expired. Next: a human
  must sign in again to <account> (Settings → Providers). Do not retry and do
  not fall back." `mcp_instructions.go` gets a routing table for the classes
  (replacing its rate_limited paragraph).
- CLI `smind task runs|logs|wait`: `rateLimitedText` generalises to
  `failureText`; the status line gains `· next: <label>`; `task wait` exit code 6
  stays for the three limit classes, new code 7 = "a human must fix setup"
  (`auth_expired`, `model_not_in_plan`). Child-event lines
  (`internal/runs/childevent_text.go`) and remote notifications get the class
  and `next:` text; `web/.../lib/child-event.ts` parses the new classes.
- Mobile: read-only text from the same wire; a UI follow-up, not in this item.

## Routing, cooldown and failover matrix

| Class | Provider cooldown (F1) | `wait` retry (F2b) | `fallback` successor (F2b) | Auto action | Resumable (`needs-continue`) |
|---|---|---|---|---|---|
| `plan_window_exhausted` | yes, future provider-wide reset (unchanged) | yes (unchanged) | yes (unchanged) | as today | yes after resetAt |
| `transient_429` | never | the bounded backoff below replaces it | only after retries are spent | retry same chat 30 s, then 2 min (Retry-After wins, clamped to [5 s, 5 min]), max 2 | yes after the retries |
| `auth_expired` | never | never | never | none | yes (the session is stored; continue after sign-in) |
| `model_not_in_plan` | never | never | never on the same account; a profile fallback is shown as an action, not taken | none | yes, with a different model on the follow-up |
| `rate_limited` residual | as today (needs a reset) | as today | as today | as today | as today |
| `cli_exited` | never | n/a | n/a | none | yes (unchanged) |

Why `auth_expired` never falls back: a credential that stopped working is a human
fix; moving to the fallback profile spends a turn on a different account and
hides the expired one. The failure is surfaced loudly instead (inbox, high).
The stall detector (`internal/stall`) keeps its `rate_limited && wait` rule via
the helper and gains no new class.

## Test scenarios (named; the implementer writes these first)

Classifier (`internal/runs/failure`, table over `testdata/`):

1. `TestClassify_Fixtures` over every harness file above.
2. `TestClassify_PlanWindowBeatsAuthAndModelText`.
3. `TestClassify_BareRateLimitIsResidualNotTransient`.
4. `TestClassify_TransientNeedsRetryAfterOrOverloadBody`.
5. `TestClassify_ModelErrorMentioningLimitStaysModel`.
6. `TestClassify_AuthErrorMentioning429StaysAuth`.
7. `TestClassify_StructuredErrorBeatsText` (ClassedError with a status).
8. `TestNextAction_PerClass` (labels, targets, allowed models named when known, plain label when not).

Runs / policy (`internal/runs`):

9. `TestFinish_StoresNewClasses` (one case per fake scenario).
10. `TestFinish_AuthExpiredSetsNoCooldownNoFallbackNoWait` (profile with `fallback` and with `wait`).
11. `TestFinish_ModelNotInPlanNeverRetriesSameAccount`.
12. `TestFinish_Transient429RetriesTwiceThenSurfaces` (backoff times, Retry-After clamp, `retry_now` after the cap, no cooldown).
13. `TestFinish_PlanWindowStillSetsCooldownAndFallback` (regression of F1/F2b).
14. `TestFinish_WeeklyModelWindowDoesNotBlockProvider` (unchanged).
15. `TestResumableKind_NewClasses`, `TestDisposition_NewClasses`.
16. `TestIsLimitKind_NoLiteralRateLimitedLeft` (a source scan test: no `== "rate_limited"` outside the helper, so AC15 stays true).

Store: 17. `TestMigration_RateLimitedWithWindowBecomesPlanWindow`, `...WithoutWindowStaysResidual`.

Wire: 18. `TestRunPayload_CarriesAction` (wsapi run.get / run.list / run.finished), 19. `TestFleetWire_ActionRoundTrip_UnknownKindTolerated`, 20. `TestPluginHook_RedactsActionLabel`.

Inbox: 21. `TestDerive_RunFailedPerClass` (attention per class, `cli_exited` none, `transient_429` none while retrying), 22. `TestDerive_RunFailedResolvesOnLaterRun`, 23. `TestDerive_RunFailedKeyStable`.

MCP / CLI: 24. `TestTaskStatus_MessageRoutesOnClass` (a Lead-facing message per class; auth says do not retry), 25. `TestTaskWait_ExitCodes` (6 for the limit classes, 7 for auth/model), 26. `TestFailureText_PerClass`, 27. `TestChildEventText_NewClasses`, 28. `TestMCPInstructions_ListEveryClass` (every class in the enum appears in the table).

Web (vitest + frames): 29. `run-error-box` per class with the control, 30. `run-footer-model` per class (the "switch" button only when a fallback exists), 31. inbox card per class and its deep link dispatches `smind:open-settings` with the right section, 32. `describeRunError` keeps the old rate-limit behaviour, 33. an unknown `action.kind` renders its label.

## Mutation table (one mutation each; the named test must FAIL, a build failure proves nothing)

| File | Mutation | Must fail |
|---|---|---|
| `internal/runs/failure/classify.go` | let a bare "429 rate limit" return `transient_429` | `TestClassify_BareRateLimitIsResidualNotTransient` |
| same | check auth before the window signal | `TestClassify_PlanWindowBeatsAuthAndModelText` |
| `internal/runs/registry.go` | call `SetProviderCooldown` for `transient_429` | `TestFinish_Transient429...` |
| `internal/runs/failover.go` | let `auth_expired` reach `startFallbackSuccessor` | `TestFinish_AuthExpiredSetsNoCooldownNoFallbackNoWait` |
| `internal/runs/failover.go` | retry `model_not_in_plan` on the same account | `TestFinish_ModelNotInPlanNeverRetriesSameAccount` |
| `internal/store` migration | drop the window condition | `TestMigration_RateLimitedWithoutWindowStaysResidual` |
| `internal/inbox/inbox.go` | derive `run_failed` for `cli_exited` | `TestDerive_RunFailedPerClass` |
| `cmd/smind/task_wait.go` | map `auth_expired` to exit 6 | `TestTaskWait_ExitCodes` |
| `cmd/smind/mcp_run_tools.go` | drop the "do not retry" sentence for auth | `TestTaskStatus_MessageRoutesOnClass` |
| `web/.../run-error-box.tsx` | render the control for `transient_429` as `Sign in` | the per-class card test |

## Frames plan

Fixture-driven (a fake run per class, no live calls), light and dark at 1280 and
390: timeline error card x 4 classes, footer + sidebar chip x 4, task header
chip, inbox card x 4 (a `run_failed` list with all four), plus the unchanged
`rate_limited` footer as a regression frame. Transition frames for the card
appearing and for the Sign-in deep link opening Settings → Providers are
captured and reviewed too (the standing no-glitch rule). Evidence goes under
`~/.smind/dogfood/run-failure-classes/` and is deleted after merge.

## Decisions

Decided in the repo already: a typed `failureKind` column on `runs`, status stays
`error` (ADR-0041 B1/D13); transient vs hard split exists in the passive quota
observer (ADR-0028); the settings deep link `smind:open-settings` exists.

DECIDED 2026-11-01 as Supervisor defaults (the Human is away and may veto
later; recorded the same way in the ADR):

- D1 **Split.** `plan_window_exhausted` and `transient_429` replace
  `rate_limited`; `rate_limited` stays only as the residual for an
  unclassifiable limit (today's behaviour, no guessing). Old rows migrate by
  rule (AC9). Short new ADR plus amendment pointers in ADR-0041 B1 and ADR-0028.
- D2 **Typed action object.** The daemon builds `action {kind, target?, label}`;
  one vocabulary for web, mobile, MCP and CLI; additive and `omitempty`;
  clients render `label` for an unknown kind.
- D3 **Auto-retry `transient_429`**: twice (30 s, then 2 min; Retry-After wins,
  clamped to [5 s, 5 min]), ON for orchestrator-sourced and profile runs, OFF
  for a plain human send (a human gets `retry_now` and the inbox item). HARD
  CAP: a retry never runs while the provider is in cooldown (it waits, or is
  dropped to the actionable failure), and every retry is a counted run, so its
  usage lands in the run usage and the per-tree guard like any other run.
- D4 (my default) the inbox gets a new kind `run_failed`; `task wait` gets exit
  code 7; `auth_expired` marking the account "needs sign-in" in Usage is NOT
  done here (follow-up).

Additional test for D3: `TestFinish_Transient429RetryHeldWhileProviderCooldown`
(mutation: skip the cooldown check; the test must fail), and
`TestFinish_Transient429RetryRunsAreCounted`.

## PR split (non-stacked; each from origin/develop; PR2 and PR3 start after PR1 merges)

| PR | Scope | Write scope (one owner) | Owner |
|---|---|---|---|
| PR1 detection + wire | classifier package + fixtures, `ClassedError`, `registry.finish`, policy matrix (failover/cooldown/retry), store migration, `NextAction`, wsapi/events/handlers/tree/fleet/plugin fields, fake scenarios, contract file (ADR-0045: wsapi + store migration) | `internal/runs/**`, `internal/taskrunner/**`, `internal/store/**`, `internal/wsapi/**`, `internal/tasktree`, `internal/fleet/**`, `internal/plugins/hooks.go` | Lead core; peer-glm for fixture files and the table tests from my exact case list |
| PR2 inbox + MCP + CLI | `run_failed` derive rule, `failureText`, MCP messages + instructions table, exit code 7, child-event text, `docs/cli.md` | `internal/inbox/**`, `cmd/smind/**`, `internal/runs/childevent_text.go`, `internal/runs/remotenotify.go` | peer-glm with step briefs, Lead reviews and runs the mutations |
| PR3 UI | error card, footer/chip, inbox card, deep link, frames, contract (UI) | `web/packages/ui/**` | peer-sonnet (screenshots), Lead reviews every frame |

Each PR: `task test`, `task lint`, one mutation per load-bearing test, plan
Progress/Validation updated, a validation contract stored via
`validation_contract_submit` for PR1 and PR3 (wsapi, migration, UI). Peers get
the standing brief: no `git push`, no `gh pr create`, no `git stash`, commit per
step; the Lead pushes. Docs (ADR + amendment pointers + MCP instructions) land in
PR1 for the ADR and PR2 for the CLI/MCP text.

Out of scope: live provider calls, mobile UI, account "needs sign-in" state in
Usage, changing the `rate_limited` window/zone parsing, any new run status.

## Progress

- 2026-11-01 Phase 1: code read; spec written and accepted (#762); D1-D3 recorded as Supervisor defaults.
- 2026-11-01 PR1 (detection + wire + classifier + migration + fixtures + policy), branch `run-failure-classes-pr1`:
  - `internal/runs/ratelimit` renamed `internal/runs/failure`; new kinds, `IsPlanLimit` / `IsLimitKind` / `IsHumanFix` / `IsKnown`; precedence plan window > typed code > auth > model > generic 429 words.
  - Sourced patterns only. Claude: "Not logged in · Please run /login", "Invalid API key", `authentication_error`, the "issue with the selected model" text (refs/claude-code CHANGELOG); codex: typed `CodexErrorInfo` now decoded from the app-server's turn error (`codex.TurnError`, `taskrunner.FailureHint`), plus "Selected model is at capacity", "rate limit exceeded:", "Quota exceeded. Check your plan and billing details.", "Model not found", "The 'x' model is not supported when using Codex with a ChatGPT account" (refs/codex). **Dropped as unsourced: every GLM error code** (1001-1003, 1211, 1220, 1302, 1305); GLM keeps the text patterns it had.
  - Deviations from the spec text: (1) a bare "usage limit reached" (no window) is `plan_window_exhausted`, not residual: the provider said usage limit; only 429/rate words without a hint are residual. (2) `needs-continue` disposition is not added for the new kinds (the action carries the step; Continue stays for cli_exited and plan windows past their reset); `resumableKind` is true for all known kinds so the footer's Continue works. (3) A minimal web shim keeps the plan-limit UI working for `plan_window_exhausted` (one helper, tests); the real UI is PR3. (4) The fleet watcher's child-event text does not carry the action yet (PR2 owns text). (5) `AllowedModels` for `pick_model` is not filled yet (no per-account allowed-model source exists in the daemon): the label is the generic one until a source exists; named gap for PR2/PR3.
  - The two GLM peers hit the 5-hour GLM limit (reset 21:29Z) after one commit; their queued retries were cancelled and the Lead did the wire plumbing and fixtures directly.

## Validation

PR1 (confirmed by the named tests, each with one mutation that made the test fail; see the PR body):

- AC1-AC6 `TestClassify_*` (precedence, ambiguity, structured code, `TestClassify_Fixtures` over 31 payloads in `internal/runs/failure/testdata/`).
- AC7/AC8 `TestNextAction_PerClass`, `TestRunFinishedEvent_CarriesAction`, `TestRunLogs_FallbackProfileIdForRateLimitedRun` (action = switch_profile), `TestRunLogs_ActionAuthExpiredIsSignIn`, `TestTreeNode_FailureActionSerialises`, `TestFleetWire_ActionRoundTrip_UnknownKindTolerated`, `TestPluginHook_RedactsActionLabel`, `TestMCPTaskStatus_CarriesAction`.
- AC9 `TestMigration_RateLimitedWithWindowBecomesPlanWindow`.
- AC12 `TestFinish_AuthExpiredSetsNoCooldownNoFallbackNoWait`, `TestFinish_ModelNotInPlanNeverRetriesSameAccount`.
- AC13 `TestFinish_Transient429RetriesTwiceThenSurfaces`, `...PlainHumanSendIsNotRetried`, `...RetryHeldWhileProviderCooldown`, `...RetryRunsAreCounted`.
- AC14 `TestFinish_PlanWindowStillSetsCooldownAndWait` plus the existing F1/F2b tests.
- AC15 consumers use the helpers (no literal `== "rate_limited"` left outside `failure/` and the tests); a source-scan test is not added (PR2 adds the MCP table check).
- AC10, AC11, AC16-AC18: PR2 / PR3.
