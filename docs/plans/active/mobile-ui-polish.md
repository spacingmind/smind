# Mobile app UI polish: design tokens + agent-run visual patterns

## Context

Milestones 1-3 (`docs/plans/completed/mobile-app-milestone-{1,2,3}.md`)
built a fully functional mobile app — pairing, workspace/task list, a
live streaming task-detail timeline, follow-up prompts, permission
approve/deny — with zero design polish. Confirmed by reading the code:

- Three screens (`mobile/src/screens/{PairingScreen,TasksScreen,
  TaskDetailScreen}.tsx`) use plain `StyleSheet.create` with **~41
  hardcoded hex color literals** between them, no shared theme, no dark
  mode, no design tokens of any kind.
- `mobile/package.json` has `@expo/ui` installed (the roadmap's stated
  target — `docs/ROADMAP.md`'s Phase 3 "Mobile app (Expo + @expo/ui)")
  but **zero usage** anywhere in `mobile/src`.
- The web UI (`web/packages/ui`) already has a mature token system —
  three completed plans (`docs/plans/completed/{ui-redesign-parity,
  visual-identity-console,web-ui-fixes}.md`) shipped a full "calm
  technical console" identity: semantic surface tokens (`surface-0..3`),
  a type-role scale, an elevation/motion vocabulary, status/diff color
  families. `docs/design.md` is the living reference for that vocabulary
  (read in full this session) — this plan ports the same *vocabulary*
  to mobile, not a new one, so "surface-2" or "status-danger" means the
  same thing on both surfaces.

Researched via `pplx -m best` (2 queries, this session) specifically for
this app's current screens:

**Query 1 — token architecture for Expo/@expo/ui.** Confirms the right
shape: one semantic token *vocabulary*, two renderers (web emits CSS
vars, mobile exports a typed theme object) — not a literal shared npm
package for a 3-screen standalone app (see Decisions). Dark mode via
`useColorScheme()` + `expo-system-ui` (`userInterfaceStyle: "automatic"`
in `app.json`), a `ThemeProvider`/`useAppTheme()` pair mirroring
`web/src/hooks/use-theme.tsx`'s shape. `@expo/ui`'s real division of
labor: native SwiftUI/Compose controls (buttons, fields, pickers,
sheets, menus) versus plain RN for bespoke/high-control surfaces (a
streaming timeline, custom cards) — not a wholesale replacement of
`StyleSheet`-based screens.

**Query 2 — mobile agent-run UI patterns (task list, live timeline,
inline permission cards).** Concrete, current (2025-2026) guidance,
citing Claude Code's and Cursor's mobile companion apps: task list rows
are **run cards** (status badge with icon+text, not color alone; a
one-line "current activity"; blocked-on-approval work sorted above
merely-running work, which stays above finished work); a live timeline
collapses routine tool calls to a compact header row (icon + human
label + duration), expandable on tap, never a raw terminal dump; a
permission request renders as an **inline card that stays in the
timeline through pending → resolved**, not a modal, so approval context
(what came right before it) is never lost.

**What's actually available to build these patterns on** (checked
against the real wire contracts before deciding scope, not assumed):

- `Task.Status` (`mobile/src/api.ts`) is a real, meaningful field set by
  `internal/workspace/task.go`'s `UpdateTaskStatus` (e.g. `"running"`) —
  the web sidebar already renders it directly
  (`app-sidebar.tsx`'s `TaskMetaRow`, `{status.toUpperCase()}`, no
  fancier mapping exists yet either). No task-level "waiting for
  approval" status string exists — that's a run-level, in-timeline
  signal (see next point), not a coarse task state.
- `internal/wsapi/events.go` has a real `permission.pending` bus topic
  (`{runId, taskId, requestId, summary, options}`, fired by
  `runs.Registry`) — a screen subscribed to it (via
  `RelayConnection.subscribe`, Milestone 2's Item 1) learns the instant
  a task's run needs approval, workspace-wide, without attaching to
  every run's `run.attach`. **There is no matching "permission resolved"
  bus topic** — resolution only appears inside the specific run's own
  `run.attach`/`run.logs` stream, which a list screen isn't attached to.
  See Decisions for the honest (not-inferred) behavior this implies.
- `internal/taskrunner/event.go`'s tool-call status vocabulary is
  exactly three values: `ToolStatusRunning` (covers ACP's
  pending+in_progress — no separate "queued" state exists),
  `ToolStatusSuccess`, `ToolStatusFailure`. **There is no raw
  stdout/output-line field on a tool-call event** — `runTimeline.ts`'s
  `tool_call` line is already exactly `{toolName, title, status}` and
  that's the full extent of what's on the wire. A "last N lines of live
  command output" capsule (a pattern Query 2 recommends) is **not
  buildable with real data today** — do not fake it.

## Decisions

- **Token vocabulary is ported by name from `docs/design.md`, not
  reinvented.** `mobile/src/theme.ts` defines `surface.0..3`,
  `status.{success,danger,warning,running}` (text tier) +
  `statusDot.{success,danger,warning,running}` (dot tier, higher
  chroma — same two-tier reasoning as web's, `design.md` §1), and a
  type-role scale (`screenTitle`/`sectionTitle`/`panelTitle`/
  `metadataLabel`/`body`/`code`) mirroring web's `--text-*` roles. Same
  words, same meaning, independently valued per platform — this is
  Query 1's "one vocabulary, two renderers" recommendation, and it's
  what makes a future contributor's mental model transfer between the
  two apps.
- **No shared npm package.** Milestone 1 already decided `mobile/` is a
  standalone Expo app, not nested in `web`'s bun workspace
  (`docs/plans/completed/mobile-app-milestone-1.md`'s Decisions) —
  introducing a shared token package now means new monorepo tooling for
  a 3-screen app. Port token *values* by hand into
  `mobile/src/theme.ts`; if the two vocabularies drift or a real
  shared-package need shows up later, that's a separate, deliberate
  decision, not a default.
- **`@expo/ui` adoption is explicitly out of scope for this pass.**
  Query 1's own guidance is that native SwiftUI/Compose controls suit
  buttons/fields/sheets/menus, while a bespoke streaming timeline stays
  plain RN — but this codebase's entire mobile test suite is
  logic-layer only (no `@testing-library/react-native`, established
  since Milestone 1), and native `@expo/ui` components can't be
  exercised by that style of test at all. Introducing a new,
  platform-specific, effectively-untestable rendering layer in the same
  pass as a token-system port is exactly the kind of large/novel-domain
  scope this session's own Paseo-orchestration experience shows GLM
  reliably stalls on (see this repo's Paseo model-choice memory).
  Rebuild all three screens on the new token system in plain RN first;
  `@expo/ui` adoption is a clean, separable follow-up once the token
  foundation exists to theme it against.
- **Dark mode defaults to system, no manual toggle UI yet.** Matches
  Query 1's recommendation and mirrors web's own default; a
  light/dark/system picker is a small, separable addition once the
  token system exists — not required for "the app looks intentional in
  both OS appearances," which is this pass's actual bar.
- **The "needs approval" list-screen indicator is session-scoped, not
  persisted across a refresh.** Since there's no `permission.resolved`
  bus topic, a naive "remember every taskId that ever fired
  `permission.pending`" would accumulate stale badges forever once a
  request is resolved elsewhere. Instead: `TasksScreen` resets its
  pending-approval set to empty on every `load()` (pull-to-refresh or
  first mount) and only accrues taskIds from `permission.pending`
  events received live *since that reset* — an honest "something new
  needs your attention since you last checked," never a claim about
  current ground truth the screen can't actually verify. State this
  plainly in the empty/badge copy if there's any ambiguity risk (e.g. a
  badge is additive-since-refresh, not omniscient).
- **Tool-call cards show name + title + one of three real statuses
  (running/success/failure), collapsed by default with tap-to-expand
  for the title's full text if truncated — no fabricated output
  preview, no percentage/progress bar.** This is the honest ceiling of
  what `ToolStatusRunning/Success/Failure` + `toolName`/`title` support;
  Query 2's "show last few lines of live output" pattern needs a wire
  field smind's event model doesn't have and is not being added here
  (a `internal/taskrunner` wire-contract change is out of scope for a
  rendering-only pass).
- **No new Go/wsapi work.** Every pattern above is buildable against
  already-existing RPCs and event topics — matches Milestone 2/3's own
  "nothing new to build on the Go side" precedent.
- Continues every standing mobile-app bias: no navigation library,
  logic-layer test discipline (fake/harness-backed connections, not
  component rendering), smallest-real-slice sequencing.

## Acceptance Criteria

### Item 1 — token foundation + PairingScreen

- `mobile/src/theme.ts` exports `lightTheme`/`darkTheme` objects (the
  token vocabulary above: surface/status/statusDot/type-role/spacing/
  radius) and an `AppThemeProvider`/`useAppTheme()` pair
  (`mobile/src/theme/ThemeProvider.tsx` or colocated) that resolves
  `system` via `useColorScheme()` by default.
- `app.json` sets `userInterfaceStyle: "automatic"`;
  `expo-system-ui` is installed if required for Android dark-mode
  switching (confirm the actual requirement for this Expo SDK version
  before adding the dependency).
- `PairingScreen.tsx` (the smallest screen, ~7 hex literals today) is
  fully rebuilt on the theme: zero hardcoded hex/rgb literals remain in
  the file (grep-checkable), correct rendering in both light and dark
  (verified by a token-presence/no-hardcoded-color style check, see
  Test Scenarios — mirroring web's own
  `no-hardcoded-colors.test.ts`/`token-presence.test.ts` pattern).
- No visible regression to `PairingScreen`'s existing behavior (QR/URL
  entry, connect, error states) — this item is a re-skin, not a
  behavior change.

### Item 2 — TasksScreen: run-card list

- Each task renders as a card (not a plain row): title, a status
  badge using real `task.Status` text **and** an icon (never color
  alone, per Query 2), space/workspace context line.
- Sort order surfaces attention first: tasks with a live
  `permission.pending` hit (this session, per Decisions) sort above
  tasks with `Status === "running"`, which sort above finished/errored
  tasks. Ties broken by existing order (no new backend sort needed).
- `TasksScreen` subscribes to the `permission.pending` topic (via
  `conn.subscribe`, already-proven Milestone 2 mechanism) for the
  screen's lifetime; unsubscribes on unmount (no leaked subscription,
  matching every prior milestone's detach discipline).
- Zero hardcoded hex/rgb literals remain in `TasksScreen.tsx`.
- Existing behavior preserved: pull-to-refresh, empty state, error +
  retry, grouped-by-space layout, tap-to-open-task.

### Item 3 — TaskDetailScreen + permission cards: timeline visual polish

- Text/thinking lines, tool-call rows, and permission cards all render
  via the token system — zero hardcoded hex/rgb literals remain in
  `TaskDetailScreen.tsx` and `runTimeline.ts`'s any inline style
  concerns (if it has none, note that explicitly in Validation).
- A tool-call row shows an icon for its real status (spinner-equivalent
  for running, check for success, x for failure — static icons are
  fine, no animation requirement), the tool name, and the title
  (truncated with tap-to-expand if long) — collapsed by default,
  matching Query 2's "scan the header, expand on demand" pattern.
- Permission cards (Milestone 3's functional `PermissionBoard` UI)
  visually restyle onto the token system: pending state uses the
  `status.warning`/approval-adjacent token, resolved state uses
  `status.success`, error state uses `status.danger` — the card stays
  in place in the timeline through all three states (already true
  functionally since Milestone 3; this item is the re-skin).
- The follow-up compose box (Milestone 3 Item 1) restyles onto the
  token system (input card surface, send button using the token
  system's action color) — same functional behavior, new visual
  treatment.
- Dark mode renders correctly throughout (manual check — this
  screen's automated tests are logic-layer, not visual).

## Test Scenarios

- **Item 1**: a `theme.test.ts` (or colocated) asserting both
  `lightTheme`/`darkTheme` define every token key the other does (no
  mode with a missing token — mirrors web's `token-presence.test.ts`);
  a lightweight grep-style test (or a lint rule, whichever is more
  idiomatic for this codebase's existing test tooling) asserting
  `PairingScreen.tsx` contains no `#[0-9a-fA-F]{3,8}` literal outside
  the theme file itself.
- **Item 2**: unit test for the sort function (pure function extracted,
  not buried in the component) — given task statuses and a set of
  taskIds with a pending-approval hit, attention-needing tasks sort
  first, ties preserve original order; a subscribe/unsubscribe test
  against a fake/harness connection (same style as
  `RelayConnection.test.ts`) confirming `permission.pending` events for
  a task not yet in the pending set add it, and confirming the pending
  set resets to empty at the start of each `load()` call; same
  hardcoded-hex-literal check as Item 1, scoped to `TasksScreen.tsx`.
- **Item 3**: `runTimeline.ts`'s existing tests (already covering
  event→line mapping) extended if the tool-call line gains any new
  renderable field for the collapsed/expand distinction; same
  hardcoded-hex-literal check scoped to `TaskDetailScreen.tsx`; manual
  dark-mode check (no automated visual regression tooling in this
  codebase yet — note as a known gap, not a blocker).
- Full `npx tsc --noEmit && npm test` clean after every item, matching
  every prior milestone's discipline.

## Progress

- [x] Item 1 — token foundation + PairingScreen
- [ ] Item 2 — TasksScreen run-card list
- [ ] Item 3 — TaskDetailScreen + permission cards visual polish
- [ ] Hand off implementation via Paseo (GLM as primary implementer,
      per the user's standing preference — direct `deny` + specific
      `send_agent_prompt` redirect if it shows the "many turns, no
      commits" stall pattern; escalate to Sonnet only if that doesn't
      produce real progress within a few more turns).
- [ ] Independent verification of agent-reported work before merge.
- [ ] (Not started, future scope) `@expo/ui` adoption for native
      controls (buttons/fields/sheets/menus) once the token foundation
      exists to theme it against — deliberately deferred, see
      Decisions.

## Validation

To be filled in as each item lands, mapping back to each Acceptance
Criterion with the specific test or manual check that confirmed it.

### Item 1

- `mobile/src/theme.ts` exports `lightTheme`/`darkTheme` (surface-0..3,
  status/statusDot/diff, the type-role scale, elevation shadow objects,
  spacing/radius/duration) plus a pure `resolveAppTheme(scheme)`. Hex
  values are hand-converted from the plan's oklch literals (grayscale
  achromatic conversion + the full OKLab->linear-sRGB->gamma pipeline for
  the chromatic ones); `foregroundMuted` isn't in the plan's port list
  but is the standard shadcn `muted-foreground` companion
  (`oklch(0.556 0 0)`/`oklch(0.708 0 0)`) to the exact background/
  foreground/muted triad given, so it's derived the same way, not
  invented — noted here since it's the one token value not directly
  handed to this session.
- **Split from the plan's suggested single-file layout:** this
  codebase's vitest has no react-native/Metro transform (no
  screen/component was ever unit-imported before this plan — only
  logic-layer modules). Any file `theme.test.ts` imports transitively
  that does `from 'react-native'` fails to parse (Flow syntax) under
  vitest's plain SSR transform. So `AppThemeProvider`/`useAppTheme` live
  in `mobile/src/theme/ThemeProvider.tsx` (the plan's explicitly-offered
  alternative to colocating), importing `useColorScheme` from
  `react-native`; `theme.ts` itself has zero react-native import and
  stays directly testable.
- `theme.test.ts`: recursive key-path comparison confirms `lightTheme`/
  `darkTheme` define the identical key set (mirrors web's
  `token-presence.test.ts`); `resolveAppTheme` resolves `'dark'` to
  `darkTheme` and `'light'`/`null`/`undefined` to `lightTheme`.
- `app.json`'s `userInterfaceStyle` is `"automatic"`. Confirmed (not
  assumed) that `expo-system-ui` isn't required as an npm dependency for
  this SDK version: `@expo/prebuild-config`'s `withDefaultPlugins.js`
  bundles an *unversioned* copy of expo-system-ui's own
  `withAndroidUserInterfaceStyle`/`withIosUserInterfaceStyle` config
  plugins and applies them unconditionally regardless of whether the
  `expo-system-ui` package itself is installed — so the native
  light/dark switching this manifest key drives works without adding
  the dependency. (The package would still be needed for *JS-side*
  system-UI APIs like `setBackgroundColorAsync`, which this pass doesn't
  use.)
- `PairingScreen.tsx` rebuilt on `useAppTheme()`:
  `noHardcodedColors.test.ts` (new, mirrors web's
  `no-hardcoded-colors.test.ts`) greps the file for `#[0-9a-fA-F]{3,8}`
  and asserts none — confirmed green, plus a manual `grep` during
  development. Behavior unchanged (same idle/connecting/error states,
  same `RelayConnection.connect` call) — this item touched only
  `StyleSheet` values and added a `theme`/`styles` memo, not any
  handler logic.
- Dark mode: no automated visual check (none exists in this codebase);
  deferred to a manual device/simulator pass alongside Item 3's manual
  dark-mode check, per the plan's own "known gap, not a blocker" framing
  for visual regression tooling.
- `npx tsc --noEmit && npm test`: clean (55 tests passing, up from the
  50-test baseline).
