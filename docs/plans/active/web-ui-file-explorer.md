# Web UI: file explorer + CodeMirror editor

## Acceptance Criteria

- New daemon methods (`internal/wsapi`), backed by a new package (suggest
  `internal/files`, following the `internal/runs`/`internal/workspace`
  pattern of "package owns the real filesystem operations, wsapi just
  wires params/results"):
  - `file.list {taskId, path?}` → the entries (name, whether it's a
    directory, size for files) of `path` (default: the task's worktree
    root) *within that task's worktree*.
  - `file.read {taskId, path}` → the file's content as a UTF-8 string. A
    file that isn't valid UTF-8 (binary) returns a clear error rather than
    silently mangling bytes — there's no binary editor in this task's
    scope.
  - `file.write {taskId, path, content}` → writes `content` to `path`
    inside the worktree, creating the file if it doesn't exist.
- **Path sandboxing is a hard security requirement, not a nice-to-have**:
  every one of these methods resolves `path` against the task's real
  `WorktreePath` (from `store.Task`, via the same `workspace.Manager` every
  other task-scoped method already uses) and rejects (with a clear error,
  not a panic or a silently-wrong result) any path that would resolve
  outside that worktree root — `..` traversal, absolute paths escaping the
  root, symlinks that point outside it. Write a real adversarial test
  suite for this specifically: `../../etc/passwd`-style traversal, a
  symlink planted inside the worktree pointing outside it, an absolute
  path. This is the highest-risk part of this task; treat it that way.
- Frontend: a self-contained `FileExplorerPane` component (new file(s)
  under `web/packages/ui/src/components/` — exact name your choice, keep
  it discoverable) taking `{ client, task }` props (same shape
  `TaskDetailPane` already takes), rendering a collapsible directory tree
  (`file.list`, lazily expanding subdirectories) and, on selecting a file,
  a CodeMirror 6 editor pane showing its content (`file.read`) with a way
  to save changes (`file.write`) — a save button and/or Ctrl+S/Cmd+S,
  your choice, but make it discoverable (not just an invisible keybinding).
  Add CodeMirror 6 (`@codemirror/*` / `codemirror` packages) as new
  dependencies.
- **Do NOT wire `FileExplorerPane` into `App.tsx` or `task-detail.tsx`.**
  This is deliberate, not an oversight: two other features (a diff viewer,
  a terminal) are being built in parallel against the same shell files,
  and having three agents independently edit `App.tsx`'s layout would
  produce merge conflicts. Build and test `FileExplorerPane` as a fully
  working, independently mountable component (proven by your own component
  tests rendering it standalone, plus the real-daemon manual verification
  below) — the actual integration into the app shell (adding it as a pane
  or tab) happens in one follow-up step after all three parallel features
  have merged.

## Test Scenarios

- Go: real filesystem tests for `internal/files` (or wherever you put this
  logic) covering: listing a directory, reading a file, writing a file
  (both creating new and overwriting existing), and the adversarial
  sandboxing cases above — each must return an error, not a result that
  leaks data outside the worktree. Use a real temp git worktree (see
  `internal/wsapi/run_test.go`'s `newTestTask`/`newTestRepo` helpers for
  the established pattern of creating one) — not a mocked filesystem.
- `internal/wsapi` tests for `file.list`/`file.read`/`file.write` proving
  the wire shape round-trips correctly end to end (real WS connection,
  same pattern as `internal/wsapi/run_test.go`).
- Frontend component tests (jsdom + Testing Library + a `FakeWsClient`,
  same pattern `task-detail.test.tsx` already established) for
  `FileExplorerPane`: the tree renders `file.list`'s result, selecting a
  file fetches and displays its content via `file.read`, editing and
  saving calls `file.write` with the edited content, a `file.write`
  failure surfaces an error without losing the user's edits.
- Manual/E2E verification against the real built binary, same honestly-
  scoped approach as prior web UI tasks (no real browser available in this
  environment — confirm via a from-scratch Node script driving the exact
  wire sequence the component makes, plus whatever build/typecheck
  evidence is available; state plainly what wasn't actually visually
  verified).
- `go build ./...` / `gofmt -l` / `go vet ./...` / `go test -race ./...`
  clean. `bunx tsc -b` clean, `bun run test` (`task test:web`) passes.
  `task build` succeeds; check `internal/server/dist/.gitkeep` as usual.

## Decisions

- New backend package for file operations, not folded into
  `internal/workspace` — file read/write/list is a distinct concern from
  workspace/task lifecycle management, even though both operate on a
  task's worktree.
- CodeMirror 6, per the user's explicit direction for this UI stack.
- Explicitly NOT wiring into `App.tsx`/`task-detail.tsx` — see Acceptance
  Criteria. This is being built in parallel with a diff viewer and a
  terminal against the same shell; integration is a deliberate later step.
- Explicitly out of scope: binary file viewing/editing, file
  create/delete/rename, multi-file tabs (one file open at a time is
  enough for this pass).

## Progress

- [ ] `internal/files` (or equivalent): list/read/write, real sandboxing
- [ ] `internal/wsapi`: `file.list`/`file.read`/`file.write` + tests
- [ ] `FileExplorerPane` component (tree + CodeMirror 6 editor) + tests
- [ ] Verification (typecheck/tests/build + real-daemon E2E script)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
