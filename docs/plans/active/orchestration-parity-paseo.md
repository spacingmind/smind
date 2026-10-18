# Orchestration parity with Paseo (gap analysis + P0/P1 build)

**Question (2026-10-11):** does smind already have Paseo's orchestration
surface (orchestration skills, worktree setup, and so on)? If not, build it.
The goal is for smind to replace Paseo as the orchestrator for daily
development.

**Short answer:** the *run/chat/queue* half is at parity or ahead
(`task_send` queue + provenance, usage, hierarchy, permission-mode guard).
The *workspace/provider/discovery* half is not: an orchestrating agent
cannot list workspaces, list providers or profiles, branch a task off
`origin/develop`, check out a branch or PR, archive a task, or change a
running task's settings through MCP. There is no worktree setup/teardown
hook and no bundled orchestration skill. This plan lists every gap, builds
the ones that need no new decision, and lists the rest with a
recommendation.

Sources: Paseo's live MCP tool catalog (the tools this session runs on),
`refs/paseo/{skills,public-docs/{mcp,worktrees,skills,agent-profiles}.md,paseo.json}`,
the local-only note `docs/research/local/paseo-skills-profiles-2026-09.md`,
and smind at `origin/develop` 57ffa05
(`cmd/smind/mcp*.go`, `internal/wsapi/handlers.go`, `internal/workspace`).

## 1. Inventory

### 1.1 Paseo's orchestration surface

| Area | Tools / mechanism | What it does |
|---|---|---|
| Agents | `create_agent` (title, `provider` = `provider/model`, `initialPrompt`, `workspaceId`, `settings{modeId,thinkingOptionId,features}`, `labels`, `notifyOnFinish`) | Spawns a subagent (parented to the caller) and runs the first prompt. |
| | `send_agent_prompt` (`background`, `notifyOnFinish`) | Follow-up; agent-scoped default is background + notify. |
| | `get_agent_status`, `get_agent_activity`, `list_agents` (cwd/status/since filters) | Status, transcript tail, enumeration (survives context loss). |
| | `cancel_agent` (interrupt the turn), `kill_agent`, `archive_agent` (soft-delete, interrupts) | Stop / remove. |
| | `update_agent` (name, labels, `settings{modeId,model,thinkingOptionId,features}`) and `set_agent_mode` | Live runtime changes on an existing agent. |
| | **Wait/notify:** a finish / error / needs-permission *notification arrives by itself*; the skill says "don't poll". | Push, not pull. |
| Workspaces | `create_workspace` (`isolation` local/worktree; mode `branch-off` + `baseBranch`/`branchName`, `checkout-branch` + `branch`, `checkout-pr` + `prNumber`/`forge`; `worktreeSlug`; `title`) | Worktree branch-off from a chosen ref, existing branch, or a PR. |
| | `list_workspaces`, `rename_workspace`, `archive_workspace` | Archive removes the owned worktree after the last reference goes. |
| Worktree lifecycle | `paseo.json` (read from the *committed* base branch): `worktree.setup` / `teardown` (shell script or array; env `PASEO_SOURCE_CHECKOUT_PATH`, `PASEO_WORKTREE_PATH`, `PASEO_BRANCH_NAME`), `worktree.terminals`, `worktree.servicePorts` (range or `portScript`) | Deps install, `.env` copy, migrations, per-worktree port, teardown on archive. |
| Workspace scripts | `list_workspace_scripts`, `start_workspace_script`, `stop_workspace_script`; `scripts.<name>` = plain command or supervised `service` (port `$PASEO_PORT`, reverse-proxy URL, health) | Named test/lint/dev-server commands per worktree. |
| Permissions | `list_pending_permissions`, `respond_to_permission` | List and answer (Paseo allows the orchestrator to answer; smind deliberately does not, see ADR-0017). |
| Providers / models | `list_providers`, `inspect_provider` (modes, features), `list_models`, `list_profiles` (human-authored bundles with `notes`) | Discovery + the delegation contract (profile notes tell the orchestrator which bundle fits). |
| Terminals | `create_terminal`, `list_terminals`, `capture_terminal`, `send_terminal_keys`, `kill_terminal` | Drive a shell. |
| Schedules / heartbeats | `create/list/inspect/update/pause/resume/run_once/delete_schedule`, `schedule_logs`, `create_heartbeat`, `delete_heartbeat` | Cron-started agents; cron reminders back to the caller. |
| Browser | `browser_*` (navigate, click, fill, snapshot, screenshot, evaluate, tabs, …; opt-in) | Drive a browser host. |
| Tool gating | `daemon.mcp.injectIntoAgents`, per-provider `disabledTools` | Spawned agents get the Paseo tools automatically; gating per profile. |
| Skills | Bundled `skills/{paseo,paseo-handoff,paseo-committee,paseo-advisor,paseo-help,paseo-plugin}`; synced to `~/.agents/skills`, `~/.claude/skills`, `~/.codex/skills` with a sha256 manifest (`.paseo-managed-files.json`); startup auto-repair; UI toggles | Procedure for the orchestrating agent: read profiles → materialize into `create_agent` → don't poll → wait for notify. |

### 1.2 smind's surface (today, `origin/develop`)

**MCP (`smind mcp serve`, ADR-0017), 10 tools:** `task_new`
(`workspaceId, title, spaceId?, parentTaskId?`), `task_list`
(`workspaceId, parentTaskId?`), `chat_list`, `chat_new`, `task_send`
(`taskId, prompt, provider?, chatId?, permissionMode?, autoAccept?, profileId?, whenBusy?` default `queue`,
`fromTaskId?/fromChatId?` provenance), `task_status`, `task_logs`,
`task_permissions` (read-only), `task_wait` (single `runId`, 120 s default,
returns early on a pending permission), `task_stop`. No approval tools by
decision (ADR-0017 #1, ADR-0019 #6); `task_send` rejects auto-approving
modes unless they come from a human-authored profile.

**Daemon wsapi methods that exist but have no MCP tool:** `workspace.*`,
`space.*`, `task.get`, `task.archive`, `task.move`, `task.diff`,
`task.files`, `task.stage`, `task.commit`, `task.createPr`, `chat.rename`,
`chat.archive`, `chat.queueList`, `chat.queueCancel`, `run.list`,
`run.setPermissionMode`, `run.listConfigOptions`, `run.setConfigOption`,
`provider.list` (providers + permission-mode catalog with `autoApproves`),
`profile.*`, `terminal.*`, `usage.list/summary`, `mcp.*` (agent MCP
servers), `account.*`, `file.*`.

**CLI:** `smind serve|relay|workspace|account|space|task|profile|usage|mcp|version`;
`task new|ls|send|runs|attach|logs|stop|permissions|approve|options|set-option|chat|queue`.

**Task = worktree lifecycle (`internal/workspace`):**
`CreateTask` runs `git worktree add <config.Dir()>/worktrees/<ws>/<slug> -b smind/task-<slug>`
off the workspace checkout's **current HEAD** (no base-ref parameter);
`ArchiveTask`/`DeleteTask` first commit everything uncommitted
(`git add -A` + a checkpoint commit on the task branch), then
`git worktree remove --force`; a checkpoint failure aborts before removal.
Ignored files (a copied `.env`, `node_modules`) are **not** preserved.
`ArchiveTask` does not look at running runs. There is no setup/teardown
hook, no per-task port, no scripts/services concept. `Space.EnvData` is an
opaque string nothing reads.

**Other pieces:** agent profiles (ADR-0014: provider, permissionMode,
autoAccept, thinkingLevel, **notes**; **no model field**); agent MCP servers
(ADR-0018, `smind mcp add|ls|rm|enable|disable`, so `smind mcp serve` can
already be added as an agent's MCP server by hand); prompt queue (ADR-0021,
persisted, `reject|queue|interrupt`, survives restart); run usage
(ADR-0020); task hierarchy with depth guard 2 (`smind-control-parity`,
completed parts: config options + hierarchy; the GLM smoke test is still
open). Runs have **no model concept** anywhere (`internal/taskrunner` has no
model field); ACP agents expose model/thinking only as live config options.

## 2. Gap table

Effort: XS < ½ day, S ≈ 1 day, M 2–3 days, L > 3 days (GLM coder time,
including tests). "Decision" = needs an ADR or the user's call (AGENTS.md
rule d), so it is **not built in this plan**.

| # | Capability | Paseo | smind today | Gap | Pri | Effort | Decision? |
|---|---|---|---|---|---|---|---|
| G1 | Discover workspaces | `list_workspaces` | `workspace.list` wsapi only | Orchestrator can't learn the `workspaceId` that `task_new` requires | P0 | XS | No |
| G2 | Discover providers + modes | `list_providers`, `inspect_provider` | `provider.list` wsapi only (modes + `autoApproves`) | No MCP tool | P0 | XS | No |
| G3 | Discover profiles (+ notes) | `list_profiles` | `profile.list` wsapi/CLI only | No MCP tool; `task_send profileId` is blind | P0 | XS | No |
| G4 | Re-find runs/tasks after context loss | `list_agents` (filters) | `task_list` ok; `run.list` filters by chat only, no MCP | Orchestrator that loses a `runId` can't recover it | P0 | S | No |
| G5 | Task detail | `get_agent_status` | `task.get` wsapi only (status, branch, worktree path, parent) | No MCP tool | P0 | XS | No |
| G6 | Branch a worktree off a chosen ref | `create_workspace` branch-off + `baseBranch` (`origin/develop`) | Always off the checkout's current HEAD | Workflow needs `origin/develop`; stale/dirty HEAD today | P0 | M | No |
| G7 | Task from an existing branch | `checkout-branch` | none | missing | P0 | M | No |
| G8 | Task from a PR | `checkout-pr` | none | missing (GitHub only via `refs/pull/N/head`) | P1 | M | No |
| G9 | Archive / clean up a task from MCP | `archive_workspace`, `archive_agent` | `task.archive` wsapi + UI; no MCP; no active-run guard | missing; archiving under a live run is unsafe | P0 | S | No |
| G10 | Change a running task's permission mode | `set_agent_mode`, `update_agent` | `run.setPermissionMode` wsapi; no MCP; no server-side orchestrator guard | missing; guard (ADR-0019 #6) must be server-side | P1 | M | No (applies ADR-0019 #6) |
| G11 | Change thinking level of a running task | `update_agent settings.thinkingOptionId` | `run.setConfigOption` wsapi/CLI; no MCP | missing (ACP providers only; Claude has none) | P1 | M | No (fail-closed on `mode` options) |
| G12 | Wait for any of N runs | notify-on-finish (push) | `task_wait` takes one `runId` | Two coders ⇒ sequential or polling | P1 | S | No (additive input) |
| G13 | Bundled orchestration skill(s) | `paseo`, `-handoff`, `-committee`, `-advisor` | none (only dev-time `.agents/skills`) | Agents have no procedure for delegate/wait/review/queue/stop | P0 | M | CLI install no; auto-sync daemon = ADR (D11) |
| G14 | Orchestration guidance in-band | skill only | none; `mcp.NewServer` passes no `Instructions` | Every MCP client could receive a condensed guide at `initialize` | P0 | XS | No |
| G15 | Leader/coder pattern | profiles + skill | profiles have `notes`, hierarchy has `parentTaskId` + depth 2 | Needs skill text + the discovery tools above; no new code beyond G13 | P1 | S | No |
| G16 | Queue list/cancel from MCP | n/a (steer) | `chat.queueList/Cancel` wsapi + CLI | Missing, **blocked on `feat/queue-cancel-kind`** (another leader owns `cancel_kind`) | P1 | S | Wait for that branch |
| G17 | Worktree **setup/teardown** (deps, env copy, migrations) | `paseo.json` `worktree.setup/teardown` | none | Fresh task worktree has no deps/`.env`; every task starts broken for JS/Go-with-env repos | P0 | L | D1 **approved 2026-10-11** → ADR-0024, PR-E |
| G18 | Per-task port allocation | `servicePorts`, `$PASEO_PORT` | none | Parallel dev servers collide | P1 | M | In ADR-0024, PR-E |
| G19 | Workspace scripts / services | `scripts`, `start/stop_workspace_script` | none (terminals only) | no named, supervised commands | P2 | L | Yes: D1 follow-on |
| G20 | Model discovery + update | `list_models`, `update_agent settings.model` | **no model concept** in runs/profiles/`provider.list` | Can't choose or list models | P1 | L | D2 **approved 2026-10-11** → ADR-0025, PR-F |
| G21 | Approve/deny permissions | `respond_to_permission` | none by decision | n/a | n/a | n/a | **D3 recommend keep ADR-0017** |
| G22 | Push notification on finish/permission | `notifyOnFinish` | none (ADR-0017 §3 defers) | Orchestrator must block in `task_wait` | P1 | M | **Yes: D4** |
| G23 | Schedules / heartbeats | full set | none | ROADMAP Phase 5, demand-driven | P2 | L | **D5 defer** |
| G24 | Terminals over MCP | `create/capture/send_keys/kill/list_terminal` | `terminal.*` wsapi only | Orchestrator already has its own shell | P2 | M | D6 defer |
| G25 | Browser tools | `browser_*` | none (agents can get Playwright MCP via ADR-0018) | out of scope | P2 | L | D7 don't build |
| G26 | Inject orchestration MCP into spawned agents | `injectIntoAgents` | `smind mcp add smind -- smind mcp serve` by hand | recursion/cost policy | P2 | S | **D8** |
| G27 | Task diff/commit/PR from MCP | n/a (git via shell) | `task.diff/commit/createPr` wsapi | `createPr` pushes to a remote (outward-facing) | P2 | S–M | **D9** |
| G28 | Create a workspace from MCP | `create_workspace` (adopts a path) | `workspace.create` wsapi/CLI | Registers an arbitrary path as a place agents run | P2 | XS | **D10 recommend human-only** |
| G29 | Rename / labels on tasks, archive chat from MCP | `rename_workspace`, labels, `update_agent` | `chat.archive`, `chat.rename` wsapi | cosmetic | P2 | S | No (later) |
| G30 | Skill auto-sync, selection UI, startup repair | `syncSkills` + manifest + UI | none | daemon subsystem | P2 | L | **D11 ADR** |
| G31 | Dogfood gap log from the `cancel_kind` leader (`orchestration-and-metering.md` "Step 3 dogfood gap log", landed in #265) | n/a | 11 gaps (D-G1…D-G11) | folded in below, section 5 | see §5 | see §5 | see §5 |

## 3. Build scope (no ADR needed)

Four PRs against `develop`, none merged by the leader. New MCP tools go in
**new files** (one registration call each) so the PRs stay mergeable.

Cross-cutting rules for every item:

- **ADR-0017 #1 / ADR-0019 #6 hold.** No approve/deny tool. Nothing new may
  let an orchestrating caller select an auto-approving mode, `autoAccept`,
  or an ACP `mode`-category config option. Server-side enforcement, not
  just client-side in `smind mcp serve`.
- **Never lose uncommitted user work.** Archive keeps the checkpoint-first
  order; a failed checkpoint aborts and the worktree remains.
- **No new execution of commands from untrusted input.** Ref/branch/PR
  arguments reach `git` as discrete argv (never via a shell), are rejected
  if they start with `-`, contain control characters, or fail
  `git check-ref-format`/`rev-parse --verify`, and are separated from
  options with `--` where git supports it.
- New tools are read-only except `task_archive`, `task_set_mode`,
  `task_set_option`, and the extended `task_new`. Each state the tool
  changes is reachable by a human via the UI/CLI already.

### PR-A: MCP discovery tools (G1–G5, G14)

New read-only tools: `workspace_list`, `task_get`, `run_list`
(`taskId?`, `chatId?`, `status?`, newest first, bounded), `provider_list`,
`profile_list` (includes `notes`). `run.list` gains optional `taskId` and
`status` filters (wsapi, backwards compatible: today's `chatId`-only call is
unchanged). `mcp.NewServer` gets `ServerOptions.Instructions`: a ≤ 40-line
condensed orchestration guide (the six-step loop: discover → create task →
send → wait → review → archive; never approve; queue semantics).

Acceptance criteria:

1. `workspace_list` returns every workspace with id, title, path; empty list
   (not null) when none.
2. `task_get` returns one task (id, title, status, branch, worktreePath,
   parentTaskId) and a clear "not found" tool error for an unknown id.
3. `run_list` with `taskId` returns that task's runs across its chats;
   with `status=running` only live runs; unknown task ⇒ empty list; the
   legacy `chatId`-only wsapi call returns what it returned before.
4. `provider_list` carries each provider's modes with `autoApproves`, so
   an orchestrator can pick a legal `permissionMode`.
5. `profile_list` returns id, name, provider, permissionMode, autoAccept,
   thinkingLevel, notes.
6. `initialize` returns non-empty `instructions` that name every registered
   tool and state that approvals are human-only.
7. The registered catalog contains no tool whose name matches
   `approve|deny|respond` (pinned test, so a later tool can't slip one in).

Named tests: `TestMCPTools_WorkspaceList`, `TestMCPTools_WorkspaceListEmpty`,
`TestMCPTools_TaskGet`, `TestMCPTools_TaskGetNotFound`,
`TestMCPTools_RunListByTask`, `TestMCPTools_RunListStatusFilter`,
`TestRunList_LegacyChatIDUnchanged` (wsapi),
`TestMCPTools_ProviderListMarksAutoApprovingModes`,
`TestMCPTools_ProfileListIncludesNotes`,
`TestMCPServer_InstructionsNameEveryTool` (diffs the instructions text
against the registered tool names, so it fails when a tool is added without
documenting it), `TestMCPServer_NoApprovalTools`.

### PR-B: task from base ref / branch / PR, and archive (G6–G9)

`task.create` (wsapi) and `task_new` (MCP) gain **one** of
`baseRef` (new branch off that ref, e.g. `origin/develop`), `branch`
(check out an existing branch), `prNumber` (GitHub:
`git fetch <remote> pull/<N>/head`, branch `smind/pr-<N>-<slug>`). Omitting
all keeps today's behavior (off HEAD). `task_archive` (MCP) wraps
`task.archive` and the **daemon refuses to archive a task with a running or
queued run** (error names the run; the caller must `task_stop` first). The
active-run guard lives in the daemon so the web UI gets it too.

Acceptance criteria:

1. `baseRef: "origin/develop"` creates the worktree at that ref's commit,
   not at the checkout's HEAD; the task branch is `smind/task-<slug>`.
   Before resolving a `<remote>/<branch>` ref, the daemon does a bounded
   best-effort `git fetch` of that one branch (timeout; failure falls back
   to the existing remote-tracking ref and is not fatal).
2. `branch: "feat/x"` checks out the existing local (or `origin/` tracking)
   branch into the worktree without creating a new branch; if the branch is
   already checked out in another worktree the call fails with git's reason
   and creates no task row and no directory.
3. `prNumber: N` fetches the PR head and creates `smind/pr-N-<slug>`; a
   non-GitHub remote or a missing PR yields a clear error and leaves no
   row or directory behind. `prNumber <= 0` is rejected.
4. Supplying more than one of `baseRef`/`branch`/`prNumber` is rejected.
5. Ref-injection: a `baseRef`/`branch` beginning with `-`, or containing
   whitespace/control characters/`..`, is rejected before git is invoked;
   `--upload-pack=...`-style values never reach a git argv in option
   position.
6. `task_archive` on a dirty task leaves the work on the branch as the
   checkpoint commit and removes the worktree directory (existing
   behavior, now pinned over MCP); on a checkpoint failure the worktree
   and the task stay intact.
7. `task_archive` while a run is active (running or queued) fails with the
   run id in the error; after `task_stop` it succeeds.
8. The tool description of `task_archive`/`task_new(branch)` says that the
   checkpoint commit lands on the checked-out branch and that ignored files
   (e.g. `.env`, `node_modules`) are not preserved.

Named tests: `TestManager_CreateTask_BaseRef`,
`TestManager_CreateTask_BaseRefFetchFailureFallsBack`,
`TestManager_CreateTask_BaseRefRejectsOptionLikeInput`,
`TestManager_CreateTask_BaseRefUnknown`,
`TestManager_CreateTask_CheckoutExistingBranch`,
`TestManager_CreateTask_CheckoutBranchAlreadyCheckedOut`,
`TestManager_CreateTask_FromPR` (fetch step through a package-level seam,
real local temp repos otherwise, same pattern as `pr_test.go`),
`TestManager_CreateTask_FromPRInvalid`,
`TestTaskCreate_RefModesMutuallyExclusive` (wsapi),
`TestMCPTools_TaskNewBaseRef`, `TestMCPTools_TaskArchivePreservesUncommittedWork`,
`TestMCPTools_TaskArchiveCheckpointFailureKeepsWorktree`,
`TestTaskArchive_RefusesActiveRun`, `TestTaskArchive_RefusesQueuedRun`,
`TestTaskArchive_AllowedAfterStop`.

### PR-C: live settings and wait-any (G10–G12)

`run.setPermissionMode` gains the same `source` argument `run.start`
already has (ADR-0021): for a non-human source the daemon rejects any mode
with `AutoApproves` and `autoAccept=true`. New MCP tools: `task_set_mode`
(`runId, modeId`), `task_options` (list ACP config options), `task_set_option`
(`runId, configId, value`) which, **fail-closed**, only accepts an option
whose category is `thought_level` or `model`; an option with category
`mode`, an unknown/empty category, or an unknown option id is rejected.
`task_wait` accepts `runIds` (2–16) as an alternative to `runId` and
returns as soon as any one reaches a terminal state or has a pending
permission, naming that run.

Acceptance criteria:

1. Over MCP, `task_set_mode` with a legal (asks-a-human) mode succeeds and
   `task_status` reflects it; with `bypassPermissions`/Codex `full-access`/
   any `AutoApproves` mode it fails with the ADR-0019 explanation. Over
   raw wsapi with `source` = `orchestrator` (or any non-human value) the
   same rejection holds; with `source` = `human` (UI/CLI) behavior is
   unchanged.
2. `task_set_mode` on Codex fails with the existing "no mid-turn switch"
   error; on a finished run it fails clearly.
3. `task_options` lists options (id, label, category, current value) for an
   ACP run and returns the documented "not supported" outcome for
   claude-native/codex-native.
4. `task_set_option` changes a `thought_level` option on the fake ACP
   agent; changing an option whose category is `mode` is rejected even when
   its value names a non-auto-approving choice; unknown id rejected with
   the daemon's reason.
5. `task_wait` with `runIds` returns the first finished run and names it;
   timeout with none done ⇒ `timedOut: true`; a pending permission on any
   listed run returns early with that run's `pendingPermission`;
   `runId` plus `runIds`, or > 16 ids, is rejected; the single-`runId`
   path is byte-for-byte unchanged (existing `task_wait` tests untouched).
6. Open verification (not a test): what `glm-acp-agent` really advertises
   for its thinking option (category and ids). If it is not
   `thought_level`, record it in Validation and propose the narrowest
   widening (decision, not silent).

Named tests: `TestRunSetPermissionMode_OrchestratorCannotPickAutoApproving`,
`TestRunSetPermissionMode_OrchestratorCannotSetAutoAccept`,
`TestRunSetPermissionMode_HumanUnchanged`,
`TestMCPTools_TaskSetMode`, `TestMCPTools_TaskSetModeRejectsAutoApproving`,
`TestMCPTools_TaskOptionsNotSupportedProvider`,
`TestMCPTools_TaskSetOptionThoughtLevel`,
`TestMCPTools_TaskSetOptionRejectsModeCategory`,
`TestMCPTools_TaskSetOptionRejectsUncategorized`,
`TestMCPTools_TaskWaitAnyOf`, `TestMCPTools_TaskWaitAnyOfTimeout`,
`TestMCPTools_TaskWaitAnyOfPermissionPending`,
`TestMCPTools_TaskWaitRejectsBothAndTooMany`.

### PR-D: bundled orchestration skills + `smind skill` (G13, G15)

Skills are plain `SKILL.md` files embedded in the binary
(`internal/skills/bundle/<name>/SKILL.md`, `go:embed`), installed on demand
by `smind skill install [--target claude|agents|codex|all] [--force]` into
`~/.claude/skills/<name>/`, `~/.agents/skills/<name>/`,
`~/.codex/skills/<name>/`. Also `smind skill ls` (bundled, installed,
modified) and `smind skill uninstall`. Explicit command only: **no startup
sync, no daemon subsystem, no UI** (those are D11). Bundle v1:

| Skill | Content |
|---|---|
| `smind` | Reference: tool catalog, the discover → task_new(baseRef) → task_send → task_wait → review → archive loop, `whenBusy` semantics, permissions are human-only (surface `task_permissions` to the human), profile materialization, how to stop/archive, how to add `smind mcp serve` as an agent MCP server for nested orchestration (depth 2). |
| `smind-leader` | The leader/coder pattern used here: leader writes spec, spawns ≤ 2 coders in own worktrees, numbered steps with one commit each, review adversarially, one PR per item, never merge unasked. |
| `smind-handoff` | Self-contained briefing template + `task_new`/`task_send` launch. |
| `smind-advisor` | Second opinion with a human-authored read-only/`plan`-mode profile; "analysis only" suffix. |
| `smind-committee` | Two contrasting-provider analyses, then reconcile. |

Acceptance criteria:

1. `smind skill install --target all` writes every bundled skill to the
   three homes and a manifest `.smind-managed-files.json` (path → sha256)
   in each skill directory; a second run changes nothing.
2. A file the user edited (hash differs from the manifest) is **not**
   overwritten without `--force`; the command reports it and exits 0 with
   the skill marked "modified".
3. Install and uninstall only ever touch `<home>/<bundled-name>/…` for
   names in the bundle; a user's other skills, and unmanaged files inside a
   bundled skill's directory, are never modified or removed.
4. Every bundled `SKILL.md` has YAML frontmatter with `name` equal to its
   directory and a non-empty `description`.
5. Every `snake_case` MCP tool name a skill mentions exists in the
   registered MCP catalog, and every tool in the catalog is mentioned by
   the `smind` skill (docs cannot drift from code).
6. `$HOME` is resolved once; a target dir that is a symlink escaping the
   home is refused.

Named tests: `TestSkillInstall_WritesBundleToAllTargets`,
`TestSkillInstall_IdempotentNoRewrite`,
`TestSkillInstall_DoesNotClobberUserEditedFile`,
`TestSkillInstall_ForceOverwritesEdited`,
`TestSkillInstall_LeavesOtherSkillsAndUnmanagedFiles`,
`TestSkillUninstall_RemovesOnlyManagedFiles`,
`TestSkillInstall_RefusesSymlinkEscape`, `TestSkillLs_ReportsModified`,
`TestSkillBundle_FrontmatterValid`, `TestSkillBundle_ToolNamesMatchCatalog`.

PR-D's text references PR-A/B/C tool names, so it is based on the stacked
result of A–C (or lands after them); the catalog test enforces that.

### PR-E: worktree setup/teardown + per-task port (G17, G18; ADR-0024)

Backend + CLI first (PR-E), web second (PR-E2). Accepted design: daemon-side,
human-authored per-workspace scripts; background execution; setup counts as
"busy" for run start; failed setup keeps the worktree; best-effort teardown
after the checkpoint; port range from config. See
[ADR-0024](../../decisions/0024-worktree-setup-teardown-and-ports.md).

Acceptance criteria:

1. A workspace with `setup_script` set: `task.create` returns immediately
   with `setupStatus: "running"`; the script runs with cwd = the worktree
   and env `SMIND_SOURCE_CHECKOUT_PATH`, `SMIND_WORKTREE_PATH`,
   `SMIND_BRANCH`, `SMIND_TASK_ID`, `SMIND_WORKSPACE_ID` (and `SMIND_PORT`
   when a range is configured); on exit 0 the task becomes `ok`, with the
   output tail (≤ 64 KiB, merged stdout+stderr) in `setup_log`.
2. Non-zero exit, timeout (process group killed, no leftover child), or a
   daemon restart mid-setup ⇒ `failed` with a clear `setup_error`; the
   worktree and task are intact; a `running` task found at daemon start
   becomes `failed` ("interrupted by daemon restart") and is not re-run.
3. While `running`, `run.start`/`task.prompt` with `whenBusy` `queue` or
   `interrupt` enqueue and are delivered when setup finishes (including
   when it finishes `failed`); `reject`/unset errors naming the setup.
   A queued prompt is not lost across the transition.
4. `task.retrySetup` re-runs the script on a non-running task; on a
   `running` task it errors.
5. Archive/delete: checkpoint commit first, then teardown (best-effort,
   its failure/timeout appended to `setup_log` and never blocking the
   archive), then `git worktree remove`. A checkpoint failure aborts before
   teardown. Archiving a task whose setup is `running` kills the script
   first. A workspace with no scripts behaves exactly as today
   (`setup_status` NULL).
6. Ports: with `worktrees.ports.range` set, each new task gets a distinct
   free port (partial unique index on non-archived tasks + a loopback bind
   probe); archiving frees it; exhaustion fails task creation with a clear
   error; with no range configured nothing changes. `SMIND_PORT` is in the
   setup env, the task's terminals, and Claude-native runs.
7. Only humans can author scripts: `workspace.setScripts` exists in wsapi
   and the CLI (`smind workspace setup show|set|clear`); **no MCP tool**
   reads or writes script text, `task_new` has no script argument, a
   committed `smind.json`/`paseo.json` in the repo is ignored. `task_get`
   and `workspace_list` expose `setupStatus`/`setupError`/`port`/
   `hasSetupScript` but never the script text.
8. Windows: a configured script makes task creation fail fast with a clear
   "not supported on Windows" message.
9. Script env values are never shell-interpolated: a branch named
   `x$(touch /tmp/pwned)` (git allows `$`, `(`, `)`) does not execute
   anything from the env.

Named tests: `TestSetup_RunsInWorktreeWithEnv`, `TestSetup_AsyncCreateReturnsRunning`,
`TestSetup_FailureKeepsWorktree`, `TestSetup_TimeoutKillsProcessGroup`,
`TestSetup_LogTailCapped`, `TestSetup_DaemonRestartMarksFailed`,
`TestSetup_QueuedPromptDeliveredAfterSetup`,
`TestSetup_QueuedPromptDeliveredAfterFailedSetup`,
`TestSetup_RejectWhenBusyNamesSetup`, `TestSetup_RetryRunsAgain`,
`TestSetup_RetryWhileRunningRejected`, `TestTeardown_RunsAfterCheckpointBeforeRemove`,
`TestTeardown_FailureDoesNotBlockArchive`,
`TestTeardown_NotRunWhenCheckpointFails`, `TestArchive_KillsRunningSetup`,
`TestPorts_AllocatesDistinctFreePorts`, `TestPorts_ReleasedOnArchive`,
`TestPorts_SkipsPortInUse`, `TestPorts_ExhaustionFails`,
`TestPorts_ConcurrentCreatesUnique` (run with `-race`),
`TestSetup_EnvNotInterpolated` (the `$(touch ...)` branch),
`TestWorkspaceSetScripts_NoMCPTool` (catalog contains no tool that can set
scripts; `task_get` output has no script text), `TestSetup_RepoFileIgnored`,
`TestSetup_WindowsUnsupported` (build-tagged or via an OS seam),
`TestMigrate_SetupColumns`. CLI: `TestWorkspaceSetup_SetShowClear`.
PR-E2 (web): workspace settings editor for the two scripts, task setup
badge (running/ok/failed) with the log tail; vitest names
`workspace setup editor saves via workspace.setScripts`,
`task shows setup failed badge with the error`; **light + dark screenshots**
against a sandbox daemon (isolated `SMIND_HOME`/port) reviewed by the
leader.

### PR-F: model selection (G20; ADR-0025)

Backend + CLI + MCP first (PR-F), web second (PR-F2). See
[ADR-0025](../../decisions/0025-model-selection.md).

Acceptance criteria:

1. `model` accepted on `run.start`, `task.prompt`, profile create/update,
   MCP `task_send`; absent/empty = provider default and the spawn is
   byte-identical to today's (no `--model`, no set_config_option, no
   `model` in `thread/start`).
2. `runs.model` and `agent_profiles.model` columns (nullable, additive
   migration, old DBs read back NULL); queued prompts keep `model`.
3. Syntax validation (1–100 chars of `[A-Za-z0-9._:/@\[\]-]`, no leading `-`)
   on every entry point before any spawn; `--model -x`/spaces/newlines/
   101 chars rejected.
4. `provider.list`/`provider_list` carry `models` + `modelsDiscovered`:
   claude-native static catalog (any syntax-valid id still accepted);
   ACP from the `model`-category config option of the lazy probe; codex
   from `model/list`, with the "unavailable ⇒ no models, string accepted"
   fallback. Never blocks.
5. Applying: Claude `WithModel`; ACP `session/set_config_option` after
   session/new (or load) and before the first prompt, only when the
   requested value differs; Codex `model` in `thread/start`/`thread/resume`.
   A rejection by the agent fails the run start with the agent's error.
   With a discovered catalog an unknown ACP/Codex model is rejected up
   front listing valid ids.
6. Live switch via `run.listConfigOptions`/`run.setConfigOption` and MCP
   `task_options`/`task_set_option`: ACP's own `model` option; for
   claude-native a synthetic `model` option routed to `SetModel`
   (default ⇒ `nil`); Codex rejects with "applies from the next run".
   PR-C's fail-closed category allowlist stays, with `model` included.
7. A profile's `model` is used when the profile is selected and is
   overridden by an explicit `model` on the call.
8. CLI: `smind task send --model`, `smind profile new/edit --model`,
   `smind profile ls` shows it.
9. Not a permission change: choosing any model is allowed for an
   orchestrator (no `autoApproves` interplay); a test pins that
   `task_send` with `model` and a restrictive `permissionMode` still
   rejects an auto-approving mode.

Named tests: `TestModel_ValidationRejectsBadStrings`,
`TestRunStart_ModelEmptyIsDefault`, `TestRunStart_ModelPersistedOnRun`,
`TestMigrate_ModelColumns`, `TestChatQueue_ModelPreserved`,
`TestProfile_ModelRoundTrip`, `TestProfile_ModelOverriddenByCall`,
`TestProviderList_ClaudeStaticModels`, `TestProviderList_ACPModelsFromProbe`,
`TestProviderList_CodexModelsFromModelList`, `TestProviderList_UndiscoveredAcceptsString`,
`TestRunClaudeNative_WithModelOption`, `TestRunACP_AppliesModelConfigOption`
(fake agent), `TestRunACP_ModelRejectedFailsStart`,
`TestRunCodex_ModelInThreadStart` (fake app server),
`TestRunACP_UnknownModelRejectedWhenDiscovered`,
`TestConfigOptions_ClaudeSyntheticModelOption`,
`TestSetConfigOption_ClaudeModelCallsSetModel`,
`TestSetConfigOption_CodexModelRejected`,
`TestMCPTools_TaskSendModel`, `TestMCPTools_ProviderListHasModels`,
`TestMCPTools_TaskSetOptionModelCategoryAllowed`,
`TestMCPTools_TaskSendModelDoesNotUnlockAutoApproving`. CLI:
`TestTaskSend_ModelFlag`, `TestProfile_ModelFlag`.
PR-F2 (web): composer model picker (reads `provider.list`), profile editor
field, run header chip; vitest `composer sends model on run.start`,
`profile editor saves model`; **light + dark screenshots** against a
sandbox daemon.

## 4. Decisions

D1 and D2 were approved by the user on 2026-10-11 and recorded as ADR-0024
and ADR-0025 (built as PR-E and PR-F above). D3–D11 were accepted as
recommended on the same date: keep ADR-0017's no-approval rule, wait-any
before push, defer schedules/heartbeats, MCP terminals and browser tools,
opt-in manual injection of `smind mcp serve`, PR creation stays human/CLI,
`workspace_create` human-only, CLI `smind skill install` first.


| # | Item | Why it's a decision | Recommendation |
|---|---|---|---|
| D1 | **APPROVED 2026-10-11 → ADR-0024.** Worktree setup/teardown + per-task port (+ later scripts/services) (G17–G19) | New exec-of-commands surface (code execution on task create), and the file/format question: repo-committed file read from the base branch (Paseo's `paseo.json`) vs daemon-side config. A cloned untrusted repo would get code execution on the first `task_new` with the Paseo model. | **Daemon-side, human-authored, per-workspace config** (new `workspaces.setup_script`/`teardown_script`, set only via CLI/web, never via MCP or any non-human source), matching the ADR-0014/0018 precedent that standing config is the human's. Commands run as an argv list or `sh -c` of the stored script in the worktree with `SMIND_SOURCE_CHECKOUT_PATH`, `SMIND_WORKTREE_PATH`, `SMIND_BRANCH`, `SMIND_PORT` (allocated from a configurable range, recorded on the task); bounded time, output kept in a task event; setup failure marks the task `setup_failed` and keeps the worktree. Optional later: a repo `smind.json` honoured only after a trust-on-first-use hash approval by a human. Needs an ADR. |
| D2 | **APPROVED 2026-10-11 → ADR-0025.** Model selection (G20) | Runs/profiles have no `model` field; adding one is a data-model/wire change across run.start, profile, provider.list. | Add `model` to the run config and profile (ADR), populated from ACP model config options and a static Claude catalog; then `provider_list` can list models and `task_set_option` covers live switches. Until then GLM/Claude default models apply. |
| D3 | **Approval tools over MCP** (G21) | Would reverse ADR-0017 #1 / ADR-0019 #6, the user's standing rule. | **Keep as is.** The wait/notify pair already surfaces the pending request so the human is pulled in; improve that (D4) instead of delegating approval. |
| D4 | **Push notification to the orchestrator** (G22) | ADR-0017 §3 deferred MCP resources/notifications; client support varies. | Ship wait-any (PR-C) now; revisit push only if wait-any proves insufficient in daily use. |
| D5 | **Schedules & heartbeats** (G23) | ROADMAP Phase 5, demand-driven. | **Defer.** The host's own `/loop` / cron covers it today; no demand yet in smind use. |
| D6 | **Terminals over MCP** (G24) | New remote-exec surface for agents. | **Defer**: the orchestrator already has its own shell; add only if a non-shell orchestrator appears. |
| D7 | **Browser tools** (G25) | Large surface, host-bound. | **Don't build**; `smind mcp add playwright …` (ADR-0018) already gives spawned agents a browser. |
| D8 | **Auto-inject `smind mcp serve` into spawned agents** (G26) | Recursion and cost policy; self-reported `parentTaskId` can't enforce the depth guard. | Opt-in only: documented manual `smind mcp add` in the `smind` skill; no automatic injection until the depth/concurrency guard (O2 Wave 2) exists. |
| D9 | **`task_create_pr` / `task_commit` via MCP** (G27) | Pushes to a remote / writes history; outward-facing. | Read-only `task_diff` is safe to add later without an ADR; PR creation stays human/CLI until asked. |
| D10 | **`workspace_create` via MCP** (G28) | Registers an arbitrary path as somewhere agents run. | **Human-only** (CLI/web). |
| D11 | **Skill auto-sync, selection UI, startup repair** (G30) | New daemon capability + sync contract. | CLI `smind skill install` first (this plan); revisit once the bundle has proven useful. |

## 5. Dogfood gap log fold-in (G31)

The `cancel_kind` leader ran one real task end to end through smind (GLM
peer driven over `smind mcp serve`; 11 gaps, D-G1…D-G11 below to avoid
clashing with this plan's G-numbers; source:
`orchestration-and-metering.md`, "Step 3 dogfood gap log"). This plan does
not duplicate its code (`cancel_kind` is on develop, #265); it maps each gap
to a PR here or to a decision.

| Dogfood gap | Severity | Overlap with this plan | Disposition |
|---|---|---|---|
| D-G1 MCP `task_status` / `task_logs` always fail output validation (raw-JSON `input`/`payload` typed as an array) | **Blocker** | none; my PR-A tests did not cover event-bearing runs | **PR-G (P0)**: declare those fields `any`; stdio e2e test on a run that has events |
| D-G2 permission wall: ~43 approvals for one task; no allowlist | High | ADR-0017 #1 / ADR-0019 #6 are intentional (D3: keep) | Not built. Product/doc items: the `smind` skill documents the human-authored "worktree sandbox" profile recipe (PR-D); a CLI to flip `autoAccept` live on one run is a **decision (new)**, see §4 D12 |
| D-G3 write prompt hides what it writes (whole-file `Write`) | High | none | **PR-G (P1)**: permission summary shows size and line-count change, flags "overwrites existing file" (summary text only, no new approval path) |
| D-G4 `task_wait` returns at once while a permission is pending, so a driver can't wait | High | Same function as G12 (wait-any, PR-C); wait-any inherits the problem | **PR-G (P0)**: optional `ignorePermissionIds` on `task_wait` / `runIds` calls so it blocks until a *new* permission, a terminal state, or the timeout; description says how a human approves out of band |
| D-G5 GLM stops at `max_turn_requests`, reported as `done`; turn cap not configurable | High | none | Config key `agents.glm.maxTurns` passed as `--max-turns` is **PR-G (P1)**; a distinct `needs-continue` run state is a wire/run-model change: **decision D13** |
| D-G6 CLI can approve but not reject | Med | none (human path, not MCP) | **PR-G (P1)**: `smind task deny <runId> [requestId]`, `task approve --help` works |
| D-G7 MCP too thin to review work | Med | G1–G5 and G9 here cover `task_get`, `run_list`, `provider_list`, `task_archive` (PR-A/B); G16 queue tools were waiting on `cancel_kind` | **PR-G (P1)**: read-only `task_diff`, `queue_list`, `queue_cancel` (unblocked by #265; cancel reuses `chat.queueCancel` unchanged), `usage_summary`; `RunID` column in `task queue ls` |
| D-G8 GLM usage numbers look wrong | Med | none | Needs a raw capture from a real multi-turn GLM run; **not built** (needs a live GLM account; tracked in `orchestration-and-metering.md`) |
| D-G9 driver pays twice for long prompts | Med | none | **PR-G (P2)**: `task_send` `promptFile` restricted to a path **inside the task's worktree** (no `..`, no symlink escape, size-capped) |
| D-G10 setup frictions: worktree starts from HEAD, no `node_modules`, `task new` prints only `created` | Med | G6 (`baseRef`) and G17 (setup scripts, PR-E) fix the first two | `smind task new` printing id/path/branch is **PR-G (P2)**; `--port`/env for the daemon port stays a papercut |
| D-G11 `task logs` very noisy for GLM | Low | none | Not built (UI/CLI polish; `--compact` can follow on demand) |

### PR-G: dogfood fixes (D-G1, D-G3, D-G4, D-G5 config, D-G6, D-G7 rest, D-G9, D-G10 print)

Based on PR-C (it changes `task_wait`). Acceptance criteria:

1. `task_status` and `task_logs` succeed on a run whose events carry
   `input`/`payload` objects; output is valid against the generated schema.
2. `task_wait` (single or `runIds`) accepts `ignorePermissionIds` (ids of
   already-surfaced requests); with every pending request ignored it keeps
   waiting until a new request, terminal state, or timeout; unknown ids are
   harmless; default behavior unchanged.
3. Permission summaries for a file write state byte size and, for an
   existing file, `old -> new` line counts plus an "overwrites existing
   file" marker; the approval path itself is untouched.
4. `agents.glm.maxTurns` (config.yaml, default unset = agent default) is
   passed as `--max-turns <n>` to the GLM ACP command; invalid values
   rejected at config load.
5. `smind task deny <runId> [requestId]` rejects a pending request via
   `run.respondPermission` (human CLI only; no MCP tool); `smind task
   approve --help` prints usage.
6. MCP read-only tools `task_diff` (task id, capped size), `queue_list`
   (chat id), `usage_summary` (wraps `usage.summary`), and `queue_cancel`
   (item id; wraps `chat.queueCancel`, which is a state change a human can
   already make and cannot widen permissions).
7. `task_send` `promptFile`: path resolved relative to the task's
   worktree, rejected if absolute, containing `..`, escaping via symlink,
   or over 64 KiB; mutually exclusive with `prompt`.
8. `smind task new` prints id, branch and worktree path.

Named tests: `TestMCPTools_TaskStatusWithEvents`, `TestMCPTools_TaskLogsWithEvents`
(stdio e2e, event-bearing run), `TestMCPTools_TaskWaitIgnorePermissionIds`,
`TestMCPTools_TaskWaitAnyIgnorePermissionIds`,
`TestPermissionSummary_WriteShowsLineCountChange`,
`TestPermissionSummary_WriteNewFileSize`, `TestLoad_GLMMaxTurns`,
`TestGLMCommand_PassesMaxTurns`, `TestTaskDeny_RejectsPending`,
`TestTaskApprove_HelpFlag`, `TestMCPTools_TaskDiff`, `TestMCPTools_QueueListAndCancel`,
`TestMCPTools_UsageSummary`, `TestMCPTools_NoApprovalToolInCatalog` (extended),
`TestMCPTools_TaskSendPromptFile`, `TestMCPTools_TaskSendPromptFileRejectsEscape`
(absolute, `..`, symlink, oversize), `TestTaskNew_PrintsIDBranchPath`.

New decisions raised by the fold-in (not built, recommendation attached):

- **D12: CLI/UI to flip `autoAccept` live on one run** (D-G2). `run.setPermissionMode`
  with a human source already allows it; the gap is discoverability. Recommend
  a `smind task mode <runId> [--auto-accept on|off] [mode]` human-only CLI,
  needs no ADR (it exposes an existing human-source RPC) but is your call.
- **D13: `needs-continue` run state for `max_turn_requests`** (D-G5). Today
  such a run ends `done` with only `stopReason` telling the truth. Recommend
  adding it as a distinct terminal status, which ripples through `run.status`,
  the CLI, `task_wait`, the web timeline and the queue's "finished" trigger;
  needs an ADR.
  *Decided 2026-10-18 (user): ADR-0027's recommendation, not a status.* A derived `disposition` (`needs-continue`) beside the unchanged run
  status, computed from status + stop reason (`runs.DispositionFor`: `done` ending at `max_turn_requests`, `max_tokens` or
  `budget_exceeded`) and never stored, so it survives a restart for free. Additive on `run.list` (`Disposition`), `run.logs`,
  `run.status` events and the `task.prompt`/`run.attach` result (`disposition`), MCP `task_status`/`task_wait`/`task_logs`/`run_list`
  and `smind task runs` (`done (needs-continue)`). `task_wait` still returns `state: "done"` with a message saying the work is not
  finished, so the queue's "finished" trigger and every status consumer are unchanged. The web run footer reads the disposition
  (an unknown cap still offers Continue; an older daemon falls back to the stop-reason cases). Only `needs-continue` is built: the
  rest of ADR-0027 section 4's dispositions need the handoff tools.

## Progress

- [x] Phase A: inventory + gap table (this file); G31 dogfood gap log folded in (§5)
- [x] ADR-0024 (D1) and ADR-0025 (D2) written, Accepted; D1-D11 answered (2026-10-11)
- [x] PR-A discovery tools + server instructions: #264 (merged)
- [x] PR-B task from baseRef/branch/PR + `task_archive` + archive guard/latch: #266 (merged)
- [x] PR-E worktree setup/teardown + ports (backend + CLI): #270 (merged)
- [ ] PR-C live settings + wait-any: #269 (open, CI green)
- [ ] PR-G dogfood fixes (D-G1 blocker, D-G3..D-G10): #273 (open, stacked on #269)
- [ ] PR-F model selection (backend + CLI + MCP): #272 (open); PR-F2 web: #276 (open, stacked on #272)
- [ ] PR-E2 web setup editor/badge: #275 (open)
- [ ] PR-D bundled skills + `smind skill install|ls|uninstall`: code complete in branch `feat/orchestration-skills` (carries temporary integration merges of #269/#273); PR opens after they merge, rebased onto develop
- [x] D12 (live `autoAccept` CLI) and D13 (`needs-continue`) answered by the user (2026-10-18); D13 shipped as a derived `disposition`, not a status (PR "feat(runs): derived needs-continue disposition (D13)")

## Validation

Per PR, with what was and was not verified (details in each PR body):

- **PR-A (#264):** TestRunList_*, TestMCPTools_{WorkspaceList,TaskGet,RunList*,ProviderListMarksAutoApprovingModes,ProfileListIncludesNotes}, TestMCPServer_InstructionsNameEveryTool (fails when a tool is undocumented), TestCapRuns. Criteria 1-7 met.
- **PR-B (#266):** ref validation and git argv safety (`TestManager_CreateTask_BaseRefRejectsOptionLikeInput`), single-level refs/SHAs, `prNumber <= 0` rejected at every layer; archive guard fails closed, latch set *before* the active check (`TestTryBeginArchive_LatchSetBeforeActiveCheck` fails on the old order), `run.start` refuses archived tasks. Not verified: PR path against a real GitHub remote (test seam only).
- **PR-E (#270):** setup/teardown/port tests listed in the PR; review fixes: `Start` honors setup-busy, `RetrySetup` atomic, `discardTask` never deletes a pre-existing branch (regression `TestCreateTask_ExistingBranchPortExhaustionKeepsUserBranch`, verified to fail on the old behavior). Not verified: real `npm ci`, Windows.
- **PR-C (#269):** source-guarded `run.setPermissionMode`/`run.setConfigOption` (fail-closed on catalog lookup error and on non-`thought_level|model` categories), wait-any. Not verified: GLM's real option category.
- **PR-G (#273), PR-F (#272), PR-F2 (#276), PR-E2 (#275):** see PR bodies; web PRs include light+dark screenshot reviews against sandbox daemons (isolated `SMIND_HOME`/port). Not verified anywhere: real GLM/Codex agents for model lists and `--max-turns`.
- **Recurring, unrelated:** `TestServer_TerminalAttach_*` and a few `@smind/ui` `App.test.tsx` tests time out intermittently under load; they pass when rerun alone.
- **D13 (derived `needs-continue` disposition):** `TestDispositionFor`, `TestDispositionIsNotAStatus` (status values unchanged), `TestRegistry_DispositionNeedsContinueSurvivesRehydrate` (live, listed and after a restart), `TestRunDispositionNeedsContinueOnAMaxTurnStop` / `TestRunDispositionAbsentForANormalFinish` (run.list, run.logs and the run.status event of a real fake-agent run ending `max_turn_requests`/`max_tokens`), `TestMCPTools_DispositionNeedsContinue` (task_wait keeps `state: done` and says the work is not finished; task_status, task_logs, run_list), `TestRunStatusCellShowsTheDisposition`; web: `describeRunFooter` cases (known caps, unknown cap, ignored unless done, older daemon unchanged), `RunTimeline` Continue on a disposition, `useRunTimeline` run.list/run.logs carry it. Light and dark screenshots of the run footer on a real daemon (`smind serve` with a fake GLM agent): "Hit the tool-turn limit · not finished · Continue" next to a normal run with no warning. Not verified: a real GLM agent hitting `--max-turns`; `budget_exceeded` (nothing emits it yet).
