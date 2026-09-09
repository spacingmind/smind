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

- **Status mapping (task.files).** `git diff --name-status` against the
  same snapshot index taskDiff builds; codes mapped A→"added",
  M→"modified", D→"deleted". Any other code (e.g. R) is carried through
  lowercased rather than collapsed — callers never see a lie.
- **Staged-state exposure.** `task.files` includes `staged` per file,
  cross-referenced with `git status --porcelain` on the *real* index:
  an entry counts as staged when its index (X) column is neither ' '
  (unmodified) nor '?' (untracked). "?? " lines therefore report
  `staged: false`, which is correct.
- **taskFileDiff is taskDiff + pathspec.** Same base resolution + same
  snapshot-index env + `-- <path>` appended — a per-file view is always
  a consistent slice of the whole-task diff, no second diff mode.
- **Error envelope (empty stage).** `workspace.ErrNothingStaged`
  sentinel ("nothing staged to commit"); the wsapi layer wraps it in
  the standard error envelope (method-prefixed message), no git stderr.
  The check is `git diff --cached --name-only` being empty before
  invoking commit — a staged-but-identical-to-HEAD file also lands
  here, matching git's own "nothing to commit" judgment.
- **Summary shape.** `{commit: full SHA, subject: first message line
  (pre-trailers), files: staged file count}`.
- **Agent trailers.** Appended as a blank-line-separated footer:
  `<message>\n\nSmind-Agent: <provider>\nSmind-Task: <taskID>` (taskID
  is the numeric DB id; no "task-" prefix). Agent author with empty
  `agent` is refused before any git runs.
- **Commit identity/env.** Plain inherited-env `git commit -m` in the
  worktree dir, same runGit helpers as gitWorktreeCheckpoint — no
  committer overrides; human vs. agent is distinguished solely by the
  trailers (ADR 0006 rationale: the marker travels with the commit).
- **Taskrunner helper scope.** `(*Runner).CommitTask(taskID, provider,
  message)` calls `wm.CommitTask(..., "agent", provider)`. Not wired as
  an agent-visible tool — no agent asks for it today.
- **UI author surface.** The commit bar always sends
  `author: "human"`; no agent-author control exists in the UI this pass.

## Progress

- [x] task.files / task.fileDiff / task.stage / task.commit backend
- [x] Wire tests
- [x] Taskrunner CommitTask helper (agent trailers)
- [x] Per-file diff viewer + stage checkboxes + commit bar
- [x] UI tests
- [x] Verification (both chains)

## Validation

- **task.files** — `internal/wsapi` TestServer_CommitFlow: two changed
  files listed with correct statuses (modified/added) and
  `staged:false` initially; workspace TestManager_TaskFiles adds the
  empty case and staged-state observability. ✔
- **task.fileDiff** — wire: notes.txt slice contains the added content,
  unchanged path returns empty; workspace TestManager_TaskFileDiff
  checks slice-consistency (README diff has no notes hunks). ✔
- **task.stage** — wire: stage→files shows staged; unstage round-trips;
  workspace TestManager_TaskStage asserts the real `git status
  --porcelain` flips M·/·M too. ✔
- **task.commit** — wire: nothing-staged error is the clean message
  ("nothing staged"), human commit of the staged-only set → branch tip
  (rev-parse HEAD == returned sha; diff-tree records only README.md);
  agent trailers byte-exact (TestServer_TaskCommit_AgentTrailers +
  workspace byte-exact test incl. refusal cases: empty agent name,
  invalid author). ✔
- **Taskrunner helper** — `CommitTask` calls the same primitive with
  agent trailers; compiles and is covered via the workspace-level
  trailer tests (no separate subprocess run needed — it's a two-line
  delegation). ✔
- **UI** — diff-viewer-pane tests (10 suites green): per-file grouping
  renders per-entry task.fileDiff via diff2html; collapse/expand; stage
  checkbox → task.stage + state update; commit disabled until staged +
  non-empty message; success shows subject+sha (abcdef12) and re-issues
  task.files; stage/commit failures render inline; terminal
  run.status-triggered refresh still works (and ignores other tasks). ✔
- **Verify chains** — `go build ./... && gofmt -l . && go vet ./... &&
  go test -race ./... && task test && task lint` green; `bunx tsc -b`,
  `bun run test` (103 tests), `task build` green (dist/.gitkeep
  restored after the emptyOutDir wipe). ✔
