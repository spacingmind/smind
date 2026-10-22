# Mobile redesign — ZCode visual language on native `@expo/ui` + Expo Router

Size: L (8) — scope 2, PRs 2 (eight), risk 1, decisions 1 (ADR-0036), verification 2 (390 px light+dark smoke incl. transition
frames; no real device). **Status: completed 2026-10-22** — all eight PRs merged; native (device/simulator) verification is the one open item, listed under Follow-ups.

Redesigns the Expo app (`mobile/`) with ZCode's tokens/type/radius/density (ZCode = visual north star, `refs/zcode`) using
`@expo/ui` native components (SwiftUI / Jetpack Compose) wherever one exists and RN only where it does not. **Presentation layer
only**: relay pairing, device identity, daemon password, queue, stop and needs-continue behave exactly as before. Research and the
component-by-component mapping: `docs/research/local/mobile-zcode-design.md` (local, gitignored; Revision 2). Navigation decision:
[ADR-0036](../../decisions/0036-mobile-expo-router-native-stack.md).

Rulebook (cite in code comments and PR bodies where a rule drives a choice): `expo-design-system`, `expo-native-ui`, `expo-ui`,
`expo-animation`, `expo-router`, `vercel-react-native-skills` (`.claude/skills/*/SKILL.md`). `sleek-design-mobile-apps` is not used to
generate UI (external, paid).


## Scope and PRs

Each PR branched off `origin/develop` (no stacking; the user merged). **Order (user, 2026-10-20): R-1 and R0 first, then M1** — it froze the shared
surface (`theme.ts`, `src/ui/*`, `scripts/smoke/lib.mjs`, `package.json`) — **then M2, M3 and M4 in parallel** (three Peers, three worktrees, disjoint write
scopes), **M5 after M4** (same screen). Mobile-only write scope (`mobile/**`, this plan, ADR-0036, one Go harness command). Unchanged throughout:
`mobile/src/relay/**`, `api.ts`, `chatQueue.ts`, `useChatQueue.ts`, `runFollower.ts`, `followUpPrompt.ts`, `permissionRequests.ts`, `stopControl.ts`,
`queueRows.ts`, `taskSections.ts`, `taskAttention.ts`, `runFooter.ts`, `adoptedPrompt.ts`, device identity / secure store.

| PR | Outcome |
|---|---|
| **R-1** #430 | Expo SDK 57 → 58 (its own PR: the SDK 58 router rework made a router PR on 57 pointless) |
| #435 | `npm audit` triage after SDK 58 (27 → 18 findings; the rest are build tooling, no patched version exists) |
| **R0** #436 | Expo Router native stack, behaviour-identical (ADR-0036): `app/` routes, `ConnectionProvider`, `routeGate`, native headers, route params |
| **M1** #449 | Foundations: Zai tokens, `UiHost` tint, UI primitives, `AppIcon` registry, modular smoke harness |
| **M2** #454 | Pairing, connection status cards, daemon-password prompt |
| **M3** #455 | Task list (grouped cards, status slot/word, skeleton/empty/error) + connection sheet + devharness `set-task-status` |
| **M4** #453 | Transcript (bubbles, markdown, tool rows, permission cards, needs-continue, auto-scroll, loading state), composer extracted |
| **M5** #460 | Composer shell, queue strip, Stop / Send / Send now, keyboard dock, haptics, legacy theme keys removed |

## Acceptance Criteria

As built. A criterion that changed during the work says **Deviation** with the decision and its reason; the original wording is in the git history of this file.

### R0 — Expo Router
1. `mobile/package.json` `main` is the entry; `App.tsx` is gone; routes exist only under `mobile/app/` (`_layout.tsx`, `pairing.tsx`, `connection.tsx`, `index.tsx`,
   `task/[id].tsx`) with no logic beyond wiring; screens/hooks stay in `mobile/src/`. **Deviation:** `main` is `index.ts`, which installs the native CSPRNG first
   (#437) and then imports `expo-router/entry` last (Expo Router's documented custom-entry pattern).
2. `ConnectionProvider` holds the only live `RelayConnection | null` and is the only code that closes it or resets the device identity for the session;
   `disconnect()` returns to pairing; `resetIdentity()` returns to pairing only if `resetDeviceIdentity()` resolved. (The connection lives in a ref; `replaceConnection`
   / `dropConnection` are pure and tested.)
3. `routeGate(hasConn, state)`: `pairing` without a connection, `status` for `password`/`locked`/`pending`/`revoked`/`failed`, `app` otherwise. Guards use only it.
4. Screens hidden by a guard flip are unmounted; pairing → approved hand-off lands on the task list.
5. Task detail takes `id` and `title` params (`Task <id>` without a title; a non-numeric id shows the "couldn't load" state); the header carries the task title (from the params
   on the first frame) and the native back button returns to the list.
6. Native headers carry the titles. **Deviation:** the list header's action is `Connection` (opens a sheet with Disconnect, since M3), and the status screen's header is the
   constant `Connection` (since M2; the card carries the specific title). No screen hard-codes the old 64 px top padding; colours come from tokens.
7. The reconnecting banner shows during `connecting`/`reconnecting` in the list and detail screens (also while loading / on error).
8. No behaviour regression (pairing and its cards, identity reset, daemon password, list + refresh, transcript, permission respond, needs-continue, queue + cancel, Stop, Send,
   Send now): unit, typecheck, live integration and the full smoke pass.
9. ADR-0036 and this plan are in the repo; ADR numbering has no gap or clash.
10. The smoke runs at 390×844 @2x by default (`SMOKE_VIEWPORT` overrides), on `SMOKE_PORT`; selectors drive the native header.

### M1 — Foundations
1. `theme.ts` carries the Zai values (background `#f8f8f8`/`#161616`, card `#ffffff`/`#2b2b2b`, primary `#000000`/`#ffffff`, destructive, success, warning,
   `running` = warning); no teal; pure `withAlpha`; type roles are 18/16 headings + 14/13/12/10. **Deviation:** `foregroundSubtle` alpha is 0.65 (light) / 0.64 (dark) instead of
   ZCode's 0.6 (4.04:1 and 4.44:1 at 0.6, below AA for small text); two status tiers are kept (`status` = AA text tier, `statusDot` = Zai fill tier).
2. Radius `{4, 6, 8, 12, 16, full}`; `boxShadow` strings only; `borderCurve: 'continuous'` on rounded containers.
3. `UiHost` passes `seedColor={theme.primary}` and `colorScheme={theme.mode}`: every `@expo/ui` `Button` is neutral (no iOS blue). `TextButton` is the one text-variant
   action (the web renderer paints raw text buttons in a low-contrast grey).
4. `AppIcon` is the only icon entry; one pure registry (SF Symbol / material-symbols asset / web glyph) with a drift test.
5. `theme.test.ts` passes with the new tokens (WCAG AA for foreground, subtle, every status text colour, waiting; `subtlest` is placeholder-only).
6. The smoke is modular: an orchestrator, `scripts/smoke/lib.mjs`, auto-discovered stage files, `--stage`, `SMOKE_OUT`; seeded task-list baseline frames.
7. `noHardcodedColors` scans `src/screens`, `src/ui`, `app` recursively.
8. Shared primitives are token-only: `Banner`/`ReconnectingBanner` (Reanimated on an `Animated.View`, reduced-motion aware), `Card`, `Badge`, `StatusDot`, `EmptyState`,
   `Skeleton`, `useScreenInsets`, `TextButton`.
9. Dependencies for M1–M5 installed once (`expo-haptics`, `react-native-keyboard-controller`, `@expo/material-symbols`, `react-native-safe-area-context`).

### M2 — Pairing and connection
1. Pairing is a scroll view with the URL `TextInput` and a `Connect` filled `@expo/ui` Button in a `Card`, certificate help below; copy unchanged. **Deviation:** not a
   `FieldGroup`/`FieldSection` — the universal web `FieldGroup` is a full-flex `ScrollView` with a hard-coded grey background that breaks the dark theme and the
   keyboard-safe scroll (documented in `PairingScreen.tsx`). Native `FieldGroup` was not evaluated.
2. Status cards and the password prompt share the card anatomy: neutral border, semantic dot, selectable mono command block, `@expo/ui` Buttons (first action filled, the
   rest `TextButton`, a single action outlined), same labels and actions; password submit/lock behaviour unchanged.
3. Smoke frames light + dark: pairing, pending, revoked, outdated, password-required/incorrect/locked, and the Connect → card transitions.

### M3 — Task list
1. A flattened virtualized `FlatList` (headers + rows + empty items; memoised primitive-prop rows; stable callbacks; `RefreshControl`); `taskSections.ts` ordering unchanged;
   a `Needs approval` badge for attention rows; status = dot/spinner + a status word. **Deviation:** no `getItemType` — RN's `FlatList` has no such prop (it is FlashList's).
2. Empty/error states are `EmptyState` + `@expo/ui` Button; the connection `BottomSheet` (status, device label, red Disconnect) works on web. **Deviation:** the rows are plain RN
   views, not `List`/`ListItem` (the universal `ListItem` always renders a focusable `Pressable`), and the sheet is painted with the elevated `overlay` token.
3. Smoke frames: the list with tasks in created/running/done/error/stopped (devharness `set-task-status`), empty, sheet open + its transition frames.

### M4 — Transcript
1. User bubble, unboxed assistant markdown, tool row, meta divider, permission card, needs-continue banner; consecutive assistant chunks coalesce; basic markdown (bold, italic,
   inline code, links limited to `http(s)`/`mailto`, lists, fenced code incl. an unclosed streaming fence); text is `selectable`.
2. Auto-scroll sticks to the bottom while live unless the user scrolled up; a `Jump to latest` control appears otherwise.
2a. No frame of the task screen is an empty page: a run-strip + transcript skeleton while loading, asserted at ~90 ms after the push in light and dark; web async routes are
   off (`asyncRoutes.web: false`) so the first push is not a blank pane before any screen code runs.
2b. The composer + queue region moved verbatim into `src/screens/task/Composer.tsx` (then redesigned by M5).
3. `runTimeline`, `permissionRequests`, `runFooter` tests unchanged and green; new tests for coalescing, markdown, stick-to-bottom, permission button variants.

### M5 — Composer, queue, stop
1. A rounded (2xl) composer shell: the multiline `@expo/ui` `TextInput` at full width, then three labelled native Buttons below it, right-aligned — `Send now` (`TextButton`,
   only live with text), `Stop` (outlined, only live; `Stopping…` disabled), `Send` (filled, disabled without text); same labels, same calls (`whenBusy`, `requestStop`).
2. Queue chips (a floating card strip) keep the per-row `accessibilityLabel` and Cancel; the dock follows the keyboard (`react-native-keyboard-controller`) and the bottom inset; haptics
   on send / stop / permission answers / failed send (iOS only, one per action). **Verified on web only** (see Follow-ups).
3. `integration.queue`, `integration.stop`, `integration.devices`, `integration.password` and the smoke queue/stop/continue frames pass; transition frames cover queue chip in/out
   and the Stop click.
4. The legacy theme keys (`surface[]`, `foregroundMuted`, `ring`, `elevation`, the seven old type roles) are gone; a test asserts it.

## Test Scenarios

Vitest has no React Native transform (screens are not unit-importable), so every behavioural rule lives in a pure module with unit tests; screens are covered by the web smoke
(`smoke:web:full`, Playwright, 390 px, light + dark, with transition frames) and the node integration suites. As built, in `mobile/src/__tests__/`:

- R0: `gate.test.ts`, `routes.test.ts` (incl. `taskHeaderTitle`), `connectionActions.test.ts` (reset-then-disconnect order, `replaceConnection`, `dropConnection`).
- M1: `theme.test.ts` (token values, contrast, completeness, `withAlpha` — there is no separate `withAlpha.test.ts`), `icons.test.ts` (drift), `noHardcodedColors.test.ts`.
- M2: `statusCardView.test.ts`.
- M3: `taskListItems.test.ts`, `taskRowView.test.ts`.
- M4: `coalesceMessages.test.ts`, `markdown.test.ts`, `stickToBottom.test.ts`, `permissionButtons.test.ts`.
- M5: `composerActions.test.ts`, `haptics.test.ts`.
- Smoke stages (`mobile/scripts/smoke/stages/`): `10-pairing`, `20-device-lifecycle`, `25-task-list`, `30-outdated-daemon`, `40-needs-continue`, `45-transcript`, `50-queue-stop`, `60-password`.

## Decisions

- ADR-0036: Expo Router native stack (user 2026-10-20); navigation guards by connection state (`routeGate`). SDK 58 first, as its own PR.
- Zai neutral palette; running = warning; 14 px base; system font; grouped task-list card; theme picker deferred; basic markdown + auto-scroll included.
- Tokens: Zai values; two status tiers (`status` AA text, `statusDot` fill); `foregroundSubtle` alpha 0.65/0.64 for AA.
- `@expo/ui` Buttons are tinted by `UiHost seedColor`/`colorScheme` — no wrapper components; `TextButton` is the single exception (web text-button contrast). Three labelled
  native buttons in the composer (the universal Button has no accessibility label, so no icon-only buttons).
- Icons: `@expo/ui` `Icon` through one registry, chrome only, text-glyph web fallback, drift test.
- Android exact-colour `PrimaryButton.android.tsx` allowed; default is `seedColor` (Material tonal grey on Android).
- Sleek (sleek.design) is not used. Web `FieldGroup` and `ListItem` are not used (see the M2/M3 deviations).

## Progress

- 2026-10-20 — Research and proposal (local doc); user decisions; ADR-0036 and this plan.
- 2026-10-20 — **R-1** #430 (SDK 58; `vitest.config.mjs` + `tsconfig.vitest.json` because the SDK 58 `expo` package has no `./tsconfig.base` export); #435 audit triage.
- 2026-10-20 — **R0** #436, rebased onto SDK 58 and develop (E2EE v2 landed meanwhile: `test:integration` and the smoke run the normal way). An independent read-only review found
  a stale task list under a pushed task, a banner missing while loading, a weak smoke wait, a stale-closure connection ref and test gaps — all fixed.
- 2026-10-22 — **M1** #449 (three Peers: tokens, modular smoke, UI foundation; fixed `Banner` Reanimated props on a plain `View`, dark subtle-text contrast, web text-button contrast;
  a pre-existing race in `integration.queue` that failed CI was fixed in the same PR).
- 2026-10-22 — **M2** #454, **M3** #455, **M4** #453 in parallel (disjoint scopes). Lead review changed: M2 header `Connection`; M3 sheet on the `overlay` token and static rows
  (no focus ring), `assertTasks` ownership; M4 `asyncRoutes.web: false` after the transition frames showed a blank first push.
- 2026-10-22 — **M5** #460: composer, queue strip, haptics; fixed the dock padding overridden by `KeyboardAvoidingView`, the web `TextInput` drop-shadow hairline, a stage 45 race
  (`chosen:` vs `resolved:`), and removed the legacy theme keys.

## Validation

Every PR was verified locally before merge (unit, typecheck, the live Go devharness integration suites, the full 390 px light + dark web smoke with transition frames); screenshots
were reviewed light and dark by the Lead and the Supervisor and are not committed.

| PR | Typecheck / unit | Integration (live harness) | Full smoke |
|---|---|---|---|
| R-1 #430 | clean / 218 | 24 passed, 1 skipped | passed (36 frames, identical modulo random ids to the SDK 57 baseline) |
| R0 #436 | clean / 362 | 25 passed, 1 skipped | passed (50 frames incl. push/pop transitions) |
| M1 #449 | clean / 397 | 25 passed, 1 skipped | passed (52 frames) |
| M2 #454 | clean / 404 | 25 passed, 1 skipped | passed |
| M3 #455 | clean / 425 | 25 passed, 1 skipped (+ `go test` devharness) | passed (68 frames) |
| M4 #453 | clean / 458 | 25 passed, 1 skipped (+ `go test ./internal/taskrunner/...`) | passed (68 frames, first-push assertion) |
| M5 #460 | clean / 486 | 25 passed, 1 skipped | passed twice in a row |

Acceptance: every criterion above is confirmed on web, with the deviations recorded in place. The skipped integration test is the pre-existing "harness has -password" guard.

## Follow-ups (not blocking; native verification is the one open item)

Everything native has been **unverified throughout** — no simulator/emulator on the development machine, and Expo Go for SDK 58 is not released (a development build is required).
A device/simulator pass should check, in this order:
1. The app boots on iOS and Android (dev build); relay pairing, device approval, password prompt, task list, transcript, composer end to end against a real daemon.
2. `UiHost` tint on iOS (SwiftUI buttons black/white) and the Android Material palette from a neutral seed (tonal grey, not pure black/white — add `PrimaryButton.android.tsx` with
   `colors` if exact colours matter).
3. The native header: swipe-back, Android hardware back, large-title/blur behaviour, the `Connection` sheet detents.
4. The composer: `KeyboardAvoidingView` + the bottom inset (a small extra gap when the keyboard is open is possible), haptics (iOS), text-field growth, queue chip fade.
5. `AppIcon` on a device (SF Symbols / XML drawables); `FieldGroup`/`ListItem` on native (rejected only because of their web fallbacks).
6. The `Needs approval` list badge with a live permission request (covered by unit tests only), and the loading skeleton on a genuinely slow load.
7. Later, with the attachments lead (ADR-0038): mobile image downscaling.
