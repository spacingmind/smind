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
**bundled** UI with a scoped IPC bridge, like Zode and Paseo.

## Decision

1. **Bundled UI, served by a Rust-side loopback proxy.** The desktop
   app embeds the `web/packages/ui` build. On launch, Rust starts an
   HTTP server on `127.0.0.1` that (a) serves the bundled UI as static
   assets and (b) reverse-proxies `/api/*` and `/ws` to the currently
   selected daemon. The webview loads the loopback origin, so the UI's
   same-origin assumptions hold unmodified: `/api/token` is a
   same-origin GET, and `/ws` upgrades same-origin against the daemon.
   Switching hosts in the picker re-points the proxy; the UI can just
   reload. WebSocket upgrade headers are forwarded as-is; the proxy is
   a dumb byte pipe, not a second API client.
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

## Open sub-decisions

- **Relay transport: Rust-side or TS-side.**
  (a) Rust commands wrapping `mobile`-compatible pairing/crypto and
  bridging to the loopback proxy — crypto stays audited Rust, the
  bundled UI stays transport-agnostic; cost: a second protocol
  implementation surface in `desktop/`.
  (b) TS client reusing the mobile pairing code
  (`mobile/src/relay/*`) compiled into the UI — one code path with
  mobile; cost: E2EE keys live in webview JS and the WS relay must
  still satisfy same-origin via the proxy.
  *Recommendation: (a) Rust-side* — key material stays out of the
  webview, and `daemon-client` already proves the pattern.
- **Version skew: bundled UI vs older/newer daemons, without daemon
  changes.**
  (a) Feature-detect at startup (probe endpoints/events the UI needs,
  degrade gracefully).
  (b) Fall back to loading the daemon-served UI when its `/healthz`
  advertises a mismatched version.
  *Recommendation: (a) feature-detect as the default, keep (b) as the
  escape hatch for "old daemon, new app"*, since (b) re-introduces the
  ADR-0012 path one release behind.
- **Proxy port choice and its security.** Any local process can hit
  the loopback proxy once it's up (it inherits the daemon's
  unauthenticated `/api/token` posture, and the daemon already trusts
  loopback). Fixed port = predictable target; random ephemeral port =
  discovery cost only. *Recommendation: random port + per-launch
  secret* delivered to the bundled origin via a set-once cookie (or
  IPC-injected header); proxy rejects requests without it. Not a
  strong boundary — defense against casual local probing, not a
  malicious same-machine process.

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
- The daemon's `/ws` `CheckOrigin` default continues to see a
  loopback-to-loopback same-origin upgrade through the proxy; no
  Origin header rewriting is performed or needed.
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
