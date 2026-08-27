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

- [x] `internal/files` (or equivalent): list/read/write, real sandboxing
- [x] `internal/wsapi`: `file.list`/`file.read`/`file.write` + tests
- [x] `FileExplorerPane` component (tree + CodeMirror 6 editor) + tests
- [x] Verification (typecheck/tests/build + real-daemon E2E script)

## Validation

(Filled in as each Acceptance Criterion is confirmed — command run, test
name, or manual check.)

- New daemon methods backed by a new package: `internal/files/files.go`
  implements `List`/`Read`/`Write` against a `root` (the task's
  `WorktreePath`) plus a client-supplied `path`; `internal/wsapi/files.go`
  wires `file.list`/`file.read`/`file.write` into `methodHandlers`
  (`internal/wsapi/handlers.go`), looking up each task's `WorktreePath` via
  `workspace.Manager.GetTask` the same way every other task-scoped method
  already does, and erroring clearly if a task has no worktree (nil
  `WorktreePath`, or has been archived and the worktree directory is
  gone — proven by `TestServer_File_ArchivedTask_Errors`).
- `file.read` rejects non-UTF-8 content instead of mangling it: proven by
  `internal/files/files_test.go`'s `TestRead_NonUTF8_Rejected`.
- `file.write` creates a new file and overwrites an existing one: proven by
  `TestWrite_CreatesNewFile` / `TestWrite_OverwritesExistingFile`, and
  end-to-end over the wire by `TestServer_FileList_Read_Write_RoundTrip`
  (list root -> read README.md -> write a change -> read again and confirm
  it landed -> write a brand-new file -> list again and see both).
- **Path sandboxing** (the highest-risk part): every path is resolved via
  `internal/files.resolveInRoot`, which (1) joins/cleans the client path
  against the worktree root (rejecting `..` traversal outright via
  `filepath.Join`+`Clean`'s normal semantics), (2) resolves symlinks along
  the *entire* resulting path via `filepath.EvalSymlinks` — tolerating a
  not-yet-existing leaf (`file.write`'s create case) by walking up to the
  nearest existing ancestor, resolving *that*, and rejoining the missing
  leaf verbatim (`resolveExistingPrefix`) — and only *then* (3) checks
  containment of the fully-resolved, symlink-free path against the
  fully-resolved root via a separator-aware prefix check
  (`withinRoot`, `candidate == root || strings.HasPrefix(candidate,
  root+separator)`) that treats a sibling directory sharing root's string
  prefix (e.g. `/foo/bar-evil` vs `/foo/bar`) as outside. Symlinks are
  resolved *before* containment is checked, not after, so a symlink
  planted inside the worktree can't smuggle access to anywhere else on
  disk. Adversarial Go tests, all passing, at both layers:
  - `internal/files/files_test.go`: `TestSandbox_DotDotTraversal_Rejected`,
    `TestSandbox_DeepDotDotTraversal_Rejected`,
    `TestSandbox_AbsolutePathOutsideRoot_Rejected` (+
    `..._AbsolutePathInsideRoot_Allowed` as the positive control),
    `TestSandbox_SymlinkToOutsideFile_Rejected`,
    `TestSandbox_SymlinkToOutsideDirectory_Rejected` (covers both an
    existing target via `List` and the missing-leaf `Write` case through a
    symlinked ancestor directory),
    `TestSandbox_SiblingDirectorySharingPrefix_NotTreatedAsContained`,
    `TestSandbox_RootItself_Allowed`.
  - `internal/wsapi/files_test.go`:
    `TestServer_File_PathTraversal_Rejected` proves the wire-level
    handlers reject `..` traversal, an absolute path outside the root, and
    that a rejected `file.write` never actually creates anything outside
    the sandbox (`os.Stat` on the target path confirms it wasn't created).
  - Re-confirmed against the real running daemon (see Manual/E2E below),
    including a real symlink planted inside a real task worktree pointing
    at a real file outside it.
- `FileExplorerPane` (`web/packages/ui/src/components/file-explorer-pane.tsx`
  + `code-mirror-editor.tsx` + `web/packages/ui/src/hooks/use-file-explorer.ts`):
  a lazily-expanding directory tree (`file.list` per directory, only on
  first expand) and a CodeMirror 6 editor (`@codemirror/*`/`codemirror`
  added as dependencies) for the selected file, with a visible Save button
  (disabled until the buffer is dirty) plus a Ctrl/Cmd-S keybinding, both
  calling `file.write`. Takes `{ client, task }` exactly like
  `TaskDetailPane`. **Not** wired into `App.tsx`/`task-detail.tsx`, per the
  Acceptance Criteria (parallel diff-viewer/terminal work against the same
  shell files) — proven working standalone by
  `file-explorer-pane.test.tsx` rendering it directly against a
  `FakeWsClient`, no app shell involved.
- Frontend test scenarios, all passing
  (`web/packages/ui/src/components/file-explorer-pane.test.tsx`, 7 tests):
  tree renders `file.list`'s result; lazy expansion only fetches
  `file.list` once per directory (collapse/re-expand doesn't re-fetch);
  selecting a file fetches and displays its content via `file.read`;
  editing (dispatched through the real mounted `EditorView`'s own
  `dispatch` API via a test-only `editorViewRegistry` — see that file's
  doc comment for why: jsdom's contentEditable/input-event simulation
  isn't reliable enough to trust for driving CodeMirror's own DOM-mutation
  pipeline, so the test drives the same public API a real keystroke
  ultimately reaches, exercising this component's own onChange wiring
  rather than CodeMirror's internal event capture) and saving calls
  `file.write` with the edited content; a `file.write` failure surfaces
  `save failed: <message>` without losing the edit (buffer and Save-enabled
  state both preserved); switching the selected file loads the new file's
  content, replacing the old; Ctrl-S (a real `keydown` dispatched at the
  `EditorView`'s `contentDOM`, confirmed via a smoke test to actually reach
  CodeMirror's keymap dispatch in jsdom) triggers `file.write` the same as
  clicking Save.

Commands run: `go build ./...` (clean), `gofmt -l .` (no output),
`go vet ./...` (clean), `go test -race ./...` (all packages pass,
including the new `internal/files` and `internal/wsapi` file-handling
tests). `bunx tsc -b` (clean). `bun run test` / `task test:web` (23 tests
across all 4 web test files, all passing, up from 16 before this task).
`task build` succeeded; `internal/server/dist/.gitkeep` was deleted by the
Vite build as expected (same as every prior web UI task) and restored
(`touch` + `git add`) before committing.

Manual/E2E against the real built binary: built `bin/smind` and
`internal/taskrunner/fakeagent`, ran the real daemon against an isolated
`SMIND_HOME` and a real git-initialized workspace repo, created a real
workspace + task via the real CLI (`smind workspace create`,
`smind task new`). A from-scratch Node script (built-in `fetch`/
`WebSocket` only, Node 26, no browser) opened a real WebSocket connection
and drove the exact `file.list`/`file.read`/`file.write` wire sequence
`FileExplorerPane` makes: listed the real worktree root (saw `.git` and
`README.md`), read `README.md`'s real content, wrote a change, read again
and confirmed the new content landed, wrote a brand-new file and confirmed
it appeared in a fresh `file.list`, then ran three adversarial checks
against the live daemon (not just unit tests): a deep `../` traversal
toward `/etc/passwd`, an absolute `/etc/passwd` path, and a real symlink
planted inside the real task worktree directory (found via
`$SMIND_HOME/worktrees/1/<slug>`) pointing at a real file in a separate
temp directory outside the worktree — all three were rejected by the
running daemon with the expected "path ... escapes the worktree root"
error, and the symlink case specifically proves the sandbox check happens
against a real filesystem symlink, not just the unit tests' simulated one.
Every assertion in that script passed; the daemon, temp `SMIND_HOME`, temp
repo, and temp outside-symlink-target directory were all torn down
afterward (nothing left running or on disk).

What was **not** verified, honestly, consistent with every prior web UI
task in this repo: no real browser is available in this sandbox, so actual
pixel rendering, CSS layout/scrolling of the tree and editor panes, real
mouse click/drag behavior, and CodeMirror's own real-browser keyboard/IME
input handling were not visually checked. jsdom + `@testing-library/react`
verify real React render/effect/cleanup/state semantics and (per the
smoke tests run during development) that a real `EditorView` mounts and
its `Mod-s` keymap dispatch actually fires in jsdom — but not what the
editor looks like, how it scrolls with a long file, or whether real typing
via a physical keyboard in a real browser produces the same `onChange`
calls (CodeMirror's own DOM-mutation-observing input pipeline is
CodeMirror's to test, not this codebase's — see
`code-mirror-editor.tsx`'s `editorViewRegistry` doc comment).
