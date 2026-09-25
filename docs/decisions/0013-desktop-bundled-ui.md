# 0013: Desktop bundled UI with a scoped IPC bridge, connecting to any smind daemon

## Status

Proposed

Supersedes the "the webview loads the daemon's web UI" commitment of
ADR-0012 (`docs/decisions/0012-desktop-thin-client.md`); the rest of
ADR-0012 (Rust-side tray, notifications, shortcuts, daemon-client
crate) stands.

## Context

ADR-0012's thin client has one window pointed at one daemon
(`desktop/src-tauri/src/lib.rs`, `watch_daemon_and_navigate`), with
`SMIND_DAEMON_URL` as the only knob. The user wants the desktop app to
be a first-class client that can connect to **any** smind daemon — the
local default `http://127.0.0.1:4648`, an arbitrary URL reached through
an SSH tunnel or Tailscale, and the Phase 3 E2EE relay pairing the
mobile app already uses (`mobile/src/relay/*`, `internal/relay/*`,
ADR-0007/ADR-0011) — with a host picker and a persisted connection
list, like Paseo's host registry
(`refs/paseo/packages/app/src/types/host-connection.ts`).

The daemon's HTTP posture constrains any browser-side UI:

- it binds `127.0.0.1` only (`internal/server/server.go`, `Addr`);
- `GET /api/token` is unauthenticated with no CORS
  (`web/packages/ui/src/lib/daemon.ts` fetches it same-origin);
- `/ws?token=` uses gorilla's **default** `CheckOrigin`
  (`internal/wsapi/server.go`), which rejects a mismatched `Origin`.

The web UI assumes same-origin throughout (`daemon.ts` relative
fetches; `ws-client.ts` builds the WS URL from `location.host`). The
decision, already made: the backend does **not** change — no CORS, no
Origin allowlist, no new endpoints — and the desktop app ships a
**bundled** UI with a scoped IPC bridge, like ZCode and Paseo.

## Decision

1. **Bundled UI, served by a Rust-side loopback proxy.** The desktop
   app embeds the `web/packages/ui` build. On launch, Rust starts an
   HTTP server on `127.0.0.1` that (a) serves the bundled UI as static
   assets and (b) reverse-proxies `/api/*` and `/ws` to the currently
   selected daemon. The webview loads the loopback origin, so the UI's
   same-origin assumptions hold unmodified: `/api/token` is a
   same-origin GET, and `/ws` upgrades same-origin against the daemon.
   Switching hosts in the picker re-points the proxy; the UI can just
   reload. The proxy is a byte pipe, not a second API client. One header
   rule matters: it **drops the `Origin` header** on proxied requests
   (gorilla's default `CheckOrigin` admits requests with no `Origin`,
   the same path the Rust `daemon-client` already relies on), rather
   than depending on `Host` passing through unrewritten.
   For a **relay** connection there is no HTTP daemon to proxy to: the
   Rust side answers `/api/token` itself (relay admission, ADR-0011,
   replaces the daemon token) and bridges `/ws` JSON-RPC frames onto the
   E2EE relay channel, so the bundled UI still sees one same-origin
   `/ws` regardless of transport.
2. **Scoped IPC bridge.** Native capabilities the UI needs (window
   controls, open-URL, dialogs, relay crypto if placed in Rust — see
   open items) are exposed as an explicit **per-command allowlist** in
   the default capability
   (`desktop/src-tauri/capabilities/default.json`, today `local: true`,
   `permissions: []`), granted only to the bundled loopback origin —
   never to any remote/daemon origin. No wildcard `invoke` bridge.
3. **Connection list, persisted client-side.** A registry of saved
   daemons (local default, arbitrary URLs, relay pairings), modeled on
   Paseo's `HostConnection` union — directTcp / remoteSsh / relay
   entries with stable ids, plus a picker with "local first" ordering
   — but persisted in desktop app storage, not in the daemon.

## Alternatives considered

- **Direct cross-origin connection** (webview loads
  `http://127.0.0.1:4648` or a tunneled URL directly, UI calls
  `http://daemon:port/api/...`). Rejected: needs daemon changes (CORS
  on `/api/*`, `CheckOrigin` override on `/ws`) that the user has
  ruled out, and still leaves the UI unbundled.
- **Tauri custom-protocol only** (`tauri://` serving the bundled UI,
  no loopback server). The UI assets load, but `ws://` and relative
  `fetch("/api")` cannot target the daemon from the custom origin:
  WebSocket over a custom scheme isn't supported by the webview, and
  cross-origin `/ws` fails `CheckOrigin`. This is why the loopback
  proxy exists.
- **Daemon-served UI plus `remote.urls`** (keep ADR-0012, enumerate
  daemon origins in a capability with IPC grants). Still one-origin-
  per-daemon, gives a remote-content IPC grant to whatever that origin
  serves, and doesn't reach non-HTTP transports (relay) at all.
- **Electron.** Mature multi-origin story, but abandons the existing
  Tauri 2 shell (`desktop/`), Rust daemon-client crate, and plugin
  surface for a heavier runtime — no capability model at all without
  hand-rolling one.

## Sub-decisions (user, 2026-09-25)

- **Relay transport: Rust-side.** Rust holds the E2EE keys and bridges
  `/ws` JSON-RPC frames onto the relay channel, answering `/api/token`
  itself. The bundled UI stays transport-agnostic, and key material
  never enters the webview.
- **Version skew: feature-detect, with a daemon update flow like
  Paseo's.** The UI feature-detects what it needs and degrades
  gracefully. When the app is **newer** than a daemon it can manage,
  the UI offers "update daemon and restart". This is modeled on Paseo's
  `desktop/src/daemon/daemon-manager.ts` (`shouldRestartForVersion`:
  app version vs the `daemonVersion` the daemon reports) and
  `app/src/desktop/components/desktop-updates-section.tsx`.
  - Remote, tunneled and relay daemons are not the app's to update, so
    for them it only shows a notice.
  - **Version signal (user, 2026-09-25: approved).** This is a small
    additive daemon change and does not change the architecture. The
    version is stamped at build time via `-ldflags`, exposed as
    `smind --version`, and added as a `version` field in the
    `/healthz` JSON. The existing `status`/`service` fields are
    unchanged and no new endpoint is added.
  - **App-managed local daemon (user, 2026-09-25: approved, all three
    hosts).** The app may install, update and restart the *local*
    daemon it manages. There are three hosts:
    - **Windows + WSL2:** runs `wsl.exe` to fetch the Linux binary
      from the GitHub Release, replace it, and restart `smind serve`.
    - **macOS native:** runs the darwin binary as an app-managed
      process, e.g. a launchd agent or a sidecar.
    - **Windows native:** runs a windows binary directly. **Blocked
      today:** `GOOS=windows go build ./cmd/smind` fails in
      `internal/terminal` (`syscall.Kill`; `creack/pty` has no ConPTY
      support). A native-Windows daemon needs a Windows terminal
      backend first, which is its own plan.

    This is where ADR-0012's deferred "sidecar mode" lands.
    Remote/tunneled/relay daemons are never updated by the app. They
    only get a "daemon is older than this app" notice.

- **Proxy port: random port + per-launch secret**, exchanged for an
  `HttpOnly; SameSite=Strict` cookie as described in Security notes.

## Consequences

- The desktop app can connect to local, tunneled, and relay daemons
  with zero daemon changes; switching is a picker action that
  re-points the proxy.
- The bundled UI and daemon API can now drift in version — skew
  becomes a real (mitigated) risk ADR-0012 had eliminated by design.
- `web/packages/ui` gains a desktop build target; its same-origin
  assumptions are preserved by construction (loopback origin), not by
  edits.
- The offline fallback page and `SMIND_DAEMON_URL` navigation flow in
  `desktop/src-tauri/src/lib.rs` are replaced by the bundled UI +
  host-picker flow; tray/notify/shortcut behavior is unaffected.
- One more local listener per launch (the proxy), which the security
  notes below bound.

## Security notes

- The proxy binds `127.0.0.1` only, matching the daemon's own
  posture (`internal/server/server.go`).
- The per-launch secret + random port (open item 3) protects the
  proxy from other local processes; it is not a defense against a
  compromised same-user process, which can already read the daemon's
  own token the same way.
- IPC grants stay in the default capability's allowlist and are
  scoped to the bundled origin (`local: true` window list); daemon or
  relay content never receives `invoke` access. This keeps ADR-0012's
  "no remote-content IPC" stance.
- The daemon's `/ws` `CheckOrigin` default sees an upgrade with no
  `Origin` header (the proxy drops it), exactly like the existing Rust
  `daemon-client`; no daemon-side change.
- Through the proxy, a *remote* daemon's `/api/token` becomes reachable
  from this machine's loopback — the per-launch secret is what keeps
  other local processes from using the proxy as a token oracle.
- Relay pairing keys, if Rust-side (recommended), never enter the
  webview; admission auth stays exactly as ADR-0011 defines it.

## Cross-references

- `docs/decisions/0012-desktop-thin-client.md` — superseded in part.
- `docs/decisions/0007-relay-architecture.md`,
  `docs/decisions/0011-relay-admission-auth.md` — relay transport and
  admission this ADR builds on without change.
- `internal/server/server.go`, `internal/wsapi/server.go`,
  `web/packages/ui/src/lib/daemon.ts`,
  `web/packages/ui/src/lib/ws-client.ts` — the same-origin posture
  the loopback proxy preserves.
- `refs/paseo/packages/app/src/types/host-connection.ts` — host
  registry model referenced for the connection list.
