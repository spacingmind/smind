# More failure classes, Claude effort, and an auto-mode gate for compatible endpoints

Status: **spec, not started** (2026-11-09). Nothing is implemented until the Human answers the decisions below. Extends
[`run-failure-classes.md`](run-failure-classes.md) (ADR-0050) and [`codex-reasoning-effort.md`](codex-reasoning-effort.md); touches
[ADR-0019](../../decisions/0019-provider-native-permission-modes.md) (permission modes) and ADR-0046 (provider specs). Three items, each its own PR chain:

1. **R3a** — new human-action failure classes, readable guidance for every class, and the label-safety tests.
2. **R3b** — Claude effort as a provider-scoped `thinkingLevel` with per-model gating.
3. **R3c** — gate Claude `auto` permission mode for provider instances with a custom `ANTHROPIC_BASE_URL`.

Items touching approvals or credentials (R3a's denials class and R3c) get the high-risk second read at implementation (two fresh-context reviewers).

## Facts (read from the code and the reference sources; nothing probed)

- Failure kinds today: `plan_window_exhausted`, `transient_429`, `auth_expired`, `model_not_in_plan`, `rate_limited`, `cli_exited`
  (`internal/runs/failure/classify.go`). `IsHumanFix` is `auth_expired || model_not_in_plan` and drives `ExitCode` 7 (`kinds.go`, `action.go`).
  `Guidance(kind)` (`action.go`) is a Lead-facing routing line for every one of the six; the human-facing sentence for `cli_exited` is the raw
  "the agent CLI exited without a result" in `web/.../run-footer-model.ts` (`FAILURE_LABELS`) and `mobile/src/runFooter.ts`, shown inside the Continue prompt.
- Codex's `CodexErrorInfo` has `contextWindowExceeded`, `sessionBudgetExceeded` and `tooManyDenials`
  (`refs/codex/codex-rs/app-server-protocol/src/protocol/v2/shared.rs`). `taskrunner.FailureHint` (`internal/taskrunner/failure_hint.go`) maps the
  throttle and auth classes and **deliberately returns none for these three** ("a human must act"), so today they classify as unclassified errors.
  `TurnError.Info` carries the class name (`internal/codex/client.go`).
- Codex's session budget is a Codex-side rollout (token) budget with shared accounting (`core/src/agent/control/budget.rs`, `rollout_budget.rs`),
  **not smind's tree budget**. smind's budget (`internal/runs/budget.go`, the header's "Set budget") refuses a *launch* with RPC code `budget_exceeded`;
  it never ends a run as a failure (a `budget_exceeded` stop reason also exists in `disposition.go`'s needs-continue set but no code emits it yet). `too many denials` comes from Codex's guardian (auto-review) path (`guardian_*` tests).
- Claude equivalents: none are typed. The CLI compacts automatically, and the strings that reach a user are API/CLI text ("Prompt is too long",
  "Context limit reached", `refs/claude-code/CHANGELOG.md` lines 275 and 881; a budget result subtype `error_max_budget_usd` is named in the SDK changelog but not in the
  CLI refs and smind passes no `--max-budget-usd`, so it is unconfirmed). Detection for Claude is text only and unconfirmed without real runs (see D3).
- Claude effort: `claude --help` documents `--effort <level>` (`low, medium, high, xhigh, max`); the Go SDK has `WithEffort` (passthrough, unvalidated)
  and smind never sets it for Claude (`internal/taskrunner/runner.go` uses `codex.WithEffort` only). The CHANGELOG states effort is **per model**
  (`/effort` saves per model), `xhigh`/`max` with thinking off used to fail and the CLI now sends `high` in that case (CHANGELOG:4083; the error text at :2599), an organisation's
  `maxEffortLevel` cap lowers the level and the CLI says which one it uses (:393, :3656), and some gateways reject the effort beta (:649). The type definitions
  in `refs/claude-code/mods/types/claude-code.d.ts` say the active level is reported "after any silent downgrade for the selected model" and is absent "for a model without effort". `ultrathink` runs at `xhigh` on Opus 5.5, Sonnet 5.5, Haiku 5.5 and Fable 5.1 through Anthropic's API. smind's Claude catalog is exactly
  those four plus the aliases `sonnet|opus|haiku` (`internal/taskrunner/models.go`). The TypeScript SDK reports the applied level on `system/init` (`effort`, its changelog) and a hook's `effort.level`;
  the vendored Go SDK v0.4.2 does **not** parse it (`SystemMessage` is `{Subtype, Raw}`, `messages.go`), so it is readable only from `SystemMessage.Raw`.
- `ResolveLevel(catalog, kind, model, level, explicit)` (`internal/taskrunner/level.go`) already implements the rule: explicit unsupported level is an
  error naming the supported ones; an inherited one (profile pin, queue, failover, old row) is dropped with a note; Claude's vocabulary is today the fixed
  `off|standard|extended` (thinking), Codex's comes from the model catalog (`Efforts`).
- Auto mode: `claudeModes()` offers `auto` (`AutoApproves: true`) unless the **daemon's** environment sets `CLAUDE_CODE_USE_BEDROCK`/`VERTEX`
  (`claudeAutoModeAvailable`, `internal/taskrunner/modes.go`). A provider instance's own `ANTHROPIC_BASE_URL` (`providers.Spec.Env`, ADR-0046; recipes set
  `EndpointKind: anthropic`) is not consulted. The CLI's own notes: behind an LLM gateway auto mode's safety check can be unreachable ("calls the server can't
  check are asked about or denied, with no fallback to the built-in classifier"; `claude -p` now exits 1 when actions are refused for lack of a check;
  `CLAUDE_CODE_AUTO_MODE_SERVER=0` keeps the client-side classifier; a gateway answering 403/422 to the check used to fail every turn).

## R3a — failure classes and guidance

### Acceptance Criteria

- AC1 Three new kinds, additive: `context_window_exceeded`, `session_budget_exceeded`, `too_many_denials`. Each is a human-fix class: never retried, never failed
  over, never on cooldown, never resumable by a plain "continue" (`IsHumanFix` true; `ExitCode` 7; `IsKnown` true).
- AC2 Classification: Codex's three `CodexErrorInfo` values map through `FailureHint` to new `Code*` constants and beat text (structured wins, as today). Claude and
  other harnesses classify by documented text only, with fixtures taken from the reference changelog strings; an ambiguous text never classifies.
- AC3 Each new class has an `Action` and a `Guidance` line:
  - `context_window_exceeded` → action `new_chat` ("Start a fresh chat for this task"; the same session cannot take the next turn); compaction is offered only if D2 says so.
  - `session_budget_exceeded` → action `raise_budget`; the label names whose budget it is (D1): the provider's own session budget (Codex config) is **not** smind's "Set budget".
  - `too_many_denials` → action `review_policy` (target `settings:permission-policy`), label "Review the permission policy". It never offers to approve, allow or retry.
- AC4 Every existing class has a human sentence, not only the Lead guidance: `cli_exited` reads as "The agent stopped before finishing (its session is saved). Continue
  to pick up where it left off." and the other five get one plain sentence each. One daemon-owned table (`failure.Describe(kind)`), carried additively on the wire as `action.summary`
  (omitted for an unknown kind; `failureNote` is taken by the MCP Lead routing note), replaces the clients' private label maps (`FAILURE_LABELS` in web and mobile).
- AC5 Wire additive: `failureKind` accepts the new values; `action.kind` gains `new_chat`, `raise_budget`, `review_policy` (clients render `label` for an unknown kind).
  Old rows and old clients are unchanged.
- AC6 Surfaces: web run error card, footer, sidebar hint and inbox `run_failed` item; mobile run footer (text); CLI `task runs|logs|wait` status line (`· next: <label>`);
  MCP `task_status|task_wait` `message` and `mcp_instructions.go` routing table; child-event lines. A new class with a missing label in any surface fails a test.
- AC7 Label safety: no action label, guidance or failure sentence of these classes matches `/merge|approve|always[ _-]?allow/i`, in Go and in the web/mobile sources (the
  same style as `stewardSafety.test.ts`, which scans `steward`, `policy` and `screens/steward` sources).

### Test scenarios

`TestClassify_CodexNewClasses` (typed code beats text), `TestClassify_ClaudeContextTextFixtures` (changelog strings; an ambiguous string does not classify),
`TestFailureHint_NewCodexInfos`, `TestIsHumanFix_NewClasses`, `TestExitCode_NewClassesIs7`, `TestNextAction_NewClasses` (kind, target, label), `TestGuidance_PerClass` (all nine
non-empty and each says what NOT to do), `TestDescribe_EveryKnownClassHasASentence`, `TestDescribe_CLIExitedIsReadable`, `TestFinish_NewClassesNeverRetryNeverCooldownNeverFailover`,
`TestNextAction_SummaryOnEveryClass`, `TestDerive_RunFailedNewClasses` (inbox, attention high), `TestFailureText_PerClass` (CLI), `TestMCPInstructions_ListEveryClass`,
`TestFailureLabels_NoApprovalWords` (Go: guidance, labels, descriptions), web `failure-class.safety.test.ts` and mobile `failureSafety.test.ts` (source scan with the regex),
`failure-class.test.ts` / `run-footer-model.test.ts` / `runFooter.test.ts` (every class has a sentence; the Continue prompt uses the daemon's `failureNote`).
Mutations (each named test must then fail): the hint mapping removed → `TestFailureHint_NewCodexInfos`; a class dropped from `IsHumanFix` → `TestFinish_NewClassesNeverRetry...`;
a label containing "approve" → `TestFailureLabels_NoApprovalWords`; `cli_exited` sentence reverted to the raw text → `TestDescribe_CLIExitedIsReadable`.

### PR split

- **R3a-1 (M, 5):** `internal/runs/failure` kinds, codes, classify, actions, `Describe`, guidance; `FailureHint`; the `finish` routing; wire fields; MCP/CLI text. No UI.
- **R3a-2 (M, 5):** web and mobile surfaces, inbox item, label-safety tests, frames (light and dark, 1280 and 390, every new card and the `cli_exited` card).

## R3b — Claude effort

### Acceptance Criteria

- AC8 `thinkingLevel` for claude-native accepts, besides `off|standard|extended`, the effort ids `low|medium|high|xhigh|max`; an effort id is sent as `--effort <id>` (`claudecode.WithEffort`) and
  leaves the thinking mode at the SDK default. `off` keeps meaning thinking off (no effort sent), so the invalid pair "xhigh with thinking off" cannot be expressed.
- AC9 Per-model gating through the existing `ResolveLevel`: the Claude catalog gains `Efforts` per model (D4). An explicit effort the model does not list is rejected naming the supported ones;
  an inherited one (profile pin, queued item, failover successor) is dropped with a note and never kills the run.
- AC10 A provider instance with a custom `ANTHROPIC_BASE_URL` offers only the safe subset (D5) because a gateway may reject the effort beta; an unknown instance model never sends an effort.
- AC11 The applied level is shown: smind reads `system/init.effort` from `SystemMessage.Raw` (no SDK change); the run header shows "effort <level>" and a downgrade (an organisation cap) is visible as a
  session note; when the CLI reports none the requested level is shown as "requested" (D6).
- AC12 Surfaces: composer selector (Claude shows both thinking words and effort ids, grouped), profile editor, CLI `--thinking`, MCP `task_send`, mobile composer; Codex and ACP behaviour unchanged.

### Test scenarios

`TestClaudeLevels_IncludeEffortIDs`, `TestResolveLevel_ClaudeEffortPerModel` (table: Fable/Opus/Sonnet/Haiku 5.x, aliases, unknown model, instance), `TestResolveLevel_ClaudeExplicitUnsupportedRejected`,
`TestResolveLevel_ClaudeInheritedDroppedWithNote`, `TestRunClaudeNative_EffortSendsFlag` (argv log), `TestRunClaudeNative_OffSendsNoEffort`, `TestRunClaudeNative_InstanceBaseURLOnlySafeSubset`,
`TestCatalog_ClaudeModelsCarryEfforts`, `TestProfile_ClaudeEffortPinSurvivesFailoverAsNote`, composer and profile editor tests, `runFooter` effort chip. Mutations: effort sent with `off` → `TestRunClaudeNative_OffSendsNoEffort`;
gating skipped for inherited levels → `TestResolveLevel_ClaudeInheritedDroppedWithNote`; instance subset removed → `TestRunClaudeNative_InstanceBaseURLOnlySafeSubset`.

### PR split

- **R3b-1 (M, 5):** Go: vocabulary, catalog efforts, `ResolveLevel`, runner flag, argv tests, wire.
- **R3b-2 (S, 3):** web and mobile selectors, profile editor, CLI help, frames.

## R3c — gate auto mode behind a custom base URL

### Acceptance Criteria

- AC13 For a claude-native provider instance whose spec env sets `ANTHROPIC_BASE_URL` (the detection `providers.go` uses to mark an instance's vendor generic; `applyRecipeFields` repeats it for recipe instances only), the catalog's `auto` mode is **not offered by default**; a
  run or profile naming it is rejected with a message ("auto mode needs Anthropic's own safety check, which an LLM gateway usually cannot answer; use default or acceptEdits, or pick it explicitly if your
  gateway forwards the check", D7). `bypassPermissions` is unchanged and is never offered as a substitute.
- AC14 The gate reads the instance's resolved spec, not the daemon's process env; the existing Bedrock/Vertex rule stays.
- AC15 An inherited `auto` (a profile pin, a queued item, a failover successor onto such an instance) is dropped to the instance's default mode with a visible note, never a silent upgrade to a more permissive mode.
- AC16 UI: the mode picker shows `auto` disabled with the one-line reason for such an instance (no hidden entry); the failure of an auto run on a gateway (a refused action with no safety check) is classified
  with the existing human-fix path, not as a generic error (text fixture from the CLI changelog).
- AC17 The opt-in (D7) is a per-instance human setting, stored with the instance and never settable by an agent or an MCP tool (ADR-0019 decision 6).

### Test scenarios

`TestClaudeModes_AutoGatedForBaseURLInstance`, `TestClaudeModes_AutoOfferedForDirectAnthropic`, `TestClaudeModes_BedrockVertexStillGated`, `TestValidatePermissionSettings_AutoRejectedOnGatewayInstance`,
`TestResolveMode_InheritedAutoDroppedWithNote`, `TestInstanceAutoOptIn_HumanOnly` (no MCP tool, no orchestrator source), `TestClassify_GatewayAutoRefusalIsHumanFix`, `mode-picker.test.tsx` (disabled with reason),
mobile picker test. Mutations: gate removed → `TestClaudeModes_AutoGatedForBaseURLInstance`; an agent-source opt-in accepted → `TestInstanceAutoOptIn_HumanOnly`; inherited auto kept → `TestResolveMode_InheritedAutoDroppedWithNote`.

### PR split

- **R3c-1 (M, 5):** Go gate, validation, opt-in field, failure text; high-risk second read (permissions).
- **R3c-2 (S, 3):** picker UI and frames.

## Decisions for the Human (each with a recommendation)

**Taken (2026-11-09, Supervisor):** D1-D9 as recommended, with this wording fix: the human sentence ships as **`action.summary`**, not `failureNote` (that name is already the MCP Lead routing note, `cmd/smind/failure_note.go`).

| # | Decision | Recommendation |
|---|---|---|
| D1 | `session_budget_exceeded` is **Codex's own** rollout budget, not smind's tree budget ("Set budget" is a different object). Link the action to "Set budget" anyway, or word it as the provider's budget? | **Word it as the provider's budget** (label: "Raise the session budget in Codex's settings, or start a fresh chat"), and keep "Set budget" for smind's own `budget_exceeded` launch refusal (a separate card). |
| D2 | `context_window_exceeded`: offer **compact** as an action, or only a fresh chat? Sending `/compact` through a headless Claude or Codex turn is not verified. | **Fresh chat only in v1**; add compact after a verified probe (Haiku, pre-approved) shows it works on each harness. |
| D3 | Claude class detection is text-only and unconfirmed. Ship fixtures from the changelog strings only, or hold the Claude half until a real run is captured? | **Ship Codex (typed) now; ship the Claude text patterns behind exact-match fixtures**, and add real captures when a run produces one (never guess a broader pattern). |
| D4 | Claude per-model effort table: the changelog names Opus 5.5, Sonnet 5.5, Haiku 5.5 and Fable 5.1 as supporting `xhigh`; the exact per-model set (and `max`, and `low`) is not documented in the refs. | **Static conservative table**: all five ids for those four models and their aliases, none for any other id; revisit with the CLI's reported applied level. |
| D5 | Instances with a custom base URL: allow effort at all? (a gateway may reject the effort beta; the CLI retries without it.) | **Allow only `low|medium|high`**, and drop silently-with-a-note if the CLI reports none applied. |
| D6 | Show the applied effort: the vendored SDK does not parse `system/init.effort`. Read it from `SystemMessage.Raw` in smind, or change the SDK? | **Read it from `Raw` in smind** (no SDK release); show "requested" when the CLI reports none. |
| D7 | Auto mode behind a gateway: **off by default with a per-instance human opt-in**, or hard-off? | **Off by default with an explicit per-instance opt-in** (some gateways do forward the check; `CLAUDE_CODE_AUTO_MODE_SERVER=0` is a CLI-side workaround we do not set for the user). |
| D8 | Put the human sentence on the wire as `action.summary` (named `failureNote` when drafted; that name is already the MCP routing note) (one table, clients stop keeping their own), or leave clients to word it? | **Wire it** (AC4): one source of truth, mobile and CLI get the same wording with no UI release. |
| D9 | Order of the PR chains. | **R3a-1, R3a-2, R3c-1, R3c-2, R3b-1, R3b-2** (the failure sentences and the auto gate fix user-visible confusion today; effort is new capability). |

## Progress

- 2026-11-09: spec drafted from the code and reference sources; no probes run.
- 2026-11-09: R3a-1 built (kinds, codes, classify, hints, actions, `Describe`, guidance, routing, inbox, CLI, MCP text). The wire carrier is `action.summary`. Not in R3a-1: web and mobile surfaces (R3a-2).
- 2026-11-09: R3a-2 built: web (`lib/failure-text.ts` one fallback table, failure cards and controls for the three classes and `cli_exited`, sidebar signal, footer sentence, Continue prompt from the daemon sentence, inbox policy link, child-event tone), mobile (`failureText.ts`, run footer, `run_failed` inbox kind), label-safety source scans on both, e2e frames. Follow-ups noted from the frames: the card and the footer repeat the same sentence for the human-action classes; the per-message toolbar still shows its generic retry icon on those failures.

## Validation

Pending implementation.
