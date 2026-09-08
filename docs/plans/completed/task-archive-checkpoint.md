# Archive task: checkpoint work before removing the worktree

Fixes the data-loss bug found during the dual-mode UI research
(`docs/research/dual-mode-ui.md`): archiving a task runs
`git worktree remove --force` (`internal/workspace/task.go`), silently
destroying unreviewed agent work with no recovery path.

## Acceptance Criteria

- Archiving a task first checkpoints its worktree onto the task's
  branch: `git add -A` + `git commit` of everything (including
  untracked files) — so the task's branch retains all work even after
  the worktree directory is removed. Mirror what VS Code does before
  removing a worktree.
- No empty checkpoint commit when the worktree is clean (reuse the
  existing diff-empty check from `internal/workspace/git.go`'s taskDiff
  machinery — if the worktree-vs-base diff is empty AND there is
  nothing uncommitted, skip the commit). Decide and record the exact
  cleanliness condition in Decisions.
- Checkpoint commit message is clearly machine-generated, e.g.
  `smind: checkpoint before archive` (decide exact form in Decisions).
- Archive of an already-clean task behaves exactly as today.
- No CLI/wsapi surface changes — this is internal to
  `internal/workspace`'s archive path.

## Test Scenarios

- Go test (real git worktree, following `internal/workspace/diff_test.go`
  / `wsapi`'s newTestTask patterns): create a task, write an uncommitted
  change + an untracked file, archive the task, then verify the task
  branch's tip commit contains both changes (branch must outlive the
  worktree — find it in the workspace repo after archive).
- Clean task: archive produces no new commit on the task branch.
- `go build ./...` / `gofmt -l .` / `go vet ./...` / `go test -race ./...`
  clean; `task test`, `task lint` pass.

## Decisions

- **Cleanliness condition — `git status --porcelain` output is empty.**
  The spec's literal condition ("worktree-vs-base diff empty AND nothing
  uncommitted") was refined: the AND-conjunction is wrong for the
  checkpoint's purpose. A checkpoint's job is only "commit whatever is
  sitting uncommitted in the worktree right now" — it does not care
  whether the task has *committed* changes relative to base, only
  whether a commit is *possible*. Under the literal AND-condition, a
  worktree with real committed work but a clean status (a fully
  committed task — a normal, desirable end state after a tidy agent
  run) would need a second check anyway to avoid an empty commit, and
  the conjunction contributes nothing: "diff empty AND nothing
  uncommitted" collapses to "nothing uncommitted" whenever any commit
  exists, and a clean-but-committed worktree must skip the commit in
  either formulation. `git status --porcelain` output being empty is
  exactly "nothing uncommitted" (staged, unstaged, or untracked) and is
  precisely the condition under which the following `git commit` would
  fail with "nothing to commit" — so it is both necessary and
  sufficient. It reuses the same runGit machinery as `taskDiff` in
  `internal/workspace/git.go` (per the spec's intent to reuse the
  existing git plumbing), just scoped to "anything to commit?" rather
  than "what changed since base?".
- **Checkpoint commit message — the constant
  `checkpointCommitMessage = "smind: checkpoint before archive"`**
  (git.go), one exact string, asserted verbatim by the dirty-worktree
  test.
- **Commit identity/env — none set explicitly.** `internal/workspace`
  already runs git via `runGit`, which inherits the invoking
  environment with no GIT_AUTHOR/GIT_COMMITTER overrides, and the
  checkpoint commit uses the same path. The worktree belongs to the
  user's own repo, whose `user.name`/`user.email` config applies;
  adding a smind-specific identity would diverge from how every other
  git call in the package behaves.
- **Checkpoint failure aborts the archive before removal.**
  `ArchiveTask` returns the checkpoint error and does not run
  `git worktree remove`: removing the worktree with the checkpoint
  unwritten is exactly the data-loss bug being fixed, so an error
  (leaving the task unarchived and recoverable) is preferable to a
  half-archive.
- **`gitWorktreeRemove` keeps `--force`.** After a successful
  checkpoint there is nothing uncommitted left, so `--force` no longer
  risks discarding reviewable work; it only guards against
  refuse-to-remove edge cases (ignored-but-present leftovers,
  submodules). Its doc comment now states the checkpoint precondition.

## Progress

- [x] Checkpoint-before-remove in archive path
- [x] Tests (dirty + clean cases)
- [x] Verification (race/tests/lint)

## Validation

- **Archive checkpoints onto the task's branch** —
  `TestManager_ArchiveTask/"checkpoints uncommitted and untracked work
  onto the task branch"`: a real worktree with an uncommitted tracked
  modification plus an untracked file is archived; the task branch,
  resolved in the workspace repo after the worktree directory is gone,
  has moved, its tip subject is exactly `smind: checkpoint before
  archive`, and its tree contains both the modified `README.md`
  content and `notes.txt`.
- **No empty checkpoint on clean** —
  `TestManager_ArchiveTask/"clean worktree archives without a
  checkpoint commit"`: branch tip hash is identical before and after
  archive.
- **No empty checkpoint when work is already committed** —
  `TestManager_ArchiveTask/"worktree with only committed changes
  archives without a checkpoint commit"`: a worktree with a real
  commit and clean status archives without moving the branch tip,
  confirming the cleanliness condition is status-based, not
  diff-vs-base-based (per Decisions).
- **Archive of an already-clean task behaves exactly as today** —
  existing subtests ("removes worktree", "task with no worktree_path
  archives cleanly", "safe when worktree already externally deleted")
  still pass unchanged.
- **No CLI/wsapi surface changes** — change is confined to
  `internal/workspace` (`task.go` archive path, `git.go` helpers);
  grep for `ArchiveTask` callers shows `internal/wsapi/handlers.go`
  merely proxies `wm.ArchiveTask(p.ID)` and is untouched.
- **Build/vet/race/lint** — `go build ./...`, `gofmt -l .`,
  `go vet ./...`, `go test -race ./...` all clean; `task test` and
  `task lint` pass.
