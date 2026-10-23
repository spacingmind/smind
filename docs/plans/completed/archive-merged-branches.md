# Archive risk dialog, merged-branch cleanup and Restore (ADR-0039 §9, research item G8)

Size: **L (6)** — scope 2 (daemon + web), PRs 1 (two PRs: daemon, web; built in parallel against the wire below, verified together), risk 2
(branch deletion and worktree re-creation are data-loss paths), decisions 0 (H3 and the Supervisor defaults below settle them), verification 1
(UI screenshots; throwaway repos and a scripted `gh`, never a real forge). One Lead; every PR non-stacked off `develop`.

Spec: [ADR-0039 §9](../../decisions/0039-git-pr-state.md) (policy and the data it needs, all in place since PRs 2–4: `branch_owned`, `base_ref`/`base_sha`,
`TaskGit.merged`, `origin.ahead`, `commits.aheadOfBase`, `worktree.*`, the PR snapshot). Status: **completed 2026-10-23** (daemon #544, web #545; every acceptance criterion is confirmed in Validation).

## Decisions (fixed)

- **H3 (Human, 2026-10-22):** delete `smind/task-*` branches **only when merged**; unmerged branches are kept with a risk warning in the archive dialog; archived tasks
  get **Restore**.
- **Supervisor defaults, 2026-10-23 (the Human may veto):** (a) **no "Merge locally into base"** in v1 (PRs merge on GitHub); (b) **auto-archive after a PR merge stays
  off**; a later opt-in.
- **Approved with this plan:** never delete a remote branch; an owned-but-unmerged branch can be deleted only with an explicit `force`; the squash/rebase-merge case is
  recognised (PR merged and the branch tip equals the PR head sha); Restore is disabled with a sentence (`branch_gone`) when the branch no longer exists.
- Archiving through `task.archiveFinished`/`task.archive` from the tree actions, the CLI and MCP (`task_archive`) **never deletes a branch** (agents and bulk actions do not
  destroy branches; the policy only fires from the single-task dialog with an explicit choice).

## Behaviour

**When a branch may be deleted** (all must hold, decided by the daemon at archive time, never by the client): `branch_owned = 1`; the task is **merged** (`merged.state ==
"merged"`: ancestor of `origin/<base>` after one bounded fetch, or the task's PR is merged and the branch tip equals `pr.headSha`); the worktree is **clean** (a checkpoint
commit would make the branch unmerged); the branch is not checked out in any other worktree. Otherwise the branch is kept. With `force` an **owned** branch may be deleted
while unmerged or dirty (the dialog's explicit "delete it anyway"); a branch that smind did not create is never deleted, even with `force`.

**Order matters** (the existing archive checkpoints dirty work as a commit on the branch): decide eligibility **before** the checkpoint (fresh local status + one bounded
fetch), then checkpoint, teardown, `git worktree remove`, and only then `git branch -D` (a checked-out branch cannot be deleted). `-D` is used after smind's own merged
check, because git's `-d` refuses a squash-merged branch. A failure to delete leaves the task archived with the branch kept and a warning, never a half state.

**Restore** recreates the worktree from the kept branch: `git worktree add` at the original path when it is free (else a fresh stamped path), from the local branch, else from
`origin/<branch>` as a local tracking branch; then claim a port, clear `archived_at` and re-run the workspace setup script exactly as for a new task (an untrusted fork task
stays setup-blocked). If neither exists: `branch_gone` ("The branch `x` no longer exists, so there is nothing to restore. It was deleted after it was merged.").

## Wire (frozen; the daemon and web PRs build to it)

- `task.archivePreview {taskId}` → `{ branch:{name, owned}, merged:{state, via}, base:{ref}, risks:{ uncommitted:{files, added, deleted}, commitsAheadOfOrigin, commitsAheadOfBase,
  published }, ignoredFilesLost: true, hasTeardown, canDeleteBranch, deleteBranchDefault, deleteBlockedReason? }` — local git and cache only, no network, no fetch. `canDeleteBranch`
  is whether the **non-forced** policy would delete now; `deleteBranchDefault` is `canDeleteBranch` (the checkbox default); `deleteBlockedReason` is a sentence ("This is your own
  branch; smind never deletes it.", "The branch is not merged anywhere yet.", "The worktree has uncommitted changes; they are saved to the branch, so it is kept.").
- `task.archive {taskId, deleteBranch?: bool, force?: bool}` → the archived `Task` (unchanged shape) with the new `BranchDeletedAt` set when the branch was deleted. `deleteBranch`
  without eligibility and without `force` is a typed error and **nothing is archived**: `branch_unmerged`, `branch_dirty`, `branch_not_owned`, `branch_checked_out` (fixed sentences,
  code in `rpcError.code`); `force` on a not-owned branch is `branch_not_owned`. Refuses run-scoped callers when `deleteBranch` is set.
- `task.restore {taskId}` → the restored `Task`; errors `not_archived`, `branch_gone`, `base_unknown` is not applicable; refuses run-scoped callers.
- `task.list {workspaceId, archived: true}` → the workspace's archived non-system tasks (newest archive first), each with `BranchDeletedAt`.
- Schema: `tasks.branch_deleted_at TIMESTAMP` (additive migration). Events: archive keeps `task.archived`; Restore publishes the restored task so every client's tree shows it again
  without a reload (use the lifecycle topic the web tree already inserts from; if that is `task.created`, add a test that no "new task" side effect fires).

## Acceptance criteria

### Daemon

- AC1. `tasks.branch_deleted_at` exists (schema + idempotent migration) and `Task.BranchDeletedAt` is returned by `task.get`/`task.list`.
- AC2. **Preview.** `task.archivePreview` returns the fields above for a clean merged owned task (`canDeleteBranch` true, default on), an owned unmerged task (false, reason), a
  dirty merged task (false, `uncommitted` counts, reason), a task on the user's own branch (false, not owned), and a task whose PR is merged by squash (merged via `pr`); it
  spawns no fetch and no forge call.
- AC3. **Policy.** `task.archive {deleteBranch:true}` deletes exactly when owned ∧ merged ∧ clean ∧ not checked out elsewhere, otherwise returns the matching typed error and leaves
  the task **unarchived, worktree intact**; `force` deletes an owned unmerged/dirty branch; `force` on a not-owned branch is refused; a remote branch is never touched.
- AC4. **Order.** Eligibility is decided before the checkpoint: a dirty merged task keeps its branch (checkpoint commit preserved) and is archived with a warning-free result when
  `deleteBranch` was not requested; with `deleteBranch` it is `branch_dirty`.
- AC5. **Squash/rebase merge.** A task whose PR is merged with the branch tip equal to the PR head sha is deleted via `-D` although `git branch -d` would refuse.
- AC6. **Never in bulk or by agents.** `task.archiveFinished`, the CLI and MCP `task_archive` never delete a branch; `task.archive` with `deleteBranch` refuses run-scoped callers.
- AC7. **Restore.** From a kept branch: worktree recreated, port claimed, `archived_at` cleared, setup re-run (blocked for an untrusted fork task); from a branch present only as
  `origin/<name>`: tracking branch created; no branch anywhere: `branch_gone`; a task that is not archived: `not_archived`; the restored task is visible to a second client through
  events.
- AC8. `task.list {archived:true}` returns archived tasks only, scoped to the workspace, excluding system tasks.
- AC9. Failure atomicity: a failing `git branch -D` leaves the task archived with the branch kept and a logged warning; a failing worktree add during Restore leaves the task archived
  and no stray directory.

### Web

- AC10. The archive dialog for one task shows the risk rows from the preview ("Uncommitted changes (12 added, 3 deleted lines) — saved to the branch first", "N unpushed commits",
  "Branch `x` is merged into `develop`" / "not merged anywhere", the ignored-files warning) and a **Delete branch** checkbox: default on only when `deleteBranchDefault`; disabled with
  `deleteBlockedReason` when the policy forbids it; for an owned unmerged branch an unchecked destructive-text option "Delete the branch anyway — N commits are not merged anywhere" that
  sends `force`. Nothing is deleted without the user seeing it.
- AC11. Typed archive errors render as one sentence in the dialog and the dialog stays open.
- AC12. An **Archived** view per workspace lists archived tasks (title, when, whether the branch survives) with **Restore**, disabled with the `branch_gone` sentence when
  `BranchDeletedAt` is set; Restore puts the task back in the tree live.
- AC13. When the task's PR is merged, the diff header's primary action becomes **Archive…** (opens the dialog), replacing Create/Update PR.
- AC14. **Visuals.** Light and dark screenshots of the dialog in each state (merged-clean, unmerged-owned, dirty, user branch, error), the Archived view and a Restore, with
  transition frames (dialog open/close, task leaving and re-entering the tree); no mid-animation glitch.

### Cross-cutting

- AC15. `task test`, `task lint` and `cd web && bunx tsc -b packages/ui` pass on every PR and are recorded in its body; live checks use isolated daemons and throwaway repos with a scripted
  `gh`; nothing touches a real forge; ADR-0024 gets a one-line amendment (archive may delete merged owned branches; Restore) when this lands.

## Test scenarios (names are the contract)

**store / workspace** (`internal/store`, `internal/workspace`): `TestMigrate_BranchDeletedAt_Idempotent`, `TestArchivePreview_CleanMergedOwned`, `TestArchivePreview_OwnedUnmerged`,
`TestArchivePreview_DirtyMerged`, `TestArchivePreview_UserBranch`, `TestArchivePreview_SquashMergedByPR`, `TestArchivePreview_NoNetwork`, `TestArchive_DeletesMergedOwnedBranch`,
`TestArchive_KeepsUnmergedBranch`, `TestArchive_ForceDeletesOwnedUnmerged`, `TestArchive_ForceRefusedForUserBranch`, `TestArchive_DirtyMergedKeepsBranchAndCheckpoint`,
`TestArchive_DeleteRefusedLeavesTaskActive`, `TestArchive_BranchCheckedOutElsewhere`, `TestArchive_SquashMergeUsesForceDelete`, `TestArchive_NeverDeletesRemoteBranch`,
`TestArchive_BranchDeleteFailureKeepsArchive`, `TestRestore_FromKeptBranch`, `TestRestore_FromOriginOnly`, `TestRestore_BranchGone`, `TestRestore_NotArchived`,
`TestRestore_UntrustedStaysBlocked`, `TestRestore_RunsSetupAndClaimsPort`, `TestRestore_WorktreeAddFailureLeavesNoDirectory`, `TestListTasks_ArchivedOnly`.

**wsapi / agents** (`internal/wsapi`, `cmd/smind`): `TestTaskArchivePreview_RPC`, `TestTaskArchive_TypedErrors`, `TestTaskArchive_DeleteBranchRefusesRunScoped`,
`TestTaskArchiveFinished_NeverDeletesBranches`, `TestMcpTaskArchive_NeverDeletesBranches`, `TestTaskRestore_RPC`, `TestTaskRestore_EventShowsTaskAgain`, `TestTaskList_Archived`.

**web** (`web/packages/ui`): `archive-dialog.test.tsx` (`it shows the risk rows from the preview`, `it defaults Delete branch on only when merged and owned`, `it explains why deletion is
disabled`, `it offers delete-anyway for an owned unmerged branch and sends force`, `it shows a typed error and stays open`), `archived-tasks.test.tsx` (`it lists archived tasks with Restore`,
`it disables Restore with the branch_gone sentence`, `it restores a task into the tree live`), `pr-action.test.tsx` (`it offers Archive when the PR is merged`).

**live** (isolated daemon, throwaway repos with a local bare origin, a scripted `gh` that reports a merged PR; never a real forge): a merged task archived with Delete branch (branch gone
locally, `origin` untouched), the same for a squash-merged one, an unmerged one kept (and deleted with "anyway"), a user-branch task kept, a dirty one kept with its checkpoint, Restore of a
kept branch (worktree back, setup ran) and `branch_gone` after deletion.

## PR split (non-stacked, built in parallel against the frozen wire)

| PR | Content | Peers |
|---|---|---|
| A | **Daemon**: migration, `task.archivePreview`, `task.archive` policy + typed errors, `task.restore`, `task.list {archived}`, restore event, MCP/CLI/tree never delete | peer-sonnet |
| B | **Web**: archive risk dialog, Archived view + Restore, merged-PR **Archive…** action, query keys/hooks, tests; screenshots | peer-sonnet (GLM when its quota allows, with step-by-step briefs) |

Order: A ‖ B, then the Lead verifies live and by screenshots once A is on a branch; B merges after A.

## Out of scope

Merge locally into base; auto-archive after merge (opt-in, later); deleting remote branches; undo of a branch deletion beyond what git's reflog gives; archiving/restoring many tasks at once;
mobile.

## Implementation notes

Decided while implementing (2026-10-23):

- The eligibility decision and the mechanics both live in `workspace.Manager` (preview, the bounded base fetch, `ArchiveTaskWith`, `RestoreTask`), fed by `gitstate.Service.Fresh` through `SetGitFacts`, so the policy has one home and the workspace tests exercise it for real; `workspace` imports `gitstate` (no cycle).
- `task.archive` and `task.restore` accept `{id}` or `{taskId}` (the CLI, MCP and older callers send `id`; the web sends both).
- `branch_checked_out` is also a Restore error (the branch is checked out in another worktree). Restore publishes **`task.updated`**, not `task.created` (a restored task is not new; the web tree upserts on both and a test asserts no `task.created` fires); the status becomes `created` because the prior status is not stored.
- `risks.commitsAheadOfOrigin` means commits no `origin/*` ref contains (what deleting the branch would lose), not the ahead count of `origin/<branch>`.
- `force` without `deleteBranch` is ignored. Even with `force` a dirty worktree is checkpointed before `git branch -D`, and without `force` the branch tip is re-checked after the checkpoint (moved ⇒ keep and log). Concurrent Restore calls are serialised per task.
- The `branch_gone` sentence says only that the branch no longer exists (a forced delete is unmerged, so "deleted after it was merged" would be false).


## Progress

- 2026-10-23: plan written (ADR-0039 §9, H3, Supervisor defaults a/b). Daemon (#544) and web (#545) halves implemented, verified live together and merged; #545 was rebased once after the G12 web PR (#548) landed (`PrOperation` union and `PrAction` imports/reason logic merged; `tsc` found no duplicates).

## Validation

How each AC was satisfied (daemon #544; web #545). The two branches were merged in a scratch worktree for the live run.

- **AC1–AC9 (daemon):** every test name in the plan's `store / workspace` and `wsapi / agents` groups exists and passes (plus `TestRestore_FreshPathWhenOriginalIsTaken`, `TestRestore_ConcurrentCallsKeepTheWorktree`, `TestTaskArchive_DeletesAndForces`); `TestTaskArchiveFinished_NeverDeletesBranches` and `TestMcpTaskArchive_NeverDeletesBranches` first assert the branch *would* be deletable, so they prove something.
- **Live (isolated daemon on port 4881, throwaway repo with a local bare origin, a scripted `gh` reporting a merged PR; nothing touched a real forge):** previews for a merged-clean task (`canDeleteBranch`, default on), an owned-unmerged one (off, "not merged anywhere"), a dirty-merged one (off, "saved to the branch, so it is kept"), a user-branch task ("smind never deletes it") and a squash-merged one (`merged via pr`, deletable). Archiving through the real UI: the merged-clean and squash-merged branches were deleted, the dirty one and the user's own branch were kept, the unmerged owned one was deleted only through "Delete the branch anyway" (`force`); `origin` still had only `develop`. The Archived view listed all five with "branch kept"/"branch deleted"; Restore of the kept dirty task re-created its worktree and put it back in the tree live (and out of the Archived list), Restore of deleted ones was disabled with the `branch_gone` sentence.
- **Web ACs 10–14 and screenshots:** light and dark screenshots of the dialog (merged-clean, dirty, unmerged-owned, user branch, squash), the Archived view, the restore frames at 0/80/250/1500 ms, and the diff header's **Archive…** replacing Create/Update PR for a merged PR. Found and fixed in review: the Archived row's meta line was truncated to "branch del…" beside the Restore button (now its own line) and the `branch_gone` sentence claimed the branch had been merged.
- **AC15:** `task test`, `task lint` and `cd web && bunx tsc -b packages/ui` are recorded in each PR body. ADR-0024's one-line amendment is below.

### Acceptance-criteria coverage (2026-10-23 close-out)

| ACs | Where | Evidence |
|---|---|---|
| AC1–AC9 (migration, preview, policy, order, squash, never in bulk/by agents, Restore, archived list, failure atomicity) | #544 | the named store/workspace/wsapi/MCP tests; the live run (merged-clean and squash-merged deleted, dirty and user branch kept, unmerged only via `force`, `origin` untouched, Restore of a kept branch) |
| AC10–AC14 (risk dialog, typed errors, Archived view + Restore, Archive… on a merged PR, visuals) | #545 | `archive-dialog`, `archived-tasks`, `pr-action` tests; light/dark screenshots incl. restore frames |
| AC15 (checks, no real forge, ADR-0024 amendment) | both | each PR body records `task test`/`task lint`/tsc; scripted `gh` only; ADR-0024 amended in #544 |

Known residuals (not blockers): a real forge/PR merge was never exercised (scripted `gh`); `merged: unknown` is reported as "not merged anywhere yet" (fail-closed, imprecise); a delete-archive can hold the task's archive guard for up to the 20 s base fetch; the new tests carry `!windows` build tags. Out of scope and still open: "Merge locally into base" and auto-archive after a PR merge (Supervisor defaults a/b; the Human may revisit).
