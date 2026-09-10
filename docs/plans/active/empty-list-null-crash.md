# P0: fix empty-list null crash + empty-daemon test coverage

Incident: fresh install crashes ("Cannot read properties of null
(reading 'map')"). Both audits agree on root cause and fix — see
docs/research/uiux-audit.md and docs/research/test-gap-audit.md.

## Acceptance Criteria

### Backend (root cause)

- `internal/store`'s three SQL list functions (workspace/space/task)
  return `[]` not nil: initialize with `make([]T, 0)` like the
  in-memory registries (runs/terminal/files) already do. Also fix
  `task.files`'s Go side identically (it has the same nil-slice bug
  even though its client guards today).
- **Go contract tests**: for EVERY list RPC (workspace.list,
  space.list, task.list, task.files, run.list, terminal.list,
  file.list, account.list, provider.list), one wire-level test against
  an empty daemon/store asserts the raw JSON result is `[]` (or the
  correct empty object shape), never `null` — decode the raw envelope
  bytes, not a typed struct, so `null` would fail the test (typed
  decode would silently accept it).

### Client (defense in depth)

- `useWorkspaceTree` (app-sidebar.tsx) and any other component calling
  a list RPC tolerates a `null` result (`?? []` at the RPC boundary) —
  the wire discipline is backend-owned, but a fresh daemon of any
  version shouldn't white-screen the UI.
- FakeWsClient keeps its current shape; the null-tolerance is covered
  by the App-level test below.

### Regression (the incident itself)

- A jsdom **"empty daemon" App-level test**: every list RPC resolves
  `null` → App renders the sidebar empty state, no throw. This is the
  direct regression test for the incident.

## Test Scenarios

- Go: the contract tests above (one per list RPC, empty store).
- Web: the empty-daemon App test; existing suites still pass.
- Manual verify: `rm -rf $SMIND_HOME && smind serve` + browser → empty
  state renders (record result in Validation honestly; if no browser
  available, the jsdom test stands in).

## Decisions

- Store fixes are exactly `var x []T` → `x := make([]T, 0)` in the four
  spots the audit named: `internal/store/workspaces.go:53`,
  `spaces.go:54`, `tasks.go:57`, `internal/workspace/git.go:251`
  (`taskChangedFiles`). Other `var x []T` sites in internal/store
  (accounts, routing_decisions, quota_snapshots, terminal_sessions,
  ListWorkspaceAccountIDs) stay per the audit — none cross the wire.
- Contract tests live in `internal/wsapi/empty_list_test.go`, split in
  two: `TestServer_EmptyDaemonListResultsAreArrays` (the 7 RPCs
  callable with a completely empty store) and
  `TestServer_EmptyTaskListResultsAreArrays` (task.files + file.list,
  which require an existing task — a clean worktree and an empty dir
  respectively). A shared `emptyListResult` helper sends each request
  and asserts raw `resp.Result` bytes are the expected literal prefix
  (`[]` / `{"files":[]}` / `{"providers":[`), never `null`.
  provider.list asserts a prefix only, since its providers content is
  a hardcoded literal that varies.
- Client guards: `?? []` / `.then((r) => r ?? [])` at the RPC boundary
  in `useWorkspaceTree` (workspace.list, space.list, task.list),
  `useTaskAttention` (run.list), `TerminalPane` (terminal.list), and
  `useFileExplorer` (file.list). task.files was already guarded
  (diff-viewer-pane) and provider.list is never null (Go literal), so
  no change there.
- The App-level regression test answers `workspace.list` and
  `run.list` with literal `null` (the only list RPCs that fire on a
  connect with zero workspaces — space.list/task.list never fire when
  the outer list is empty) and asserts "No workspaces yet." renders.

## Progress

- [x] store nil-slice fixes (4 functions)
- [x] contract tests (all list RPCs)
- [x] client null guards + empty-daemon App test
- [x] Verification (both chains) + manual check

## Validation

- `go build ./...` clean; `gofmt -l .` empty; `go vet ./...` clean.
- `go test -race ./...` — all packages pass, including the two new
  contract tests (9 RPC shapes asserted).
- `task test` (go test + web suite) passes; `task lint` (vet + gofmt)
  passes.
- Web: `bunx tsc -b` clean; `bun run test` 112/112 across 10 files,
  including the new "an empty daemon (every list RPC resolving null)
  renders the sidebar empty state without throwing" App test (12
  App.test.tsx tests total). `task build` succeeded; dist restored to
  committed `.gitkeep`-only state.
- Manual browser check not run in this sandbox (no browser); the jsdom
  empty-daemon test stands in, per the scenario's own allowance.
