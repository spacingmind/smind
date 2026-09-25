# Desktop bundled UI + connection list (ADR-0013, part 1)

## Context

ADR-0013 (`docs/decisions/0013-desktop-bundled-ui.md`, Accepted 2026-09-25) moves the Tauri desktop app from "the webview loads the daemon's web UI" (ADR-0012) to a **bundled** web UI.

The UI is served by a **Rust-side loopback proxy** that reverse-proxies `/api/*` and `/ws` to the selected daemon. There is also a Paseo-style **connection list** and a scoped IPC bridge.

**This plan is part 1.** Scope:
- the bundled UI;
- the loopback proxy with the per-launch secret;
- the connection list for **local + arbitrary-URL** daemons;
- the desktop platform layer in the web UI.

**Out of scope, each getting its own plan later:**
- the relay transport (Rust-side E2EE bridge);
- version skew / "update daemon and restart";
- the app-managed local daemon (WSL/macOS);
- ZCode's desktop chrome (custom titlebar). That is `zcode-visual-parity` P5.

**Hard constraints (user):**
- **No daemon/backend architecture change:** no CORS, no Origin allowlist, no new endpoints. The web UI's same-origin assumptions (`web/packages/ui/src/lib/daemon.ts` fetches `/api/token`; `lib/ws-client.ts` builds the WS URL from `location.host`) are preserved by construction, through the proxy.
- The daemon's `/ws` uses gorilla's default `CheckOrigin`, so the proxy **drops the `Origin` header**. That is the same no-Origin path the Rust `desktop/daemon-client` already uses.

**What exists (on develop after #194):**
- `desktop/src-tauri/src/lib.rs`, which today provides:
  - single-instance;
  - the tray with pending count;
  - hide-on-close;
  - `Ctrl+Shift+S`;
  - the native menu;
  - window state;
  - WinRT/notify-rust notifications with click → navigate;
  - `smind://` deep links;
  - `watch_daemon_and_navigate` + `desktop/ui/offline.html`.
- `desktop/daemon-client`, with its unit-tested pure logic.
- `capabilities/default.json`, which grants nothing.
- The Windows build via `.github/workflows/desktop-windows.yml`.

## Acceptance Criteria

- **AC1: bundled UI.**
  - `tauri build` bundles the `web/packages/ui` production build. Use `beforeBuildCommand` / `beforeDevCommand` and a `frontendDist` pointing at the web build output, or embed the assets in the Rust binary.
  - The daemon's own embedded UI (`internal/server/dist`) stays exactly as it is.
  - The web build gains a **build-time** desktop flag (e.g. `VITE_SMIND_DESKTOP=1`). The daemon-embedded build never sets it.
- **AC2: loopback proxy.**
  - On launch, Rust binds `127.0.0.1:0` (random port).
  - It serves the bundled assets, with SPA fallback to `index.html`.
  - It reverse-proxies `/api/*` (HTTP) and `/ws` (WebSocket upgrade, bidirectional frames) to the **selected** connection's base URL, **dropping `Origin`**.
  - It streams responses without buffering.
  - Switching the selected connection re-points the proxy without restarting the app.
- **AC3: per-launch secret.**
  - A random secret (≥128 bits) is generated per launch.
  - The window opens `http://127.0.0.1:<port>/?k=<secret>`. The proxy validates `k`, sets `smind_desktop=<secret>; HttpOnly; SameSite=Strict; Path=/`, and redirects to `/` with `k` stripped.
  - **Every** other request without a valid cookie is rejected with 403. That covers assets, `/api/*` and `/ws`.
  - The secret is never obtainable from an unauthenticated request.
  - The window's navigation is restricted to the proxy origin: external links open in the OS browser via the opener plugin, from Rust.
- **AC4: connection list (local + URL).**
  - It is persisted as JSON in the app data dir.
  - Entries: `{id, kind: "local" | "url", label, baseUrl}`. There is a built-in `local` entry, `http://127.0.0.1:4648` or `SMIND_DAEMON_URL` if set, which can't be deleted.
  - The last-selected entry is remembered.
  - `url` entries accept `http(s)://host:port` (tunnels, Tailscale); anything else is rejected.
  - Commands: list / add / remove / select / get-current.
- **AC5: scoped IPC bridge.**
  - Only the commands needed for AC4 are exposed (plus, optionally, `open_external(url)` for http(s) links).
  - They are exposed through a capability scoped to the proxy origin **only** (`remote.urls` pattern for `http://127.0.0.1:*`), with no plugin permissions beyond those commands.
  - Every command validates its input in Rust.
  - Document the capability and each command in the plan's Decisions section.
  - If no pattern can pin the exact random port, rely on AC3's cookie gate plus a Rust-side check that the invoking webview's current URL is the proxy origin. Document which approach you used.
- **AC6: web UI desktop platform layer.**
  - Add `web/packages/ui/src/lib/platform.ts` as a single `isDesktop` / `desktop` API surface, built on the build-time flag and `@tauri-apps/api` imported dynamically only in desktop builds.
  - Components never touch `window.__TAURI__` directly.
  - A **host picker**, shown only in desktop builds, somewhere sensible: a sidebar header dropdown, or Settings → Connections, or both. It lists connections, shows which one is current and whether it is reachable, and supports add URL / remove / switch. Switching reloads the UI against the new daemon.
  - When the current daemon is unreachable, the UI shows a clear "can't reach <label> (<url>)" state with retry and "switch connection". This replaces `offline.html` for the bundled flow.
  - The web build without the flag is unchanged. Existing web tests stay green.
- **AC7: Rust-side features follow the selected connection.** The daemon-client (tray pending count, notifications, `permission.pending`) reconnects to whichever connection is selected. Notification click and deep links navigate **within the proxy origin**: `#/workspace/…/task/…` routes on `http://127.0.0.1:<port>/`.
- **AC8: no regressions.**
  - `cargo test` (daemon-client + src-tauri) passes.
  - Web tests and typecheck pass; `task test` and `task lint` pass.
  - The `desktop-windows` workflow is green and builds NSIS + MSI.
  - No daemon (Go) changes.

## Test Scenarios

- **Rust unit tests:**
  - secret generation, cookie validation, and constant-time compare;
  - the request gate (no cookie → 403 for asset, `/api/token` and `/ws`; valid cookie → pass; `?k=` exchange → Set-Cookie + redirect);
  - Origin stripped from proxied headers;
  - connection-list persistence round trip, URL validation (reject `file:`, `javascript:`, a missing port if required, garbage), the built-in local entry can't be removed, and the selection is remembered;
  - route building for notification/deep-link navigation against the proxy origin.
- **Rust integration test:** start the proxy against a stub HTTP+WS server (in-process) and assert:
  - `/api/token` passthrough;
  - a WS echo round-trip through the proxy;
  - the upstream sees no `Origin`;
  - re-pointing to a second stub works.
- **Web tests:**
  - `platform.ts` is false or no-op in a normal build;
  - the host picker renders only when `isDesktop`, using a mocked desktop API;
  - list / add / remove / switch call the right commands;
  - the unreachable state shows the label, URL and actions.
- **Live (WSLg), recorded in Validation:**
  - the app loads the bundled UI against a local `smind serve`;
  - `curl http://127.0.0.1:<port>/api/token` without the cookie returns 403;
  - adding a second daemon on another port (temp `SMIND_HOME`) and switching to it works;
  - stopping the daemon shows the unreachable state;
  - the tray and notifications follow the selection.
- **Windows manual (user):** the same, against the WSL daemon.

## Decisions

- Proxy implementation: pick a mature Rust HTTP stack (e.g. `axum` + `hyper` + `tokio-tungstenite`, or `hyper` alone) and record the choice here.
- Recording the chosen capability pattern and command list here is mandatory (AC5).

## Progress

- [ ] AC1 bundled UI build
- [ ] AC2 loopback proxy
- [ ] AC3 secret gate
- [ ] AC4 connection list
- [ ] AC5 IPC bridge
- [ ] AC6 web platform layer + host picker + unreachable state
- [ ] AC7 Rust features follow selection
- [ ] AC8 regressions + Windows build

## Validation

To be filled in.
