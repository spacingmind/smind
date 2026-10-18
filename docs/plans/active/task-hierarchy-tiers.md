# Task hierarchy tiers (supervisor → leader → coder), made visible and manageable

**Question (2026-10-11):** does smind now manage tasks like Paseo, but with
*clearer layering*? The user runs a 3-tier workflow every day (a supervisor,
then leaders, then coders). On Paseo the sub-agents are shown mostly flat,
with only a `paseo.parent-agent-id` label. smind should make the tiers
obvious and manageable.

**Short answer:** the *data model and daemon guards* are already ahead of
Paseo (a real `tasks.parent_task_id`, a depth guard, MCP/CLI parenting).
The *surface* is not: the web UI has no hierarchy at all, there is no
tree/rollup read API, and the per-tree concurrency guard promised by O2
Wave 2 was never built. This plan lists every gap, builds the ones that need
no architecture decision, and lists the rest with a recommendation.

Sources: `origin/develop` 17d0c6a (`internal/store/tasks.go`,
`internal/workspace/task.go`, `internal/runs/{registry,queue}.go`,
`internal/wsapi/handlers.go`, `cmd/smind/mcp*.go`,
`web/packages/ui/src/{components/app-sidebar.tsx,components/task-detail.tsx,
lib/workspace-tree.ts,hooks/use-task-attention.ts}`); Paseo's live MCP
`list_agents` output (flat; the only link is the label
`paseo.parent-agent-id`); `refs/paseo/packages/app/src/subagents/*`
(`select.ts`, `track-presentation.ts`, `archive-finished.ts`); and
[orchestration-and-metering.md](orchestration-and-metering.md) §O2.

## 1. What exists today (verified on `origin/develop`)

- **Store:** `tasks.parent_task_id` (FK, nullable); `CreateTask` validates
  parent exists and is in the same workspace; `ListTasksByParent`,
  `TaskDepth`; `DeleteTask` detaches children (`parent_task_id = NULL`).
- **Daemon:** `workspace.Manager.CreateTask(WithParentTask)` enforces
  `orchestration.maxDepth` (default 2: root = depth 0, so three tiers);
  `task.create` / `task.list` accept `parentTaskId`; `task.get` and every
  task event carry `ParentTaskID` (Go field name on the wire, no json tags).
  `task.archive` refuses while the task itself has a running/queued run
  (`Registry.TryBeginArchive`) but says nothing about children.
- **CLI/MCP:** `task new --parent`, `task ls --parent`, MCP `task_new` /
  `task_list` `parentTaskId`; `task_wait` wait-any over 2–16 `runIds`;
  profiles with model (ADR-0014/0025); usage per run (ADR-0020); prompt
  queue (ADR-0021); bundled `smind-leader` skill (#279).
- **Not there:** `grep parentTaskId|ParentTaskID web/packages/ui/src` is
  empty (the TS `Task` type does not even declare the field). The
  `orchestration.maxConcurrentRunsPerTree` guard in the O2 spec has no
  code: `git grep maxConcurrentRunsPerTree` hits only the plan text.

## 2. Gap table

Effort: XS < ½ day, S ≈ 1 day, M 2–3 days (GLM coder time, with tests).
"Decision" = needs the user's call (AGENTS.md rule d).

| # | Capability | Paseo (live / `refs/paseo`) | smind today | Gap | Pri | Effort | Decision? |
|---|---|---|---|---|---|---|---|
| H1 | Children nested under parent in the sidebar | Flat list; sub-agents live in a composer "track pill" + a sub-agent list on the parent (`subagents/select.ts`), not in the sidebar | Flat per space; no `ParentTaskID` in the TS type | Build the nesting, collapse, depth indent, tier styling | P0 | M | No |
| H2 | Attention rollup on a collapsed parent | Pill lists per-state counts, "no mark for done" (`track-presentation.ts`) | Workspace/space rows have an aggregate dot; a task row has its own dot only | A child that needs a permission / failed / finished-unread must bubble to its collapsed parent | P0 | S | No |
| H3 | "Spawned by" breadcrumb + children strip with status pills, jump parent/child | Pill above the composer opens the sub-agent list | Header shows status/branch/setup/usage only | Build breadcrumb + strip + jump | P0 | M | No |
| H4 | Archive a parent with live children | `archive-finished.ts`: explicit "archive finished sub-agents" action, never implicit | `task.archive` ignores children; archived parent leaves live children orphaned with no UI cue | Confirm dialog listing live children; archive the parent alone (no cascade); orphans render at top level | P0 | S | No (no cascade) |
| H5 | Tree rollups: usage/cost + status summary | none (usage is per agent) | `usage.summary groupBy task` sums per task; nothing sums a subtree | One `task.tree` read model; header shows "2 running · 1 needs you · 3 done" and tree usage | P1 | M | No |
| H6 | Per-tree concurrency guard | none (`create_agent` is unbounded) | `maxDepth` only; a leader can start any number of coders | O2 Wave 2 spec: 5th concurrent run in a tree is rejected, frees up when one ends, configurable, atomic | P0 | M | No (config key already in the O2 spec) |
| H7 | MCP: whole org in one call | `list_agents` (flat, label-parented) | `task_list` (one level, no status/usage/permissions) | `task_tree` MCP tool + `task.tree` wsapi | P0 | M | No (additive) |
| H8 | MCP: wait for all/any child | push notifications | `task_wait runIds` (needs the ids) | `task_wait {childrenOf}` resolves the running runs of a task's children | P1 | XS | No (additive input) |
| H9 | Tier/role label | none (label only) | nothing | Derive from structure; **do not add a field** (see §6) | P1 | XS | **Yes: report, recommend derive** |
| H10 | Orphans / stale trees | n/a | `ListTasks` hides archived tasks, so a live child of an archived parent has no parent in the list | Define and test: orphan = root; reconnect re-derives the whole tree | P0 | S | No |
| H11 | Cascade archive / bulk stop of a tree | `archive-finished` only (explicit, finished only) | none | Two explicit actions, never a cascade on archive: **Archive finished descendants** + **Stop all in tree** (user-approved 2026-10-18; spec in PR-H11, §5) | P2 | M | Decided 2026-10-18 (§6.2) |

## 3. Decisions already made (no ADR needed)

1. **Tiers are derived, not stored** (H9). A task's tier comes from its
   position: no parent + has live children = **supervisor**; parent +
   live children = **leader**; parent + no live children = **coder**; no
   parent and no children = untiered (plain task, no label). Computed by one
   pure function in the UI (`tierOf`) and one in Go (`tasktree.TierOf`), same
   rules, both unit-tested. No schema change.
2. **The hierarchy trumps the space grouping.** A child renders under its
   parent wherever the parent renders, even if its own `SpaceID` differs
   (`task_new` makes children ungrouped by default). A child whose parent is
   archived, deleted, or not in the loaded list is an **orphan** and renders
   as a top-level row in its own space/ungrouped bucket (never dropped).
3. **Archive never cascades.** Archiving a parent archives only the parent
   (the existing guard still refuses while *its own* run is active). The UI
   dialog lists the live children so the user sees what becomes top-level.
4. **Usage rollup includes archived descendants; counts do not.** Archiving a
   coder must not make the tree's cost drop, so `usage.subtree` sums every
   descendant reachable through `parent_task_id`, archived or not. Status
   counts and node lists include only live (non-archived) tasks.
5. **Guard semantics** (from the O2 spec): per *tree* = root task + all its
   descendants (a lone root task is a tree of one). Limit
   `orchestration.maxConcurrentRunsPerTree`, default 4, validated `>= 1`.
   A run counts while `StatusRunning` (including while waiting for a
   permission). Exceeding it returns a model-readable error; **all**
   `whenBusy` modes reject when the chat is idle and the tree is full. A
   prompt already *queued* is never cancelled by the guard: it stays queued
   and is retried when a run in the tree ends.

## 4. Wire shapes (the contract between the PRs)

### 4.1 `task.tree` (wsapi, read-only)

Params: exactly one of `{workspaceId}` (every live root and orphan of the
workspace, with subtrees) or `{taskId}` (that task and its live
descendants; the task need not be a root).

Result: `{ "nodes": TreeNode[], "truncated": boolean }`, flat, depth-first
pre-order, siblings ordered by task id; at most 500 nodes, then
`truncated: true`.

```
TreeNode = {
  id, parentId | null, workspaceId, title, depth,   // depth from the queried root(s)
  taskStatus,                                       // task.Status
  tier: "supervisor" | "leader" | "coder" | null,
  childCount,                                       // live direct children
  run: {
    state: "running" | "waiting_permission" | "done" | "error" | "stopped" | "idle",
    runningRunIds: string[],                        // this task's own running runs
    pendingPermissions: number,                     // this task's own
  },
  counts,        // over live descendants (not self):
                 //   { running, waitingPermission, failed, done, idle }
  usage: { own: UsageTotals, subtree: UsageTotals, archivedDescendants: number }
}
UsageTotals = { runs, inputTokens|null, outputTokens|null, cacheReadTokens|null,
                cacheWriteTokens|null, reasoningTokens|null, costUsd|null }
```

`state` is the task's *latest run* (waiting_permission wins while any run
has a pending request). A field no run reported is `null`, never `0`
(ADR-0020: nullable never means 0). `subtree` = `own` + every descendant's
`own`, **each task's own runs counted exactly once** (no double counting).

### 4.2 MCP `task_tree`

Input `{workspaceId?, taskId?}` (exactly one). Output
`{ roots: TreeNodeNested[], truncated, summary }` where each nested node is
a `TreeNode` plus `children: TreeNodeNested[]`, and `summary` is a one-line
string for the model, e.g. `"3 tasks: 2 running, 1 waiting_permission"`.

### 4.3 MCP `task_wait {childrenOf}`

`childrenOf: <taskId>` instead of `runId`/`runIds`: resolves the *running*
runs of that task's direct live children (via `task.tree`) and waits for the
first terminal/permission, same output as wait-any. No running child runs →
returns immediately with `state` `idle` and a `message` listing the
children's states. More than 16 running runs → the 16 oldest-started, with a
`message` saying so.

## 5. Acceptance criteria and named tests

### PR-G: per-tree concurrency guard (backend)

- **G-AC1** `orchestration.maxConcurrentRunsPerTree` in
  `internal/config`, default 4, `Load` rejects `< 1`.
  Tests: `TestDefault_OrchestrationMaxConcurrentRunsPerTree`,
  `TestLoad_OrchestrationMaxConcurrentRunsPerTreeOverride`,
  `TestLoad_OrchestrationMaxConcurrentRunsPerTreeRejectsZero`.
- **G-AC2** `Registry.SetMaxConcurrentRunsPerTree(n)`; unset still enforces
  4 (like `maxDepth`'s unwired default). The tree root of a run is resolved
  from the store **before** `reg.mu` is taken (parent links are immutable
  while a run exists) and cached on the `run`; the count over running runs
  with the same root and the registration `reg.runs[id] = r` happen in **one
  `reg.mu` critical section** in both `Start` and `StartWhenBusy` (queue
  delivery goes through `Start`). Returns `*TreeBusyError{RootTaskID, Limit,
  Running}` whose message is model-readable ("Task tree concurrency limit
  reached (4 runs already running in task tree #N): wait for one to finish
  (task_wait) or stop one (task_stop), then try again").
- **G-AC3** Tests (`internal/runs/tree_guard_test.go`, all with `-race`):
  - `TestTreeGuard_FifthConcurrentRunRejected`: root + 2 children + 1
    grandchild, 4 runs running, 5th `Start` (any task of the tree) returns
    `*TreeBusyError`.
  - `TestTreeGuard_SlotFreesWhenRunEnds`: after one run finishes the 5th
    starts.
  - `TestTreeGuard_SeparateTreesDoNotShareBudget`: tree A at the limit does
    not block tree B.
  - `TestTreeGuard_ConcurrentStartsAdmitExactlyLimit`: 32 goroutines
    `Start` in one tree at limit 4 → exactly 4 succeed, 28 get
    `*TreeBusyError` (atomicity).
  - `TestTreeGuard_ConcurrentStartWhenBusyAdmitsExactlyLimit`: same via
    `StartWhenBusy`.
  - `TestTreeGuard_WaitingPermissionRunStillCountsAsSlot`.
  - `TestTreeGuard_QueuedPromptStaysQueuedAndDeliversWhenSlotFrees`: an
    item whose delivery hits the full tree is **not** cancelled
    (`ChatQueueCancelAuto` never recorded) and starts after a run in
    another chat of the tree ends (finish must retry delivery for queued
    chats beyond its own).
  - `TestTreeGuard_ConfiguredLimitHonoured` (limit 2).
  - `TestTreeGuard_RootResolutionIsOneStorePassOutsideLock` (no store read
    while holding `reg.mu`: a test seam counts `reg.mu`-held store calls).
- **G-AC4** wired in `cmd/smind/serve.go`; the error reaches wsapi
  `run.start` / `task.prompt` callers and MCP `task_send` unchanged. Test:
  `TestRunStart_TreeBusyErrorIsReturnedToWSClient` (`internal/wsapi`).

### PR-T: `task.tree`, MCP `task_tree`, `task_wait {childrenOf}` (backend)

- **T-AC1** new package `internal/tasktree`: pure
  `Build(tasks []store.Task, runs []RunFact, usage map[int64]UsageTotals,
  pending map[int64]int, root ...)` → nodes; `TierOf`. No double counting; a
  cycle in `parent_task_id` (corrupt data) terminates (visited set) rather
  than looping. Tests (`internal/tasktree/tree_test.go`):
  - `TestBuild_ThreeTierOrderAndDepth`,
  - `TestBuild_OrphanOfArchivedParentIsRoot`,
  - `TestBuild_SubtreeUsageCountsEachRunOnce` (a→b→c: own+descendants, equal
    to a direct sum over all runs; adding a task never changes a sibling),
  - `TestBuild_ArchivedDescendantUsageIncludedCountsExcluded`,
  - `TestBuild_UnreportedFieldsStayNullNotZero`,
  - `TestBuild_CountsRunningWaitingFailedDone`,
  - `TestBuild_CycleTerminates`,
  - `TestBuild_TruncatesAt500`,
  - `TestTierOf_Table`.
- **T-AC2** `Registry` read helpers (new file, no edits to existing
  functions): `PendingPermissionCounts() map[int64]int` (in-memory, one lock
  pass) and the latest-run-state read; `task.tree` handler in `internal/wsapi`
  joins store tasks, `store` per-task usage sums, and those. One store pass
  per call (no per-node query). Tests (`internal/wsapi/task_tree_test.go`):
  `TestTaskTree_Workspace`, `TestTaskTree_RootedAtTask`,
  `TestTaskTree_ParamsExactlyOne`, `TestTaskTree_WaitingPermissionState`,
  `TestTaskTree_CrossWorkspaceTaskIsError`,
  `TestTaskTree_WideTree200ChildrenIsOneStorePass` (performance guard).
- **T-AC3** MCP `task_tree` and `task_wait {childrenOf}`, with
  `mcp_instructions.go` and the bundled `smind` skill updated.
  Tests (`cmd/smind/mcp_tree_test.go`): `TestMCPTaskTree_Nested`,
  `TestMCPTaskTree_ExactlyOneOfWorkspaceOrTask`,
  `TestMCPTaskWait_ChildrenOf_ReturnsFirstFinished`,
  `TestMCPTaskWait_ChildrenOf_NoRunningChildrenReturnsIdle`,
  `TestMCPTaskWait_ChildrenOf_ExclusiveWithRunId`,
  `TestMCPTaskWait_ChildrenOf_CapsAt16`.

### PR-U1: sidebar task tree (web)

- **U1-AC1** `Task` TS type declares `ParentTaskID?: number | null` (exactly
  that text, `/** The parent task id; null/absent for a root task. */`).
- **U1-AC2** pure `lib/task-tree.ts`: `nestTasks(tasks)` → `TaskNode[]`
  (`{task, depth, children, tier}`), O(n), cycle-safe, orphans = roots,
  stable id order; `descendantsOf`; `attentionRollup(node, attention,
  runStatus)` → `{ reason | null, count }` over *all descendants* counting
  `permission`, `error`/failed, `finished` (unread); `tierOf`. Tests
  (`lib/task-tree.test.ts`): `nests children under parent in id order`,
  `orphan whose parent is missing is a root`, `child in another space
  nests under its parent`, `cycle does not loop`, `depth is derived from
  the chain`, `rollup counts permission before error before finished`,
  `rollup counts every descendant once`, `rollup ignores a seen finished
  child`, `tierOf table`.
- **U1-AC3** (tier-on-row part superseded 2026-10-12 by [sidebar-polish.md](../completed/sidebar-polish.md) S4: the tier is now in the hover card and aria-label, not on the row; depth indent is per-row padding) the sidebar tree view renders nested rows: depth indent with a
  guide line per level (no change to root rows' geometry), a chevron on a
  parent row to collapse/expand (default expanded; state kept per task in
  `localStorage`, key `smind.sidebar.collapsedTasks`), a child-count chip,
  and a tier chip (`supervisor`/`leader`/`coder`, muted, no new colour). A
  **collapsed** parent shows an attention rollup (the most urgent
  descendant reason's dot + the count of descendants needing attention) in
  the row's reserved attention slot area; an expanded parent shows none
  (its children show their own). Layout stability: the reserved slots do not
  shift when a rollup appears. Space/workspace aggregate dots unchanged.
  Pinned, search and group-by-status lists stay flat (a child row there shows
  its tier chip only). Tests (`app-sidebar.test.tsx` additions):
  `renders a child nested under its parent`,
  `collapsing a parent hides its subtree`,
  `collapsed parent shows the attention rollup of a descendant needing a permission`,
  `collapsed parent rollup shows the most urgent reason and a count`,
  `expanded parent shows no rollup`,
  `rollup appears live when run.status error arrives for a grandchild`,
  `child created via task.created event nests live`,
  `task.archived of a parent leaves its live child as a root row`,
  `reconnect refetch rebuilds the same tree`,
  `a 300-task wide tree renders without re-rendering unrelated rows`
  (memo guard).
- **U1-AC4** screenshots (light + dark): 3-tier tree expanded, collapsed
  with a rollup, an orphan; viewed by the leader.

### PR-U2: task header + archive confirm + rollup (web)

- **U2-AC1** Header: a "Spawned by <parent title>" breadcrumb link when
  `ParentTaskID` is set (loaded via `task.get`; shows "(archived)" and is
  inert if the parent is archived); a **children strip** with one status
  pill per live child (title + `StatusDot`; running / needs you / failed /
  done) that jumps to the child; a **status summary** for the subtree
  ("2 running · 1 needs you · 3 done", omitted when there are no
  children); a **usage rollup** chip for the subtree from `task.tree`
  (`formatTokens`/`formatCost`, "—" for unreported, total in a tooltip with
  own vs descendants). Children come from `task.list {workspaceId,
  parentTaskId}` and refetch on `task.created|updated|archived|deleted` and
  on reconnect/`event.dropped`.
  Tests (`task-detail.test.tsx` additions + `task-lineage.test.tsx`):
  `shows spawned-by breadcrumb and jumps to the parent`,
  `no breadcrumb for a root task`,
  `archived parent breadcrumb is inert`,
  `children strip lists live children with status pills`,
  `clicking a child pill selects that child`,
  `children strip updates when a child is created`,
  `status summary counts running, needs you and done`,
  `usage rollup shows em dash when nothing reported and never 0`,
  `usage rollup does not double count own runs`.
- **U2-AC2** `ArchiveTaskDialog` fetches the task's live children when
  opened; if any, lists them (title + status) under "These tasks will stay
  and become top-level" and the confirm button reads "Archive parent only";
  no children → today's dialog unchanged; a child-list fetch failure
  blocks nothing but says it could not check. Tests
  (`crud-dialogs.test.tsx`): `archive dialog lists live children`,
  `archive dialog with no children is unchanged`,
  `archive dialog confirms with task.archive for the parent only`
  (asserts no other `task.archive` call),
  `archive dialog child-list failure still allows archive with a warning`.
- **U2-AC3** screenshots (light + dark) of the header with the strip and the
  archive dialog; viewed by the leader.

### PR-H11: "Archive finished descendants" and "Stop all in tree"

Size: L (6) — scope 2 (daemon + web), PRs 1, risk 1 (races, data loss),
decisions 0 (user approved 2026-10-18; shape below), verify 2 (screenshots +
live daemon).

Quoting §6.2 (the approval's basis): "an explicit 'Archive finished
descendants' action (Paseo's `archive-finished` model: finished only, never
running, never silent) and a 'Stop all runs in this tree' action.
Recommendation: build those two as explicit buttons later, not a cascade on
archive." §3.3 stays true: `task.archive` itself still never cascades.
Reference pattern: `refs/paseo/packages/app/src/subagents/archive-finished.ts`
(explicit action over a snapshot of *finished-only* rows, per-row guard
re-check at execution time, per-row failure collection, retry only on
still-eligible failures).

**Scope boundaries**

- **Surface:** wsapi RPCs + web UI only. The plan lists no CLI parity and no
  MCP tool, so neither is built; an agent must not be able to cascade-archive
  or kill a tree through MCP (test below pins that). The wsapi/UI is the
  human surface (there is no caller-identity concept on wsapi; "human only"
  = "not exposed over MCP").
- **Tree** = the task plus its live (non-archived) descendants (same `tasktree`
  Build as `task.tree`, `Root{TaskID}`).
- Both RPCs take `{taskId, dryRun?}`; `dryRun: true` computes and returns the
  selection without touching anything. The dialog's counts come from the dry
  run, so UI count == server selection (one source of truth).

**Definitions**

- A task is **finished** when its latest run state (`task.tree` `run.state`,
  i.e. in-memory + rehydrated runs) is `done`, `error` or `stopped` **and**
  it has no queued prompt. `idle` (never ran / no known run), `running`,
  `waiting_permission` are *not* finished.
- **Archive eligible** (descendants only, never the task itself): finished
  AND every live descendant of it is also archive-eligible. A finished task
  with an unfinished descendant is *skipped* (reason `has unfinished
  descendants`) so we never orphan running work. Archiving is per task,
  post-order (deepest first), reusing `Registry.TryBeginArchive` +
  `workspace.Manager.ArchiveTask` (checkpoint-first, unchanged). Not
  transactional (git + store); partial progress is reported, never hidden. A
  task whose archive fails keeps its ancestors (they become `skipped`,
  reason `descendant failed to archive`). Bounded: at most 100 tasks per call
  (post-order prefix, so subtree closure holds); `remaining` reports the rest.
- **Stop** (the task itself *included*: "all in tree"): (1) cancel every
  queued prompt of every chat in the tree, so a freed per-tree slot is never
  refilled by queue delivery; (2) `Stop` every running run, parents before
  children, then wait for each to reach a terminal state under one shared
  10 s deadline; (3) re-scan (at most 2 more passes) for runs a still-live
  parent started mid-operation. A run that ends on its own mid-operation is
  *not* an error. A run still running at the deadline is `failed`. Idempotent
  (second call on a stopped tree stops nothing).

**Wire** (additive, flat, no null arrays)

```
Item     = { id: number, title: string }
task.archiveFinished {taskId, dryRun?, ids?} ->   // ids: optional allow-list (the dry run's eligible ids)
  { dryRun, eligible: Item[], archived: Item[],
    skipped: (Item & {reason: string})[], failed: (Item & {error: string})[],
    remaining: number }
    // dryRun: eligible filled, archived/failed empty; non-dry: eligible is
    // the selection, archived/failed its outcome, skipped = finished tasks
    // not archived (and lost races, reason from TryBeginArchive).
task.stopTree {taskId, dryRun?} ->
  { dryRun, runs: (Item & {runId: string})[], queuedPrompts: number,
    stopped: (Item & {runId: string})[],
    failed: (Item & {runId: string, error: string})[], queuedCancelled: number }
    // dryRun: runs + queuedPrompts filled; non-dry: stopped/failed/queuedCancelled.
```
`ids` (added after review, additive): when present the confirm archives only
the eligible tasks whose id is listed, so a task that finished while the
dialog was open is never archived unseen; the web sends the dry run's ids.
Errors: unknown task, archived task, bad params (`taskId` required).

**H11-AC1 (runs)** `Registry.StopAndWait(ctx, runID) error` (new file, no
edits to existing funcs): `Stop` + wait on the run's `closedCh` or `ctx`;
already-terminal and unknown-to-memory runs return nil. Tests
(`internal/runs/tree_stop_test.go`, `-race`): `TestStopAndWait_ReturnsWhenRunFinished`,
`TestStopAndWait_ContextDeadlineReturnsError`, `TestStopAndWait_AlreadyFinishedIsNil`.

**H11-AC2 (wsapi archive)** `internal/wsapi/task_tree_actions.go`,
`task.archiveFinished` registered in `handlers.go`. Tests
(`internal/wsapi/task_tree_actions_test.go`, `-race`):
- `TestArchiveFinished_ArchivesOnlyFinishedDescendants` (root + children in
  done/error/stopped/running/idle: only done/error/stopped archived; root,
  running, idle untouched),
- `TestArchiveFinished_DeepTreeArchivesChildrenBeforeParents` (3+ levels;
  order asserted; root never archived),
- `TestArchiveFinished_SkipsFinishedParentWithUnfinishedDescendant`,
- `TestArchiveFinished_FailedChildKeepsItsAncestors` (injected archive
  failure seam),
- `TestArchiveFinished_QueuedPromptMakesTaskNotFinished`,
- `TestArchiveFinished_RunStartingMidOperationIsSkippedNotArchived`
  (task flips to running between selection and archive: refused by
  `TryBeginArchive`, reported `skipped`, worktree intact),
- `TestArchiveFinished_IdempotentSecondCallArchivesNothing`,
- `TestArchiveFinished_DryRunChangesNothing`,
- `TestArchiveFinished_CapsAtHundredAndReportsRemaining` (seam lowers the cap),
- `TestArchiveFinished_OtherTreesUntouched`,
- `TestArchiveFinished_UnknownArchivedAndMissingTaskErrors`,
- `TestArchiveFinished_ArchiveEventsEmittedPerTask` (a `task.archived` event
  per archived task; clients re-derive the tree from them).

**H11-AC3 (wsapi stop)** `task.stopTree` in the same file. Tests:
- `TestStopTree_StopsEveryRunInSubtreeIncludingRoot`,
- `TestStopTree_OnlyTouchesTheSubtree` (a sibling tree's and a sibling
  subtree's runs keep running),
- `TestStopTree_CancelsQueuedPromptsBeforeStopping` (no queued item delivered
  after; `ChatQueueCancelUser` recorded),
- `TestStopTree_FreesSlotsUnderTheTreeGuard` (tree at the limit: after the call
  a new `Start` in the tree succeeds, queued prompt did not take the slot),
- `TestStopTree_RunFinishingMidOperationIsNotAnError`,
- `TestStopTree_RunStartedDuringStopIsStoppedBySecondPass`,
- `TestStopTree_StuckRunIsReportedFailed` (deadline seam),
- `TestStopTree_DryRunStopsNothing`,
- `TestStopTree_IdempotentSecondCallStopsNothing`,
- `TestStopTree_UnknownAndArchivedTaskErrors`,
- `TestTreeActions_NotExposedOverMCP` (neither name appears in the MCP tool
  catalog).

**H11-AC4 (web)** wire types in `lib/task-tree-actions.ts` + pure
`treeActionAvailability` (tests `lib/task-tree-actions.test.ts`:
`stop is enabled when any task in the subtree runs`,
`stop disabled with reason when nothing runs`,
`archive enabled when a descendant is finished`,
`archive disabled with reason when no descendants`,
`archive disabled with reason when descendants are all unfinished`,
`self is never counted for archive but is for stop`). Dialogs
(`components/tree-action-dialogs.tsx`, tests `tree-action-dialogs.test.tsx`):
- `archive finished dialog runs a dry run on open and lists the tasks`,
- `archive finished dialog confirm calls task.archiveFinished once for the task`,
- `archive finished dialog with nothing eligible disables confirm and says why`,
- `archive finished dialog says how many tasks are kept and why`,
- `archive finished partial failure keeps the dialog open and lists failures`,
- `stop tree dialog title counts running tasks and is singular for one`,
- `stop tree dialog notes queued prompts that will be cancelled`,
- `stop tree dialog confirm calls task.stopTree once for the task`,
- `stop tree dialog lists runs that failed to stop`,
- `dry run failure blocks confirm and shows the error`,
- `a dry run landing after close or for another task is ignored`.
Menus (`app-sidebar-crud.test.tsx`, `app-header-task.test.tsx` additions;
entries on every parent task's sidebar dropdown + context menu and on the
header ⋯ menu; icons only via `lib/icons.ts`, drift test stays green):
- `parent task menu offers archive finished and stop all in tree`,
- `a task without descendants offers neither archive finished nor a stop beyond its own run` (rule pinned in the test),
- `stop entry is disabled with its reason when nothing runs`,
- `archive finished entry is disabled with its reason when nothing is finished`,
- `choosing an entry opens its dialog` (sidebar and header),
- `header menu entries behave like the sidebar's`.

**H11-AC5 (screenshots, leader-reviewed)** light + dark: the task menu with
both entries (enabled, and one disabled-with-reason), both confirm dialogs
(counts + list), the partial-failure state of one dialog; and mid-transition
frames of the dialog open animation, each frame viewed.

**H11-AC6 (live)** an isolated daemon (own `SMIND_HOME`/port): a real 3-tier
tree with fake-provider runs, driven through the real UI/WS, confirming the
dialogs' counts equal the server selection and both actions work end to end.

### Cross-cutting

- `task test`, `task lint`, `cd web && bunx tsc -b packages/ui` green on every
  PR. No `pkill`/`killall` by name; isolated `SMIND_HOME` + port for the
  sandbox daemon.
- Docs: this file's Progress/Validation are kept current; the config key is
  added to the O2 section's status line.

## 6. Decisions for the user

1. **Role/tier field — recommendation: derive, don't add one.** The three
   tiers are derivable from structure (§3.1), so no column is needed for the
   UI or for `task_tree`. Known limitation: if the supervisor is *not* a
   smind task (it is your own Claude/Paseo session), the leaders are roots
   with children and would be labelled "supervisor", and the coders
   "leader"/"coder" by their own position. If that is your normal setup,
   say so and the fix is a *display* setting (names per depth: e.g. depth
   0 → leader), not a data-model change. An explicit `role` column would only
   be warranted if you want a task's tier to differ from its position, or a
   profile to *force* its tier. **Not built; waiting on you only if you want
   the explicit field.**
2. **Cascade archive / bulk stop of a tree (H11).** *Decided 2026-10-18
   (user): build the two explicit actions below, no cascade on archive; spec
   in PR-H11 (§5).* Archiving
   checkpoint-commits the worktree but drops ignored files (`.env`,
   `node_modules`), so an unconfirmed cascade can lose work. If you want it:
   an explicit "Archive finished descendants" action (Paseo's
   `archive-finished` model: finished only, never running, never silent) and
   a "Stop all runs in this tree" action. Recommendation: build those two
   as explicit buttons later, not a cascade on archive.
3. **`task.tree` is a new public wsapi method** (flat nodes + a nested MCP
   view). It is additive and exactly what the requested `task_tree` tool
   needs; flagging it because AGENTS.md (d) lists public API shape.

## Decisions (during the build)

- 2026-10-18 H11: *Stop all in tree* includes the task itself; *Archive
  finished descendants* never does. A finished task with an unfinished
  descendant is skipped (never orphan live work). Queued prompts are
  cancelled by Stop-all (otherwise the per-tree guard's retry-on-slot-free
  would refill the slots). No CLI/MCP parity: not in the plan, and an agent
  must not be able to bulk-kill/archive a tree. Counts in the dialogs come
  from a server dry run (one source of truth); menu enable/disable is a
  client heuristic over task.tree-shaped data and the dialog is the
  authority.

## Progress

- 2026-10-11 Phase A: gap analysis and this spec written.
- 2026-10-18 H11: spec written (PR-H11, §5) before implementation.

## Validation

(filled as each criterion is confirmed)
