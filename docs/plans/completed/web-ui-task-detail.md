# Web UI: task detail + agent timeline

## Acceptance Criteria

- Clicking a task in the sidebar (`AppSidebar`'s `WorkspaceItem`/task rows)
  selects it and shows a task detail pane in `App.tsx`'s main content area,
  replacing the current "Select a task to get started" placeholder — no
  full page navigation, this is client-side state (selected task id lifted
  into `App`).
- The detail pane shows the selected task's identity (title, status,
  branch) and a chat-log-style timeline of its runs (`run.list` filtered by
  `TaskID`, chronological, most recent last), each entry showing that run's
  collected text and terminal status once known.
- A run that is still `running` when the pane opens streams its remaining
  output live into the timeline (`run.attach`: backfill of everything
  emitted so far, then the live tail) — regardless of whether this browser
  tab/connection was present when the run started. This is a real UI-level
  proof of the Registry's cross-connection reattach guarantee established
  in `docs/plans/completed/run-registry-and-cli.md`: a run started via the
  CLI (or another browser tab) and still going must appear live here too.
- Finished runs show their full history via `run.logs` (no live
  subscription needed for something already terminal).
- A prompt form (provider select — `claude-native` | `glm`, the only two
  `taskrunner.Provider` values that exist today, see
  `internal/taskrunner/provider.go`; a text input; a submit control) starts
  a new run and immediately streams it live.
- The prompt form uses `run.start` + `run.attach`, **not** `task.prompt` —
  this is the same reasoning `docs/plans/completed/run-registry-and-cli.md`
  already established for the CLI's `task send`: `task.prompt` stops its
  run when its own request/connection context goes away, which is wrong
  for a UI where switching tasks or closing the tab should only detach, not
  kill the agent. `run.start` returns the run id immediately (that request
  is never in flight when the user navigates away), then `run.attach`
  streams it — `run.attach`'s own cancellation is already detach-only.
- Switching away from a task (selecting a different one, or the component
  unmounting) cleanly unsubscribes from any active `run.attach` without
  stopping the run — verified by starting a run, switching to a different
  task and back, and confirming the run is still going (or finished
  normally) with its output intact: no gap, no truncation, no double
  subscription.
- No stale-update races: switching rapidly between tasks doesn't leave a
  previous task's now-irrelevant in-flight request or live subscription
  updating the *new* selection's view — guard the same way
  `useWorkspaceTree` already does (a `cancelled` flag / cleanup-time
  unsubscribe), applied to the run-list fetch and the live `run.attach`
  subscription both.

## Test Scenarios

- Component-level behavior is proven with real, executable tests — not
  just informal narrative — using Vitest + jsdom +
  `@testing-library/react` (new to this package; add it) driven against a
  fake `WsClient`-shaped object (same pattern `ws-client.test.ts`'s
  `FakeSocket` already established for the WS layer itself, one level up:
  here the fake stands in for `WsClient`'s public surface — `call`/
  `callStream` — not the socket underneath it). Cover:
  - Selecting a task fetches and renders its run history.
  - A running run's live chunks append to the timeline as they arrive (not
    buffered until a terminal event).
  - Switching to a different task and back doesn't duplicate the first
    task's timeline entries or leak its live subscription into the second
    task's view.
  - Unmounting (or switching away) while a run is actively streaming calls
    the fake client's unsubscribe/abort path, not a real `run.stop`-shaped
    call — detach, not stop.
  - Submitting the prompt form calls `run.start` then `run.attach` (in that
    order, as two distinct calls) — not `task.prompt`.
  - A rapid double-switch (task A → B → A) doesn't leave task B's
    now-stale fetch overwriting task A's re-selected view once it resolves
    late.
- Manual/E2E verification against the real built binary, honestly scoped
  like the prior task: no real browser is available in this environment
  (confirmed last task — `npx playwright install chromium` fails for lack
  of root), so use the same from-scratch-script approach that stood in for
  it before (a Node script using built-in `fetch`/`WebSocket` driving the
  exact same call sequence the component makes) to confirm the real
  daemon's responses match what the component code expects, end to end:
  `run.start` → `run.attach` backfill+live → a second, independent
  connection also seeing the run via `run.list`/`run.logs` mid-stream
  (proving the cross-connection guarantee this feature's whole value
  proposition rests on). State plainly what was and wasn't actually
  verified, same as last time.
- `go build ./...` / `gofmt -l` / `go vet ./...` / `go test -race ./...`
  unaffected (no Go changes expected — everything needed already exists:
  `run.start`/`run.attach`/`run.list`/`run.logs`).
- `bunx tsc -b` clean, `bun run test` (now via `task test:web`, wired into
  `task test` per the prior task) passes with the new component tests
  included.
- `task build` still succeeds; check `git status` for the
  `internal/server/dist/.gitkeep` regeneration issue as usual.

## Decisions

- `run.start` + `run.attach`, not `task.prompt`, for the reasons above —
  consistent with the CLI's already-established choice, not a new
  precedent.
- Provider is a fixed two-option select (`claude-native`, `glm`) rather
  than a dynamically-discovered list — there is no provider-discovery
  endpoint yet, and inventing one speculatively is out of scope here.
- Adds `@testing-library/react` + jsdom to `web/packages/ui`'s Vitest setup
  — the previous task's honest limitation (no real browser available) is
  real and stays real, but component-level interaction/render logic (as
  opposed to actual pixels) can and should be verified for real via jsdom
  rather than left to narrative description.
- The provider select uses a plain native `<select>` (styled to match the
  existing shadcn input/button primitives) rather than adding shadcn's
  `Select` component: no `Select` primitive existed in this codebase yet,
  it's a fixed two-option list, and pulling in a new Radix-based
  primitive just for that felt like scope creep beyond this task's actual
  need. Revisit if/when a real dynamic-options select is needed
  elsewhere.
- Explicitly out of scope, deferred to later tasks: file explorer,
  CodeMirror 6, diff viewer, xterm terminal, permission prompts UI, and any
  run.stop/cancel control in the UI itself (stopping a run from the web UI
  is a natural near-future addition but isn't needed for this task's core
  loop of "see history, watch live, send a prompt").

## Progress

- [x] `@testing-library/react` + jsdom added to the Vitest setup
- [x] Task selection lifted into `App.tsx`; sidebar rows are clickable
- [x] Task detail pane: run timeline (history + live streaming)
- [x] Prompt form (`run.start` + `run.attach`)
- [x] Component tests for the above
- [x] Verification (typecheck/tests/build + real-daemon E2E script)

## Validation

(Filled in as each Acceptance Criterion is confirmed — command run, test
name, or manual check.)

- Vitest + jsdom + `@testing-library/react`: added as devDependencies
  (`web/packages/ui/package.json`), `vitest.config.ts` switched
  `environment: "node"` -> `"jsdom"`, include pattern widened to
  `*.test.{ts,tsx}`, and a `setupFiles` entry
  (`src/test/setup.ts`) registers `@testing-library/jest-dom/vitest`
  matchers, `IS_REACT_ACT_ENVIRONMENT` (React 19 requires this explicitly
  under Vitest), a `window.matchMedia` stub (jsdom doesn't implement it,
  and the shadcn Sidebar primitives call it unconditionally), and
  `cleanup()` after every test. `@testing-library/jest-dom` was also
  added (not in the original plan text, but needed for `toBeInTheDocument`/
  `toHaveTextContent` — the standard RTL/Vitest pairing).
- Clicking a task selects it, client-side, no navigation: `App.tsx` lifts
  `selectedTask` state; `AppSidebar`/`WorkspaceItem`
  (`web/packages/ui/src/components/app-sidebar.tsx`) take
  `selectedTaskId`/`onSelectTask` props and wire the task row's
  `onClick` + `isActive`. Proven by
  `app-sidebar.test.tsx`'s "clicking a task row invokes onSelectTask with
  that task", and the placeholder is now conditionally replaced by
  `TaskDetailPane` in `App.tsx`.
- Detail pane identity + run timeline (history + live streaming):
  `web/packages/ui/src/components/task-detail.tsx` +
  `web/packages/ui/src/hooks/use-run-timeline.ts`. Proven by
  `task-detail.test.tsx`'s "selecting a task fetches and renders its run
  history" (run.list -> run.logs for an already-terminal run) and
  "streams a running run's live chunks into the timeline as they arrive,
  not buffered until the terminal event" (run.attach backfill+live via a
  fake client, chunks visible before the terminal response resolves).
- Cross-connection reattach (a run started elsewhere still streams live
  here): proven at the component level by the same "streams a running
  run's live chunks" test (the fake run.list already reports the run as
  `running` before the pane's own run.attach call exists, exactly the
  "started by someone else" case) and, end-to-end against the real
  daemon, by `verify.mjs` (see below): connection A's run.attach receives
  5 backfilled/live chunks with real ~300ms gaps (1203ms total spread,
  ruling out buffered delivery) while an independent connection B
  observes the same run via `run.list`/`run.logs` mid-stream and again
  after it finishes.
- Finished runs show full history via `run.logs` (no subscription):
  covered by the same "selecting a task fetches and renders its run
  history" test, and by `verify.mjs`'s final `run.logs` call on
  connection B after the run reaches `done`.
- Prompt form starts a run via `run.start` + `run.attach`, never
  `task.prompt`: `task-detail.test.tsx`'s "submits the prompt form via
  run.start then run.attach, never task.prompt" asserts the exact call
  order (`run.start` before `run.attach`, as two distinct fake-client
  calls) and that `task.prompt` is never called. Re-confirmed against the
  real daemon by `verify.mjs`'s `run.start` timing assertion (resolves in
  1ms, never blocking on the run itself).
- Switching away (or unmounting) detaches without stopping the run:
  `task-detail.test.tsx`'s "aborts (does not stop) an actively streaming
  run.attach on unmount" (asserts the fake client's `run.attach` call's
  `AbortSignal` is aborted, and that `run.stop` is never called) and
  "switching to a different task and back doesn't duplicate entries or
  leak the live subscription" (same abort assertion on task switch, plus
  proves the run's full text is intact — no gap/truncation — once
  reselected). Re-confirmed against the real daemon by `verify-detach.mjs`:
  a `run.attach` cancelled via `task.cancel` on its own request id (the
  same mechanism `WsClient`'s `AbortSignal` path uses) settles in 1ms,
  and a fresh connection immediately afterward sees the run still
  `running` with its pre-detach backfill (`"before hang"`) intact — then
  an explicit cross-connection `run.stop` (from a third call, not the one
  that started or attached to the run) is what actually stops it,
  confirming detach and stop are genuinely different operations. No
  orphan `fakeagent` subprocess remained after the daemon's graceful
  shutdown (`ps aux | grep fakeagent` empty).
- No stale-update races: `task-detail.test.tsx`'s "a rapid double-switch
  (A -> B -> A) discards task B's now-stale fetch instead of letting it
  overwrite the re-selected task A view" — task A's first `run.list` call
  is left pending, the selection moves to B and back to A (issuing a
  *second* `run.list` for A), and only then is the stale first call
  resolved; asserted it never renders, while the second (fresh) call's
  data does.

Commands run: `go build ./...`, `gofmt -l .`, `go vet ./...`,
`go test -race ./...` (all packages, unaffected by this task — no Go
changes), `bunx tsc -b` (clean), `bun run test` / `task test:web` / `task
test` (17 tests total across `ws-client.test.ts`, `app-sidebar.test.tsx`,
`task-detail.test.tsx` — all passing), `task lint`, `task build`
(`internal/server/dist/.gitkeep` regenerated as a real file by the Vite
build each time, as with every prior task; restored via `git checkout --
internal/server/dist/.gitkeep` before committing).

Manual/E2E: built `bin/smind` and a `internal/taskrunner/fakeagent`
binary, ran the real daemon (`SMIND_ACP_COMMAND` pointed at fakeagent, an
isolated `SMIND_HOME`) against a real git-initialized workspace repo,
created via the real CLI (`smind workspace create`, `smind task new`).
Two standalone Node scripts (`/tmp/smind-e2e/verify.mjs` and
`verify-detach.mjs`, built-in `fetch`/`WebSocket` only, no browser, no
bundler) drove the exact same `run.start`/`run.attach`/`run.list`/
`run.logs`/`task.cancel` wire calls the component/hook make, against
independent WebSocket connections standing in for separate browser tabs.
Both scripts' every assertion passed (see their own inline checks); full
transcripts are not committed (temp scripts, outside the repo) but the
call sequences and shapes they exercise are exactly
`web/packages/ui/src/hooks/use-run-timeline.ts`'s.

What was **not** verified, honestly: no real browser is available in
this sandbox (`npx playwright install chromium` fails for lack of root,
confirmed by the prior task and not re-attempted here) — so actual pixel
rendering, CSS layout, focus/keyboard behavior, and real click-vs-anchor
interaction on `SidebarMenuSubButton` (which renders as an `<a>` with no
`href`) were not checked visually. jsdom + `@testing-library/react`
verify real React render/effect/cleanup/state semantics (the component
tree actually re-renders as events arrive, effects actually clean up in
the right order, DOM nodes with the right `textContent` actually appear)
but not what it looks like or whether a real mouse click on the actual
rendered pixels works. The prompt form's native `<select>` (no shadcn
Select primitive existed in this codebase; adding one felt like scope
creep for a fixed two-option list, see Decisions) was only exercised via
its DOM value, not visually.
