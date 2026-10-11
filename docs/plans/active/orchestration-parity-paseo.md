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
| G17 | Worktree **setup/teardown** (deps, env copy, migrations) | `paseo.json` `worktree.setup/teardown` | none | Fresh task worktree has no deps/`.env`; every task starts broken for JS/Go-with-env repos | P0 | M–L | **Yes: D1** (trust model + file format + exec of repo/daemon-defined commands) |
| G18 | Per-task port allocation | `servicePorts`, `$PASEO_PORT` | none | Parallel dev servers collide | P1 | M | Yes: part of D1 |
| G19 | Workspace scripts / services | `scripts`, `start/stop_workspace_script` | none (terminals only) | no named, supervised commands | P2 | L | Yes: D1 follow-on |
| G20 | Model discovery + update | `list_models`, `update_agent settings.model` | **no model concept** in runs/profiles/`provider.list` | Can't choose or list models | P1 | L | **Yes: D2** (data model: `model` on run config + profile) |
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
| G31 | Dogfood gaps from the `cancel_kind` leader ("Step 3 dogfood gap log") | n/a | **not landed yet** | fold in when it appears | ? | ? | pending |

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

## 4. Decisions needed (not built here)

| # | Item | Why it's a decision | Recommendation |
|---|---|---|---|
| D1 | **Worktree setup/teardown + per-task port (+ later scripts/services)** (G17–G19) | New exec-of-commands surface (code execution on task create), and the file/format question: repo-committed file read from the base branch (Paseo's `paseo.json`) vs daemon-side config. A cloned untrusted repo would get code execution on the first `task_new` with the Paseo model. | **Daemon-side, human-authored, per-workspace config** (new `workspaces.setup_script`/`teardown_script`, set only via CLI/web, never via MCP or any non-human source), matching the ADR-0014/0018 precedent that standing config is the human's. Commands run as an argv list or `sh -c` of the stored script in the worktree with `SMIND_SOURCE_CHECKOUT_PATH`, `SMIND_WORKTREE_PATH`, `SMIND_BRANCH`, `SMIND_PORT` (allocated from a configurable range, recorded on the task); bounded time, output kept in a task event; setup failure marks the task `setup_failed` and keeps the worktree. Optional later: a repo `smind.json` honoured only after a trust-on-first-use hash approval by a human. Needs an ADR. |
| D2 | **Model selection** (G20) | Runs/profiles have no `model` field; adding one is a data-model/wire change across run.start, profile, provider.list. | Add `model` to the run config and profile (ADR), populated from ACP model config options and a static Claude catalog; then `provider_list` can list models and `task_set_option` covers live switches. Until then GLM/Claude default models apply. |
| D3 | **Approval tools over MCP** (G21) | Would reverse ADR-0017 #1 / ADR-0019 #6, the user's standing rule. | **Keep as is.** The wait/notify pair already surfaces the pending request so the human is pulled in; improve that (D4) instead of delegating approval. |
| D4 | **Push notification to the orchestrator** (G22) | ADR-0017 §3 deferred MCP resources/notifications; client support varies. | Ship wait-any (PR-C) now; revisit push only if wait-any proves insufficient in daily use. |
| D5 | **Schedules & heartbeats** (G23) | ROADMAP Phase 5, demand-driven. | **Defer.** The host's own `/loop` / cron covers it today; no demand yet in smind use. |
| D6 | **Terminals over MCP** (G24) | New remote-exec surface for agents. | **Defer**: the orchestrator already has its own shell; add only if a non-shell orchestrator appears. |
| D7 | **Browser tools** (G25) | Large surface, host-bound. | **Don't build**; `smind mcp add playwright …` (ADR-0018) already gives spawned agents a browser. |
| D8 | **Auto-inject `smind mcp serve` into spawned agents** (G26) | Recursion and cost policy; self-reported `parentTaskId` can't enforce the depth guard. | Opt-in only: documented manual `smind mcp add` in the `smind` skill; no automatic injection until the depth/concurrency guard (O2 Wave 2) exists. |
| D9 | **`task_create_pr` / `task_commit` via MCP** (G27) | Pushes to a remote / writes history; outward-facing. | Read-only `task_diff` is safe to add later without an ADR; PR creation stays human/CLI until asked. |
| D10 | **`workspace_create` via MCP** (G28) | Registers an arbitrary path as somewhere agents run. | **Human-only** (CLI/web). |
| D11 | **Skill auto-sync, selection UI, startup repair** (G30) | New daemon capability + sync contract. | CLI `smind skill install` first (this plan); revisit once the bundle has proven useful. |

## 5. Coordination

The `cancel_kind` leader (`feat/queue-cancel-kind`) is dogfooding smind
orchestration. As of this plan's first commit neither that branch nor its
"Step 3 dogfood gap log" is on `origin` (`git ls-remote` empty, no new
section in `orchestration-and-metering.md` on `origin/develop`). G16
(queue tools over MCP) is deliberately left to land after its `cancel_kind`
change, to avoid touching `chat.queueCancel`. Fold its gap list into the
table (G31) when it appears.

## Progress

- [x] Phase A: inventory + gap table (this file)
- [ ] G31: fold in the dogfood gap log when it lands
- [ ] PR-A: discovery tools + MCP instructions
- [ ] PR-B: baseRef/branch/PR task creation + `task_archive`
- [ ] PR-C: `task_set_mode`, `task_options`/`task_set_option`, `task_wait` wait-any
- [ ] PR-D: bundled skills + `smind skill install|ls|uninstall`
- [ ] D1–D11 answered by the user

## Validation

(filled in as each PR is verified: test names, `task test`, `task lint`,
sandbox-daemon MCP smoke runs with an isolated `SMIND_HOME` and port.)
