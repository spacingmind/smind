# 0012: Desktop thin client — UI loaded from the daemon, native features from a Rust-side event subscription

## Status

Accepted

## Decision

The desktop app is a Tauri 2 **thin client** over an already-running
daemon (`http://127.0.0.1:4648` by default, overridable via
`SMIND_DAEMON_URL`). Three commitments define it:

1. **The main webview loads the daemon's own web UI, not a bundled
   copy.** UI and API can never drift in version, and the daemon needs
   no changes. On startup the window shows a small local fallback page
   ("daemon not reachable … retrying") and switches to the daemon URL
   once `GET /healthz` answers — covering the daemon-starts-later and
   daemon-restarts cases the same way.
2. **Every native feature is driven from the Rust side.** A
   Tauri-2-remote URL gets no IPC unless a capability explicitly lists
   it under `remote.urls`; we list nothing. Tray (Open/Quit),
   hide-on-close, single-instance focus, the global visibility
   shortcut, and OS notifications are all implemented in Rust. The
   notification path is a Rust-side daemon client (`desktop/
   daemon-client`, deliberately GUI-free so its logic is unit-testable
   without webkit2gtk) that fetches `GET /api/token`, opens
   `/ws?token=...`, sends `events.subscribe` for `permission.pending`,
   and turns each event's `summary` into a native notification,
   reconnecting with exponential backoff (reset after a stable
   connection) and resubscribing on every reconnect.
3. **Bundling the daemon as a Tauri sidecar is explicitly deferred.**
   The user runs `smind serve` (today inside WSL2, reached through
   WSL's localhost forwarding). Sidecar mode — spawning and supervising
   the daemon from the app — is a later item, not a hidden dependency
   of this one.

Out of scope here (per the plan): auto-update, autostart, the
native-call abstraction layer, the settings UI, and Windows/macOS
packaging/CI.

## Alternatives considered

- **Bundling the daemon as a sidecar from day one.** One download, no
  "start the daemon first" step — but it couples desktop release
  cadence to daemon release cadence, forces process lifecycle/error
  surface decisions (restart policy, log routing, version negotiation)
  into the first shipping desktop item, and WSL2 makes local-daemon the
  natural deployment anyway. Deferred.
- **A bundled frontend talking IPC to Rust commands that proxy the
   daemon API.** Would allow richer native integration later, but
   duplicates the web UI and guarantees version drift between the
   daemon's UI and the desktop's. Contradicts the "UI from the daemon"
   user decision outright.
- **Granting the remote daemon URL IPC permissions in a capability.**
   Would let the daemon's page call plugins directly — but the daemon
   serves a browser UI with no knowledge of Tauri, and a remote-content
   IPC grant is a standing privilege for whatever that origin serves.
   Driving everything Rust-side needs no grant at all.
- **Notifications via daemon→OS push (e.g. a notify-send hook in the
   daemon).** Fewer moving parts per notification, but couples the
   daemon to a desktop environment it must not assume (it runs headless
   and in WSL), and the desktop client needs the reconnecting /ws
   subscription anyway for liveness.

## Rationale

`docs/ROADMAP.md` Phase 4 calls for a Tauri 2 wrapper; the smallest
version of it that delivers real native value (tray, shortcuts,
notifications) is one whose webview is *just a viewport* onto the
daemon's existing UI. The daemon's auth posture already fits this
exactly: `GET /api/token` is an unauthenticated same-origin GET and
`/ws` takes the token as a query param (see `internal/server/server.go`
and `internal/wsapi/server.go`), and gorilla's default upgrader
`CheckOrigin` admits requests with no `Origin` header — which is what
the Rust client sends — so no daemon change was needed for a
non-browser client. Splitting the wire logic into `daemon-client`
(a plain Rust crate) keeps `cargo test` green on machines without
Linux GUI system deps, which is also the crate the future sidecar mode
would reuse.

## Cross-references

- `docs/plans/active/desktop-tauri-shell.md` — implementation plan
  (user decisions, research findings, acceptance criteria).
- `docs/decisions/0005-*.md` — wsapi event notification wire shape the
  Rust client parses (`{"event": {"topic", "seq", "payload"}}`).
- `internal/wsapi/events.go` — `permission.pending` payload
  (`{runId, taskId, requestId, summary, options}`) mirrored in
  `desktop/daemon-client/src/protocol.rs`.
- `desktop/daemon-client/`, `desktop/src-tauri/` — the crates this ADR
  describes.
