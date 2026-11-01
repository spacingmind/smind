# Web demo mode (the real UI against a scripted, in-browser daemon)

Size: L (6) — scope 1, PRs 1, risk 1, decisions 2, verification 1

Spec for [ADR-0051](../../decisions/0051-web-demo-mode-and-app-spacingmind-com.md) (Proposed; the direction is the Human's, 2026-11-01). Related, **not built**:
[ADR-0052](../../decisions/0052-hosted-web-app-and-official-relay.md) (a draft for the Human). Owner: Windows Lead C. Two PRs, both based on `develop`, never stacked:
**PR 1** = the two ADRs and this plan (docs only); **PR 2** = demo mode (code). Out of scope until the Human's infra steps (ADR-0051 section 9): any deploy workflow,
wrangler config, DNS, and the landing link (landing PR #34 belongs to another Lead). Nothing under `mobile/` or the landing repo is touched.

## Acceptance Criteria

**Build and isolation**

1. `bun run --filter @smind/ui build:demo` produces `web/packages/ui/dist-demo/` from a clean checkout with no daemon, no Go toolchain and no network at build or run time.
   `dist-demo` is gitignored and is not under `internal/server/dist` or `desktop/ui/app`.
2. `bun run --filter @smind/ui preview:demo` serves `dist-demo` on `127.0.0.1:4173` only (`--strictPort`), and nothing else listens.
3. The default production build (`build`) and the desktop build (`build:desktop`) contain **no** demo code: the string `Demo — not connected` and the `src/demo` chunk names are absent from their output.
4. `dist-demo` contains none of the forbidden strings: `/api/token`, `/api/login`, `new WebSocket(`, a `ws://` or `wss://` URL, a `http(s)://` URL to `localhost`/`127.0.0.1`/`[::1]`, `relay.spacingmind`, `smind.bearer`, `grpc`, `/healthz`
   (the two form placeholders `http://localhost:8080` and `http://localhost:54545/callback` are the only allow-list). `scripts/check-demo-build.mjs` enforces this and exits non-zero otherwise.
5. `dist-demo/index.html` carries a CSP `<meta>` with `connect-src 'none'`, `default-src 'none'`, no `'unsafe-eval'`, no `'unsafe-inline'` in `script-src`, and a `_headers` file carries the same policy plus `frame-ancestors 'none'`,
   `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`. The sha256 in `script-src` equals the hash of the inline theme-bootstrap script actually in `index.html`.

**Plug-in**

6. Demo mode runs the **real** `WsClient` over a `DemoSocket` implementing `SocketLike`; `App` receives `connect={connectDemo}`. `App.tsx` has no demo connect branch; the diff outside `src/demo/` is limited to
   `main.tsx`, `lib/ws-client.ts`/`lib/daemon.ts` (`__SMIND_DEMO__` guards), the one connection-label string, `vite.config.ts`, `package.json`, `.gitignore`.
7. Every method the UI calls has a demo rule (`read`, `scripted`, `local` or `refused`); an unlisted method answers an `RpcError` with code `demo_unsupported`, an unlisted write is `refused` with `demo_read_only` and the message "Read-only in the demo."
8. State is in memory per page load; reload resets it; no token or session is ever written; the only storage written is the UI's own preference keys.

**Scenario ("tour")**

9. **A run streaming tool calls:** the landing task shows a run in progress on first paint: thinking, assistant text, at least three `tool_call` cards (Read, Edit, Bash) and a `usage` event, each rendered by the production timeline.
10. **A permission request the visitor can Allow or Deny:** the run reaches a `permission_request` with a write/edit detail and diff preview. Nothing advances it except `run.respondPermission`. **Allow** completes the edit, runs the tests and finishes (`done`);
    **Deny** makes the agent acknowledge, propose a plan and finish. Both branches update the inbox row.
11. **A diff tab:** the task's Diff tab renders a real-looking unified diff of at least three files through the production diff viewer; allowing the permission adds the file the request was about.
12. **A terminal tab:** the Terminal tab replays canned `go test` output with recorded delays, ends at a prompt, echoes typed characters locally and answers Enter with one dim line saying nothing runs here; no input is executed or sent anywhere.
13. **The inbox:** `inbox.list` shows at least four items (a pending permission linked to the live run, a validator verdict, a failed run, a stalled-task notice); `seen`, `snooze` and `resolve` update the list and the sidebar badge.
14. **Usage:** the usage views render fixtures (an account with 5h and weekly windows, a month of spend by day and by model).
15. **Sending a prompt:** a typed prompt appears as the user's message and the canned reply streams; it works repeatedly; the text goes nowhere else.
16. Scenario data is synthetic: no real username, home path, email, token-shaped string or the repo owner's paths (a test scans it); every timestamp is relative to `clock.now`.

**Disabled, faked and banner**

17. Settings pages render fixtures read-only; every write shows "Read-only in the demo."; there is no login screen and no way to enter a daemon address.
18. `DemoBanner` reads "Demo — not connected to a daemon. Scripted data; nothing you type leaves your browser." with an "Install smind" link to `https://spacingmind.com`; it has `role="status"`, is **not dismissible**, is visible at 1280 and 390 px in light and dark without overlapping the shell,
    and the sidebar connection label reads "Demo (scripted)".
19. No visual fork: no demo-only component other than the banner, no demo-only styling beyond layout offset by the banner's height; the existing `no-hardcoded-colors` and token tests pass unchanged.

**Size and network**

20. Budget (checked by `check-demo-build.mjs`; numbers live in that script): demo code ≤ 60 KB gzip in a lazily imported chunk; initial JS ≤ 1,150,000 B gzip (measured production baseline 1,067,437 B gzip on `origin/develop` `5e09caf1`); initial CSS ≤ 35 KB gzip; no single demo asset > 30 KB gzip.
    The measured numbers of this build are recorded under Validation, and the budget is revisited there if a number is wrong.
21. In the browser the demo makes **zero** WebSocket connections and **zero** requests other than same-origin static assets; an injected `fetch("/api/token")` and `new WebSocket("ws://127.0.0.1:1")` are blocked by the CSP.

**Process**

22. `go build`, `go test` for packages touched (none expected), the web unit suite, `tsc -b`, and the repo linters pass; no lockfile churn; no build artifact committed.
23. Frames (light and dark at 1280 and 390) are written to `%USERPROFILE%\.smind\dogfood\web-demo\frames\`, not committed.

## Test Scenarios

Named up front. Vitest unless stated; files under `web/packages/ui/src/demo/` unless stated. IDs are referenced in Validation.

**Dispatcher and plug-in** (`demo-socket.test.ts`)

- `D-01 connectDemo resolves a real WsClient and daemon.info answers the demo daemon`
- `D-02 an unlisted method answers RpcError demo_unsupported; a write answers demo_read_only with the read-only message`
- `D-03 coverage guard: every method string the UI passes to call/callStream has a rule` (static scan of `src/**` excluding tests; the writes the demo refuses are listed by name in `REFUSED_METHODS`)
- `D-04 a script plays its steps in order on an injected clock: events reach callStream's onEvent, then the terminal result`
- `D-05 an ask step pauses until run.respondPermission; allow runs the allow branch, deny the deny branch; an unknown requestId is an error; an unanswered ask stays paused`
- `D-06 task.cancel and run.stop abort the matching script; nothing is emitted afterwards`
- `D-07 prefers-reduced-motion collapses step delays`
- `D-08 notifications carry a per-connection monotonic seq and respect events.subscribe topics`
- `D-09 sending a prompt streams the canned reply and lists the run with the chat's runs; twice in a row works; the text is only ever held in session state`
- `D-10 terminal replay: attach emits base64 data events in order, resize is a no-op, write echoes locally, Enter prints the dim demo line, nothing is executed`
- `D-11 inbox seen/snooze/resolve mutate the in-memory list and push the inbox notification`
- `D-12 a second connectDemo (reconnect) shares the page-load session state`
- `D-13 refused rules never mutate state`

**Scenario hygiene** (`scenario.test.ts`)

- `S-01 the scenario type-checks against lib/types and every task/run/chat id referenced exists`
- `S-02 no real paths, emails, token-shaped strings or owner names in the scenario`
- `S-03 every timestamp derives from clock.now`
- `S-04 the permission request's detail and diff parse in the production permission card helpers`

**Banner and wiring** (`demo-banner.test.tsx`, `main-demo.test.tsx`)

- `B-01 the banner renders the exact text, role=status, the install link, no dismiss control`
- `B-02 App renders against connectDemo: the sidebar lists the scenario workspaces and the connection label is "Demo (scripted)"`
- `B-03 the production entry (no __SMIND_DEMO__) renders no banner and never imports src/demo`

**Build guards** (`web/packages/ui/scripts/check-demo-build.test.mjs`, `node --test`)

- `G-01 check-demo-build fails when dist-demo contains /api/token, new WebSocket(, ws://, localhost or relay.spacingmind`
- `G-02 check-demo-build fails over the gzip budget for demo code, for initial JS, for initial CSS`
- `G-03 check-demo-build passes on a clean synthetic dist and honours the per-file allow-list`
- `G-04 the CSP script-src hash equals sha256 of the inline bootstrap script in index.html, and a changed script without a new hash fails`
- `G-05 the production build output contains no "Demo — not connected" and no src/demo chunk` (script reads a built dist given as an argument; CI runs it after `build`)

**Playwright smoke** (`web/e2e/specs-demo/demo.spec.ts`, config `web/e2e/playwright.demo.config.ts`; webServer = `preview:demo` on `127.0.0.1:4173`; projects `light-1280`, `dark-390`, `E2E_FULL=1` adds the other two)

- `DM-01 loads with the banner and the sidebar tour; zero websockets; only same-origin static requests`
- `DM-02 the live task streams tool calls and reaches the permission card; Allow finishes the run and adds the diff file`
- `DM-03 Deny finishes the run on the deny branch`
- `DM-04 Diff tab renders at least three files`
- `DM-05 Terminal tab shows the canned output and the prompt`
- `DM-06 Inbox shows the items; resolving one lowers the badge`
- `DM-07 Usage view renders`
- `DM-08 sending a prompt streams the canned reply`
- `DM-09 a settings write shows the read-only message`
- `DM-10 banner is visible and does not overlap the shell at 390 px`
- `DM-11 the CSP blocks an injected fetch("/api/token") and new WebSocket("ws://127.0.0.1:1")`

## Decisions

- **ADR-0051** (Proposed): the socket seam (`DemoSocket` under the real `WsClient`), typed scenario data, banner and read-only policy, `vite build --mode demo` → `dist-demo`, the CSP, the budget, hosting options with the recommendation of a Cloudflare Worker static-assets project on `app.spacingmind.com`, and the Human's infra steps.
- **ADR-0052** (Proposed, draft): not built; the Human decides H1–H14.
- Lead defaults, Human may veto: banner text and link target; non-dismissible banner; `demo_read_only` wording; budget numbers; no analytics; the scenario's content ("tour": one live task plus finished and failed ones).
- Heavy-library code splitting (xterm, CodeMirror, diff2html, pdf.js) in the **product** UI is out of scope here; it is the follow-up that would cut the demo's initial load.
- No new dependency is added (the demo uses React, the existing `WsClient`, Vite and vitest; the CSP hash plugin is ~30 lines of Node).

## Progress

- 2026-11-01: ADR-0051, ADR-0052 and this plan written (PR 1, docs only). Baseline production bundle measured: one 3,539,994 B JS chunk (1,067,437 B gzip), 166,378 B CSS (27,171 B gzip).
- Not started: PR 2 (demo mode implementation, tests, Playwright smoke, size check, frames).

## Validation

*(Filled in by PR 2: for each acceptance criterion, the test ID or manual check that confirmed it, the measured bundle numbers, the commands run and their results.)*
