# Test-gap audit: the fresh-install `null.map` crash class

Audit doc, not a plan. Read-only — no fixes applied here. Triggered by a
real first-run crash: `internal/wsapi` returns `result: null` for
`workspace.list`/`space.list`/`task.list` when the underlying store slice is
`nil`, and `web/packages/ui/src/components/app-sidebar.tsx`'s
`useWorkspaceTree` calls `.map` on it unconditionally. Every unit test
passes (web 104/104, Go full race suite) because no test — front-end or
back-end — ever exercises an empty database.

## 1. Root-cause the gap

The crash site: `app-sidebar.tsx:59-69`.

```ts
const list = await client.call<Workspace[]>("workspace.list");
const withTree = await Promise.all(
  list.map(async (ws) => {           // <- crashes here if list === null
    const [spaces, tasks] = await Promise.all([
      client.call<Space[]>("space.list", { workspaceId: ws.ID }),
      client.call<Task[]>("task.list", { workspaceId: ws.ID }),
    ]);
    ...
    for (const task of tasks) {      // <- `for...of null` throws a
                                      //    *different* message ("tasks is
                                      //    not iterable") if this line is
                                      //    reached instead
```

`client.call<TResult>()` is the second half of the gap:
`web/packages/ui/src/lib/ws-client.ts:454` does `return env.result as
TResult;` — a type assertion, not a runtime check. Whatever JSON value the
daemon sent, including a literal `null`, is handed to the caller typed as
`Workspace[]` with zero validation.

Why no test caught it — three independent, compounding reasons, all
verified against the actual test code (not assumed):

- **`FakeWsClient` never auto-populates anything, so every test author
  picks the response by hand, and every one of them picked a populated or
  empty-but-real array — never `null`.** `web/packages/ui/src/test/fake-
  ws-client.ts:36-46`: `call`/`callStream` just push a `RecordedCall` onto
  a pending queue; nothing about the fake itself defaults, seeds, or
  constrains what a test resolves it with. The fake is faithful to the
  wire's *shape* (a promise resolved with whatever value the test passes)
  but every call site that exercises it does supply a real value:
  `app-sidebar.test.tsx:85-88`'s `resolveWorkspaceTree` helper always calls
  `client.nth("workspace.list", 0).resolve([workspace])` — a real
  `Workspace[]` literal, and `client.nth("space.list", 0).resolve(spaces)`
  where every call site passes either `[]` or a populated array (never
  `null`, never omitted). No test in the file ever resolves a list call
  with `null`, so `useWorkspaceTree`'s `.map` was never asked to handle it.
- **`App.test.tsx` drives a real `WsClient` over a `FakeSocket`, and same
  story: every response is a hand-picked literal.** `fake-socket.ts:39-42`'s
  `emit` just JSON-stringifies whatever envelope the test passes and feeds
  it to the socket's message listeners — it does not simulate what an
  empty SQLite table actually round-trips as. `App.test.tsx`'s `resolveSidebar`
  helper (`App.test.tsx:89-100`) calls `respond(socket, "space.list", [])`
  and `respond(socket, "task.list", tasks)` — `[]`, never `null`. Every one
  of the file's ~10 tests goes through `resolveSidebar` or an inlined
  equivalent, and every one supplies `[WORKSPACE]` for `workspace.list` —
  there is no test in this file for zero workspaces at all.
- **Neither fake could have caught it even if a test tried the empty case
  correctly, unless the test author separately knew to write `null`
  instead of `[]`** — the fakes impose no wire-shape discipline. That's the
  deeper gap: the *fakes* are what stand between "the developer's mental
  model of the RPC contract" and "what SQL actually returns," and nothing
  enforces that the fakes' vocabulary include `null`.

So of the four hypotheses posed for this audit:

- "fakes always return populated arrays" — **partially true**: most do, but
  some tests do use `[]` (e.g. `space.list` in the common case).
- "fakes return `[]` but never `null`" — **confirmed**, precisely; see
  above.
- "Go wsapi tests use pre-seeded stores so list endpoints never return nil
  slices" — **confirmed**, see §2.
- "JSON null round-trip never exercised" — **confirmed**, and it's the root
  cause underneath the other two: nothing in the stack, front or back, ever
  constructs the `null` case and pushes it through the real (de)serializers.

## 2. Go-side mirror

`internal/wsapi`'s only test that exercises `workspace.list` / `space.list`
/ `task.list` end-to-end over a real WS connection is
`TestServer_WorkspaceSpaceTaskCRUDRoundTrip`
(`internal/wsapi/wsapi_test.go:343`). It creates a workspace
(`wsapi_test.go:352-358`), *then* lists (`:367`, asserting `len(workspaces)
!= 1`); creates a space, *then* lists (`:393-416`, asserting `len(spaces)
!= 1`); creates a task, *then* lists (`:424-447`, asserting `len(tasks) !=
1`). Every list call in the suite is preceded by a create for the exact
scope being listed. There is no test anywhere in `internal/wsapi` that
calls `workspace.list`/`space.list`/`task.list` against a store with zero
matching rows — the empty/fresh-install path is structurally absent from
the suite, not just under-asserted.

What the handlers actually return, confirmed by reading the store layer
(not assumed from the handler alone):

- `internal/store/workspaces.go:53,59`: `var workspaces []Workspace` —
  Go's zero value for a slice is `nil`; the loop only appends on a row, so
  zero rows leaves it `nil`. Returned as-is at `:61`.
- `internal/store/spaces.go:54,60`: same pattern, `var spaces []Space`.
- `internal/store/tasks.go:57,63`: same pattern, `var tasks []Task`.
- `internal/workspace/{workspace,space,task}.go`
  (`workspace.go:124-126`, `space.go:30-32`, `task.go:140-142`) are thin
  pass-throughs — `ListWorkspaces`/`ListSpaces`/`ListTasks` return exactly
  what the store gave them, `nil` included.
- `internal/wsapi/handlers.go:151-155,183-193,221-231`
  (`handleWorkspaceList`/`handleSpaceList`/`handleTaskList`) return that
  value directly as the handler's `any` result — no wrapping, no
  `make([]T, 0)` normalization.
- `internal/wsapi/conn.go:184-189`'s `sendResult` calls `marshalOrNull`
  (`wsapi.go:73-79`), which is just `json.Marshal(v)` — and
  `json.Marshal(nil []Workspace)` is the JSON literal `null`, not `[]`.
  That's not a Go-test-visible bug at all: `go test`'s in-process
  `json.Unmarshal(resp.Result, &workspaces)` into a `[]store.Workspace`
  happily accepts `null` and leaves `workspaces` as `nil` — `len(nil) ==
  0`, so `if len(workspaces) != 1` would fail loudly on a *populated* case
  but a hypothetical *empty* case would just silently produce `len ==
  0`, indistinguishable in a Go test from a correctly-returned `[]`. A Go
  test asserting only `len(result)` can never tell `null` and `[]` apart —
  the wire distinction only matters to a client language (JS) whose `null`
  and `[]` are not interchangeable. This is exactly why the bug is
  invisible from the Go side even in principle, not just in practice.

Contrast: `internal/wsapi/handlers.go:119-133`'s `handleAccountList`
*does* defend against this — `result := make([]accountResult, len(stored))`
guarantees a non-nil (possibly zero-length) slice regardless of what
`registry.List()` returned, so `account.list` can never wire-marshal to
`null`. Same defensive pattern in `internal/runs/registry.go:742-750`
(`Registry.List`, via `make([]RunSummary, 0, len(rs))`) and
`internal/terminal/registry.go:709-727` (`Registry.List`, via
`make([]SessionStatus, 0, len(ss))`) and `internal/files/files.go:89`
(`List`, via `make([]Entry, 0, len(dirEntries))`). Four of the seven
list-shaped RPCs already guard against nil; the three that don't
(`workspace.list`, `space.list`, `task.list`) are exactly the three that
crashed, and not coincidentally — the difference is whether the specific
author of that handler happened to build the result with `make(..., 0,
...)`+append versus a bare `var x []T`. There is no shared convention or
lint rule enforcing one over the other.

## 3. Sweep for siblings

Searched every `internal/store` function matching `var \w+ \[\]` (the
nil-slice construction pattern) and traced each to whether it crosses the
wire directly, plus every web hook/component calling `.call(...)` on a
list-shaped RPC and checked for a null guard before `.map`/`.filter`/`for
...of`/indexing.

| file:line | RPC | crash condition | severity |
|---|---|---|---|
| `app-sidebar.tsx:61` (`list.map`) | `workspace.list` | zero workspaces (true fresh install) | **fresh-install hit** — this is the reported crash, first thing rendered after connecting |
| `app-sidebar.tsx:69` (`for (const task of tasks)`) | `task.list` | a workspace exists but has zero tasks (e.g. just-created workspace, or all tasks archived out — actually check: `ListTasks` has no archived filter, so this needs literally zero rows) | **fresh-install-adjacent** — hit the moment a user gets past workspace.list with one workspace but nothing in it yet; distinct error message (`... is not iterable`, not `reading 'map' of null`) |
| `app-sidebar.tsx:81` (`spaces.map`) | `space.list` | a workspace with zero spaces | **fresh-install-adjacent** — hit on literally every workspace today, since Space wiring is new and nothing has spaces yet by default (`space-model-wiring.md`'s own acceptance criteria calls zero-spaces "today's common case") |
| `diff-viewer-pane.tsx:60` (`result.files ?? []`) | `task.files` | task with no changed files | **defended already** — see below, the one place in the sweep that got it right |
| `internal/workspace/git.go:251` (`var files []TaskFile`) | `task.files` | task with no changed files (clean worktree) | Go-side: wire-shape is `{"files": null}` for a clean task (`taskFilesResult.Files` has no `omitempty`, and the nil slice marshals to `null` same as §2). Web-side is already guarded (`?? []`), so **not currently exploitable**, but it's the same class of Go bug and would bite any *other* consumer of `task.files` that doesn't happen to null-guard |
| `use-task-attention.ts:99` (`runs.filter(...)`) | `run.list` | none currently — `internal/runs/registry.go:742` always `make()`s a non-nil slice | latent only: the client trusts the wire shape with no guard; safe today purely because the Go side happens to be defensive (§2) |
| `use-run-timeline.ts:208` (`list.filter(...)`) | `run.list` | same as above | latent only, same reasoning |
| `use-file-explorer.ts:96` (`entries` stored directly, later rendered as a tree) | `file.list` | none currently — `internal/files/files.go:89` always `make()`s | latent only |
| `terminal-pane.tsx:228` (`.then((sessions) => ...)`, indexes/searches `sessions`) | `terminal.list` | none currently — `internal/terminal/registry.go:709-727` always `make()`s | latent only |
| `task-detail.tsx:215` (`ProviderListResult`) | `provider.list` | none — `taskrunner.SupportedProviders()` (`internal/taskrunner/provider.go:45-52`) is a hardcoded non-nil literal | not exploitable, no store involved |
| `internal/store/accounts.go:67` (`var accounts []Account`) | `account.list` | none currently — `handleAccountList` (`handlers.go:128`) wraps with `make([]accountResult, len(stored))` before returning, and the web UI doesn't currently call `account.list` at all (no call site found) | not exploitable today on either side |
| `internal/store/workspaces.go:89` (`var ids []int64`, `ListWorkspaceAccountIDs`) | none — not wired to any wsapi handler | n/a | not reachable over the wire at all currently |
| `internal/store/routing_decisions.go:47`, `internal/store/quota_snapshots.go:47`, `internal/store/terminal_sessions.go:110` (all `var x []T` nil-slice patterns) | none — each is consumed only by internal subsystems (`internal/routing`, `internal/quota`, `internal/terminal`'s startup rehydration) via plain Go `range`, which is nil-safe | n/a | not reachable over the wire; ranging over a nil slice in Go never panics, unlike JS `.map` on `null` |

Net: **the three handlers that crash on fresh install are exactly
`workspace.list`, `space.list`, `task.list`** — the only three list RPCs
whose Go implementation is a bare `var x []T` *and* whose web consumer is
on the unconditional first-render path with no null guard. `task.files`
is the same Go-side bug but happens to be caught client-side already. Every
other list RPC in the codebase is either Go-side defended (`make(...)`) or
not reachable on an empty-database first render at all (`run.list`,
`terminal.list`, `file.list` all require an existing task to even be
callable).

## 4. Test-infrastructure fixes

Ordered by value (what it would have caught ÷ cost to add):

1. **Go contract test: one `internal/wsapi` test asserting the *raw wire
   bytes* of an empty list result are `[]`, not `null`, for every list
   RPC.** Would directly catch this bug and the `task.files` sibling — it
   tests the exact thing that broke (`json.Marshal(nil)` vs `make([]T,
   0)`), at the layer where the fix actually belongs. Cheapest to write
   (one small test function per RPC, no new fixtures — a fresh
   `newTestWorkspaceManager`/`newTestWSServer` with *nothing* created is
   already the natural "empty" state, the opposite of what
   `TestServer_WorkspaceSpaceTaskCRUDRoundTrip` currently sets up). Must
   assert on the raw `json.RawMessage`/string bytes (`resp.Result`), not
   on `len(decoded)` — §2 showed why decoding into a Go slice first makes
   `null` and `[]` indistinguishable and defeats the point.
2. **Make `FakeWsClient` (and the `App.test.tsx` `respond` helper) refuse
   to silently accept `null` for a call typed as returning a list, and add
   one variant test per list-consuming component that explicitly resolves
   with `null` to prove the component tolerates it.** This is the fix
   aimed at the *front-end* half of the bug and is what would have caught
   it even if the Go side were never fixed (defense in depth — a client
   should not trust the wire). Cost: `FakeWsClient` itself shouldn't
   auto-reject arbitrary values (it's deliberately unopinionated, per its
   own doc comment at `fake-ws-client.ts:13-22`, "so tests can drive exact
   ordering and race scenarios") — the more surgical version is a handful
   of new test cases per component (`app-sidebar.test.tsx`,
   `terminal-pane`, `use-file-explorer`, etc.) that resolve with `null`
   instead of `[]` and assert no crash + a sane empty render. Medium cost
   (one test per consumer, ~9 sites found in §3), high value (catches
   regressions in either direction, Go or web).
3. **A jsdom "empty daemon" `App`-level test: connect succeeds, then
   `workspace.list` (and, if reached, `space.list`/`task.list`) resolve
   with literal `null`, assert the sidebar renders "No workspaces yet."
   (or equivalent) instead of throwing.** This is the single test that
   would have caught the *actual reported incident* end-to-end, at the
   same altitude as the bug was discovered (a real first run). Cheapest
   possible framing: extend `App.test.tsx`'s existing `resolveSidebar`
   helper (`App.test.tsx:89-100`) with a sibling that passes `null`
   instead of `[]`/`[WORKSPACE]`, reusing all existing plumbing
   (`FakeSocket`, `respond`). Low cost, and it's the regression test for
   *this specific incident*, so it should exist regardless of the other
   two.
4. **A snapshot/schema check of the wire envelope shape per list RPC**
   (e.g. golden JSON files or a JSON-schema assertion that a list RPC's
   result is always `type: array`, never `["array", "null"]`) would
   generalize (1) into something that can't regress silently as new list
   RPCs get added, since it's enforced structurally rather than per-RPC by
   hand. Higher cost (new tooling/convention), lower immediate value than
   (1)-(3) since it's prevention-of-recurrence rather than catching the
   current bug — worth doing only after (1)-(3) exist and a second
   instance of this bug class has actually happened once.

## 5. Process note: did the spec process ask for this and get skipped, or never ask?

Checked the plans that shaped `app-sidebar.tsx`'s current logic and the one
active plan that will touch it next:

- **`docs/plans/completed/web-ui-foundation.md`** (the plan that first
  wired `workspace.list`/`task.list` into the sidebar) — its Test
  Scenarios section (`:38-64`) requires a WS client round-trip test, a
  `GET /api/token` test, and manual verification "against the real built
  binary: start `smind serve`, **create a workspace/task via the CLI**...
  confirm the sidebar reflects them" (`:54-56`). Every scenario, automated
  and manual, presupposes data already exists. Zero scenarios mention an
  empty/first-run/pre-seed state. The gap isn't "the scenario existed and
  was skipped" — **it was never written**.
- **`docs/plans/completed/space-model-wiring.md`** (the plan that added
  the `spaces.map`/`ungroupedTasks` grouping logic that's the second crash
  site in §3) — its Test Scenarios (`:36-65`) explicitly call out "a
  workspace with **zero spaces** (today's common case, and every workspace
  that existed before this task) still renders exactly like it does today,
  flat, no regression" (`:50-52`) — which is exactly the `flat =
  workspace.spaces.length === 0` branch (`app-sidebar.tsx:223`) and *is*
  covered (`app-sidebar.test.tsx:117-132`). So this plan's authors were
  actively thinking about "zero of a nested collection" as a scenario to
  name — but only one level down (zero spaces *within* a workspace, zero
  tasks *within* a space/workspace). Nowhere does it ask for "zero
  workspaces at all" — the outermost, most basic empty case, the one that
  actually crashed. The manual E2E step (`:59-65`) again starts from
  "create a workspace, a space in it, a task inside that space, and a
  second task with no space" — populated by construction.
- **`docs/plans/completed/web-ui-diff-viewer.md`** shows the repo's spec
  process *can* produce exactly the right kind of scenario when someone
  thinks to ask for it: `:41-46` requires testing "the no-changes case"
  for `task.diff` at the Go level, `:47-48` explicitly requires "an
  `internal/wsapi` test proving `task.diff`'s wire shape round-trips over
  a real WS connection," and `:51-53` requires the frontend's "no-changes
  case shows a clear empty state." That's the wire-shape-for-the-empty-
  case discipline that §4's recommendation (1) wants generalized — it
  exists as a *pattern* in this repo's spec culture, just was never applied
  to `workspace.list`/`space.list`/`task.list` (whose "no-changes case" is
  "no rows exist yet," an even more basic version of the same idea) nor to
  `task.files` (§3's Go-side sibling).
- **`docs/plans/active/tab-registry-side-dock.md`** (not yet completed,
  next in line to touch this code) — its Test Scenarios (`:32-43`) cover
  cross-task tab identity, close behavior, and attention badges; no
  empty/first-run scenario named, same pattern as the other plans. Since
  it's active, this is an opportunity: the plan should gain an explicit
  "zero tasks in the sidebar" scenario before implementation, not after.

**Conclusion for §5:** this is a systematic omission in how Test Scenarios
get written, not a case of a named scenario being dropped during
implementation. The plans that *do* name a "zero collection" scenario
(space-model-wiring's "zero spaces") get it right and it ships tested
(`app-sidebar.test.tsx:117-132` passes and is a real regression guard).
The plans never ask "what if the parent collection itself is empty" —
every scenario, across every plan reviewed, implicitly assumes at least
one workspace exists by the time the sidebar is being tested. AGENTS.md
rule (c)'s requirement for named test scenarios is being followed
faithfully; the scenarios themselves have a blind spot for the very first
state a real user ever sees.

## Top 3 fixes, ranked

1. **Go contract tests asserting empty-list wire shape is `[]` not `null`**
   for `workspace.list`/`space.list`/`task.list` (and `task.files`) — fixes
   the bug at its source, cheapest to add, and is the one existing repo
   pattern (`web-ui-diff-viewer.md`'s wire-shape tests) this incident shows
   needs to be applied more broadly.
2. **A jsdom "empty daemon" `App`-level regression test** (workspace.list
   resolves `null`) — this is the direct regression test for the actual
   incident, cheap to add by extending the existing `resolveSidebar`
   helper, and would fail loudly if this exact bug class ever comes back
   even if the Go-side fix is somehow reverted or bypassed.
3. **A `null`-resolving variant test per list-consuming component** (via
   `FakeWsClient`), proving components tolerate a `null` result even when
   the Go side is behaving — defense in depth against the next handler
   that's written as `var x []T` instead of `make([]T, 0, ...)`, since
   nothing currently stops that pattern from being reintroduced.
