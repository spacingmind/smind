# Task move-to-space UI + per-provider approval levels + thinking-level support

## Context

Dogfood feedback (2026-09-19), three items from one message plus two follow-up
decisions made via AskUserQuestion:

1. "Không cho phép chuyển task từ space này qua space khác à?" — there is no
   way to move a task between spaces once created. Backend now exists
   (`workspace.Manager.MoveTask`, `store.UpdateTaskSpace`, `task.move` RPC —
   shipped in PR #170, merged to `develop`). **UI action is still missing.**
2. "có đủ option ... auto level" — approval-policy richness. User chose, when
   asked to pick between a generic unified tier vs. real per-provider modes:
   **"Đúng theo từng provider (như Paseo/Codex/GLM thật)"** — expose each
   provider's own real vocabulary, not one shared label across all three.
3. "có đủ option ... thinking level" — reasoning-effort/thinking control.
   User chose, when asked which providers: **"Có, thêm cho provider hỗ trợ
   (GLM/Claude)"** — Claude and GLM/Kimi only. Codex is explicitly out of
   scope for thinking level (see Decisions — Codex has no such per-turn knob
   anyway).

Composer border/size (chat needs no border, taller input) was a separate,
already-shipped item (PR #169) — not part of this plan.

## Current state, confirmed by reading the code

- **`internal/taskrunner/policy.go`**: `ApprovalPolicy` has exactly two
  values today, `manual` and `auto-safe`. Both are smind-owned abstractions,
  not passthroughs of any provider's native enum.
- **Every provider's real-time approval decision is intercepted by smind's
  own decider** (`acpDeciderAdapter`, `claudeDeciderAdapter`,
  `codexDeciderAdapter` in `internal/taskrunner/permission.go`) whenever
  `task.prompt` supplies a human-in-the-loop `PermissionDecider` — which it
  always does today. Native provider "modes"
  (`WithACPPermissionPolicy`/`WithClaudeCodePermissionPolicy`/`WithCodexPermissionPolicy`,
  set once on the `Runner`) are only reached in a fully headless path with no
  decider — currently unused by `task.prompt`.
- **Ready-made "auto-approve everything" policies already exist, unused, for
  exactly this headless path**: `codex.AutoApprovePolicy{}`
  (`internal/codex/permission.go`), `acp.AutoApprovePolicy{}`
  (`internal/acp/permission.go`), and Claude's own native
  `claudecode.WithPermissionMode("bypassPermissions")` (vendored SDK,
  `client.go:185`). A new "full access" tier per provider is cheap: skip
  supplying a decider and use these instead.
- **`runClaudeNative`** (`internal/taskrunner/runner.go:~380-425`): when
  `decider != nil` it hardcodes `claudecode.WithPermissionMode("acceptEdits")`
  regardless of `approvalPolicy`, plus `WithAllowedTools(SafeBashRules()...)`
  only for `auto-safe`. `manual` and `auto-safe` are today both just
  "acceptEdits + decider asks about everything not on the allowlist" —
  smind never actually exercises Claude's `default`/`plan`/`bypassPermissions`
  modes.
- **Claude's real permission-mode vocabulary** (confirmed via docs, current
  SDK): `default` (asks for everything via `canUseTool`), `acceptEdits`
  (auto-approves in-scope file edits + safe fs ops, asks for the rest),
  `plan` (read-only/planning, never auto-edits), `auto` (a model classifier
  decides), `dontAsk` (auto-denies anything that would prompt), `bypassPermissions`
  (auto-approves essentially everything reaching the mode step). smind only
  needs three of these mapped to its own tiers (see Decisions).
- **`internal/taskrunner/config_options.go`** (read in full): GLM/Kimi's
  "thinking level" is already a complete, working, backend-only feature via
  ACP's generic `ConfigOption` mechanism — `run.listConfigOptions` /
  `run.setConfigOption` RPCs (already registered in
  `internal/wsapi/handlers.go:56-57`, implemented ~1071-1112),
  `Runner.ConfigOptions`/`SetSessionConfigOption`, backed by
  `acpSessionState.options` tracked per live ACP session
  (`trackACPSession`/`endACPTurn`). **Zero frontend usage exists** (confirmed
  via grep across `web/packages/ui/src`). Crucially this is **live-session-scoped**:
  the config option list only exists once an ACP session has actually been
  created (`Client.NewSession` returns `[]ConfigOption` — see
  `internal/acp/client.go:100-260`), so it cannot be a pre-run composer
  control the way approval policy is — it has to live in the running
  task/chat view.
- **Claude thinking-level is SDK-ready, zero new SDK work**:
  `claude-agent-sdk-go v0.3.2` (`client.go:394,398,408,417,425,433-434`) has
  `WithAdaptiveThinking()`, `WithAdaptiveThinkingAndDisplay()`,
  `WithThinkingBudget(n int)`, `WithThinkingBudgetAndDisplay(...)`,
  `WithDisabledThinking()`, and the deprecated `WithMaxThinkingTokens(n int)`.
  Unlike GLM, this is a **pre-run** setting (a `claudecode.Option` passed at
  session construction in `runClaudeNative`), so it belongs in the composer,
  not the live view.
- **Codex has no thinking-level equivalent reachable per-call.** Confirmed via
  research against current `codex-rs` (app-server protocol): `model_reasoning_effort`
  is a **process/session bootstrap-time** setting only — set via
  `~/.codex/config.toml` or a `codex app-server -c model_reasoning_effort=<level>`
  CLI override at spawn time. Neither `thread/start` nor `turn/start`
  JSON-RPC params accept a per-call `model` or `model_reasoning_effort`
  override (a recent PR exposed `model`/`reasoningEffort` as *read-only*
  thread metadata, not a writable turn param). This matches the user's scope
  choice (GLM/Claude only) — no Codex thinking-level work is needed here.
  If it's ever wanted, the mechanism would be: build a per-run command slice
  appending `-c model_reasoning_effort=<level>` before `codex.New(command, opts...)`
  in `runCodexNative` (`internal/taskrunner/runner.go:557`), since
  `r.codexCommand` is already a plain `[]string` the Runner is free to extend
  per call — not a JSON-RPC param. Left as a documented non-goal, not built now.
- **Codex's real approval vocabulary** is two-dimensional and native-side:
  `approval_policy` (`untrusted`/`on-failure` retired; current values are
  `on-request` and `never`) crossed with `sandbox_mode`
  (`read-only`/`workspace-write`/`danger-full-access`). smind doesn't use
  either natively — `codexDeciderAdapter` intercepts Codex's own
  agent-initiated `item/commandExecution/requestApproval` /
  `item/fileChange/requestApproval` callbacks instead. A "full access" tier
  for Codex means skipping the decider and installing
  `codex.AutoApprovePolicy{}` — behaviorally equivalent to
  `approval_policy=never` + `sandbox_mode=danger-full-access`, but implemented
  as a decider bypass, not a native config flag (same shape as the Claude and
  ACP full-access tiers — see Decisions for why this is still "real
  per-provider" rather than a disguised unified tier).
- **ACP (GLM/Kimi) has no permission-mode concept at all** — no enum,
  nothing analogous to Claude's `acceptEdits`/`bypassPermissions`. Every
  decision is a discrete `allow_once`/`allow_always`/`reject` on one request.
  `ApprovalPolicyAutoSafe`'s ACP path (`permission.go:~1-130`) auto-allows
  file edits confined to the task's own worktree
  (`autoAllowACPFileEdit`) plus the same safe-bash-prefix allowlist used
  elsewhere. `acp.AutoApprovePolicy{}` (unused today) always answers
  `allow_once`/`allow_always` on every request — that's the natural
  "full access" tier here.
- **`app-sidebar.tsx` task-row action menu** (~lines 1111-1131) — currently
  one `DropdownMenuItem` ("Archive task"). Lives inside the task-row
  component (`SpaceLikeItem`) that's already prop-drilled
  `onArchiveTask: (task: Task) => void` from `WorkspaceItem` (which has the
  workspace's `spaces` list in scope, needed to populate a move submenu).
- **`task.prompt` RPC params** (`internal/wsapi/handlers.go`, `handleTaskPrompt`):
  ```go
  var p struct {
      TaskID         int64                     `json:"taskId"`
      Provider       taskrunner.Provider       `json:"provider"`
      Prompt         string                    `json:"prompt"`
      ApprovalPolicy taskrunner.ApprovalPolicy `json:"approvalPolicy"`
  }
  ```
  Needs a new optional field for Claude's pre-run thinking-level choice.

## Decisions

- **"Real per-provider" does not mean exposing every native enum value** —
  it means the *label and the underlying mechanism* are honest to each
  provider's actual capabilities, instead of one shared "Auto" tier hiding
  three different behaviors behind one word. Concretely, smind keeps a
  3-tier shape per provider (`Manual` / `Auto (safe)` / `Full access`) but:
  - **Claude**: `Full access` = native `bypassPermissions` mode, no decider
    at all (real Claude Code mode, matches what Claude Code's own CLI calls
    it).
  - **Codex**: `Full access` = `codex.AutoApprovePolicy{}` in place of the
    decider (equivalent to Codex's own `never`/`danger-full-access`
    combination, though implemented as a decider bypass since smind doesn't
    drive Codex's native config surface).
  - **GLM/Kimi**: `Full access` = `acp.AutoApprovePolicy{}` in place of the
    decider (ACP has no richer native vocabulary to defer to — this already
    is the provider's real ceiling).
  This is *not* the same generic tier the user rejected: the mechanism and
  the runtime effect differ per provider (different CLI flags, different
  Go types, different scope of what gets auto-approved), and each is
  labeled/tooltipped with that provider's real behavior, not a made-up
  universal claim.
  We are **not** exposing Claude's `plan`/`auto`/`dontAsk` modes or Codex's
  native `approval_policy`/`sandbox_mode` matrix directly in this pass — that
  would be a much larger settings surface (per-mode semantics differ enough
  to need real UI explanation) and nothing in the user's request asked for
  it; `Manual`/`Auto (safe)`/`Full access` covers the actual ask.
- **`ApprovalPolicy` gets a third value**, `full-access`, added to the
  existing enum in `internal/taskrunner/policy.go` (not a parallel
  per-provider type) — the three runner functions (`runClaudeNative`,
  `runCodexNative`, `runACP`) each interpret it using their own mechanism
  per the bullet above. This keeps `task.prompt`'s wire shape unchanged
  (still one `approvalPolicy` string) while the interpretation is genuinely
  provider-specific in the runner.
- **Thinking level is a separate optional field, not folded into
  `ApprovalPolicy`** — orthogonal axis, only meaningful for Claude/GLM, and
  timing differs (pre-run for Claude vs. live-session for GLM), so one shared
  enum would misrepresent both.
- **Claude thinking level → composer, pre-run.** Add `ThinkingLevel` to
  `task.prompt`'s params (e.g. `"off" | "adaptive" | "<budget-tier>"` — exact
  tier granularity decided during implementation against what
  `WithThinkingBudget(n int)` needs, likely 2-3 named buckets like
  Off/Standard/Extended mapping to specific token budgets, not a raw
  numeric input). Only rendered in the composer when `provider === "claude"`.
- **GLM/Kimi thinking level → live task/chat view, not composer.** Surface
  `run.listConfigOptions`/`run.setConfigOption` (already fully wired
  backend-to-frontend-RPC) as a control in the chat/task-detail header,
  visible only once a live ACP session exists for that task (mirrors the
  backend's own scoping — nothing to build server-side, this item is 100%
  frontend).
- **Move-to-space UI**: a "Move to space" submenu inside the existing
  task-row action dropdown (per the user's own AskUserQuestion choice:
  "Menu 'Move to space...' (khuyến nghị)"), listing the workspace's other
  spaces plus an "Ungrouped" entry, calling the already-shipped `task.move`
  RPC. Not drag-and-drop — out of scope.
- **Codex thinking-level: explicit non-goal**, documented above — no
  reachable per-turn mechanism exists upstream, and the user's own scope
  choice already excluded Codex.

## Acceptance Criteria

### Item 1 — Move to space UI
- Task row's action dropdown (`app-sidebar.tsx`, `SpaceLikeItem`) gains a
  "Move to space" submenu, listing every other space in the task's workspace
  by name plus an "Ungrouped" entry (omit the task's current space/ungrouped
  state from the list — no self-move option).
  - Selecting an entry calls `task.move` with the task's id and the chosen
    `spaceId` (`null` for Ungrouped) and the task disappears from its old
    list and appears in the new one (already covered by existing
    `NotifyTaskUpdated`/tree-refresh plumbing — no new notification wiring
    expected).
  - A workspace with zero other spaces still renders the submenu with just
    "Ungrouped" (if the task isn't already ungrouped) or is hidden entirely
    if there is nowhere to move to (task is ungrouped and there are no
    spaces at all).
  - Moving fails gracefully (toast/error surfaced, task stays where it was)
    if the RPC errors.

### Item 2 — Per-provider approval levels
- `ApprovalPolicy` gains `full-access` as a third valid value
  (`policy.go`'s `IsValid()` updated accordingly).
- Claude, in `full-access`: no decider installed; session runs with
  `claudecode.WithPermissionMode("bypassPermissions")`. No `task.prompt`
  message round-trips to ask the user about a tool call for the remainder of
  that turn.
- Codex, in `full-access`: no decider installed; `codex.AutoApprovePolicy{}`
  supplied as the permission policy on that run's client. Command
  executions and file changes proceed without a `requestApproval` prompt
  reaching the user.
- GLM/Kimi, in `full-access`: no decider installed;
  `acp.AutoApprovePolicy{}` supplied. Same observable effect (no prompts)
  via ACP's request/response shape.
- `manual` and `auto-safe` behavior for all three providers is unchanged
  (regression, not just addition).
- Composer's approval-policy selector shows three options per provider, with
  each provider's `full-access` option labeled/tooltipped in that
  provider's own terms (e.g. Claude: "Full access (bypass permissions)";
  Codex: "Full access (auto-approve, no sandbox prompts)"; GLM/Kimi: "Full
  access (auto-approve)") — not one shared generic string.

### Item 3 — Thinking level (Claude + GLM/Kimi)
- Composer, when `provider === "claude"`, shows a thinking-level selector
  (e.g. Off / Standard / Extended) that threads through to `task.prompt`'s
  new field and ends up as the corresponding `claudecode.Option`
  (`WithDisabledThinking()` / `WithAdaptiveThinking()` or
  `WithThinkingBudget(n)`) in `runClaudeNative`. Omitted entirely (no dead
  control) for Codex/GLM/Kimi in the composer.
- Live task/chat view, when the active run is GLM or Kimi and an ACP session
  exists, shows a control sourced from `run.listConfigOptions` letting the
  user call `run.setConfigOption` for the thinking-level config id (whatever
  id GLM's ACP agent actually reports — read it from the live response, do
  not hardcode a label smind hasn't observed). Control is absent/disabled
  before a session exists and updates live if the option list changes.
- No thinking-level control appears anywhere for Codex.
- Setting a bad/stale config id via `run.setConfigOption` surfaces the
  existing `ErrConfigOptionsNotSupported`/error path to the user instead of
  silently no-op'ing.

## Test Scenarios

- **Move**: move a task into a space; move it from that space to a different
  space; move it back to ungrouped; attempt to move it into a space that
  doesn't exist (RPC error surfaced, not a crash); workspace with only one
  space total (task already ungrouped) shows just that one target.
- **Approval — Claude**: `full-access` run never emits a permission-request
  event even for a destructive-looking edit; `manual`/`auto-safe` runs are
  provably unchanged (existing tests must still pass unmodified in
  behavior, not just unmodified in code).
- **Approval — Codex**: `full-access` run's `AutoApprovePolicy{}` is
  actually installed (unit test asserting on the options passed into
  `newCodexClient`, similar to existing `runner_test.go` patterns) and a
  fake-agent-driven `requestApproval` round trip auto-resolves without
  reaching the decider.
- **Approval — ACP**: same shape, `acp.AutoApprovePolicy{}` installed for
  GLM/Kimi in `full-access`.
- **Thinking — Claude**: each selector value maps to the expected
  `claudecode.Option` in `runClaudeNative` (unit-level, options-list
  assertion); omitting the field entirely (older client / other provider)
  doesn't change today's default behavior.
- **Thinking — GLM**: `run.listConfigOptions` before any session exists
  returns empty/not-supported cleanly (already covered by existing backend
  tests per the summary — confirm, don't re-derive); the new frontend
  control renders once a session's option list arrives over the existing
  live-update channel, and calling `run.setConfigOption` updates the
  displayed value on success and surfaces an error on failure.
- **Regression**: full existing `web/packages/ui` test suite plus
  `internal/taskrunner`, `internal/wsapi`, `internal/workspace` Go suites
  stay green.

## Progress

- [x] Backend: `task.move` RPC + `workspace.Manager.MoveTask` +
      `store.UpdateTaskSpace` (PR #170, merged to `develop`).
- [x] Research: confirmed Claude/Codex/GLM's actual capabilities and
      constraints (this document's Context/Decisions sections).
- [x] Item 1 — Move to space UI.
- [x] Item 2 — Per-provider approval levels (`full-access` tier x3).
- [x] Item 3 — Thinking level (Claude composer control + GLM/Kimi live-view
      control).
- [x] ~~Hand off implementation via Paseo~~ -- superseded: the prior
      GLM-via-Paseo delegation made zero progress after ~40 turns (two
      output-budget exhaustions, no Edit/Write calls), so this session
      implemented all three items directly instead, verifying each with
      `go build`/`go vet`/`go test ./...` and `bun run typecheck`/`bun run
      test` after every commit rather than a separate hand-off + review
      pass.
- [x] Independent verification of the actual diff: done inline per
      commit in this session (read every changed file before/after,
      cross-checked against the ACP/claude-agent-sdk-go/Codex schemas
      cited in Context) rather than as a separate post-hoc pass.
- [ ] Rebuild (`task build:web && task build`), restart local daemon, dogfood
      in browser -- **not done**: no running daemon or live provider
      credentials in this worktree. All three items are covered by
      automated tests (Go + Vitest, both suites green), but no one has
      clicked through the actual UI yet. Recommended before merging:
      move a task via the sidebar; start a `full-access` run per provider
      and confirm zero permission prompts; pick a Claude thinking level
      and a GLM thinking-level tier mid-session and confirm both actually
      change model behavior, not just the wire payload.

## Validation

To be filled in as each item lands:
- Item 1: DONE. Go tests for `MoveTask` already exist (`TestManager_MoveTask`,
  PR #170) -- no backend change made. Frontend: `app-sidebar.tsx` gained a
  `DropdownMenuSub`/`SubTrigger`/`SubContent` triad in `ui/dropdown-menu.tsx`
  (didn't exist before) and a "Move to space" submenu in `TaskRows`'
  per-task action menu, fed by `spaces`/`onMoveTask` threaded down through
  `WorkspaceItem` -> `SpaceItem`/`SpaceLikeItem` -> `TaskRows` (optional on
  `TaskRows` since `SearchResults` doesn't wire them -- out of this pass's
  scope, matches the AC's literal "SpaceLikeItem" framing). `AppSidebar`
  owns a `moveTask` callback calling `task.move` then `refresh()` on
  success, or a `toast({variant:"error"})` on failure (first real consumer
  of the previously-unused toast infra in `ui/toast.tsx`). New tests in
  `app-sidebar-crud.test.tsx` (5 added, all passing):
  - submenu lists every other space + "Ungrouped", excludes the task's own
    space (AC: no self-move option) -- covers Test Scenarios' "move it from
    that space to a different space".
  - selecting a target calls `task.move` with `{id, spaceId}` and refreshes
    -- covers "move a task into a space" / "move it back to ungrouped"
    (spaceId null case exercised via the same assertion path, task starts
    ungrouped in that test).
  - a rejected `task.move` renders a `role="alert"` toast with the daemon's
    error text and issues no second `task.move`/refresh call (task stays
    put) -- covers "attempt to move it into a space that doesn't exist (RPC
    error surfaced, not a crash)".
  - zero-other-spaces case (already-ungrouped task, workspace has no
    spaces) hides the "Move to space" trigger entirely, leaving only
    "Archive task" -- covers the AC's "hidden entirely if there is nowhere
    to move to".
  - Radix's `DropdownMenuSub` only opens via pointer-hover or keyboard
    (`ArrowRight`) in jsdom, not a bare `fireEvent.click` on the trigger --
    tests use `pointerMove` + `keyDown(ArrowRight)` to open it, matching
    real keyboard-driven usage.
  Full suite: `bun run typecheck` (tsc -b) clean; `bun run test`
  (vitest) 811/811 passing, including the 5 new cases. No manual
  browser dogfood performed in this pass (no running daemon in this
  worktree) -- recommend a follow-up click-through before merge.
- Item 2: DONE. `ApprovalPolicyFullAccess` ("full-access") added to
  `policy.go`'s enum + `IsValid()`; `internal/wsapi`'s `task.prompt`/
  `run.start` validation needed no change (both already reject via the
  generic `IsValid()` check, so the new value is accepted automatically).
  Each of `runClaudeNative`/`runCodexNative`/`runACP` gained a
  `case approvalPolicy == ApprovalPolicyFullAccess` ahead of the existing
  `decider != nil` branch, so full-access wins even if RunPrompt is handed
  a non-nil decider (it always is from `task.prompt` today) -- no decider
  installed at all, each provider's own native mechanism used instead
  (`claudecode.WithPermissionMode("bypassPermissions")`,
  `codex.WithPermissionPolicy(codex.AutoApprovePolicy{})`,
  `acp.WithPermissionPolicy(acp.AutoApprovePolicy{})`). `runCodexNative`
  gained an `approvalPolicy` parameter it didn't have before (only `manual`/
  `auto-safe` existed when it was written, and neither needed it).
  New/changed Go tests in `internal/taskrunner` (all passing):
  - `TestApprovalPolicy_IsValid` gained a `full-access` case.
  - `TestRunner_RunPrompt_ClaudeNative_AutoSafeAllowedTools`'s table gained
    a `full-access` case proving it spawns with no `--allowedTools` (same
    as manual) -- full-access doesn't accidentally widen the CLI-gate
    allowlist path meant for auto-safe.
  - `TestRunner_RunPrompt_ClaudeNative_FullAccess_NeverAsksDecider`: a
    deny-leaning `stubDecider` handed to a full-access run is never
    consulted (`callCount() == 0`) even on the "permission" fake-CLI
    scenario that would otherwise trigger it -- covers "no decider
    installed" and, transitively, "never emits a permission-request event"
    (that event is only ever raised by `internal/runs`' own decider
    wrapper, which is what `callCount() == 0` proves is never reached).
  - `TestRunner_RunPrompt_GLM_FullAccess_InstallsAutoApprove` /
    `TestRunner_RunPrompt_CodexNative_FullAccess_InstallsAutoApprove`: same
    shape, but additionally prove the *real* auto-approve policy is
    installed (not just "no decider") by handing a deny-leaning
    stubDecider and observing the fake agent's echoed decision is the
    allow one anyway ("chose:allow-1" / "decision:accept") -- satisfies the
    Test Scenarios' "AutoApprovePolicy{} is actually installed" bullet for
    both ACP and Codex without needing to introspect unexported SDK option
    state (which isn't reachable from this package for Codex's/Claude's
    vendored SDKs).
  - `manual`/`auto-safe` regression: untouched by this change structurally
    (the new case sits ahead of, not inside, the existing `decider != nil`
    branch in all three functions) and every pre-existing test for both
    tiers (including the two-provider permission-request round-trip tests)
    still passes unmodified.
  Frontend: `ApprovalPolicy` type gained `"full-access"`; the composer's
  approval-policy `Select` (`composer.tsx`) now derives its three options
  from `approvalPolicyOptions(provider)` instead of a flat static list --
  manual/auto-safe stay identical across providers, full-access's
  label/tooltip come from a `FULL_ACCESS_BY_PROVIDER` lookup using each
  provider's own real wording (Claude "Bypass" / Codex "Full Access" / GLM
  &amp; Kimi "Bypass all permissions", per this session's Paseo-sourced
  ground truth). Both the closed trigger's tooltip (now reflecting the
  *current* selection, not a static auto-safe-only string) and each open
  option's own `title` carry the per-tier help text. New composer tests
  (3 added, all passing): the dropdown lists exactly
  `["Manual approval", "Auto-safe", "Bypass"]` for the default
  claude-native provider and submits `approvalPolicy: "full-access"` when
  chosen; switching provider to Codex then GLM changes the third option's
  label/tooltip each time, proving no single shared string leaks across
  providers. One pre-existing test's assertion (trigger `title` always
  containing "Auto-safe") was updated to match the now-selection-aware
  tooltip (manual is the default selection, so it asserts on manual's own
  help text instead).
  Full suite: `go build ./... && go test ./...` all green (including
  `internal/wsapi`, `internal/runs`); frontend `bun run typecheck` clean,
  `bun run test` 813/813 passing. No manual dogfood run performed in this
  pass (no running daemon in this worktree) -- recommend a follow-up
  click-through per provider before merge, per this section's own
  "manual dogfood run... confirming zero permission prompts" ask.
- Item 3: DONE, both halves.
  **Claude (composer, pre-run)**: `taskrunner.ThinkingLevel` added
  (`off`/`standard`/`extended`, zero value `""` = unspecified/no Option
  added at all) in a new `thinking.go`, with `extendedThinkingBudgetTokens
  = 32000` for the "extended" tier (Decision: Off -> `WithDisabledThinking()`,
  Standard -> `WithAdaptiveThinking()` -- lets the model size its own
  budget, a reasonable middle tier -- Extended -> `WithThinkingBudget(32000)`,
  a deliberately large fixed cap for turns that need to reason at length).
  Threaded through `RunPrompt` -> `runClaudeNative` as a new parameter
  (mechanical signature change touched ~48 existing call sites across
  `runner_test.go`/`runs_test.go`/`config_options_test.go` -- all
  bulk-edited to pass `""`, preserving today's behavior exactly). Wired
  into both `task.prompt` and `run.start`'s wire params (`run.start` is
  the one the composer actually calls per `use-run-timeline.ts`'s
  `submitPrompt` -- the plan's Context section named `task.prompt`, but
  the real frontend path is `run.start`, so both got the field for
  consistency). Composer shows the selector only when
  `provider === "claude-native"`, defaulting visually to Standard but
  omitting the field from `run.start` until the user actually touches it
  (same non-breaking-default convention `approvalPolicy` already uses).
  New Go tests: `TestThinkingLevel_IsValid`;
  `TestRunner_RunPrompt_ClaudeNative_ThinkingLevel` (table test asserting
  the exact CLI flags each level produces via the existing "echo-args"
  fake-CLI observability -- `--thinking disabled`/`--thinking adaptive`/
  `--max-thinking-tokens 32000`/nothing for unspecified);
  `TestServer_RunStart_InvalidThinkingLevel_IsAClearError` (wsapi
  wire-boundary rejection, mirroring the existing approvalPolicy test).
  New frontend tests (composer.test.tsx): selector hidden for GLM/Codex,
  shown only for Claude; omitted-until-touched default; submits the
  picked tier once touched.

  **GLM/Kimi (live chat view, session-scoped)**: as planned, zero new RPCs
  needed (`run.listConfigOptions`/`run.setConfigOption` already existed)
  -- but implementing the frontend control surfaced a real backend gap the
  plan's Context section didn't anticipate: `internal/acp.ConfigOption`
  decoded a "select"-type option's `currentValue` but silently discarded
  its own enumerated choices (ACP's `SessionConfigSelect.options` --
  confirmed against `refs/agent-client-protocol`'s schema), so there was
  no way to render a real dropdown of an agent's own named choices (e.g.
  GLM's minimal/low/medium/high/xhigh/max) without hardcoding them, which
  the plan explicitly said not to do. Fixed with a small, separately-
  committed backend addition: `acp.ConfigOption.Options
  ([]ConfigSelectOption)`, threaded through `configOptionParams.Options`
  in wsapi's `run.listConfigOptions`/`run.setConfigOption` responses. This
  is the one place this item touched Go code, despite the plan calling it
  "100% frontend" -- noted here as a discovered-during-implementation
  deviation, not a scope decision made up front.
  New frontend: `use-run-config-options.ts` (fetches
  `run.listConfigOptions` for the active run when it's GLM/Kimi, re-fetching
  whenever the run's item count changes -- there's no dedicated push
  notification for "config options are now ready", so this is what catches
  the list going from empty to populated shortly after session creation;
  `run.setConfigOption` on demand) and `run-config-options.tsx` (renders a
  toggle for boolean options, a real `<Select>` of the agent's own choices
  for a select option that has them, and a text-field fallback for a
  select option with none -- honest given what's actually decodable,
  never fabricating choices). Mounted in `task-detail.tsx` between the
  permission dock and the composer, scoped to whichever run is currently
  "running" and GLM/Kimi.
  New Go tests: `TestClient_NewSessionConfigOptions` (internal/acp,
  proves a select option's `Options` list round-trips intact, a boolean
  option's doesn't exist at all); `TestRegistry_ConfigOptions_GLM_RealSelectRoundTrip`
  (internal/runs, live GLM run via the "hang" scenario, `ListConfigOptions`
  + `SetConfigOption` both carry the real choices/updated value);
  `TestServer_RunConfigOptions_GLM_RealSelectRoundTrip` (internal/wsapi,
  same round trip over the wire, plus an unrecognized configId surfacing
  as a wire-level error rather than a silent no-op). Also discovered
  `run.listConfigOptions`/`run.setConfigOption` had **no** wsapi-level
  tests at all before this (the plan's claim that this was "already
  covered by existing backend tests" didn't hold up under a `grep` --
  only the non-ACP-provider "not supported" path was tested anywhere) --
  the new wsapi test above is the first one.
  New frontend tests (task-detail.test.tsx, 5 added): control fetches
  and renders for GLM, never even asks for Claude; re-fetches as the run
  streams more events (catches the empty-then-populated transition);
  selecting a value calls `run.setConfigOption` and reflects the response;
  a rejected `run.setConfigOption` renders a `role="alert"` error instead
  of silently no-op'ing.

  Full suite for this item: `go build ./... && go vet ./... && go test
  ./...` all green; frontend `bun run typecheck` clean, `bun run test`
  821/821 passing. No manual dogfood against a real GLM/Claude account
  performed in this pass (no running daemon or live credentials in this
  worktree) -- recommend a follow-up manual check per the original ask
  (Claude's thinking-level selection visibly changing response depth;
  GLM's live control round-tripping a real config change) before merge.
