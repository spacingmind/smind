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
- [ ] Item 2 — Per-provider approval levels (`full-access` tier x3).
- [ ] Item 3 — Thinking level (Claude composer control + GLM/Kimi live-view
      control).
- [ ] Hand off implementation via Paseo (GLM as primary implementer, Sonnet
      5 as fallback), per standing preference.
- [ ] Independent verification of agent-reported work (re-run Go tests +
      frontend typecheck/tests, read the actual diff) before merge.
- [ ] Rebuild (`task build:web && task build`), restart local daemon, dogfood
      in browser.

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
- Item 2: new Go unit tests per provider (see Test Scenarios) + manual
  dogfood run of a `full-access` task per provider confirming zero
  permission prompts.
- Item 3: new Go unit test for Claude's option mapping; new frontend test
  for the GLM live-view control; manual dogfood check that Claude's
  thinking-level selection visibly changes response latency/depth and that
  GLM's live control round-trips a real config change.
