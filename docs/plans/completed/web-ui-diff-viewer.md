# Web UI: diff viewer

## Acceptance Criteria

- A new daemon method `task.diff {taskId}` (in `internal/wsapi`, backed by
  a small addition wherever fits best — likely `internal/workspace`, since
  it already owns every other git-worktree operation for a task via
  `os/exec` `git` calls, see `internal/workspace/git.go`) returns the
  task's real working-tree diff: everything changed in the task's git
  worktree relative to the branch it was created from (uncommitted changes
  *and* committed-but-not-yet-on-base commits — i.e. what `git diff
  <base>...HEAD` plus the worktree's uncommitted changes actually looks
  like; decide the exact git invocation and document why in this doc's
  Decisions section once you've confirmed it against a real worktree with
  both staged and unstaged changes). Return unified diff text (the
  simplest, most robust wire shape — a frontend diff-rendering library can
  parse unified diff text directly, no need to invent a structured JSON
  hunk format).
- `task.diff` on a task with no changes returns an empty (or clearly
  "no changes") result, not an error.
- Frontend: a self-contained `DiffViewerPane` component (new file(s) under
  `web/packages/ui/src/components/`) taking `{ client, task }` props,
  fetching `task.diff` and rendering it as a real side-by-side or unified
  diff view with syntax highlighting per file — add a diff-rendering
  library (e.g. `react-diff-viewer-continued`, `diff2html`, or similar;
  your choice, pick something actively maintained and compatible with
  React 19). A refresh control (button, or re-fetch on some reasonable
  trigger) since the diff changes as an agent works — this doesn't need to
  be live-streaming, a manual refresh is enough for this pass.
- **Do NOT wire `DiffViewerPane` into `App.tsx` or `task-detail.tsx`.**
  Deliberate, not an oversight — a file explorer and a terminal are being
  built in parallel against the same shell files; three agents editing
  `App.tsx` independently would conflict. Prove `DiffViewerPane` works as
  a fully self-contained, independently mountable component (your own
  component tests rendering it standalone, plus real-daemon manual
  verification below); actual integration into the app shell happens in
  one follow-up step after all three parallel features merge.

## Test Scenarios

- Go: a real test creating a task's worktree, making real changes to it
  (write a file, `git add`, maybe a real commit, leave something
  uncommitted too) and confirming `task.diff` reflects them accurately —
  use the same real-git-worktree test pattern already established
  (`internal/wsapi/run_test.go`'s `newTestTask`/`newTestRepo`). Also test
  the no-changes case.
- `internal/wsapi` test proving `task.diff`'s wire shape round-trips over a
  real WS connection.
- Frontend component tests (jsdom + Testing Library + `FakeWsClient`, same
  pattern as `task-detail.test.tsx`) for `DiffViewerPane`: renders
  `task.diff`'s result as a diff view, the no-changes case shows a clear
  empty state (not a blank/broken render), a `task.diff` failure surfaces
  an error, the refresh control re-issues `task.diff`.
- Manual/E2E verification against the real built binary, honestly scoped
  like prior web UI tasks (no real browser available — a from-scratch Node
  script driving the exact wire call the component makes, against a real
  task worktree with real changes, is the standard here now).
- `go build ./...` / `gofmt -l` / `go vet ./...` / `go test -race ./...`
  clean. `bunx tsc -b` clean, `bun run test` passes. `task build` succeeds;
  check `internal/server/dist/.gitkeep`.

## Decisions

- Unified diff text over the wire, not a structured hunk format — simpler,
  and diff-rendering libraries parse it directly.
- **The exact `task.diff` git invocation** (pinned down by actually
  experimenting against real worktrees with staged, unstaged, untracked,
  and committed-ahead-of-base changes all present at once — see
  `internal/workspace/git.go`'s `taskDiff`/`taskDiffBase`):
  1. **Finding "base"** — the commit the task's branch was created from.
     `CreateTask` never records this explicitly, and `git worktree add
     <path> -b <branch>` (no explicit start point) always branches from
     the workspace repo's HEAD at that instant, so there's no stored
     "base branch name" to diff against, and the workspace repo's *current*
     checkout could have moved on by the time `task.diff` runs. The fix:
     `git worktree add -b <branch>` records a `"branch: Created from
     HEAD"` entry as `<branch>`'s very first reflog entry — confirmed for
     real (`git log -g --format=%H <branch>`, oldest/last line). That
     commit hash is `base`, independent of anything that happens to the
     workspace repo's own checkout afterward, and needs no schema change.
  2. **Producing the diff.** Tried `git diff <base>...HEAD` (committed-only)
     plus a separately-glued `git diff`/`git diff --cached` for
     uncommitted changes — rejected: two independently-generated diffs
     with their own hunk contexts don't safely concatenate, and neither
     `git diff` nor `git diff --cached` shows brand-new untracked files
     without staging them first (which would mutate the real index).
     Landed on: copy the worktree's real index (`git rev-parse --git-path
     index`) into a throwaway temp file, run `git add -A` against *that*
     copy only (via `GIT_INDEX_FILE=<tmp>`, never touching the real
     index/working tree), then `git diff --no-color --cached <base>`
     against the snapshot. This produces one unified diff of `base`'s tree
     vs. the fully-snapshotted working tree — covering real commits ahead
     of base, staged changes, unstaged changes, *and* untracked new files
     (including deletions, which a fresh/empty temp index could not detect
     — `git add -A` needs pre-existing tracked entries to compare the
     working tree against, which is exactly why the real index is copied
     first rather than starting from scratch) — in a single pass, with
     zero side effects on the caller's real repo state. Verified
     end-to-end manually (see Validation) that a real `git status`
     immediately before and after `task.diff` is byte-identical.
  3. A worktree with nothing changed produces an empty string and no
     error (`git diff --cached <base>` against an identical snapshot
     produces empty stdout, exit 0).
- Explicitly NOT wiring into `App.tsx`/`task-detail.tsx` — see Acceptance
  Criteria; built in parallel with a file explorer and a terminal against
  the same shell.
- **Diff-rendering library: `diff2html`** (not `react-diff-viewer-continued`).
  `react-diff-viewer-continued` takes two full-text blobs (`oldValue`/
  `newValue`) and computes its own diff — it does not parse unified diff
  text directly, which is exactly the wire shape `task.diff` deliberately
  settled on (see the first Decision above), so using it would mean
  parsing the diff client-side into a structured format after all, which
  is what this project explicitly wanted to avoid. `diff2html`'s
  `Diff2HtmlUI` class (`diff2html/lib/ui/js/diff2html-ui.js`) takes raw
  unified diff text directly, renders a real side-by-side view via DOM
  (`.draw()`), and does per-file syntax highlighting via a bundled
  `highlight.js` (`.highlightCode()`) — `highlight.js` was added as an
  explicit dependency since diff2html only lists it as an optional peer.
  No React peer dependency at all (framework-agnostic), so no React 19
  compatibility question. Actively maintained (last publish within the
  current year at the time of writing).
- Explicitly out of scope: per-file stage/unstage/discard actions, inline
  commenting, live-updating without a manual refresh.

## Progress

- [x] `task.diff` backend (git diff of a task's worktree vs. its base)
- [x] `internal/wsapi`: `task.diff` + tests
- [x] `DiffViewerPane` component + tests
- [x] Verification (typecheck/tests/build + real-daemon E2E script)

## Validation

- **Go real-worktree test** (`internal/workspace/diff_test.go`,
  `TestManager_Diff`): one subtest creates a task, makes zero changes,
  confirms `Diff()` returns `""` with no error. The other subtest makes a
  real commit ahead of base, a staged change on top of it, an unstaged
  change on top of that, and a brand-new untracked file, all in the same
  worktree, then confirms the returned diff contains every one of those
  changes' distinguishing content, that the base's own unmodified content
  is never shown as removed, and that `git status --porcelain` in the
  worktree is byte-identical before and after `Diff()` (proving no
  mutation of real repo state). `go test -race ./internal/workspace/...`
  passes.
- **`internal/wsapi` round-trip test** (`internal/wsapi/diff_test.go`,
  `TestServer_TaskDiff_RoundTrip`): a real WebSocket connection sends
  `{taskId}` to `task.diff` and decodes a `{diff}` result — one subtest
  against a task with an uncommitted scenario file (diff contains its
  content), one against an untouched task (`diff == ""`, no error).
- **Frontend component tests**
  (`web/packages/ui/src/components/diff-viewer-pane.test.tsx`, 5 tests,
  `FakeWsClient`): mount issues `task.diff` with `{taskId}`; a resolved
  non-empty diff renders into a `diff2html`-produced DOM tree containing
  the changed file's path and added-line text (proving it's a real parsed
  diff view, not raw text dumped to the page); an empty-string result
  shows a distinct `data-testid="diff-empty"` "No changes." state, never a
  blank render; a rejected `task.diff` call surfaces the error text in a
  `data-testid="diff-error"` element; clicking Refresh re-issues
  `task.diff` and the view updates from the second response; with
  `client={null}` the Refresh button is disabled and no call is issued.
  All pass under `bun run test` (21/21 total across the package, no
  regressions to existing suites).
- **`go build ./...` / `gofmt -l .` / `go vet ./...` / `go test -race
  ./...`**: all clean across the whole repo (15 Go packages with tests,
  all passing).
- **`bunx tsc -b`**: clean.
- **`task build`**: succeeds end-to-end (`bun install`, web build into
  `internal/server/dist`, `go build -o bin/smind`).
  `internal/server/dist/.gitkeep` was deleted by the build as expected
  (known Vite `--emptyOutDir` behavior from every prior web UI task in
  this repo) — restored via `touch` + `git add`, confirmed `git status`
  shows no diff under `internal/server/dist` afterward.
- **Real-daemon manual verification**: built `bin/smind`, started it
  against a throwaway `SMIND_HOME` (`smind serve`), used the built CLI
  (`smind workspace create`, `smind task new`) to create a real workspace
  (backed by a real git repo in a temp dir) and a real task (a real git
  worktree). In that worktree: made a real commit ahead of base, a staged
  change, an unstaged change, and a brand-new untracked file. A
  from-scratch Node (v26, built-in `fetch`/`WebSocket`, no framework)
  script dialed the daemon's real `/ws` endpoint with the token from
  `$SMIND_HOME/token`, called `task.diff` with the real `taskId`, and
  confirmed the returned unified diff text contained every one of the
  four kinds of changes' distinguishing content. A second task with no
  changes confirmed `task.diff` returns `{"diff": ""}`. `git status
  --short` in the worktree, checked immediately before and after these
  `task.diff` calls, was identical both times — confirming no side
  effects on the real repo state from a real daemon process, not just the
  Go test's in-process check. The daemon shut down cleanly on `kill`
  (`ps aux` showed no leftover process).
  - **What this did NOT verify** (consistent with every prior web UI
    task's honestly-scoped report — no real browser is available in this
    sandbox): actual visual rendering of the side-by-side diff view,
    real syntax-highlighting colors/theme, or any CSS layout — jsdom's
    component tests confirm the DOM structure diff2html produces is
    correct and contains the right content, but not what it looks like
    rendered. That's deferred to the follow-up integration step once this
    merges alongside the file explorer and terminal features and someone
    can click around the running app in a real browser.
