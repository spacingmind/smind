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
- **AC7 (Rust features follow selection)**: `desktop/src-tauri/src/client_watch.rs` owns the daemon-client watcher's lifecycle: `connections_select` aborts the running task (`tauri::async_runtime::JoinHandle::abort`) and spawns a fresh one against the newly-selected connection's `base_url`, reusing the same `WorkspaceCache` (a plain taskId->workspaceId map, valid across a switch) and the same `Tray`. `notify::ClickContext`/`tray::build`/`deeplink::handle` were all changed from taking the selected connection's real `daemon_url` to a fixed `proxy_url` (the loopback origin), since AC7 routes must resolve against the proxy regardless of which upstream is currently selected -- this is *not* restarted on switch, since the proxy's own port never changes.
- **AC6 (web platform layer)**: `web/packages/ui/src/lib/platform.ts` exports `isDesktop` (`import.meta.env.VITE_SMIND_DESKTOP === "1"`) and a `desktop` object matching the six commands, an always-rejecting stub when not desktop. `@tauri-apps/api/core`'s `invoke` is imported dynamically, only from inside a function only ever called when `isDesktop`; confirmed by inspecting real build output (`bun run build` vs `bun run build:desktop`): the non-desktop bundle has no separate tauri chunk at all, the desktop bundle splits it into its own ~0.1kB lazy chunk, and the two main bundles differ by only ~6kB (the new platform/section/component code itself, not `@tauri-apps/api`). The host picker is Settings -> Connections (`components/settings/connections-section.tsx`, registered only `if (isDesktop)` at module load, following the existing per-file `registerSettingsSection` pattern); reachability is shown only for the *current* connection (via the app's existing `connectionStatus`), since probing a saved-but-not-selected connection would need a direct cross-origin fetch to its real daemon URL -- exactly what the loopback proxy exists to avoid. `components/desktop-unreachable.tsx` replaces `offline.html`'s job for the bundled flow: shown in `App.tsx` when `isDesktop && connectionStatus === "disconnected"` (the pre-first-successful-connect case; an established connection dropping goes through the existing automatic reconnect loop and its own status text instead, never this), with Retry (`location.reload()`, same as `offline.html`'s own) and Switch connection (jumps to Settings -> Connections).
- **Bundled UI build (AC1)**: a new `desktop/ui/app/` output directory (separate from `internal/server/dist`, which stays untouched), built by a new `web/packages/ui` script `build:desktop` (`VITE_SMIND_DESKTOP=1`, `--outDir ../../../desktop/ui/app`). `desktop/ui/app/` is committed as an empty directory (`.gitkeep`; its real contents are gitignored) so `cargo build`/`cargo test` succeed with zero embedded files even without a prior web build — `rust-embed` embeds whatever is there at Rust-compile time, and `tauri.conf.json`'s `beforeBuildCommand`/`beforeDevCommand` run the web build first so the real assets exist before that embed happens for `tauri build`/`tauri dev`. `frontendDist` stays `"../ui"` (unrelated to the embed; still backs `offline.html`, which now serves only as the true last-resort page if the loopback proxy itself fails to bind at all — the bundled-UI-can't-reach-the-daemon case is AC6's job instead).

## Progress

- [x] AC1 bundled UI build (`desktop/ui/app/` + `build:desktop` script + `rust-embed` in `desktop/src-tauri/src/assets.rs`; committed empty via `.gitkeep`)
- [x] AC2 loopback proxy (`smind_daemon_client::proxy::server`; unit + integration tests green)
- [x] AC3 secret gate (`smind_daemon_client::proxy::secret`; unit tests green)
- [x] AC4 connection list -- wired end to end: `Registry::load`/`save` against `<app data dir>/connections.json`, driven by the AC5 commands
- [x] AC5 IPC bridge (`desktop/src-tauri/capabilities/proxy.json` + `src/commands.rs`; `cargo build` confirms the capability/permission set resolves)
- [x] AC6 web platform layer + host picker + unreachable state (`lib/platform.ts`, `components/settings/connections-section.tsx`, `components/desktop-unreachable.tsx`, wired into `App.tsx`)
- [x] AC7 Rust features follow selection (`src/client_watch.rs` restarts the daemon-client watcher on `connections_select`; tray/notify/deep-link navigation now target the fixed proxy origin, not the selected connection's real URL)
- [x] AC8 regressions + Windows build

## Validation

- **Rust unit tests** (`cargo test` in `desktop/daemon-client`, 77 unit tests green, covering both the pre-existing suite and the new `proxy::*` modules below):
  - `proxy::secret`: secret length/entropy shape, two secrets differ, constant-time compare, `Set-Cookie` shape, cookie extraction among other cookies, and every gate outcome (no cookie/no k -> Forbidden; wrong cookie -> Forbidden; valid cookie -> Pass; valid `?k=` with no cookie -> ExchangeSecret; wrong `?k=` -> Forbidden; a valid cookie wins even if `?k=` is also present).
  - `proxy::server`: query-param extraction and `?k=` stripping (root path, path with other params, no query at all).
  - `proxy::connections`: URL validation (accepts http/https, strips path/query/fragment, rejects `file:`/`javascript:`/other schemes/garbage/empty), add/select/current, add rejecting invalid input, re-adding the same URL updating in place, the local entry can never be removed, removing an unknown id errors, removing the selected entry falls back to local, selecting an unknown id errors, and the persistence round trip (save then load) plus three fallback cases: missing file, malformed JSON, and a stale local entry/selected-id that's since been removed -- all recover to a fresh/consistent registry rather than failing to start.
  - Route building (`route::task_route_url`) and deep-link parsing were already covered by the prior desktop-quick-wins plan and are unaffected by this plan's changes; still green, part of the same 77.
- **Rust integration test** (`desktop/daemon-client/tests/proxy_integration.rs`, 1 test, green): starts a real proxy (via `proxy::serve`) against an in-process stub HTTP+WS server standing in for a daemon, and asserts, all in one run: no cookie/no `k` is rejected with 403 for the root path, `/api/token`, and *before* the `?k=` exchange; the exchange itself returns a redirect with `Set-Cookie`; `/api/token` passes through correctly with the cookie, and the stub never observes an `Origin` header even though the test client sent one; a `/ws` echo round-trips a text message through the bridge, and the stub's WS upgrade also never sees `Origin`; and re-pointing the registry to a second stub (`Registry::add` + `select`, live, no restart) makes the *same* running proxy return the second stub's token.
- **Web tests** (`bun run --filter '@smind/ui' test`, 1079/1079 green across 97 files, including these new ones):
  - `lib/platform.test.ts`: in a non-desktop build (env stubbed empty), `isDesktop` is false and every `desktop.*` method rejects; in a desktop build (env stubbed `"1"`, `@tauri-apps/api/core` mocked), `isDesktop` is true and each of the six methods calls the matching `invoke` command with the right args.
  - `components/desktop-unreachable.test.tsx`: shows the resolved connection's label/URL, falls back to a generic label if resolution itself fails, and Retry/Switch call their respective callbacks.
  - `components/settings/connections-section.test.tsx`: does not register in a non-desktop build; in a desktop build (module-mocked `@/lib/platform`), lists connections marking the current one, hides Remove for the local entry, and Switch/Remove/Add each call the matching command with the right arguments; an add failure surfaces the error text.
  - Full existing suite re-run confirms no regression: same 1079 passing count as before this plan's changes plus the new tests above.
- **Typecheck**: `tsc -b` (via `bun run typecheck`, and as part of both `bun run build` and `bun run build:desktop`) is clean.
- **`task test` / `task lint`** (repo root): both green -- `task test` runs the full Go suite (`go test ./...`, all packages ok, none touched by this plan) and the full web suite above; `task lint` (`go vet ./...` + `gofmt -l`) is silent (no Go files changed by this plan at all, confirmed via `git diff --stat -- internal/ cmd/` returning empty).
- **`cargo build`/`cargo test`, both crates**: `desktop/daemon-client` (77 unit + 1 integration, all green, no warnings) and `desktop/src-tauri` (`cargo build` clean, no warnings; `cargo test` is 0/0 by design -- all pure logic lives in `daemon-client`, matching the prior desktop-quick-wins plan's own precedent).
- **Full local `tauri build` dry run**: `npx tauri build --no-bundle` (from `desktop/`, `cargo` added to PATH) ran the *entire* pipeline -- `beforeBuildCommand` (`bun install && bun run --filter @smind/ui build:desktop`, producing real assets in `desktop/ui/app/`), then `cargo build --release` embedding those assets via `rust-embed` -- and produced a working release binary end to end, on Linux. This is the same command Windows CI runs (minus `--no-bundle`, which only skips the platform-specific installer step), so it's strong evidence the Windows workflow's `npx tauri build` will succeed too, short of anything Windows-installer-specific.
- **Live (WSLg, this session), against real daemons -- never touching the user's real daemon on 4648** (verified reachable again, untouched, before and after): two temp daemons were started from a freshly-built `bin/smind` (`go build -buildvcs=false`), each with its own `SMIND_HOME` and a `server.port` override in `config.yaml` (4710 and 4711 -- ports well clear of 4648 and any other daemon on this machine).
  - The debug build of the desktop app (`cargo run`), pointed at daemon A (4710) via `SMIND_DAEMON_URL`, came up, bound the loopback proxy, and its own daemon-client watcher connected to daemon A's `/ws` and subscribed successfully (confirmed in logs).
  - `curl` against the proxy port with **no cookie**: the root path, `/api/token`, and a WebSocket upgrade attempt to `/ws` all returned **403** -- the secret gate covers every request, exactly as the hard constraint requires.
  - With the real per-launch secret (read via a temporary `eprintln!` added only for this check, then removed before committing -- confirmed via `git diff`/`cargo build` afterward that no trace of it remains): the `?k=` exchange returned a `303` with the correct `Set-Cookie`; a subsequent authenticated `GET /` returned the real bundled `index.html` (not a 404/empty response, i.e. the `rust-embed`-embedded, freshly-built assets are actually being served); `GET /api/token` through the proxy returned **the exact same token** as `curl`-ing daemon A directly, confirming the passthrough end to end against a live daemon (not just the stub in the automated integration test).
  - A second app instance was pointed at an unreachable port (4799): the daemon-client watcher logged repeated `token fetch` errors with backoff and never crashed or wedged, matching the reconnect design; `curl`-ing the proxy with a wrong `?k=` still correctly returned 403 (the gate doesn't leak an "almost right" signal).
  - **Not exercised live in this sandbox** (no screenshot or input-simulation tooling available here, the same category of gap the prior desktop-quick-wins plan's Validation already recorded): actually clicking through the running window to add/switch/remove a connection via the real UI, and visually confirming the "can't reach" state/host picker render correctly. Confidence instead comes from: the Rust integration test's live re-pointing assertion (the exact mechanism `connections_select` calls), the web component tests (which exercise the real rendered React tree against a mocked desktop API), and the live curl-level confirmation above that the proxy itself behaves correctly against two different real daemons.
- **Windows workflow** (`.github/workflows/desktop-windows.yml`): updated to install `bun` (`oven-sh/setup-bun`, same pinned action `ci.yml` already uses) before the Rust toolchain step, since `beforeBuildCommand`/`beforeDevCommand` now shell out to it; also widened the trigger's `paths` to include `web/packages/ui/**`, since a web-only change can now change what the desktop build ships. The hook command itself avoids single-quoted shell syntax (`bun run --filter @smind/ui build:desktop`, no quotes needed since the package name has no spaces) specifically because Tauri's hook commands run through `cmd.exe` on Windows, where single quotes are literal characters rather than a quoting mechanism -- confirmed the unquoted form still works identically on this machine's bash/bun.
  - **Result**: pushed `feat/desktop-bundled-ui`; run [36093668522](https://github.com/spacingmind/smind/actions/runs/36093668522) (`desktop-windows`) completed **successfully in 7m38s** on the first push -- `Build installers` (`npx tauri build`) ran `beforeBuildCommand` (bun install + `build:desktop`) then the full Windows cargo build, and `Run actions/upload-artifact` produced the `smind-desktop-windows` artifact bundling both the NSIS `.exe` and MSI `.msi` installers (the workflow's own `if-no-files-found: error` would have failed the job had either been missing).
- **Not done / explicitly deferred, not because of a discovered blocker**:
  - A live human-driven Windows run against a WSL daemon (the task's own item 4's last bullet: "Windows manual (user)") -- needs a human on a real Windows machine, same as the prior plan.
  - Opening a PR (explicitly out of scope per the task's own instructions).
  - A second, additional sidebar-header connection indicator beyond Settings -> Connections: AC6 allows either "or both"; Settings -> Connections alone satisfies the AC's list/add/remove/switch/current/unreachable requirements, and the extra surface was judged not worth the added complexity for this pass.
