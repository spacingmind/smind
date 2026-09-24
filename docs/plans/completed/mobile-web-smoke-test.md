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

- [x] Item 1 — web build + PairingScreen render
- [x] Item 2 — full flow against the relay harness (best-effort)

## Validation

### Item 1 (2026-09-24)

- Deps installed via `npx expo install react-native-web @expo/metro-runtime`
  (resolved to `react-native-web@~0.21.2`, `@expo/metro-runtime@~57.0.16`).
  Playwright pinned to `1.63.0` as a devDependency — that is the release
  whose bundled Chromium is build 1243, matching the pre-cached
  `~/.cache/ms-playwright/chromium-1243`, so no browser download was needed.
- `npx expo export --platform web` succeeds (368 modules, 744KB bundle);
  no fallback to `expo start --web` was necessary.
- `npm run smoke:web` (`mobile/scripts/smoke-web.mjs`): builds the export,
  serves `dist/` from a tiny static server, opens it in headless Chromium
  in both emulated color schemes. Asserts the "smind pairing" title, a
  textarea/input for the pairing URL, the Connect button, and zero
  `pageerror`/`console.error` events during load. Exit 0;
  `pairing-light.png` + `pairing-dark.png` written to `mobile/.smoke/`
  (gitignored). Negative check: breaking the expected title made the
  script exit 1 (locator timeout), then reverted.
- `npx tsc --noEmit` clean; `npm test` 13 files / 61 tests green.
- Limitation (accepted per Context): web proves layout, theme tokens —
  the two screenshots visibly differ light vs dark background/foreground —
  `@expo/ui` components rendering at all on `react-native-web`, and the
  JS flows. It does not verify SwiftUI/Compose-specific rendering on
  native.
- Real bug found and fixed (PR #183 regression): the first run showed
  the app squeezed into a ~347px corner panel — not cosmetic, not
  web-only. Root cause: App.tsx wrapped the whole RN app in `@expo/ui`'s
  root `<Host>`; on web Host renders a plain View with no flex, and on
  native RN children inside a Host need `RNHostView`
  (.agents/skills/expo-ui/references/swift-ui.md), so native rendering
  was at risk too. Fix: removed the root Host, wrapped each @expo/ui
  Button/TextInput subtree in its own `<Host matchContents>` via the new
  `src/ui/UiHost.tsx` (matchContents implies alignSelf flex-start on
  web, so row layouts pass a restoring style: stretch for the two text
  inputs, center for Retry, flex-end for Send). Verified: rerun
  screenshots fill the full viewport in both schemes (dark background
  edge to edge), and the smoke script gained regression assertions for
  root-width fill and edge-to-edge background. `tsc` + `npm test` still
  61/61. Also recorded in mobile-expo-ui-adoption.md's Validation.

### Item 2 (2026-09-24)

`npm run smoke:web:full` builds the harness (`go build ...
internal/relay/bridge/harness`), spawns it, admits + E2EE-handshakes
once, drives Connect, and screenshots `tasks-light.png` /
`tasks-dark.png`. Confirmed green on 2 consecutive runs, no leaked
`relayharness`/Chromium processes afterward. Three real bugs found and
fixed along the way — none were the browser TLS problem the WIP
(`mobile/src/relay/grpcweb.ts`) assumed:

1. **`grpcweb.ts`'s `x-grpc-web: 1` header claim: verified true, kept.**
   Confirmed against `internal/relay/server/grpcweb.go` and the vendored
   `traefik/grpc-web@v0.16.0` source
   (`go/grpcweb/wrapper.go:IsAcceptableGrpcCorsRequest`): the wrapper only
   treats an `OPTIONS` request as an acceptable CORS preflight if
   `Access-Control-Request-Headers` contains `x-grpc-web`; grpc-web's
   content-type (`application/grpc-web+proto`) always forces a browser
   preflight regardless of custom headers, so without this header the
   preflight falls through to the raw `*grpc.Server` (which doesn't
   answer `OPTIONS`), and the browser blocks the real request with no
   `Access-Control-Allow-Origin`. The relay's own `WithOriginFunc`/
   `WithWebsocketOriginFunc` already allow every origin (by design — see
   grpcweb.go's doc comment: admission is the real access control, CORS
   here is not a security boundary), so this header addition is purely a
   protocol-conformance fix, not a security loosening.
2. **The probe's "TLS failure" was a CORS preflight rejection, not TLS.**
   Reproduced directly: a fetch identical to grpcweb.ts's but with the
   `x-grpc-web` header genuinely removed (not just renamed — a header
   named `x-grpc-web-DISABLED` still matches the wrapper's
   `strings.Contains(..., "x-grpc-web")` check, which is why an earlier
   sanity check looked like it still passed) fails in Chromium with
   `[console] error Access to fetch ... has been blocked by CORS policy:
   Response to preflight request doesn't pass access control check` —
   not a certificate error. `ignoreHTTPSErrors: true` on the Playwright
   context was working correctly the whole time. `probe-tls.mjs` is
   deleted; its finding is preserved here and in grpcweb.ts's comment.
3. **A second connect() against the same harness collides on session
   identity.** `RelayConnection.connect()`'s `DEFAULT_SESSION_ID`/
   `DEFAULT_DEVICE_ID` (`mobile/src/relay/RelayConnection.ts`) are fixed
   constants (Milestone 1's single-device assumption). The original
   smoke-web.mjs opened a fresh browser context per color scheme and
   called `pairAndAssertTasks` in each, i.e. two independent
   admit+handshake attempts against one harness process. The daemon-side
   bridge (`internal/relay/bridge/bridge.go`) treats the second one as a
   *resume* of the still-known session rather than a new session; the
   resume fails ("transport already closed cleanly"), it falls back to a
   fresh handshake, but that races the new client's hello and fails with
   `e2ee: protocol violation: expected hello, got frame type 0x02`,
   repeating until the retry backoff gives up. Fixed in the smoke script
   only (no relay/daemon change): Item 2 now runs one admit+handshake and
   gets both screenshots from that single live connection via
   `page.emulateMedia({ colorScheme })` — matches how the real app
   toggles dark mode anyway (`ThemeProvider`'s `useColorScheme` reacts to
   the same `prefers-color-scheme` media query).
4. **`proc.kill()` (SIGTERM) does not reliably terminate the harness.**
   `internal/relay/server.Run` calls `signal.NotifyContext(ctx,
   os.Interrupt, syscall.SIGTERM)` for its own locally-shadowed `ctx` —
   this disables the Go runtime's default SIGTERM-terminates behavior
   process-wide, but the harness's `main()` blocks on a bare `select {}`
   that observes no context at all, and `bridge.Run`'s copy of `ctx` is
   the original, un-cancelled one. Net effect, confirmed by hand: sending
   the harness process SIGTERM cancels only the relay's own listeners
   (closing them), while the bridge inside the same process loops forever
   retrying a connection to that now-closed port, and the OS process
   never exits — left a `relayharness` alive logging reconnect attempts
   for 8+ minutes in one investigation run. Fixed by using
   `proc.kill('SIGKILL')` in smoke-web.mjs's harness cleanup (documented
   inline); confirmed no leaked process after 2 repeat runs. Not fixed in
   the harness/relay itself (out of this item's scope, and
   `mobile/src/relay/__tests__/integration.node.test.ts` has the same
   `harnessProcess?.kill()` pattern) — worth a follow-up if anyone hits
   it elsewhere.

Not attempted: opening a task and screenshotting the task-detail screen.
The harness's `EnrollWorkspace` only creates a relay-level admission
secret, not an actual row in the daemon's own workspace store (a fresh,
empty `store.Open` temp db), so `workspace.list` legitimately returns
`[]` and the app correctly renders `(no workspace)` / "No tasks" (which
is also why the plan's assumed empty-state text, "No workspaces yet",
never appears: `TasksScreen`'s `FlatList` always has an "ungrouped"
section object in its data array, so `ListEmptyComponent` never
renders — fixed the smoke script's ready-state assertion to check for
the `Disconnect` button instead). Creating a real task via the daemon's
own RPCs would need the harness to `git init` + commit a repo
(`workspace.create` requires an existing `.git`) and materialize a `git
worktree add` (`task.create`), all under a `SMIND_HOME` sandboxed away
from the real user's `~/.spacingmind` (worktrees land under
`config.Dir()/worktrees`) — more surface than "simply via the daemon's
existing RPCs" for a best-effort item; not attempted, no relay/daemon
code touched for it.

`npx tsc --noEmit`, `npm test` (61/61), `npm run smoke:web`, and `npm
run smoke:web:full` all green; `go test ./internal/relay/...` green
(one `TestReHelloWithDifferentKeyClosesChannel` panic was seen once
under heavy concurrent load from parallel debugging processes, did not
reproduce across 5 isolated reruns or a clean full-package rerun — a
pre-existing flake, not caused by anything in this session's Go-untouched
diff).
