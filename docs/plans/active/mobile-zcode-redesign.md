# Mobile redesign — ZCode visual language on native `@expo/ui` + Expo Router

Size: L (8) — scope 2, PRs 2 (six, sequential), risk 1, decisions 1 (ADR-0036), verification 2 (390 px light+dark smoke incl. transition
frames; no real device).

Redesigns the Expo app (`mobile/`) with ZCode's tokens/type/radius/density (ZCode = visual north star, `refs/zcode`) using
`@expo/ui` native components (SwiftUI / Jetpack Compose) wherever one exists and RN only where it does not. **Presentation layer
only**: relay pairing, device identity, daemon password, queue, stop and needs-continue behave exactly as before. Research and the
component-by-component mapping: `docs/research/local/mobile-zcode-design.md` (local, gitignored; Revision 2). Navigation decision:
[ADR-0036](../../decisions/0036-mobile-expo-router-native-stack.md).

Rulebook (cite in code comments and PR bodies where a rule drives a choice): `expo-design-system`, `expo-native-ui`, `expo-ui`,
`expo-animation`, `expo-router`, `vercel-react-native-skills` (`.claude/skills/*/SKILL.md`). `sleek-design-mobile-apps` is not used to
generate UI (external, paid).

## Scope and PRs

Six PRs, each branched off `origin/develop` **after the previous one merges** (the user merges; no stacking). Mobile-only write scope
(`mobile/**`, this plan, ADR-0036, `docs/design.md` mobile notes). Unchanged throughout: `mobile/src/relay/**`, `api.ts`, `chatQueue.ts`,
`useChatQueue.ts`, `runFollower.ts`, `followUpPrompt.ts`, `permissionRequests.ts`, `stopControl.ts`, `queueRows.ts`, `taskSections.ts`,
`taskAttention.ts`, `runFooter.ts`, `adoptedPrompt.ts`, device identity / secure store.

| PR | Outcome |
|---|---|
| **R0** | Expo Router native stack, behaviour-identical (ADR-0036): `app/` routes, `ConnectionProvider`, `routeGate`, native headers, route params, smoke updated |
| **M1** | Foundations: Zai tokens in `theme.ts`, type roles, radius/`boxShadow`, `withAlpha`; `UiHost` `seedColor`/`colorScheme`; `AppIcon` registry (+ drift test); deps; smoke 390×844 @2x + seeded task-list frame |
| **M2** | Pairing + connection status cards + password prompt (`FieldGroup`, `@expo/ui` Buttons, banner) |
| **M3** | Task list (`FlatList` sections, rows, badges, empty/error, connection `BottomSheet` + `List`) |
| **M4** | Transcript (messages, markdown, assistant-chunk coalescing, tool rows, permission cards, needs-continue, auto-scroll) |
| **M5** | Composer shell, queue chips, Stop / Send / Send now (three labelled `@expo/ui` Buttons), keyboard-controller, haptics, transition frames |

## Acceptance Criteria

### R0 — Expo Router
1. `mobile/package.json` `main` is `expo-router/entry`; `index.ts` and `App.tsx` are gone; routes exist only under `mobile/app/` (`_layout.tsx`,
   `pairing.tsx`, `connection.tsx`, `index.tsx`, `task/[id].tsx`) and contain no logic beyond wiring; screens/hooks stay in `mobile/src/`.
2. `ConnectionProvider` holds the only live `RelayConnection | null` and is the only code that calls `conn.close()` / `resetDeviceIdentity()`
   for the app session; `disconnect()` closes the connection and returns to pairing; `resetIdentity()` returns to pairing only if
   `resetDeviceIdentity()` resolved (same rule as the old `handleResetIdentity`).
3. `routeGate(hasConn, state)` returns `pairing` when there is no connection, `status` for `password`/`locked`/`pending`/`revoked`/`failed`, and
   `app` for everything else (including `connecting`, `connected`, `reconnecting`, `closed`). Guards use only this function.
4. The screens a status/pairing flip hides are unmounted (their history removed); pairing → approved hand-off lands on the task list.
5. Task detail receives `id` and `title` as params; a missing title shows `Task <id>`; a non-numeric `id` shows the existing "couldn't load" state
   rather than crashing; the header title is the task title and the header back button returns to the list (no in-screen "← Back").
6. Native stack headers carry the screen titles (`smind pairing`, the workspace title, the task title, the status card title); the task list
   header has a **Disconnect** action; no screen hard-codes `paddingTop: spacing[16]` any more; header/background colours come from tokens.
7. The reconnecting banner still shows during `connecting`/`reconnecting` (inside the list and detail screens).
8. **No behaviour regression:** pairing (incl. pending/revoked/outdated/failed cards and their actions), device identity reset, daemon password
   (required/incorrect/locked, submit), task list + pull-to-refresh, run transcript, permission respond, needs-continue, queued prompts + cancel,
   Stop, Send, Send now all work as before: `npm test`, `npm run typecheck`, `npm run test:integration` and `npm run smoke:web:full` pass.
9. ADR-0036 and this plan are in the PR. `docs/decisions/` numbering has no gap or clash.
10. `smoke-web.mjs` runs at 390×844 @2x by default (`SMOKE_VIEWPORT=WxH[@dpr]` overrides) and its static server port is `SMOKE_PORT` (default 8931), so concurrent
    sessions do not collide; every smoke selector that clicked the old in-screen "Back" / title now drives the native header.

### M1 — Foundations
1. `theme.ts` light/dark values equal the Zai values in `docs/research/local/mobile-zcode-design.md` §3.1 (background `#f8f8f8`/`#161616`, card
   `#ffffff`/`#2b2b2b`, primary `#000000`/`#ffffff`, destructive `#e03131`/`#ff5c5c`, success, warning, `status.running` = warning, border/subtle/
   subtlest alphas); no teal remains. `withAlpha(hex, a)` is pure and exported. Type roles are the `text-ui-*` set (14/12/10 + 18/16 headings).
2. Radius scale is `{sm 4, md 6, lg 8, xl 12, '2xl' 16, full}`; elevation is `boxShadow` strings; `borderCurve: 'continuous'` on rounded containers.
3. `UiHost` passes `seedColor={theme.primary}` and `colorScheme={theme.mode}`; every existing `@expo/ui` `Button` is therefore neutral (no iOS-blue) on web.
4. `AppIcon` is the only icon entry point; every icon name is in one registry (iOS SF Symbol, Android material-symbols asset, web text glyph); a drift test
   fails if a registry entry lacks a platform value or an `AppIcon name=` literal is not in the registry.
5. `noHardcodedColors` and `theme.test.ts` pass with the new tokens (AA contrast for foreground/subtle on background and card; `subtlest` placeholder-only).
6. `smoke-web.mjs` captures a seeded task-list frame (the viewport default landed in R0).

### M2 — Pairing and connection
1. Pairing is a `FieldGroup`/`FieldSection` form: URL `TextInput`, `Connect` (`@expo/ui` Button filled), certificate help in the section footer; copy from
   `connectionStatus.ts` is unchanged.
2. Status cards (pending, revoked, failed, outdated, password required/incorrect/locked) share one `StatusCard`: neutral border, semantic dot, selectable
   command block, `@expo/ui` Buttons with the same labels and actions as today; password prompt submit/lock behaviour unchanged.
3. Smoke frames light+dark: pairing, pending, revoked, outdated, password-required/incorrect/locked, plus the Connect → card transition.

### M3 — Task list
1. Task list is a flattened `FlatList` (section headers + rows, `getItemType`, memoised primitive-prop rows, stable callbacks, `RefreshControl`);
   `taskSections.ts` ordering unchanged; attention rows show a "Needs approval" badge; status = leading dot/spinner + a status word (never colour alone).
2. Empty/error states are `EmptyState` + `@expo/ui` Button; the connection `BottomSheet` (`List`/`ListItem`: status, device label; Disconnect) works on web.
3. Smoke frames: seeded list (light/dark), empty, error, sheet open/closed (transition frames).

### M4 — Transcript
1. User bubble / unboxed assistant markdown / tool row / meta divider / permission card / needs-continue banner; consecutive assistant text lines coalesce;
   basic markdown (bold, italic, inline code, links, lists, fenced code); text is `selectable`.
2. Auto-scroll sticks to the bottom while live unless the user scrolled up; a "Jump to latest" control appears when not at the bottom.
3. `runTimeline`, `permissionRequests`, `runFooter` tests unchanged and green; new tests cover coalescing, markdown, stick-to-bottom state.

### M5 — Composer, queue, stop
1. Composer shell (2xl card): multiline `@expo/ui` `TextInput`, then a row of three labelled native Buttons below it, right-aligned: **Send now** (`text`, only when a
   run is live and there is text), **Stop** (`outlined`, only while live; `Stopping…` disabled), **Send** (`filled`, disabled without text) — same labels, same calls
   (`whenBusy: 'queue' | 'interrupt'`, `requestStop`).
2. Queue chips keep today's per-row `accessibilityLabel` and Cancel; the composer follows the keyboard (`react-native-keyboard-controller`) and the home indicator;
   haptics on send / permission respond / stop (iOS only, one per action).
3. `integration.queue`, `integration.stop`, `integration.devices`, `integration.password` and the smoke queue/stop/continue frames pass; transition frames cover the
   queue chip in/out and Stop → Stopping → idle.

## Test Scenarios

Vitest has no React Native transform (screens are not unit-importable), so every behavioural rule lives in a pure module with unit tests; screens are covered by the
web smoke (`smoke:web:full`, Playwright, 390 px) and the node integration suites.

- R0: `gate.test.ts` — `routeGate` truth table (no conn → pairing; password/locked/pending/revoked/failed → status; connecting/connected/reconnecting/closed → app).
  `routes.test.ts` — `taskHref`/`parseTaskParams` (numeric id, title present/absent, `id="abc"` → invalid, array params take the first). `connectionActions.test.ts` —
  the pure parts of `ConnectionProvider` (disconnect closes once; resetIdentity failure keeps the connection). Smoke (390 px): full device lifecycle with header back
  button, Disconnect in the header, the status screen for revoked/password, queue/stop/continue unchanged.
- M1: `theme.test.ts` (token values, contrast, completeness light/dark), `withAlpha.test.ts`, `icons.test.ts` (drift), `noHardcodedColors.test.ts`.
- M2: `statusCardView.test.ts` (state → card view incl. every action label), smoke frames.
- M3: `taskListItems.test.ts` (sections → flat items, item types, keys stable), `taskRowView.test.ts` (status → dot/word/badge), smoke frames.
- M4: `coalesceMessages.test.ts`, `markdown.test.ts`, `stickToBottom.test.ts`, smoke frames incl. permission card and needs-continue.
- M5: `composerActions.test.ts` (live × canSend × stopping → visible/disabled/label for the three buttons), smoke queue/stop frames.

## Decisions

- ADR-0036: Expo Router native stack (user 2026-10-20). Navigation guards by connection state (`routeGate`).
- Zai neutral palette; running = warning; 14 px base; system font; grouped task-list card (D6 as recommended); theme picker deferred; basic markdown and auto-scroll
  included (user 2026-10-20).
- Three labelled native Buttons in the composer (no icon-only buttons: `@expo/ui` Buttons have no accessibility label).
- Icons: `@expo/ui` `Icon` through one registry, chrome only, text-glyph web fallback, drift test.
- Android exact-colour `PrimaryButton.android.tsx` allowed; default is `seedColor` (Material tonal).
- Sleek is not used.

## Progress

- 2026-10-20 — Research and proposal done (local doc); user decisions received.
- 2026-10-20 — **R-1** (Expo SDK 57 → 58) split out and merged first (#430): the SDK 58 router rework made writing R0 against 57 pointless. `vitest.config.mjs` + `tsconfig.vitest.json` because SDK 58's `expo` package has no `./tsconfig.base` export.
- 2026-10-20 — **R0** (Expo Router, ADR-0036): implemented by a Peer on SDK 57, rebased by cherry-pick onto develop after #430/#431 (ADR-0035 Go core), dependencies reinstalled on SDK 58. Independent read-only review found: stale task list under a pushed task (fixed: `useFocusEffect` reload), banner missing while loading/error (fixed), weak `assertTasks` wait (fixed), stale-closure connection ref (fixed with `replaceConnection`/`dropConnection`), test gaps (added). Accepted as-is: the `title` route param is a display hint only (deep links can set any text); header title repeats the card title on the revoked screen (M2 redesigns it).
- Verification constraint found: develop's live-harness suites (`test:integration`) are skipped until the mobile E2EE v2 change lands (ADR-0035 gate), and the mobile v1 client cannot talk to the v2 Go harness, so `smoke:web:full` is run from an overlay: Go tree of 661bb08f (pre-v2) + this branch's `mobile/` (`git worktree add --detach /tmp/r0-v1 661bb08f`, `rsync mobile/`). Every M-PR needs the same until the mobile E2EE v2 PR merges.

## Validation

### R0
- `npx tsc --noEmit` clean; `npm test` 30 files, 241 passed + 4 skipped (the 4 are the ADR-0035 gate); `npx expo-doctor` 20/20; `npx expo install --check` up to date.
- `npm run test:integration`: all live-harness suites skipped by the ADR-0035 gate (not a regression of this PR).
- `smoke:web:full` (390×844 @2x, Playwright, overlay above) passed: pairing → pending → approved list → revoked, outdated daemon, needs-continue, queue (cancel, delivery), stop, password (required/incorrect/locked/ok), plus new stages: push/pop transition frames (`push-task-t{0,90,220}-{light,dark}`, `pop-task-…`; web transitions finish inside 90 ms and no frame is blank or half-drawn), "task created meanwhile appears after returning" (proves the refocus reload).
- Screenshots reviewed (light + dark): native header with back arrow + task title, no 64 px gap, Disconnect centred in the list header, status screen, queue/stop screens unchanged.
- **Unverified:** everything native (iOS/Android header, swipe-back, hardware back, SwiftUI/Compose rendering of the header `Host`), Expo Go for SDK 58 does not exist yet and there is no simulator here; live-harness suites against the E2EE v2 daemon (gated).
