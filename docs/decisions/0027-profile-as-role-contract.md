# 0027: Profile as role contract

## Status

**Proposed, docs-only** (2026-10-11). Nothing in this ADR is built and
nothing is scheduled. The user decided that the extensions below will be
implemented later, and only if real use shows that **profile `notes` plus
[`docs/WORKSPACE_PROTOCOL.md`](../WORKSPACE_PROTOCOL.md) are not enough**.
`WORKSPACE_PROTOCOL.md` is on `develop` (#290); the Paseo stopgap (five
profiles, two Paseo-tool-less Peer providers, a supervisor heartbeat) runs
on it today. Until that evidence exists this ADR is a design to pick up,
not a commitment. The numbered design below says what the extension would
be; the "Decisions for the user" list in the plan is what to answer first.

**D13 accepted and built (2026-10-18):** the derived `needs-continue` disposition of section 4, as recommended there and not as a new
run status: `runs.DispositionFor(status, stopReason)`, additive on `run.list`, `run.logs`, `run.status`, MCP `task_status`/`task_wait`/
`task_logs`/`run_list`, `smind task runs` and the web run footer (orchestration-parity plan, decision D13). Everything else in this ADR
(the rest of the dispositions, `handoff_submit`/`handoff_review`, tiers, sizing) remains Proposed as above.

Spec: [`docs/plans/active/orchestration-roles.md`](../plans/active/orchestration-roles.md).

## Context

The Supervisor → Lead → Peer workflow needs five things from a profile that
a smind profile (ADR-0014/0019/0025: provider, permission mode, thinking,
model, notes) does not carry. Paseo's profile does not carry them either, so
the stopgap enforces each by prompt or by a provider switch:

| Need | Paseo stopgap | smind today |
|---|---|---|
| Role instructions | the agent reads `WORKSPACE_PROTOCOL.md` because the brief says to | same; ADR-0014 excluded `systemPrompt` |
| Tool policy | `paseoTools.enabled: false` on the `glm-peer` / `claude-s-peer` providers | `smind mcp serve` is added by hand (ADR-0017/0018, `smind` skill); no per-profile policy |
| Allowed children | the Lead's notes say "Peer profiles only" | none: `task_send` accepts any `profileId` or raw provider |
| Limits | "20-turn limit" in a note | `agents.glm.maxTurns` (daemon-wide, #273), `orchestration.maxDepth`, the per-tree concurrency guard (#289); no per-profile turn or cost cap |
| Outcome | prose handoff; `REOPEN_REQUEST` / `DEPENDENCY_REQUEST` / `BLOCKED` are words in a message | `done` plus `stopReason`; a GLM `max_turn_requests` stop reads as `done` (orchestration-parity D-G5/D13) |

Two facts from the code shape the design:

- **The daemon does not know a run's profile.** `profileId` is resolved in
  the `smind mcp serve` process (`cmd/smind/mcp_task_send.go`) and copied
  into per-run fields; no `runs.profile_id` exists. ADR-0014 chose
  client-side copy on purpose.
- **Caller identity is self-reported.** `source` and `fromTaskId` on
  `run.start` are asserted by the client, and anything holding the daemon
  token is treated as the human (ADR-0024 states the same trust line).

## Decision (proposed)

A profile may carry an optional **contract**. A profile without one behaves
exactly as today. All contract fields are **human-authored only**
(ADR-0019 #6): `profile.create/update/delete` reject run-scoped callers (item
3), and no MCP tool writes profiles.

### 1. Contract fields (additive columns on `agent_profiles`)

| Field | Meaning |
|---|---|
| `instructions` | text (capped, 16 KiB) plus `skills`: names of bundled skills (#279) whose bodies are appended, capped 32 KiB total. The developer-instructions equivalent |
| `orchestrationTools` | bool. Whether the run gets the built-in `smind` MCP server (the orchestration catalog). Smind's `paseoTools.enabled` |
| `childPolicy` | `none` (default), `leaf` (any profile whose own `orchestrationTools` is false) or `profiles` plus `childProfileIds`. `orchestrationTools=false` forces `none` |
| `maxTurns` | per run |
| `budgetUsd`, `budgetTokens` | per run, from ADR-0020 usage |
| `subtreeBudgetUsd`, `maxConcurrentRuns` | across the descendants of the task this run belongs to; concurrency reuses the #289 tree-guard critical section, rooted at the caller's task instead of the tree root |

There is no stored `role` field. The tier label is **derived** from the
contract: `childPolicy=none` is a peer, spawns only leaves is a lead, can
spawn a lead is a supervisor. A task launched with a profile shows its
contract tier. A task without one falls back to the structural tier of the
task-hierarchy plan (`task-hierarchy-tiers.md`, PR #286, owned by another
Lead), which has the known gap that a supervisor living outside smind makes
its leaders look like supervisors. Coordinate: that plan keeps
`TierOf`; this ADR only adds an optional contract input to it, after its
PR-T lands.

### 2. Delivery per provider

Re-applied on every spawn, since smind starts one agent process per run.
That answers ADR-0014's "creation-only" objection to `systemPrompt`.

| Provider | Instructions | Tool policy |
|---|---|---|
| claude-native | `WithAppendSystemPrompt` | the daemon builds `--mcp-config`; with `orchestrationTools=false` it adds `WithStrictMCPConfig` and no `smind` server |
| ACP (GLM, Kimi) | a leading text block on the first prompt of a new session; re-sent with a marker when the instructions hash changed since that session's handle | the `smind` server is simply not in `session/new` `mcpServers` |
| codex-native | `developerInstructions` on `thread/start` and `thread/resume` | **not enforceable** (process-wide MCP config, ADR-0018 deferral): the daemon-side check in item 3 is the only gate |

The persisted `runs.prompt` and the transcript stay the user's text.
Instructions are **not** an edit of a repo or `~/.claude` file. A native
skill delivery (read-only projection plus `WithAddDirs`) is a later
optimization that would share the self-learning projector (ADR-0026).

### 3. Enforcement points

1. **Daemon resolves the profile.** `run.start`/`task.prompt` accept
   `profileId`; the daemon stores `runs.profile_id` and applies the contract.
   Contract fields cannot be overridden by the caller. `model` and `thinking`
   follow ADR-0025 (a call value wins) unless the *caller's* contract forbids
   overrides (below). Plain profiles keep today's copy semantics.
2. **Run-scoped credential.** For a run whose profile has
   `orchestrationTools=true`, the daemon mints a token bound to
   `{runId, profileId, taskId}`, valid until the run ends, and injects it in
   the env of the built-in `smind` MCP entry it builds itself. `smind mcp
   serve` presents it on dial. A connection with a run token gets a
   **fail-closed method allowlist** (the orchestration catalog's wsapi
   methods), so it cannot call `profile.*` writes, `mcp.*`, `workspace.setScripts`
   or `account.*`.
3. **Admission on `task.create`, `task.prompt`/`run.start`, `chat.create` from a
   run-scoped caller:**
   - `parentTaskId` is forced to the caller's task, and `source` to
     `agent`; self-reported values are ignored;
   - the caller must pass `profileId`, and it must satisfy the caller's
     `childPolicy`; a raw `provider`, `permissionMode` or `model` override is
     rejected (the ADR-0019 #6 pattern, applied to spawning);
   - the target task must be in the caller's own subtree (a Lead cannot act
     on a sibling Lead's tasks);
   - limits: `maxConcurrentRuns` and `subtreeBudget*` reject new runs with a
     model-readable error (like `TreeBusyError`). A run-level `maxTurns`
     goes to the provider flag (`WithMaxTurns`; the GLM command per run);
     a run budget goes to `WithMaxBudgetUSD` for Claude and to a daemon
     stop at the next usage event otherwise, ending the run with
     `endReason=budget_exceeded`.
4. `profile_list` for a run-scoped caller returns only its allowed children.

Honest limit: this is a guard against agent drift and mistakes, not a
sandbox. A Peer with a shell can read the daemon token file and speak the
wsapi as a human. The real boundary there is the provider's own
permission mode or sandbox (ADR-0019), exactly as in the Paseo stopgap.
Unenforceable limits are reported, never faked: `maxTurns` on Codex, or a
budget on a provider that reports no usage (ADR-0020: never estimated), are
shown as "not enforced" on the profile.

### 4. Structured brief, handoff and signals

The protocol's Peer handoff and `REOPEN_REQUEST` / `DEPENDENCY_REQUEST` /
`BLOCKED` become data, so "done" cannot hide a stop:

- **`endReason` / `disposition`**, derived by the daemon from the stop
  reason and the run state, additive on `run.list`, `task_status` and the
  `task_wait` result: `needs_continue` (turn limit, token limit or
  `budget_exceeded`, overriding any claim in the transcript), `unreported`
  (a contract run ended normally with no handoff), `delivered`,
  `reopen_request`, `dependency_request`, `blocked`, `failed`, `stopped`.
  **Recommendation for D13: this, not a new terminal run status.** A new
  status ripples through `run.status`, the queue's "finished" trigger, the
  CLI and the UI; a derived field does not, and `task_wait` still returns
  on it.
- **`handoff_submit`** (tool, given to every contract run, including Peers;
  it is a reporting tool, not orchestration). Fields: `outcome`, `summary`,
  `candidate{commit, base, changedPaths}`, `verification{verified, untested,
  failed, unknown}`, `residualRisk`, `ownership`, and for the three signals
  `evidence`, `consequence`, `decisionNeeded`. The daemon checks that the
  commit exists in the task worktree and that the changed paths match git.
  Stored in `run_handoffs`. Codex has no per-thread MCP, so it gets only the
  derived disposition (a fenced final-message block is a later fallback).
- **`handoff_review`** (Lead tool): `accepted | rejected` plus a reason, per
  run, so "silence or DONE is not a disposition" is visible to the
  Supervisor and in the tree. It reviews work; it has no permission effect.
  ADR-0017 #1 stands: no tool whose name matches `approve|deny|respond`.
- Optional `task_send` **`brief`** (outcome, write scope, invariants,
  acceptance, checks, reopen-when), rendered into the prompt in a fixed
  template; overlapping live write scopes in one subtree produce a warning.

### 5. Sizing

`tasks.size` (nullable points, the protocol's five axes) is an optional
field. It never gates anything. It is shown in the UI, and `profile_list
{size}` ranks profiles whose `suggestedFor` tags fit it, plus a suggested
`subtreeBudgetUsd`. A human or the Supervisor sets it.

### 6. Interaction with self-learning (ADR-0026)

The contract's instructions and skills are the **human base layer**:
mandatory, never counted against probation or capacity, never edited by the
reflector or curator. Learned skills are a lower layer, delivered after it.
Proposal: a learned skill is **scoped to a tier**, defaulting to the tier of
the contract of the run it was learned from, and projection filters by the
receiving run's tier. A Peer's "run wsapi tests with `-race`" does not
reach a Lead, and a Lead's "do not stack PRs" does not reach a Peer. The
gate additionally rejects a skill scoped to peers that names orchestration
tools. Reflector and curator runs are system runs with their own profile
(`orchestrationTools=false`, own budget) and are outside every tree's
limits. This is a note for the self-learning plan, not work in this one.

## Alternatives considered

- **Keep notes plus the protocol document only.** This is the status quo and
  the stated default. It costs drift: a Peer that ignores the note can still
  spawn, hide a turn-limit stop in "done", or overrun a budget, and nothing
  records the Lead's accept/reject. This ADR exists for the day that costs
  more than the implementation.
- **A stored `role` column.** Rejected: structure plus contract already
  determine the tier; a stored role can disagree with what the profile can
  do.
- **A distinct `needs-continue` run status.** Not recommended (item 4), but
  it is the user's call (D13).
- **Enforce only by not injecting the MCP server** (the Paseo model).
  Insufficient: Codex cannot be stripped, a user-level `smind` MCP entry or
  a manual workspace row bypasses it, and self-reported provenance cannot
  scope a Lead to its own subtree.
- **Per-profile MCP allowlists.** Deferred; only the orchestration group is
  gated here.

## Consequences

- If built: five nullable columns on `agent_profiles`, `runs.profile_id`, a
  run token and a method allowlist in `internal/wsapi`, `run_handoffs`, two
  MCP tools, and a reserved MCP server name `smind`.
- It amends ADR-0014 in two places (the `systemPrompt` exclusion and
  client-side apply for contract fields) and reverses ADR-0024/parity D8's
  "opt-in injection only" for profiles that say `orchestrationTools=true`.
  It leaves ADR-0017 #1 and ADR-0019 #6 intact.
- Until it is accepted and built, nothing changes: the stopgap and the
  protocol document are the contract.
