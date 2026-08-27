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
- Explicitly out of scope, deferred to later tasks: file explorer,
  CodeMirror 6, diff viewer, xterm terminal, permission prompts UI, and any
  run.stop/cancel control in the UI itself (stopping a run from the web UI
  is a natural near-future addition but isn't needed for this task's core
  loop of "see history, watch live, send a prompt").

## Progress

- [ ] `@testing-library/react` + jsdom added to the Vitest setup
- [ ] Task selection lifted into `App.tsx`; sidebar rows are clickable
- [ ] Task detail pane: run timeline (history + live streaming)
- [ ] Prompt form (`run.start` + `run.attach`)
- [ ] Component tests for the above
- [ ] Verification (typecheck/tests/build + real-daemon E2E script)

## Validation

(Filled in as each Acceptance Criterion is confirmed — command run, test
name, or manual check.)
