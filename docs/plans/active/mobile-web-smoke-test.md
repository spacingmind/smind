# Mobile app: web smoke test (Expo web + Playwright)

## Context

`mobile-ui-polish.md` and `mobile-expo-ui-adoption.md` both left a manual
light/dark + native-control smoke test outstanding: no iOS/Android
simulator exists on this machine (WSL2, no `adb`/`emulator`). User chose
(2026-09-24) the free substitute: run the app's **web** target and drive
it with headless Chromium via Playwright.

What makes this viable, confirmed on this machine:

- `@expo/ui`'s universal components render on web
  (`.claude/skills/expo-ui/references/universal.md`: Compose on Android,
  SwiftUI on iOS, `react-native-web`/`react-dom` on web).
- `react-dom@19.2.3` is already a dependency (`mobile/package.json`).
  `react-native-web` and `@expo/metro-runtime` are not — Expo's web
  target needs them.
- Playwright browser builds are already cached at
  `~/.cache/ms-playwright/` (`chromium-1243`, `chromium_headless_shell-1243`).
  Pin the `playwright` npm version whose bundled browser is build 1243 to
  reuse the cache; if that proves awkward, letting Playwright download its
  own build is fine (network is available).
- A real relay + daemon bridge can be started with the Go harness at
  `internal/relay/bridge/harness/main.go` — `mobile/src/relay/__tests__/
  integration.node.test.ts` shows exactly how it is spawned and how the
  pairing URL is read from its output. It uses a self-signed TLS cert
  (the node tests set `NODE_TLS_REJECT_UNAUTHORIZED=0`); in Playwright use
  `ignoreHTTPSErrors: true`.

Web is **not** native: it verifies layout, theme tokens (light/dark),
the `@expo/ui` components rendering at all, and the JS flows — not
SwiftUI/Compose-specific rendering. That limit is accepted, and must be
stated in Validation.

## Decisions

- **Verification tooling, not product code.** A script
  `mobile/scripts/smoke-web.mjs` plus an npm script `smoke:web`. It is
  **not** part of `npm test` and not wired into CI (it needs a browser and,
  for Item 2, the Go harness). Screenshots go to a gitignored
  `mobile/.smoke/` directory and are never committed.
- **Deterministic build, not the dev server.** Prefer
  `npx expo export --platform web` + a tiny static server over
  `expo start --web`, so the run doesn't depend on Metro dev-server
  timing. If `export` hits a real blocker, `expo start --web` is an
  acceptable fallback — document why.
- **Install deps with `npx expo install`**, never raw npm, so versions
  match Expo SDK 57 (`mobile/AGENTS.md`: read the v57 docs, APIs changed).
- **Real rendering bugs found are in scope to fix only if small and
  clearly scoped** (e.g. a style that breaks on web, a missing
  `Platform` guard). Anything larger: report it in Validation, don't fix.

## Acceptance Criteria

### Item 1 — web build + PairingScreen render (required)

- `npx expo install react-native-web @expo/metro-runtime` (and anything
  else Expo's web target demands) done; `npx expo export --platform web`
  succeeds.
- `npm run smoke:web` builds/serves the web bundle, opens it in headless
  Chromium, and asserts: the pairing screen renders (its title text, the
  pairing-URL input, the Connect button are present), **no uncaught page
  errors / console errors** during load.
- Screenshots in **both** color schemes (`colorScheme: 'light'` and
  `'dark'` via Playwright emulation) saved to `mobile/.smoke/`; the
  script exits non-zero on any failed assertion.
- `npx tsc --noEmit` and `npm test` still clean (61/61) — adding web deps
  must not break the existing suite.

### Item 2 — full flow against the real relay harness (best-effort)

- The script (or a `--full` mode of it) starts the Go harness, reads the
  pairing URL, pastes it into the pairing input, presses Connect, and
  asserts the task list screen renders; screenshots it (light + dark).
- If the harness workspace has tasks (or one can be created simply via
  the daemon's existing RPCs), open one and screenshot the task detail
  screen too.
- Best-effort like the prior plan's Item 2: if a concrete blocker appears
  (e.g. browser TLS/WebSocket behavior against the harness), document the
  specific blocker in Validation instead of forcing it.

## Test Scenarios

- Item 1: run `npm run smoke:web` → exit 0, two screenshots written;
  deliberately break an assertion once locally to confirm the script
  really exits non-zero (then revert).
- Item 2: run the full mode → pairing succeeds against the harness, task
  list screenshot shows the harness workspace.
- Both: `npx tsc --noEmit`, `npm test` unchanged-green.

## Progress

- [ ] Item 1 — web build + PairingScreen render
- [ ] Item 2 — full flow against the relay harness (best-effort)

## Validation

To be filled in as each item lands.
