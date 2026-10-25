# Sidebar stage view (Working / Ready to review / Done)

Size: M (5) — scope 1, PRs 1, risk 1, decisions 1, verify 1 (web only; screenshots)

Owner: Lead ITEM N. Inputs: ADR-0039 (`task.git`/`task.gitList`), ADR-0032 (shared TanStack queries/stores),
the existing "Group by status" view (AC6 of web-sidebar-attention), Paseo's sidebar status view model.

## Goal

A third sidebar view that groups tasks by where the work is in its lifecycle, derived from run state AND git/PR
state, not run state alone. Default stays the workspace/space tree.

## What exists (read)

- `lib/sidebar-status-groups.ts`: 5 run-state buckets; `app-sidebar.tsx` `StatusGroupedList`/`StatusGroupItem`
  (`useNodeOpen("status:<key>")` persists collapse in `collapsedSidebarNodes`).
- `SidebarGroupMode = "tree" | "status"` in `lib/sidebar-preferences.ts`, shared via `stores/sidebar-group-mode-store.ts`;
  global (no per-workspace mechanism exists).
- The sidebar already calls `useWorkspaceTaskGit(workspaceIds)` (one `task.gitList` per workspace, kept live by the
  `task.git` event) and `useTaskAttention` (`runStatus`, `attention`). **Confirmed: everything the stage view needs is
  already in the sidebar; no new RPC, polling, `gh` call or subscription.**
- Archived tasks are NOT in the sidebar's task list; the tree shows them per workspace through `useArchivedTasks`
  (its own RPC), only in the tree view.

## Acceptance criteria

1. AC1 Mode control: the toggle becomes a 3-way segmented control Workspace / Status / Stage; default Workspace (tree);
   choice persists across reload via the existing sidebar-preferences store. Status view is kept unchanged.
2. AC2 Groups in order Working, Ready to review, Done, then Idle (only when non-empty); empty groups are not shown.
3. AC3 Stage rules (see table) come from a pure `stageForTask`.
4. AC4 Done is collapsed by default, header shows count; every header shows a count and collapses/expands, persisted in
   `collapsedSidebarNodes` as `stage:<key>`.
5. AC5 Within a group tasks sort by `Task.UpdatedAt` descending (the same "last activity" the row hover card shows),
   ties by id descending.
6. AC6 Rows use the existing `TaskRows` rendering; each row additionally shows the workspace name quietly.
7. AC7 Live: a task moves group when run status, attention, or `task.git` state changes, with no extra daemon call.
8. AC8 Rendering the stage view issues no daemon call the tree view does not.

## Stage rules (`stageForTask(task, signals)`)

Inputs: latest run status, attention reasons, `TaskGit | undefined`.

| Condition (first match wins) | Stage |
|---|---|
| run status `running`, or attention `permission` | working |
| `git.merged.state` merged, or `git.pr.state` merged | done |
| dirty worktree, `commits.aheadOfBase > 0`, or `pr.state` open (draft included) | ready |
| otherwise (idle, nothing to review) | idle |

A failed last run follows the same rows (so with work to review it is Ready and keeps its error dot from the row).
`task.git` absent (not loaded yet) falls back to run state only: running → working, everything else → idle. It never
throws and never lands in Done.

## Decisions

- D1 Coexistence: 3-way control instead of a second icon; **Status view stays** (not replaced, nothing removed).
- D2 Queued / held retry: the sidebar has no queue data (`chat.queueList` is per-chat and subscribed only by the open
  chat; adding it would be a new subscription). A queued prompt only exists while that chat's run is running, which is
  already Working; a held retry shows as a running run. So Working = running or permission. Called out in the PR.
- D3 Archived: not listed by the sidebar's task list, so Done shows merged-PR tasks only; archived tasks stay under the
  tree's per-workspace Archived row. Fetching archived lists for the stage view would add RPCs (AC8).
- D4 Idle group: a fourth "Idle" group at the end, shown only when non-empty, so no task is hidden.
- D5 Persistence: global preference (no per-workspace mechanism exists). Mode type becomes `"tree" | "status" | "stage"`.
- D6 Sort field: `UpdatedAt`.

## Test scenarios

Pure (`lib/sidebar-stage-groups.test.ts`, table test):
- running / permission-waiting → working
- done + dirty → ready; done + ahead commits → ready
- open PR unmerged, draft PR → ready
- merged PR → done; `merged.state` merged → done
- failed last run + ahead commits → ready
- failed last run + nothing to review → idle
- no `task.git` data → no throw; running → working, otherwise idle
- a run starting moves a ready task to working; PR merging moves it to done
- groups ordering, empty groups dropped, sort by UpdatedAt desc

Component (`app-sidebar.test.tsx`):
- default mode is tree
- control switches mode and persists across remount
- Done collapsed with its count; expands on click; collapsed state persisted
- stage view sorts by activity; row shows workspace name
- fake client call counter: stage view makes no daemon calls beyond the tree view

## Progress

- [x] spec committed
- [x] stage-groups lib + tests
- [x] mode type/storage + 3-way control
- [x] StageGroupedList UI
- [x] screenshots, PR
- Merged in #597 (`feat(web): sidebar stage view (Working / Ready to review / Done)`). Plan closed in the 2026-10-25 plans hygiene pass.

## Validation

AC to PR map: AC1-AC8 all shipped in #597; the `AppSidebar stage view` describe block in `components/app-sidebar.test.tsx` and
`lib/sidebar-stage-groups.test.ts` exist on `origin/develop` (confirmed 2026-10-25).

- tsc -b packages/ui clean; ui vitest 323 files / 3626 tests pass; task lint and task test exit 0.
- AC1-AC8: covered by lib/sidebar-stage-groups.test.ts and app-sidebar.test.tsx (default tree, persistence across remount,
  Done collapsed with count and persisted expand, sort by UpdatedAt, workspace name on rows, Ready->Working and
  Ready->Done on events, equal daemon call counts tree vs stage).
- Mutation checks: dropping the aheadOfBase rule fails 4 stage tests; dropping merged.state fails 2; making Done open by
  default fails 1 sidebar test.
- Screenshots (light+dark, 1280+390, incl. transitions): /tmp/stage-view-shots/ (INDEX.md there).
- Known: titles truncate harder than in the tree at 256px because of the workspace chip; the control's highlight lags the
  list by about one frame on click.
