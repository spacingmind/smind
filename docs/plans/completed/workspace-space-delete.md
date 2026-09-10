# Delete workspace / space

Adds the missing "remove" capability the crud-ui plan (PR #69) deliberately
left out of scope: there is currently no `workspace.delete` or
`space.delete` RPC at all, so a workspace or space, once created, can never
be removed from smind — confirmed by grepping the whole backend and noted
explicitly in `docs/plans/active/crud-ui.md`'s Decisions. `task.archive`
already exists as task's own removal mechanism and is untouched by this
plan; this only adds the workspace/space layer above it.

Two explicit user decisions from this session govern scope:

1. Deleting only ever un-registers a workspace/space (and its tasks) from
   smind's own SQLite tracking. It never touches the real directory on
   disk — smind doesn't own that directory (it's the user's actual git
   repo), the same way VS Code's "Remove from workspace" doesn't delete
   the folder.
2. Deleting a workspace cascades to its spaces and all their tasks (and a
   space's delete cascades to its tasks); there is no "block if children
   exist" mode.

## Acceptance Criteria

- New `internal/store` methods: `DeleteTask`, `DeleteSpace`,
  `DeleteWorkspace`. Given `_pragma=foreign_keys(1)` is already enabled
  (`internal/store/store.go`), each must delete in FK-safe child-before-
  parent order or the delete simply fails loudly (never silently orphans):
  - `DeleteTask(id)`: `run_events` (via each of the task's `runs`) →
    `runs` → `terminal_sessions` → the `tasks` row.
  - `DeleteSpace(id)`: `DeleteTask` for every task in the space → the
    `spaces` row.
  - `DeleteWorkspace(id)`: `workspace_accounts` rows for it →
    `DeleteSpace` for every space in the workspace (cascading to their
    tasks) → `DeleteTask` for every task directly in the workspace with no
    space → the `workspaces` row.
  - No wrapping SQL transaction (this codebase has no transaction
    abstraction yet anywhere — see Decisions for why not introducing one
    here is fine).
- New `internal/workspace.Manager` methods `DeleteTask`, `DeleteSpace`,
  `DeleteWorkspace` sit above the store layer and own the git side, reusing
  `ArchiveTask`'s exact checkpoint-then-remove-worktree logic (extract the
  shared piece rather than duplicating it) for every task being removed
  that still has a live worktree — same data-loss-prevention guarantee
  `ArchiveTask` already gives a single task, now honored for every task a
  cascading workspace/space delete sweeps up. A task that's already
  archived (worktree already gone) skips the git step, same as
  `ArchiveTask` already tolerates. All git-level cleanup for the whole
  cascade happens before any store-layer delete call, matching
  `ArchiveTask`'s own "checkpoint failure aborts before removal" ordering
  — a git failure partway through a cascade must leave the DB completely
  untouched, not a partially-deleted tree.
- New `internal/wsapi` methods `workspace.delete` (`{id}`) and
  `space.delete` (`{id}`), following the existing handler/error-wrapping
  conventions (`internal/wsapi/handlers.go`). Both return a small summary
  of what was removed (counts of tasks/spaces), not just success/failure,
  so the UI can show an accurate "removed N tasks, M spaces" confirmation
  after the fact without a second round trip.
- Web UI: a "Delete workspace" action on the workspace row's existing
  context menu (`app-sidebar.tsx`, next to "Add task"/"Add space") and a
  "Delete space" action on each space row's menu, each behind a confirm
  dialog that states plainly: this only removes it from smind (files on
  disk are untouched), uncommitted work in any task worktree is
  checkpointed to its branch first, and this cannot be undone in smind.
  The confirm dialog shows the actual task/space counts about to be
  removed (fetched via the workspace tree already loaded client-side, not
  a new round trip) before the user commits.
- Out of scope this pass (explicitly deferred, not silently skipped): a
  CLI `smind workspace rm`/`smind space rm` subcommand — the user's
  request was specifically about the missing UI affordance; CLI parity is
  a natural follow-up but doubles the surface of this pass for no
  immediate need.

## Test Scenarios

- Go (`internal/store`):
  - `DeleteTask` removes the task row and its `runs`/`run_events`/
    `terminal_sessions`; a second `GetTask` on the same id errors
    not-found.
  - `DeleteSpace` removes every task in the space plus the space row
    itself; tasks in a *different* space/workspace are untouched.
  - `DeleteWorkspace` removes every space (and their tasks), every
    workspace-level ungrouped task, `workspace_accounts` rows, and the
    workspace row itself; a second workspace's rows are completely
    untouched (the cascade doesn't over-reach).
  - Deleting a workspace/space/task that doesn't exist is a clear
    not-found error, not a silent no-op.
- Go (`internal/workspace`):
  - `Manager.DeleteTask` on a task with an active worktree checkpoints
    uncommitted work (same assertion style `ArchiveTask`'s own test
    already uses — check the branch retains the work after the worktree
    is gone) before removing the worktree and the DB row.
  - A checkpoint failure aborts before any DB row is touched (the task
    and its worktree both still exist afterward) — mirrors
    `ArchiveTask`'s equivalent test.
  - `Manager.DeleteSpace`/`Manager.DeleteWorkspace` checkpoint every task
    they sweep up, not just the first one.
  - An already-archived task inside a cascade is skipped at the git step
    (no error from a missing worktree) but its row is still removed.
- Go (`internal/wsapi`): wire-level tests (existing real-connection
  pattern) for `workspace.delete`/`space.delete` — happy path (correct
  counts returned, a subsequent `workspace.list`/`task.list` no longer
  shows the removed rows), and a nonexistent id surfacing as a clear RPC
  error.
- Web: a confirm-dialog test per action (following `app-sidebar-crud.test.tsx`'s
  existing archive-confirm test pattern) — opens with the right counts,
  confirming calls `workspace.delete`/`space.delete` with the right id and
  refreshes the tree so the removed row(s) are gone from the UI; cancel
  calls neither.

## Decisions

- Cascading deletes are done as sequential store calls in strict
  child-before-parent order, not wrapped in a single SQL transaction:
  this codebase has no `*sql.Tx`-threading convention anywhere yet, and
  because the order is always children-first, a failure partway through
  never orphans anything (FK enforcement would refuse a wrong-order
  delete outright) — it just leaves a smaller, still-consistent tree that
  a retried delete finishes. Introducing a transaction abstraction for
  this alone is more machinery than the actual risk warrants.
- Reuses (via extraction, not duplication) `ArchiveTask`'s checkpoint
  logic rather than inventing a second way to not lose uncommitted work.
- Never touches anything on disk outside smind's own worktree directories
  (`config.Dir()/worktrees/...`) — the workspace's actual repo path is
  read-only from smind's perspective for this feature, full stop.
- CLI parity deferred — see Acceptance Criteria's out-of-scope note.

## Progress

- [x] `internal/store`: `DeleteTask`/`DeleteSpace`/`DeleteWorkspace` + tests
- [x] `internal/workspace`: `Manager.DeleteTask`/`DeleteSpace`/`DeleteWorkspace` + tests (checkpoint reuse)
- [x] `internal/wsapi`: `workspace.delete`/`space.delete` + tests
- [x] Web: confirm dialogs + context menu actions + tests
- [x] Verification

## Validation

- New `internal/store` methods (`DeleteTask`, `DeleteSpace`, `DeleteWorkspace`) delete in the
  documented FK-safe child-before-parent order (`internal/store/tasks.go`,
  `internal/store/spaces.go`, `internal/store/workspaces.go`), with no transaction wrapper, matching
  the AC and the Decisions section. Verified by
  `internal/store/delete_test.go` (`TestStore_DeleteTask`, `TestStore_DeleteSpace`,
  `TestStore_DeleteWorkspace`, and their `*Missing` not-found counterparts) and `go test -race
  ./internal/store/...`.
- New `internal/workspace.Manager` methods (`DeleteTask`, `DeleteSpace`, `DeleteWorkspace`,
  `internal/workspace/delete.go`) reuse `ArchiveTask`'s checkpoint-then-remove-worktree logic via
  the extracted `checkpointAndRemoveWorktree` helper (also now used by `ArchiveTask` itself). All
  git-level cleanup for a cascade runs before the single store-layer delete call, so a checkpoint
  failure anywhere in a space/workspace cascade leaves the database completely untouched. Verified
  by `internal/workspace/delete_test.go`'s `TestManager_DeleteTask`/`DeleteSpace`/`DeleteWorkspace`
  subtests (checkpoint-then-remove happy path with branch-content assertions, checkpoint-failure
  abort-before-DB-write, every task in a cascade gets checkpointed not just the first, an
  already-archived task is skipped at the git step but its row is still removed, not-found errors)
  and `go test -race ./internal/workspace/...`.
- New `internal/wsapi` methods `workspace.delete`/`space.delete` (`internal/wsapi/handlers.go`)
  return `deleteSummaryResult{tasksRemoved, spacesRemoved}`. Verified over a real WebSocket
  connection by `internal/wsapi/wsapi_test.go`'s `TestServer_WorkspaceDelete_HappyPath`,
  `TestServer_WorkspaceDelete_Nonexistent`, `TestServer_SpaceDelete_HappyPath`,
  `TestServer_SpaceDelete_Nonexistent` (correct counts, a following `workspace.list`/`task.list` no
  longer showing removed rows, nonexistent ids surfacing as clear RPC errors) and `go test -race
  ./internal/wsapi/...`.
- Web: "Delete workspace" (workspace row menu) and "Delete space" (each space row's menu) in
  `app-sidebar.tsx`, wired to `DeleteWorkspaceDialog`/`DeleteSpaceDialog` in `crud-dialogs.tsx`.
  Confirm copy states the on-smind-only/checkpoint/cannot-be-undone facts from the AC and shows the
  actual task/space counts computed client-side from the already-loaded tree (no extra round trip).
  Verified by `app-sidebar-crud.test.tsx`'s four new tests (confirm-with-counts, RPC call with the
  right id, tree refresh, and cancel calling no RPC, for both workspace and space) plus `bunx tsc -b`
  and `bun run test`.
- Full verify sequence run clean: `task build`, `task test`, `task lint`, `bunx tsc -b` (in
  `web/packages/ui`), `bun run test` (in `web/packages/ui`), and `go test -race
  ./internal/store/... ./internal/workspace/... ./internal/wsapi/...`. (One pre-existing, unrelated
  flaky timer-based test in `web/packages/ui/src/lib/reconnect.test.ts` occasionally fails under the
  full `bun run test` run and passes in isolation and under `task test`'s invocation -- untouched by
  this change.)
- CLI parity (`smind workspace rm`/`smind space rm`) remains explicitly out of scope, per the AC.

This plan is complete; moving to `docs/plans/completed/`.
