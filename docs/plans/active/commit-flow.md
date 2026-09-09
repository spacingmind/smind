# Commit flow: per-file staging, task.commit, agent trailer

Implements ADR 0006 (read it first — decisions 1/2/3 are settled).
Backend + UI; the biggest missing piece of editor mode.

## Acceptance Criteria

### Backend (`internal/wsapi` + `internal/workspace`)

- New method `task.files {taskId}`: the task's changed files — one entry
  per path in the base→worktree diff (the same diff `task.diff`
  computes), with `{path, status}` where status is the git change kind
  (added/modified/deleted; derive from the diff — decide exact mapping
  in Decisions). This is also the input for the per-file diff view.
- New method `task.fileDiff {taskId, path}`: the unified diff for ONE
  file from the same base→worktree computation (slice `task.diff`'s
  machinery by path — no new git invocation shape). Empty result for a
  path with no changes.
- New methods `task.stage {taskId, path, staged: bool}`:
  stage (`git add <path>`) / unstage (`git restore --staged <path>`) a
  single file in the task worktree's real index (unlike task.diff's
  throwaway snapshot index — staging is a real mutation; that's the
  point).
- New method `task.commit {taskId, message, author: "human"|"agent",
  agent?}`:
  - Commits the currently staged files only (`git commit` with no `-a`,
    no `git add -A`).
  - Rejects cleanly when nothing is staged (clear error, not a git
    stderr leak — decide the error envelope in Decisions).
  - When `author: "agent"`: adds trailers `Smind-Agent: <provider>` and
    `Smind-Task: <taskId>` to the message (append with proper
    formatting; refuse agent commits whose caller didn't pass a
    provider).
  - Returns `{commit: <sha>, summary}` — decide summary shape (e.g.
    subject + files count) in Decisions.
- Staging state must survive and be observable: decide whether
  `task.files` includes staged-vs-unstaged per file (recommended:
  yes, via `git status --porcelain` cross-referenced with the diff
  paths) and record in Decisions.
- Wire tests for all four methods over real WS connections against
  real worktrees (stage → files shows staged → commit → branch tip
  contains only staged paths' changes; unstage round-trip; agent
  trailer assertions; empty-stage error).

### Agent path (taskrunner)

- Expose commit capability to agents minimally for now: a
  taskrunner-level helper (not an agent tool yet — no agent asks for it
  today) `CommitTask(taskID, provider, message)` that calls the same
  workspace commit primitive with the agent trailers. Wiring it as an
  actual agent-visible tool is deliberately out of scope until an agent
  needs it; record this in Decisions.

### UI (diff viewer → review-and-commit surface)

- DiffViewerPane upgrades to a per-file review list: files grouped
  (collapsible per file), each file renders its `task.fileDiff`, with a
  per-file stage/unstage checkbox (per ADR 0006: per-file, not per-hunk)
  and a "viewed" indicator (local state is enough this pass).
- A commit bar: message input (multi-line), Commit button enabled only
  when ≥1 file staged and message non-empty; on success show the
  resulting commit subject + sha, refresh the diff/files list.
- Files list and staged state refresh after commit; existing
  run-status-triggered diff refresh keeps working.
- Permission/error surfaces: commit/stage failures render inline, no
  silent failures.

## Test Scenarios

- Go: `internal/workspace` real-git tests for stage/unstage/commit
  primitive (mixed staged set commits only those files; unstage
  round-trips; agent trailer format byte-exact; empty-stage error).
- Go: `internal/wsapi` wire tests (the four methods, per AC).
- Web: diff-viewer-pane tests — per-file grouping renders
  `task.fileDiff` per entry; stage checkbox calls `task.stage` and
  updates; commit disabled until staged+message; commit success
  refreshes; agent-author UI is not part of this pass (human-only UI).
- `bunx tsc -b` clean, `bun run test`, `task build`; Go verify chain
  green.

## Decisions

(To be filled by the implementer: status mapping for task.files,
 staged-state exposure, error envelopes, summary shape, porcelain
 cross-referencing, commit identity/env consistency.)

## Progress

- [ ] task.files / task.fileDiff / task.stage / task.commit backend
- [ ] Wire tests
- [ ] Taskrunner CommitTask helper (agent trailers)
- [ ] Per-file diff viewer + stage checkboxes + commit bar
- [ ] UI tests
- [ ] Verification (both chains)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
