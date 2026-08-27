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
- Exactly which git invocation defines "the diff" is left to the
  implementer to pin down against real worktree behavior and document here
  (uncommitted-only vs. also including commits ahead of base) — the
  Acceptance Criteria's intent is "everything this task has actually
  changed, that a reviewer would want to see," get the exact command right
  and explain the choice.
- Explicitly NOT wiring into `App.tsx`/`task-detail.tsx` — see Acceptance
  Criteria; built in parallel with a file explorer and a terminal against
  the same shell.
- Explicitly out of scope: per-file stage/unstage/discard actions, inline
  commenting, live-updating without a manual refresh.

## Progress

- [ ] `task.diff` backend (git diff of a task's worktree vs. its base)
- [ ] `internal/wsapi`: `task.diff` + tests
- [ ] `DiffViewerPane` component + tests
- [ ] Verification (typecheck/tests/build + real-daemon E2E script)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
