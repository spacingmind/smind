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

- **Proxy implementation**: `axum` 0.8 (routing, static SPA fallback, `/api/*` any-method handler, `/ws` `WebSocketUpgrade`) on top of `reqwest` (outbound HTTP to the selected daemon, already a `daemon-client` dependency for token/healthz) and `tokio-tungstenite` (outbound WS, already used by `client.rs`). Lives in `smind-daemon-client::proxy` (not `src-tauri`), mirroring ADR-0012's rationale for the rest of this crate: it needs to run and be tested without webkit2gtk. Static assets are served through an `AssetSource` trait (`get(path) -> Option<(Vec<u8>, content_type)>`) so tests can inject an in-memory fake; the real embedding (via `rust-embed`) lives in `src-tauri`, which is the only place that needs to know how the bundled UI's bytes got into the binary.
  - `/api/*` is streamed both ways (`reqwest::Body::wrap_stream` in, `axum::body::Body::from_stream` out), never buffered whole.
  - `/ws` is bridged frame-by-frame: axum's own `Message` and this crate's `tokio-tungstenite` dependency resolve to independently-versioned `tungstenite` crates (axum vendors 0.29, this crate uses 0.30), so axum's private `into_tungstenite`/`from_tungstenite` aren't reusable; `proxy::server` hand-converts each variant instead (Text/Binary/Ping/Pong via `.as_str()`/`Bytes` clones, Close collapsed to a bare close with no code/reason since the two `CloseFrame` types don't unify either — a bare close still terminates the connection correctly).
  - `Origin`, `Host`, `Connection` and `Cookie` are stripped from every proxied HTTP request; the outbound WS handshake is built from the target URL alone (no headers copied from the inbound upgrade at all), so it's Origin-less by construction, the same no-Origin path `daemon-client::client` already relies on.
  - Verified live in the Rust integration test (`tests/proxy_integration.rs`): token passthrough, WS echo round trip, the stub daemon never observes an `Origin` header even when the test client sends one, and re-pointing the registry to a second stub works without restarting the proxy.
- **Per-launch secret (AC3)**: 32 random bytes (`getrandom` 0.4) as base64url (`base64` 0.23), compared constant-time. `smind_desktop=<secret>; HttpOnly; SameSite=Strict; Path=/`. The gate (`proxy::secret::evaluate`) is a pure function wrapped by one axum middleware layer over the *whole* router (assets, `/api/*`, `/ws` alike) — there is no route that bypasses it. A valid `?k=` (checked before falling through to the cookie check having failed) sets the cookie and 303s to the same path with `k` stripped from the query string only (not hardcoded to `/`), so a non-root deep link opened cold still exchanges correctly.
- **Connection list (AC4)**: `proxy::connections::Registry` — a built-in `local` entry (id `"local"`, from `SMIND_DAEMON_URL`/default, never removable, re-derived from the current env on every `load` rather than trusted from the saved file) plus any number of `url` entries (id derived from the normalized base URL, so re-adding the same URL updates it in place instead of duplicating). Persisted as JSON (`connections.json` in the app data dir) via `Registry::load`/`save`; a missing or malformed file falls back to a fresh registry rather than failing startup. `validate_base_url` accepts only absolute `http(s)://host[:port]`, stripping any path/query/fragment; `file:`, `javascript:`, other schemes, and a missing host are rejected.
- **Capability / IPC bridge (AC5)**: a new capability `desktop/src-tauri/capabilities/proxy.json`, `remote.urls: ["http://127.0.0.1:*"]` (the `*` covers the per-launch random port; `127.0.0.1` is fixed), granted **only** to that pattern — never to `windows: ["main"]`/`local: true` the way `default.json` grants nothing. This is on top of, not instead of, the secret cookie gate: a capability grant only controls whether `invoke` reaches Rust at all from that origin, so the cookie is still what stops a same-machine process that isn't the app's own webview. Commands, all under `smind_desktop_lib::commands` and validating their own input:
  - `connections_list() -> Vec<Connection>`
  - `connections_add(label: String, url: String) -> Result<Connection, String>` (rejects via `validate_base_url`)
  - `connections_remove(id: String) -> Result<(), String>` (rejects `"local"`)
  - `connections_select(id: String) -> Result<Connection, String>` (rejects an unknown id; re-points the live proxy's registry, no restart)
  - `connections_get_current() -> Connection`
  - `open_external(url: String) -> Result<(), String>` (rejects anything not `http`/`https`, then hands off to `tauri-plugin-opener`)
  - The `default.json` capability (`windows: ["main"]`, `local: true`, `permissions: []`) is otherwise unchanged — the daemon/relay origin still gets nothing, and the bundled UI's own loopback origin gets *only* these six commands, nothing else.
- **Bundled UI build (AC1)**: a new `desktop/ui/app/` output directory (separate from `internal/server/dist`, which stays untouched), built by a new `web/packages/ui` script `build:desktop` (`VITE_SMIND_DESKTOP=1`, `--outDir ../../../desktop/ui/app`). `desktop/ui/app/` is committed as an empty directory (`.gitkeep`; its real contents are gitignored) so `cargo build`/`cargo test` succeed with zero embedded files even without a prior web build — `rust-embed` embeds whatever is there at Rust-compile time, and `tauri.conf.json`'s `beforeBuildCommand`/`beforeDevCommand` run the web build first so the real assets exist before that embed happens for `tauri build`/`tauri dev`. `frontendDist` stays `"../ui"` (unrelated to the embed; still backs `offline.html`, which now serves only as the true last-resort page if the loopback proxy itself fails to bind at all — the bundled-UI-can't-reach-the-daemon case is AC6's job instead).

## Progress

- [ ] AC1 bundled UI build
- [x] AC2 loopback proxy (`smind_daemon_client::proxy::server`; unit + integration tests green)
- [x] AC3 secret gate (`smind_daemon_client::proxy::secret`; unit tests green)
- [x] AC4 connection list -- pure logic + persistence (`smind_daemon_client::proxy::connections`; unit tests green); not yet wired to Tauri commands or the real app-data-dir path
- [ ] AC5 IPC bridge
- [ ] AC6 web platform layer + host picker + unreachable state
- [ ] AC7 Rust features follow selection
- [ ] AC8 regressions + Windows build

## Validation

To be filled in.
