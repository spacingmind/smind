# Mobile: create a task from the phone (M6)

Status: **completed 2026-10-22** (PR #480 and the follow-up below); native (device) verification is the one open item.

Size: M (5) — scope 2 (mobile + one api surface, no daemon change), PRs 1, risk 1 (zero-run task flow, partial failure), decisions 0, verification 1 (390 px light+dark smoke, web only).

Closes a Phase 3 gap: the phone can read, follow and answer tasks (mobile-zcode-redesign, completed) but cannot start one. Presentation follows ZCode's narrow
style on the M1 foundations (`theme.ts` tokens, `src/ui/*`, `@expo/ui` through `UiHost`); skills are the rulebook as in the redesign (expo-ui, expo-design-system,
expo-native-ui, expo-router, expo-animation, vercel-react-native-skills). Pattern reference: `refs/paseo/packages/app/src/{create-agent-preferences,provider-selection,
new-workspace-launch}` (one launch form; the last provider/model/mode is remembered per provider; model/mode come from the provider catalog).

## Contract (read from the daemon, no daemon change)

- `task.create {workspaceId, spaceId?, title, parentTaskId?, baseRef?|branch?|prNumber?}` → `Task` (`internal/wsapi/handlers.go` `handleTaskCreate`). Web's New Task
  dialog (`web/packages/ui/src/components/crud-dialogs.tsx`) sends only `workspaceId`, `spaceId?`, `title`; the first prompt is a separate composer send afterwards.
- `run.start {taskId, provider, prompt, permissionMode?, model?, thinkingLevel?, whenBusy?, chatId?}` → `{runId}` (non-blocking; the existing `startRun` does the same
  and the screen then follows it with `run.attach`). `provider.list {}` → `{providers: ProviderInfo[]}` with `id, label, modes[{id,label,description,autoApproves}],
  defaultMode, models[{id,label,default}], modelsDiscovered` (`internal/taskrunner/provider.go`).
- Mobile today lists only `workspace.list[0]` (`loadTasks`), and a task with **no run** has a disabled composer (`TaskDetailScreen`: `canSend` needs `state.run !== null`,
  the provider was inferred from the latest run) — so a task created without a first prompt would be a dead end. This plan fixes that.

## Scope and write set

Mobile only. NEW: `mobile/src/newTask.ts` (pure), `mobile/src/providerCatalog.ts` (pure), `mobile/src/newTaskPrefs.ts`, `mobile/src/screens/NewTaskScreen.tsx` +
`mobile/src/screens/newtask/*`, `mobile/app/new-task.tsx`, tests in `mobile/src/__tests__/`, smoke stage `mobile/scripts/smoke/stages/55-new-task.mjs`. EDIT:
`mobile/src/api.ts` (add `createTask`, `listProviders`, `startFirstRun`; existing functions unchanged), `mobile/app/_layout.tsx` (register the route), `mobile/app/index.tsx`
(header entry), `mobile/src/screens/TaskDetailScreen.tsx` (zero-run composer only), `mobile/scripts/smoke/lib.mjs` (only if a helper is genuinely shared). Unchanged:
relay, E2EE, `chatQueue`, `runFollower`, `permissionRequests`, theme tokens, `src/ui/*`.

## Design

- **Entry point:** a labelled `New task` action in the task-list header (`headerRight`, `TextButton`; `Connection` moves to `headerLeft`). Icon-only is not an option (the
  universal `@expo/ui` `Button` has no accessibility label, redesign decision). If the two header actions do not fit at 390 px, fall back to a bottom-docked filled `New task`
  button above the safe area — decided from the screenshot, recorded in Progress.
- **Form:** a route `app/new-task.tsx` in the `app` Protected group, `presentation: 'modal'` (native sheet-like on iOS, full-screen modal elsewhere; a card slide on web).
  Header `New task`, left `Cancel`. Body is a keyboard-safe scroll view: **Workspace** (only when `workspace.list` has more than one; default = the list's workspace),
  **Space** (only when the chosen workspace has spaces; default none), **Title** (required, single line), **First prompt** (optional, multiline), then **Provider**,
  **Model** and **Permission mode** (`@expo/ui` `Picker`, menu appearance) and a filled `Create task` button. Model and mode follow the provider (catalog-driven); a mode with
  `autoApproves` is labelled "(auto-approves)". An unchanged model/mode is omitted from the wire (daemon default).
- **Start mode (ADR-0039 G4 later):** the input carries `start: StartMode` where `StartMode = { kind: 'head' }` today, mapped by one pure `startModeParams(start)` to
  `{}` / `{baseRef}` / `{branch}` / `{prNumber}`; the form renders the field through a `StartModeField` slot that renders nothing for now. Adding "Branch / PR / issue" is then a
  new `StartMode` variant + one component, not a rewrite.
- **Submit:** `submitNewTask(conn, input, deps)` (pure, fake-connection tested): validate → `task.create` → if a prompt was given `run.start` on the new task → result
  `{ task, runId? }`; then dismiss the modal and push the task. A **partial failure** (task created, `run.start` failed) keeps the form open with the error, remembers the
  created task, and offers `Open task` — a retry only re-runs `run.start` (never a second task). Double taps are ignored while busy.
- **Zero-run tasks:** the detail composer becomes live for a task with no run; the first send uses the remembered selection else the daemon's first provider
  (`provider.list`), via `startFirstRun`. (Today's follow-ups on tasks with runs are unchanged.)
- **Preferences:** `newTaskPrefs` keeps the last provider and per-provider model/mode **in memory for the session** (mirrors Paseo's `preferences.ts` shape);
  persistence across launches is a follow-up (no storage dependency is added here).
- **Payload hygiene:** title ≤ 200 chars, prompt ≤ 100 000 chars, clamped client-side with a visible counter only near the limit; nothing else is sent.

## Acceptance Criteria

1. The task list header has a `New task` action that opens the form; `Connection` remains reachable (sheet unchanged); both are labelled and ≥ 44 pt.
2. The form shows Workspace only when more than one workspace exists, Space only when the workspace has spaces, always Title, First prompt, Provider, Model, Permission mode.
   Provider/Model/Mode options come from `provider.list`; changing the provider resets model and mode to that provider's remembered or default values.
3. `Create task` is disabled for an empty/whitespace title and while a submit is in flight; a title or prompt over the limit is clamped.
4. Submitting calls exactly `task.create` (`workspaceId`, `spaceId?`, `title` trimmed, plus `startModeParams(start)` = `{}` for HEAD) and, only with a non-empty prompt, `run.start`
   (`taskId`, `provider`, `prompt`, `model?`, `permissionMode?`, `whenBusy: 'queue'`). A `queued` result is a success. No other fields; no large payloads.
5. On success the form closes and the new task's detail screen is pushed with `id`+`title` params (header correct on the first frame); with a prompt the run is visible and
   streaming (user bubble, then agent output); without a prompt the task opens with a live composer. **Deviation:** a finished run's transcript has no user line in `run.logs`,
   so the detail screen now shows the run's own `Prompt` as the user bubble when the logs carry none (`withRunPrompt`; this also fixes every historical run). Only the latest
   run is shown, as before.
5a. With a setup script running the prompt is queued and the task opens showing the queued prompt chip; it starts when setup ends.
6. Partial failure (task created, `run.start` rejected) never creates a second task: the error is shown inline, `Open task` navigates to the created task, and retry only calls
   `run.start`. A `task.create` failure shows the daemon's message inline and keeps the input.
7. A task with no runs can send its first prompt from the detail composer (provider = remembered or first from `provider.list`); a daemon with no providers shows a clear empty
   state instead of a dead composer.
8. The start-mode seam exists (`StartMode`, `startModeParams`, `StartModeField` slot) with HEAD the only variant, and a test proves the three future variants map to the
   existing `task.create` fields (so G4 changes only the mobile form).
9. Works over the relay E2EE client (the live integration suite creates a task and starts a run through `RelayConnection`); `provider.list` failure or a dropped connection
   shows an error with Retry, never a blank form.
10. Tokens only (the `noHardcodedColors` scan stays green), `@expo/ui` controls inside `UiHost`, no regression in the existing screens (queue, stop, needs-continue, pairing).

## Test Scenarios

Unit (vitest, pure modules; names are the file names):
- `newTask.test.ts`: `validateNewTask` (empty/whitespace/over-long title, over-long prompt, trims, clamps); `taskCreateParams` (HEAD → exactly `{workspaceId, spaceId?, title}`;
  omits `spaceId` when none); `startModeParams` (head `{}`, and the future baseRef/branch/pr variants map to `baseRef`/`branch`/`prNumber`); `runStartParams` (omits unchanged
  model/mode and an empty prompt means no run); `submitNewTask` against a fake connection: title only → one `task.create`, no `run.start`; title+prompt → both in order with
  the created id; `task.create` rejects → no `run.start`, error carried; `run.start` rejects → `{ partial: task }` and a retry helper calls only `run.start`; double submit
  ignored.
- `providerCatalog.test.ts`: `defaultSelection` (remembered provider wins, else first; default model = catalog `Default`; default mode = `defaultMode`; unknown remembered
  provider/model/mode fall back), `modeOptions` labels `autoApproves`, empty catalog → `null`.
- `newTaskPrefs.test.ts`: remember/recall per provider; an unknown provider recalls nothing.
- `api.newTask.test.ts` (fake connection): `createTask`, `listProviders`, `startFirstRun` send exactly the documented fields.
- `newTaskRoute.test.ts` (or in `routes.test.ts`): the new route href and its header title.
- `noHardcodedColors` and `icons` drift tests stay green (the new files are scanned).

Live integration (`relay/__tests__/integration.newtask.node.test.ts`, joins `test:integration`): over the relay client `provider.list` returns the harness provider;
`task.create` returns a task in the harness workspace; `run.start` on it returns a runId whose `run.attach` stream ends done; `task.create` with no prompt leaves the task with no run.

Smoke (`scripts/smoke/stages/55-new-task.mjs`, 390×844@2x, light + dark): frames `new-task-form-{light,dark}` (empty), `new-task-filled-*`, `new-task-error-*` (empty title /
daemon error), transitions `new-task-open-t*` and `new-task-close-t*` (Cancel), `new-task-submit-t*` (Create → first task frame; the t90 frame must not be blank),
`new-task-created-*` (task screen with the first prompt visible), `new-task-empty-composer-*` (no-prompt task with a live composer); and the headless assertions: after submit the
daemon has the task (via `task.list`), the header shows the title, and a second tap during submit creates no second task.

## Decisions

- No ADR: no architecture choice (an existing RPC pair, a new screen, session-only prefs). The one judgment call — letting a zero-run task take its first prompt from the
  detail composer — is recorded here.
- Modal route, not an `@expo/ui` `BottomSheet`: a form with a keyboard needs a real screen; the sheet stays for the read-only connection card.
- Session-only prefs (no new storage dependency); persistence is a follow-up.

## Progress

- 2026-10-22 — Plan written (this file). Contract read from `handlers.go`, web's dialog, Paseo's launch/preferences code.
- 2026-10-22 — Lead review of the contract found ADR-0024's setup busy window: the first prompt is sent with `whenBusy: 'queue'` and `queued` counts as success (AC 4/5a).
- 2026-10-22 — M6-A data layer (api additions, provider catalog, prefs, submit pipeline, live integration test); then in parallel M6-B1 (route, form, header entry, smoke
  stage 55) and M6-B2 (zero-run composer, first-send provider, run prompt on finished runs) in two worktrees, cherry-picked onto one branch. Review fixes: stale-closure
  `setLoad`, the empty-catalog note, first-send-only `refreshLatest`, milestone labels removed from comments, the `.smoke` PNGs a Peer had committed were dropped from history.
- Header entry fit at 390 px in both schemes (`Connection` left, `New task` right), so no bottom button was needed.

## Validation

Run on the merged tree (this branch + `origin/develop` at 6ce16377), all local:

| Check | Result |
|---|---|
| `npm run typecheck` | clean |
| `npm test` | 49 files / 539 passed |
| `npm run test:integration` | 8 files / 28 passed, 1 skipped (the pre-existing password-harness guard); includes the new `integration.newtask.node.test.ts` (provider.list, task.create, first run to `done`, run-less task) |
| `npm run smoke:web:full` | passed, 122 frames at 390×844@2x, light + dark, incl. stage 55 (form, filled, open/close transitions, submit transition with a non-blank t90, created, title-only task) and every earlier stage unchanged |
| Run-less task, live (throwaway harness test, not committed) | empty state + live composer in both schemes; first send shows the user line, adopts the run, strip + reply (the provider was the machine's real `claude-native` with an expired token, so the reply was its 401 text; the `glm` path is covered by the integration suite and unit tests) |

Frames (not committed): `/tmp/m6-final-frames/` (full smoke), `new-task-*`; `/tmp/m6-b2-frames/zerorun-*`.

Acceptance: 1–4, 5 (with the recorded deviation), 6, 7, 8, 9, 10 confirmed on web. Unverified: the queued first prompt during a setup script (AC 5a) beyond unit tests/code walk; everything native.

**Correction (follow-up PR).** The table above said the created task showed its first prompt; the `new-task-created-*` frames of #480 actually showed only the reply. The
run was still live when the screen loaded, and only the finished-run path added the run's own prompt; the live-attach branch had no prompt line. Fixed by passing the
run row's prompt as the attach prelude, and the stage's assertion (which had tolerated the missing bubble) is now strict, per scheme, with a count of one task after a double
tap and the submit transition captured in dark too. Re-validated on the tree `origin/develop` (54b79b8e) + the follow-up: typecheck clean; `npm test` 49 files / 539 passed;
`test:integration` 28 passed, 1 skipped; `expo-doctor` 20/20 and `expo install --check` up to date (expo 58.0.17 and six more patches, `expo-dev-client ~58.0.14` added for the
EAS `development` profile); `smoke:web:full` passed, 125 frames. One earlier full-smoke run on this tree failed in `queue-stop` ("the delivered prompt did not appear",
the delivered run's reply merged into the previous assistant chunk, i.e. no user line for the daemon-delivered prompt); `queue-stop` alone passed twice and two further full
runs passed. The cause is not proven; suspected pre-existing adoption race (queue snapshot vs `run.status`), see Follow-ups.

## Follow-ups

- ~~Investigate the rare `queue-stop` miss~~ **Resolved** (`fix/mobile-adopted-prompt-race`). Root cause: three places attach to a run (the load, a `run.status` adoption, and
  `refreshLatest`), but only the adoption carried the run's prompt. When the previous run ends, `refreshLatest` runs (`onLiveChange(null)`); if it lists the queue-delivered run
  before that run's `run.status` arrives, it marks the run known and attaches it without a prompt line, and the follower then never signals an adoption, so the delivered prompt
  has no user line and its reply merges into the previous assistant chunk. Reproduction (deterministic): delay the follower's `onAdopt` by 150, 400 or 1000 ms (a three-line
  local patch: wrap the adopt callback's body in `setTimeout(..., Number(process.env.EXPO_PUBLIC_JITTER_MS))`, never committed) and run
  `SMOKE_PORT=… EXPO_PUBLIC_JITTER_MS=400 node scripts/smoke-web.mjs --stage queue-stop`: 3/3 failed with the exact symptom before the fix and 3/3 passed after it, while 16
  un-jittered runs (two lanes in parallel) all passed. Fix: `attachRun` always prepends the run's own prompt (queue snapshot, else `run.list`) whichever site attaches.
  The follower contract that makes the refresh path the only attach is pinned in `runFollower.test.ts`.
- Persist the last provider/model/mode across launches (secure store or async storage).
- ADR-0039 G4 start modes (HEAD / branch / PR / issue) in this form; ADR-0038 attachments in the first prompt.
- Thinking level (Claude only) is not exposed on mobile yet.
- Native (device/simulator) verification of the form: see the native checklist handed to the Supervisor. Risks specific to this PR: `UiHost` (`matchContents`) sizing
  inside the form's `ScrollView`/`Card` on native, the `router.dismiss()` then `router.push()` sequence out of a native modal, the native `Picker` menus, keyboard avoidance
  with the multiline prompt.
- The queued-first-prompt window (workspace setup script running → `queued` → queue chip → run adopted on delivery) is covered by unit tests and a code walk only; no live
  setup-script scenario exists in the harness.
- The web renderer of `@expo/ui` `Picker` paints its own palette (a blue-black box in dark), unlike the neutral inputs; native pickers are system controls.
- Only the latest run's transcript is shown on the task screen; earlier prompts and runs are not (pre-existing).
