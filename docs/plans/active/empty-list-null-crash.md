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

(Filled by implementer: exact store function changes, contract-test
 helper shape, where the `?? []` guards land.)

## Progress

- [ ] store nil-slice fixes (4 functions)
- [ ] contract tests (all list RPCs)
- [ ] client null guards + empty-daemon App test
- [ ] Verification (both chains) + manual check

## Validation

(Filled as confirmed.)
