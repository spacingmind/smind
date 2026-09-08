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

(To be filled by the implementer: exact cleanliness check, commit
 message, commit author/committer identity — note `internal/workspace`
 already runs git with some env; keep it consistent.)

## Progress

- [ ] Checkpoint-before-remove in archive path
- [ ] Tests (dirty + clean cases)
- [ ] Verification (race/tests/lint)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
