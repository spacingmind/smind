# Mobile: read-only task tree (ADR-0041 M1 / AC28 first half)

Status: active (2026-10-23).

Size: M (5) — scope 1 (mobile + a test-only harness seed), PRs 1, risk 1 (E2EE frame size, no write paths), decisions 1 (placement, below), verification 1 (390 px light+dark web smoke; no device).

On the phone, a task with children shows its subtree: tier, run state, pending permissions, rate-limit state with countdown, unread state and the cost rollup. Tapping a node opens that
task. **Strictly read-only**: no launch, stop or archive from the phone. Presentation follows ADR-0036 (`mobile/src/theme`, `mobile/src/ui/*`, `@expo/ui` via `UiHost`).
No daemon change: the contract is `task.tree {taskId}` (`internal/wsapi/task_tree.go`, node shape `internal/tasktree/tree.go`).

## Contract (read from the daemon)

`task.tree {taskId}` → `{ nodes: Node[], truncated: boolean }`; `nodes` is the flat DFS pre-order list (root first, siblings by task id, `depth` from the queried root), capped at
`tasktree.MaxNodes = 500`. Node: `id, parentId, workspaceId, title, depth, taskStatus, tier ("supervisor"|"leader"|"coder"|null), childCount (live direct children),
run{state ("running"|"waiting_permission"|"done"|"error"|"stopped"|"idle"), runningRunIds, pendingPermissions, disposition?, failureKind?, window?, resetAt? (RFC 3339 UTC)},
counts{running,waitingPermission,failed,done,idle} (live strict descendants), usage{own,subtree: {runs, input/output/cacheRead/cacheWrite/reasoningTokens: number|null, costUsd: number|null},
archivedDescendants}`. A null `costUsd` is "unknown" and is shown as `n/a`, never `$0`.

## Scope and write set

Mobile only. NEW: `mobile/src/taskTree.ts` (pure), `mobile/src/__tests__/taskTree.test.ts`, `mobile/src/screens/task/TaskTreeSection.tsx` (+ `TaskTreeRow.tsx`),
`mobile/scripts/smoke/stages/70-task-tree.mjs`. EDIT: `mobile/src/api.ts` (add `TaskTreeNode` types + `fetchTaskTree`), `mobile/src/screens/TaskDetailScreen.tsx` (mount the section
only), the device harness `internal/relay/bridge/devharness/main.go` (+ test) for a test-only `seed-tree` command. NOT TOUCHED: `mobile/app/new-task.tsx`, `mobile/src/newTask*`,
relay/E2EE, theme tokens, `src/ui/*`, the daemon.

## Decisions

1. **Placement:** a "Subtasks" section at the top of the existing task screen (`TaskDetailScreen`), rendered only when the loaded tree has more than one node (a leaf/untiered task
   has no section, no empty card). Rows navigate with `router.push('/task/<id>')`. The task list screen is unchanged. (Alternative, rejected: a new route; it would duplicate the
   task screen's header and loading.)
2. **Expand/collapse:** the section header toggles the whole list; per-node collapse is not offered (read-only, bounded list). No height animation: the content swaps in one frame, so
   there are no half-animated frames. The chevron rotates by swapping an icon, not by animating.
3. **Bounds:** the mobile renders at most `MAX_RENDERED_NODES = 100` nodes (pre-order prefix); the rest is one visible `n more` row. The daemon's `truncated` (500-node cap) adds
   `more not shown` to that row. A 500-node tree is about 200 KiB of JSON, far below the 1 MiB E2EE frame, but the render bound keeps the list small regardless.
4. **Unread / child-update state:** the daemon has no per-node unread field. A node is "unread" when its id is in the session-scoped `PendingApprovalSet` (`permission.pending` seen
   since the last reset, the same honest rule the task list uses) or it has `run.pendingPermissions > 0`. The section header also rolls up the subtree attention (waiting + failed).
5. **Rate-limit state:** `run.failureKind === 'rate_limited'` renders `Rate limited` plus `· resets in 12m` from `resetAt` against an injected `now` (pure), no countdown text when
   `resetAt` is absent or past (`Rate limited` only). The text is recomputed on a coarse timer (30 s), not per frame.
6. **Cost:** `$0.42 (subtree $1.37)`; either side that is null renders `n/a`; the subtree part is omitted when the node has no children. Cost under $0.01 renders `<$0.01`.

## Acceptance criteria

- AC1 `fetchTaskTree(conn, taskId)` calls `task.tree {taskId}` over the connection and returns typed `{nodes, truncated}`.
- AC2 A task whose tree has more than one node shows the Subtasks section with every rendered node's tier, run state, pending-permission count, cost; a leaf task shows no section.
- AC3 Nesting is indentation by `depth` from the daemon's pre-order list; order is preserved; orphaned or malformed lists never crash (depth clamped, missing fields tolerated).
- AC4 Run state words: running / waiting for permission / done / error / stopped / idle, with `needs-continue` shown as a state note; rate-limited overrides `error`.
- AC5 Rate-limited node shows `Rate limited` and a countdown when `resetAt` is in the future.
- AC6 Unread dot on nodes per Decision 4; the header shows an attention rollup (waiting + failed counts of the visible subtree).
- AC7 Cost rollup own and subtree, `n/a` when unknown, never `0`/`$0.00` for unknown.
- AC8 More than `MAX_RENDERED_NODES` nodes, or `truncated: true`, renders a visible `n more` row.
- AC9 Tapping a node opens that task; the root row is not a link to itself.
- AC10 Read-only: no control in the section launches, stops or archives anything.
- AC11 Smoke frames (light + dark, ~390 px) reviewed by eye: nested tree, waiting for permission, rate-limited with countdown, failed, cost shown and n/a, truncated, leaf task (no
  section), collapse/expand frames.
- AC12 `cd mobile && npx tsc --noEmit && npm test`, `task lint`, `task test` green.

## Test scenarios (names are `describe/it` titles in `mobile/src/__tests__/taskTree.test.ts` unless noted)

- `taskTree: buildTreeRows keeps pre-order and indents by depth`
- `taskTree: buildTreeRows is empty for a single-node (leaf) tree`
- `taskTree: buildTreeRows caps at MAX_RENDERED_NODES and reports hiddenCount`
- `taskTree: buildTreeRows adds the daemon-truncated flag to the more row`
- `taskTree: buildTreeRows tolerates a negative or missing depth`
- `taskTree: runStateLabel maps every daemon state and needs-continue`
- `taskTree: rate_limited failure overrides error and shows the countdown`
- `taskTree: rate limit without resetAt or with a past resetAt shows no countdown`
- `taskTree: formatCountdown rounds to s/m/h/d`
- `taskTree: formatCost shows n/a for null, <$0.01 for tiny, never $0 for unknown`
- `taskTree: costLabel shows own and subtree, omits subtree for a childless node`
- `taskTree: attentionRollup counts waiting and failed over the subtree`
- `taskTree: unread comes from pendingTaskIds or pendingPermissions`
- `taskTree: tierLabel maps supervisor/leader/coder and null`
- `api: fetchTaskTree sends task.tree {taskId} and returns the typed result` (`mobile/src/__tests__/api.test.ts`)
- Harness: `TestSeedTree...` in `internal/relay/bridge/devharness/main_test.go` (seed-tree builds the documented shape).
- Smoke stage `70-task-tree.mjs` (frames listed in AC11).

## Progress

- 2026-10-23: plan written; Peer 1 (pure module + api), Peer 2 (UI + harness seed + smoke) pending.

## Validation

(filled when each AC is confirmed)
