# Wire up the Space model (CLI + web UI)

## Acceptance Criteria

- Backend needs **no changes** — confirm this before writing anything else:
  `internal/store.Space`, `internal/workspace.Manager.CreateSpace`/
  `GetSpace`/`ListSpaces`, and `internal/wsapi`'s `space.create`/
  `space.list`/`space.get` already exist and work; `task.create` already
  accepts a nullable `spaceId`. This task is entirely about actually using
  what's already there from the CLI and the web UI — neither surface calls
  any `space.*` method or passes `spaceId` today.
- CLI: `smind space create <workspaceId> <title>`, `smind space ls
  <workspaceId>` (same output-shape conventions as `smind workspace
  create`/`ls` — see `cmd/smind/workspace.go`). `smind task new
  <workspaceId> <title>` gains an optional `--space <spaceId>` flag that
  passes through to `task.create`'s existing `spaceId` param; omitting it
  keeps today's behavior (task has no space) exactly as now.
- Web UI: the sidebar shows the full three-level hierarchy — Workspace →
  Space → Task — instead of today's flat Workspace → Task (which silently
  ignores `SpaceID`/space grouping entirely). Tasks with `SpaceID: null`
  still need to appear somewhere sensible (an "Ungrouped" bucket at the
  workspace level, alongside its spaces) — don't let them disappear.
- The web UI's sidebar is **read-only** for this pass, matching the
  existing pattern: nothing in the current UI creates a workspace or a
  task either (that's CLI-only today — confirm this by checking
  `web/packages/ui/src/` for any `workspace.create`/`task.create` call
  before assuming otherwise). Don't introduce a new creation-UI paradigm
  inconsistently just for Space — creating a Space is a CLI-only
  operation for this pass, same division of responsibility as everything
  else.
- Selecting a task inside a space still works exactly like selecting an
  ungrouped one — the task detail pane (`TaskDetailPane` and everything
  inside its tabs) doesn't need to know about spaces at all, only the
  sidebar's tree-building logic changes.

## Test Scenarios

- CLI: `smind space create`/`smind space ls` round-trip against a real
  daemon (manual or automated, your call, but prove it against the real
  built binary at least once). `smind task new --space <id>` creates a
  task with that `SpaceID` set — confirm via `smind task ls` or a direct
  `task.get`-equivalent check, not just "the command exited 0".
  `smind task new` without `--space` still creates an unscoped task
  (regression check — this must not break).
- Web UI: component tests (jsdom + Testing Library + the established
  `FakeWsClient`/similar pattern — see `app-sidebar.test.tsx` for the
  existing pattern to extend) proving: a workspace with two spaces, each
  with tasks, plus one ungrouped task, renders all three groupings
  correctly (both spaces visible with their own tasks, the ungrouped task
  visible too, nothing silently dropped); a workspace with zero spaces
  (today's common case, and every workspace that existed before this
  task) still renders exactly like it does today, flat, no regression;
  selecting a task inside a space still calls the same
  `onSelectTask` callback with the same `Task` shape as an ungrouped one.
- `go build ./...` / `gofmt -l` / `go vet ./...` / `go test -race ./...`
  unaffected and clean (no backend changes expected — confirm, don't
  assume). `bunx tsc -b` clean, `bun run test` passes. `task build`
  succeeds; check `internal/server/dist/.gitkeep` as usual.
- Manual E2E against the real built binary: create a workspace, a space
  in it, a task inside that space, and a second task with no space, via
  the CLI; confirm the web UI's sidebar (verify via the same from-scratch
  Node-script wire-sequence approach every prior web UI task in this
  repo's history has used, since no real browser is available in this
  environment — don't waste time re-confirming that) reflects all of it
  correctly via `workspace.list`/`space.list`/`task.list`.

## Decisions

- No backend changes — see Acceptance Criteria. If you discover the
  backend genuinely does need something (unlikely, but verify rather than
  assume), stop and explain why in your report rather than silently
  expanding scope.
- Space creation stays CLI-only this pass, matching the existing
  workspace/task creation split (CLI creates, web UI browses/interacts)
  — see Acceptance Criteria for the reasoning. A "create space" UI
  control is a reasonable future addition if it turns out to be wanted in
  practice, not built speculatively here.

## Progress

- [x] `smind space create`/`smind space ls` CLI subcommands
- [x] `smind task new --space` flag
- [x] Sidebar: Workspace → Space → Task tree, ungrouped-tasks bucket
- [x] Verification (typecheck/tests/build + real-daemon E2E)

## Validation

- **Backend needs no changes**: confirmed by reading `internal/wsapi/handlers.go`
  (`handleSpaceCreate`/`handleSpaceList`/`handleSpaceGet`/`handleTaskCreate`
  all already exist, `task.create` already accepts a nullable `spaceId`)
  and `internal/workspace/space.go` (`CreateSpace`/`GetSpace`/`ListSpaces`
  already implemented and covered by `internal/workspace/space_test.go`).
  `git status`/`git diff` after implementation show zero files touched
  under `internal/`.

- **CLI `smind space create <workspaceId> <title>` / `smind space ls
  <workspaceId>`**: added in `cmd/smind/space.go`, wired into
  `cmd/smind/main.go`'s dispatch and usage text. Round-tripped against the
  real built binary:
  ```
  $ smind workspace create /tmp/smind-e2e-repo "E2E Workspace" solo
  1  E2E Workspace  /tmp/smind-e2e-repo  solo
  $ smind space create 1 "My Feature Space"
  1  My Feature Space  1
  $ smind space ls 1
  ID  TITLE             WORKSPACEID
  1   My Feature Space  1
  ```

- **`smind task new --space <spaceId>`**: added to `cmd/smind/task.go`'s
  `cmdTaskNew` as a trailing optional flag (parsed by hand, not
  `flag.FlagSet`, since the title itself is a variadic positional argument
  -- the flag is stripped off the tail of the arg list before the
  remaining args are joined into the title). Verified against the real
  binary, including the regression check (no `--space` still creates an
  unscoped task):
  ```
  $ smind task new 1 "Task scoped to space" --space 1
  1  Task scoped to space  created
  $ smind task new 1 "Unscoped task"
  2  Unscoped task  created
  ```
  Confirmed at the data layer (not just exit code 0) via
  `sqlite3 $SMIND_HOME/smind.db "SELECT id, workspace_id, space_id, title FROM tasks;"`:
  ```
  1|1|1|Task scoped to space
  2|1||Unscoped task
  ```
  task 1's `space_id` is the created space's id; task 2's is NULL.

- **Web UI sidebar Workspace → Space → Task tree, with an "Ungrouped"
  bucket for `SpaceID: null` tasks**: implemented in
  `web/packages/ui/src/components/app-sidebar.tsx` (`useWorkspaceTree` now
  fetches `space.list` alongside `task.list` per workspace and groups
  client-side by `Task.SpaceID`; `WorkspaceItem` renders flat when
  `spaces.length === 0` and otherwise renders each `SpaceItem` plus an
  "Ungrouped" bucket when there are any ungrouped tasks). `Space` type
  added to `web/packages/ui/src/lib/types.ts`, verified field-for-field
  against `internal/store.Space`.

  Component tests in `web/packages/ui/src/components/app-sidebar.test.tsx`
  (`bun run test`, 4/4 passing in that file, 46/46 across the whole
  suite):
  - `"a workspace with zero spaces renders its tasks flat, same as before
    space grouping existed"` -- regression check: no `space.list` results
    means no "Ungrouped"/space-heading UI at all, task renders directly
    under the workspace exactly as before this task.
  - `"a workspace with two spaces plus an ungrouped task renders all three
    groupings, nothing dropped"` -- both spaces, each space's own task,
    and the ungrouped task under an explicit "Ungrouped" bucket are all
    present.
  - `"selecting a task inside a space invokes onSelectTask with the same
    Task shape as an ungrouped one"` -- clicking a task nested inside a
    space calls `onSelectTask` with the exact same `Task` object shape the
    original (still-passing) `"clicking a task row invokes onSelectTask
    with that task"` test exercises for an ungrouped task.

- **Web UI stays read-only / CLI-only creation**: confirmed via
  `grep -rn "\.create(" web/packages/ui/src/` returning zero matches
  before this task started -- no existing UI creates a workspace or task
  either, so Space creation staying CLI-only is consistent, not a new
  paradigm.

- **`go build ./...` / `gofmt -l .` / `go vet ./...` / `go test -race
  ./...`**: all clean, full repo, after the change (last run: all 17
  testable packages `ok`, `cmd/smind` correctly `[no test files]` as
  before -- this repo has no CLI unit tests, matching the existing
  `cmd/smind/workspace.go`/`task.go` convention of manual E2E-only
  verification for the CLI layer).

- **`bunx tsc -b`**: clean (run from `web/packages/ui`, where the actual
  `tsconfig.json` lives). `bun run test` (`task test:web`): 46/46 passing.

- **`task build`**: succeeded; `internal/server/dist/.gitkeep` was deleted
  by the Vite build as expected and restored with `touch` + `git add`,
  matching the documented recurring issue.

- **Manual E2E against the real built binary**: built `bin/smind`, started
  it against a temp `SMIND_HOME`, used `smind workspace create`, `smind
  space create`, `smind task new --space`, and `smind task new` (no
  space) to create the full hierarchy, confirmed via `smind space ls`/
  `smind task ls` plus a direct sqlite read (above). Then ran a
  from-scratch Node 26 script (built-in `WebSocket`, no browser) against
  the same running daemon, reproducing `workspace.list` then
  `space.list`+`task.list` per workspace exactly as `useWorkspaceTree`
  does, including the client-side grouping logic -- output confirmed the
  wire shapes match `web/packages/ui/src/lib/types.ts` field-for-field and
  the resulting tree groups correctly:
  ```
  Workspace: E2E Workspace
    Space: My Feature Space
      Task: Task scoped to space [created]
    Ungrouped:
      Task: Unscoped task [created]
  ```
