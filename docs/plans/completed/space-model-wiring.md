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

- [ ] `smind space create`/`smind space ls` CLI subcommands
- [ ] `smind task new --space` flag
- [ ] Sidebar: Workspace → Space → Task tree, ungrouped-tasks bucket
- [ ] Verification (typecheck/tests/build + real-daemon E2E)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
