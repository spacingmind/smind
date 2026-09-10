# Server-side folder picker for "New workspace"

Replaces manually typing an absolute path in the "New workspace" dialog's
Path field with a picker: a new daemon RPC lists directories on the
daemon's own host filesystem, and a dialog in the web UI walks that tree.

This is the only viable pattern here — the web UI runs in a browser, and
the File System Access API (`window.showDirectoryPicker()`) returns a
sandboxed handle, not a real OS path string `workspace.create` can use.
VS Code Web / code-server's "Open Folder" uses the same server-browses,
client-walks pattern for the same reason. Chosen over two lighter
alternatives (a recent-paths dropdown; just better inline path validation)
per an explicit user decision in this session.

## Acceptance Criteria

- New `internal/hostfs` package: `List(path string) (ListResult, error)`
  lists the directory at `path` (defaulting to the user's home directory
  when `path` is empty), unsandboxed — unlike `internal/files`, which is
  deliberately confined to one task's worktree, this browses the daemon's
  whole host filesystem, because the daemon already runs with the
  operator's own OS permissions and there is no workspace yet at the point
  this is used.
  - Returns only directories (regular files excluded), hidden
    (dot-prefixed) entries excluded, sorted by name case-insensitively.
  - Each entry reports whether it's itself a git repository (a `.git`
    entry present — same check `workspace.CreateWorkspace` already makes),
    so the picker can visually flag "this one's ready to use" without a
    second round trip.
  - Follows symlinks when deciding whether an entry is a directory (a
    plain `fs.DirEntry.Type()` check would report a symlinked directory as
    a symlink, not a directory, and hide it) — no loop protection needed
    beyond what a normal directory listing already gets from the OS.
  - Reports the resolved absolute path (`filepath.Abs` + `Clean`) and its
    parent (empty string at the filesystem root), so the UI can render a
    breadcrumb and an "Up" action without doing its own path math.
  - Clear, distinguishable errors for: path doesn't exist, path is not a
    directory, permission denied.
- New `internal/wsapi` method `fs.listDir` (params: `{path?: string}`),
  wired the same way every other handler is (`methodHandlers`), returning
  `{path, parent, entries: [{name, path, isGitRepo}]}`. No new auth model —
  covered by the same per-connection token gate the whole `/ws` endpoint
  already requires.
- Web: a folder-picker dialog (new component, e.g.
  `folder-picker-dialog.tsx`) reachable from a "Browse…" button next to
  `CreateWorkspaceDialog`'s Path input (`crud-dialogs.tsx`):
  - Opens at the daemon's home directory (empty `path` param) the first
    time it's opened in a session; shows the current path, a scrollable
    list of subdirectories (git repos visually flagged), an "Up" action,
    and a persistent "Use this folder" action that always targets whatever
    path is currently displayed (not a row selection state).
  - Clicking a row navigates into it (re-queries `fs.listDir` with that
    row's path) rather than needing a separate "open" affordance.
  - Loading and error states (e.g. permission denied navigating into a
    row) are visible in the dialog itself, and don't crash or strand the
    dialog — a failed navigation stays on the last good path.
  - "Use this folder" closes the picker and fills `CreateWorkspaceDialog`'s
    Path field with the selected absolute path; it does not itself call
    `workspace.create` — the existing Create flow (validation, error
    surfacing) is unchanged, this only fills the field.
  - The Path input itself stays editable directly too (Browse… is an aid,
    not the only way to set a path — e.g. paths not reachable by browsing,
    or a user who just wants to paste one).

## Test Scenarios

- Go (`internal/hostfs`):
  - Lists a directory's subdirectories only, excluding regular files.
  - Excludes dot-prefixed entries.
  - Flags a subdirectory containing `.git` as a git repo; a plain
    subdirectory is not flagged.
  - A symlinked directory is listed as a directory (not skipped).
  - Empty `path` defaults to the real home directory
    (`os.UserHomeDir()`).
  - Reports the correct parent, and an empty parent at `/`.
  - Nonexistent path, a path that's a regular file (not a directory), and
    a permission-denied directory (skip on environments where tests run
    as root and can't produce EACCES, e.g. via `os.Geteuid() == 0`) each
    return a distinguishable error.
- Go (`internal/wsapi`): wire-level test for `fs.listDir` (existing
  real-connection pattern) — happy path, and an invalid/nonexistent path
  surfacing as an RPC error rather than a panic or empty success.
- Web (`folder-picker-dialog.test.tsx`, FakeWsClient pattern):
  - Opening the picker calls `fs.listDir` with no path (home dir) and
    renders the returned entries.
  - Clicking a directory row re-queries `fs.listDir` with that row's path
    and updates the displayed list.
  - Clicking "Up" queries the parent path; the action is disabled/absent
    when parent is empty (filesystem root).
  - A git-repo-flagged entry renders a visible indicator; a plain one
    doesn't.
  - "Use this folder" calls the provided callback with the currently
    displayed path and closes the dialog, without calling
    `workspace.create` itself.
  - A `fs.listDir` error while navigating into a row is shown inline and
    leaves the previous listing visible (not a blank/broken dialog).
- Web (`crud-dialogs.test.tsx` addition): the "Browse…" button opens the
  picker, and selecting a folder there fills the Path input with that
  path (the existing Create-workspace submit tests are otherwise
  untouched — this only adds a way to fill the same field).

## Decisions

- Server-side directory listing + client-side walking dialog, not the
  File System Access API and not a recent-paths dropdown — see this
  file's intro; explicit user choice.
- `internal/hostfs` is a new, separate package from `internal/files`
  rather than an extension of it: the two have fundamentally different
  sandboxing models (whole host filesystem vs. one task's worktree
  directory), and conflating them risks a future change accidentally
  loosening `internal/files`' worktree confinement.
- No path allow-list/restriction (e.g. confining browsing to the home
  directory subtree) — the daemon is a locally-run process already
  operating with the operator's own OS file permissions for every
  workspace/task it manages; browsing the rest of the filesystem to pick
  a directory isn't a materially larger surface than what already exists.
- "Use this folder" only fills the Path field; it deliberately does not
  also submit the Create form, so the existing validation/error path
  (not-a-git-repo, etc.) stays exactly as it is today, exercised in one
  place.

## Progress

- [x] `internal/hostfs` package + tests
- [x] `fs.listDir` wsapi method + test
- [x] `folder-picker-dialog.tsx` + tests
- [x] Wire "Browse…" into `CreateWorkspaceDialog`
- [x] Verification

## Validation

Acceptance criteria, checked against the Test Scenarios above:

- **`internal/hostfs.List`** (`internal/hostfs/hostfs.go`,
  `internal/hostfs/hostfs_test.go`): directories-only/dot-exclusion,
  git-repo flagging, symlinked-directory following (`os.Stat` per entry,
  not `DirEntry.Type()`), empty-path-defaults-to-home (`t.Setenv("HOME",
  ...)`), parent path + empty-at-`/`, and the three distinguishable errors
  (`fs.ErrNotExist`, `ErrNotDirectory`, `fs.ErrPermission` via
  `errors.Is`) are each their own test — `go test ./internal/hostfs/...
  -v` all green (permission-denied case runs for real here since the
  sandbox user isn't root; guarded by `os.Geteuid() == 0` regardless).
- **`fs.listDir` wsapi method** (`internal/wsapi/hostfs.go`, registered in
  `internal/wsapi/handlers.go`'s `methodHandlers`): wire-level happy path
  and nonexistent-path-as-RPC-error, both added to
  `internal/wsapi/wsapi_test.go` using the existing real-`httptest.Server`
  + real `*websocket.Conn` pattern (`TestServer_FsListDir_HappyPath`,
  `TestServer_FsListDir_NonexistentPath`) — pass.
- **`folder-picker-dialog.tsx`**
  (`web/packages/ui/src/components/folder-picker-dialog.tsx` +
  `folder-picker-dialog.test.tsx`): opens at home dir (no `path` param),
  row click re-queries with that row's path, Up queries the parent and is
  disabled when parent is empty, git-repo entries render a
  `data-testid="git-repo-indicator"` badge and plain ones don't, "Use this
  folder" calls `onSelect(result.path)` + closes without touching
  `workspace.create`, and a failed navigation shows the error inline while
  the previous listing stays rendered (verified by asserting the old
  entries are still in the DOM after a rejected second call) — all 6
  scenarios pass.
- **Wired into `CreateWorkspaceDialog`**
  (`web/packages/ui/src/components/crud-dialogs.tsx`): a "Browse…" button
  (`type="button"`, so it can't submit the surrounding `<form>`) next to
  the Path input opens `FolderPickerDialog`; `onSelect` only calls
  `setPath(...)`. Covered by a new test in
  `web/packages/ui/src/components/app-sidebar-crud.test.tsx` (the file
  that already held `CreateWorkspaceDialog`'s existing tests — the plan's
  working title of `crud-dialogs.test.tsx` for this addition doesn't
  correspond to an existing file in this repo; extended the real home
  instead of creating a duplicate one) — all pre-existing
  `CreateWorkspaceDialog` tests still pass unmodified.
- Full verify sequence, all green: `task build`, `task test`, `task lint`
  (Go: `go vet` + `gofmt` check), `bunx tsc -b` (web typecheck), `bun run
  test` (136/136 web tests across 13 files, including the 2 new test
  files/additions this plan introduced).

Everything in this plan was verified directly (real temp-dir filesystems,
a real WebSocket connection, real component tests) — no step required
credentials or a real browser click-through, so nothing here is an
honesty-caveat like the OAuth login plan's manual-click requirement.
