# Homelab distribution: update, service, install scripts, Docker, direct + relay connections, desktop first-run

Implements [ADR-0031](../../decisions/0031-homelab-cli-distribution.md)
(**Proposed**, revised after a study of Paseo). Every PR below that depends on an
open decision says so; the plan is written against the ADR's recommended defaults
(★ = default changed or new because of Paseo), and a PR must not start before its
blocking decisions are answered (AGENTS.md rule d).

Size (whole plan): **XL (10)** — scope 2, PRs 2, risk 2, decisions 2, verification 2
→ split into twenty PRs (P0–P17, with P13 split in three), each sized below (WORKSPACE_PROTOCOL.md). All PRs branch
from `origin/develop`, none are stacked; where two PRs share a file they are
sequenced, or (for the three-line `cmd/smind/main.go` dispatcher additions) rebased
before merge — never stacked.

## PR split, write scopes, order

| PR | Item | Write scope | Size | Blocking decisions | Depends on |
|---|---|---|---|---|---|
| **P0** relay "workspace" → "relay host" (terminology, no wire change) | 6 | `cmd/smind/relay.go` (+ test), `internal/relay/server/workspaces.go` (store `rm`/`rotate`), CLI/doc strings, `README.md` | **S (2)** — scope 0, PRs 0, risk 1, decisions 0 (decided), verify 1 | 33 (decided) | — (before P2: same file) |
| **P1** install scripts | 3 | `scripts/install.sh`, `scripts/install.ps1`, `scripts/install_test.sh` (new), `README.md` install/update sections | **M (4)** — scope 1, PRs 0, risk 1, decisions 0, verify 2 (real Windows + Linux) | 21 | — |
| **P2** relay offer QR + quickstart | 6 | `cmd/smind/relay.go` (+ test), `internal/relay/pairing/qr.go` (L fallback, + test), `docs/homelab.md` (new) | **S (2)** — scope 0, PRs 0, risk 1, decisions 1, verify 0 | 15, 18 | — |
| **P3** desktop "Add via relay link" | 6 | `web/packages/ui/src/lib/platform.ts`, `…/settings/connections-section.tsx`, new `…/add-relay-link.tsx`, `desktop/src-tauri/src/commands.rs` (+ `lib.rs`, `build.rs`, `capabilities/proxy.json`, capability test) for `connections_preview_relay` | **M (4)** — scope 2, PRs 0, risk 1, decisions 0, verify 1 | 15, 16 | — |
| **P4** ★ direct connections + password, `smind config`, `smind set-password` | 7 (new) | `internal/server/*` (listen, login, session tokens, token gating, Host allowlist), `internal/config/*`, `internal/auth/*`, `internal/wsapi/*` (auth on upgrade), `cmd/smind/{config,setpassword,serve}.go`, `cmd/smind/main.go` (dispatch), `web/packages/ui/src/{lib/daemon.ts,components/login/*,App.tsx}` | **L (8)** — scope 2, PRs 1, risk 2, decisions 2, verify 1 (UI screenshots) | 8, 9, 10 | — (may be split P4a daemon/CLI → P4b web login) |
| **P5** ★ first-run + connection onboarding | 7 | `desktop/src-tauri/src/{app_settings.rs (new),ssh_tunnel.rs (new),daemon_manager.rs (gating + take-over fix),commands.rs,lib.rs,build.rs}`, `desktop/daemon-client/src/proxy/*` (probe), `desktop/src-tauri/capabilities/proxy.json`, `web/packages/ui/src/{lib/platform.ts,components/first-run/*,settings/{connections,daemon}-section.tsx,App.tsx}` | **L (8)** — scope 2, PRs 1, risk 2, decisions 1, verify 2 (real macOS + Windows app, light+dark screenshots) | 11, 22, 23, 24, 25 | **P3** (shared component, same files), **P4** (password field), the Windows bundled-daemon PR (`feat/windows-bundled-daemon`, same `daemon_manager.rs`) merged first |
| **P6** `smind update` (checksum) | 1 | `cmd/smind/update.go` (+ test), `internal/selfupdate/*` (new), `cmd/smind/main.go` (dispatch + usage) | **L (7)** — scope 1, PRs 1, risk 2, decisions 1, verify 2 (real Windows rename-aside, real release) | 4, 5 | — |
| **P7** release signing + CLI verification | 1 | `.github/workflows/release-please.yml` (`publish` job), `internal/selfupdate/verify.go` (+ test), embedded pubkey | **L (6)** — scope 1, PRs 0, risk 2, decisions 1, verify 2 (real release / signed dry run) | 1 | **P6** |
| **P8** ★ `smind service` (systemd, launchd) + `managedBy` + daemon log file | 2 | `cmd/smind/service.go` (+ test), `internal/service/*` (linux+darwin backends, golden units), `internal/logging/*` (rotating `daemon.log`), `internal/server/server.go` (`/healthz managedBy`), `cmd/smind/serve.go`, `cmd/smind/main.go`, `scripts/install.sh` (`--service`), `docs/homelab.md` section | **L (7)** — scope 1, PRs 1, risk 1, decisions 2, verify 2 (real systemd + launchd) | 3, 6, 7 | **P1** (`install.sh`), **P2** (docs file), **P4** (`server.go`, `serve.go`, `config.go`) |
| **P9** `smind service` Windows + `install.ps1 -Service` | 2 | `internal/service/windows*.go`, `scripts/install.ps1` (`-Service`), `docs/homelab.md` section | **M (5)** — scope 1, PRs 0, risk 1, decisions 1, verify 2 (real Windows 11) | 2 | **P8** (interface, log file), P1 |
| **P10** "older than app" notice + service recognition + `daemon.info` | 4 | `web/packages/ui/src/components/desktop-daemon-banner.tsx`, `…/settings/daemon-section.tsx`, `web/packages/ui/src/lib/platform.ts`, `desktop/src-tauri/src/daemon_manager.rs` (unmanaged kind), `internal/wsapi/handlers.go` (`daemon.info`) | **L (6)** — scope 2, PRs 0, risk 1, decisions 1, verify 2 (screenshots + real relay daemon) | 6, 17 | **P5** (same web/desktop files), **P6** (command exists), **P8** (`managedBy`) |
| **P11** ★ Docker image | 5 | `Dockerfile` (+ `docker/`), `docker/entrypoint`, `docker/docker-compose.example.yml`, `docker/Dockerfile.agents.example`, `.dockerignore`, `.github/workflows/release-please.yml` (new `docker` job), new `.github/workflows/docker-check.yml` (PR build, no push), `docs/homelab.md` section | **L (7)** — scope 1, PRs 1, risk 2, decisions 1, verify 2 (real `docker buildx`, multi-arch, run + healthcheck + `-p`) | 9, 19, 20 | **P4** (listen/password), **P7** (both edit `release-please.yml`; one owner at a time), **P8** (daemon log file) |
| **P12** ★ relay protocol v3 (WebSocket): spec, shared definition, ONE conformance suite, Go relay, multi-device | 6 | `docs/relay-protocol-v3.md`, `relay/protocol/*` (proto, golden vectors, scenarios), `relay/conformance/*`, `internal/relay/server/*` (v3 handler), `internal/relay/admission/*`, `.github/workflows/relay-ci.yml`, ADR-0007/0011 pointers | **L (8)** — scope 2, PRs 1, risk 2, decisions 2, verify 1 | 13, 31 | **P0** |
| **P13a** ★ v3 clients: Go daemon (bridge, `relay connect`, offer, CA mode, device password) | 6 | `internal/relay/{bridge,client,pairing}/*`, `cmd/smind/relay.go`, `internal/wsapi/*` (password check) | **L (6)** — scope 1, PRs 1, risk 2, decisions 1, verify 1 | 14, 30 | **P12**, **P4**, **P2** (same `relay.go`) |
| **P13b** ★ v3 clients: desktop (Rust) | 6 | `desktop/daemon-client/src/relay/*`, `desktop/src-tauri/src/commands.rs` (offer/preview/password plumbing), `web/packages/ui/src/components/settings/add-relay-link.tsx` | **L (6)** — scope 2, PRs 0, risk 2, decisions 0, verify 2 (real relay) | 14 | **P12**, **P3** (same files), **P13a** (interop) |
| **P13c** ★ v3 clients: mobile (TypeScript) | 6 | `mobile/src/relay/*`, `mobile/src/screens/PairingScreen.tsx` | **M (4)** — scope 1, PRs 0, risk 1, decisions 0, verify 2 (real device) | 14 | **P12** |
| **P14** ★ Cloudflare Worker relay in the monorepo | 6 (new) | `relay-worker/*` (own `package.json`, `wrangler.toml`, tests), workspace registration, `.github/workflows/relay-ci.yml` (Worker job) | **L (7)** — scope 1, PRs 1, risk 2, decisions 1, verify 2 (workerd) | 13 | **P12** (protocol + suite) |
| **P15** ★ open enrolment + abuse limits (Worker + Go `--open-enrolment`) | 6 (new) | `relay-worker/*`, `internal/relay/server/*` (enrolment + limits), `relay/protocol/scenarios/*` (enrolment scenarios) | **L (7)** — scope 1, PRs 1, risk 2, decisions 2, verify 1 | 27, 28, 32 | **P14**, **P12** |
| **P16** ★ Cloudflare deploy + ops (**BLOCKED on Human**) | 6 (new) | `relay-worker/wrangler.toml`, `.github/workflows/deploy-relay.yml`, `docs/relay-ops.md` | **M (5)** — scope 1, PRs 0, risk 2, decisions 1, verify 2 (real Cloudflare + reviewer approval) | 27, 29, 34 | **P14**, **P15**; Human-provided environments/secrets |
| **P17** ★ hosted relay as the default (`relay connect` with no address) | 6 (new) | `cmd/smind/relay.go`, `internal/relay/client/*`, `docs/homelab.md`, `docs/relay-privacy.md` | **M (5)** — scope 1, PRs 0, risk 1, decisions 2, verify 2 (against staging) | 14, 30, 32 | **P13a**, **P15**, **P16**, **P4**, **P0** |

**Suggested order**

1. *Wave 1:* **P0** (rename; before P2, same file) in parallel with **P1**; then **P2** and **P3** (disjoint
   scopes) — the quick wins: installers, `relay offer` QR, "Add via relay link".
2. *Wave 2 (parallel):* **P4** (direct + password), **P6** (`update`), **P12** (relay protocol v3: spec, shared
   definition, conformance suite, Go relay, multi-device). `cmd/smind/main.go` gets one-line dispatcher cases from
   P4 and P6: rebase the later one, do not stack.
3. *Wave 3 (parallel):* **P5** (first-run; after P3, P4 and the Windows bundled-daemon PR), **P7** (signing; after
   P6), **P8** (service; after P4), **P13a** (Go v3 clients; after P12, P4, P2), **P14** (Worker; after P12).
4. *Wave 4:* **P9** (after P8), **P10** (after P5, P6, P8), **P13b** (after P13a, P3), **P13c** (after P12),
   **P15** (enrolment + limits; after P14).
5. *Wave 5:* **P11** (Docker; after P4, P7, P8), **P16** (Cloudflare deploy — **starts only when the Human has
   created the GitHub Environments and secrets**), then **P17** (hosted default; after P13a, P15, P16, P4).

**Blocked on Human** (nothing below can start or finish without them; none of the other PRs needs them):

- **P16 / production:** GitHub Environment `relay-production` with required reviewers (and optionally
  `relay-staging`); environment secrets `CLOUDFLARE_API_TOKEN` (Account → Workers Scripts: Edit, Account →
  Account Settings: Read; Zone `spacingmind.com` → Workers Routes: Edit, DNS: Edit — verify in the dashboard),
  `CLOUDFLARE_ACCOUNT_ID`, `RELAY_SIGNING_KEY`, `RELAY_ADMIN_TOKEN`; the Cloudflare account on **Workers Paid**;
  confirmation that the repository plan supports required reviewers; a billing usage alert (decision 27).
- **P17 / switching the default on:** the privacy page on spacingmind.com (landing-repo follow-up) and the first
  approved production deploy.
- **Pending Human answers on the rest of the 34 decisions**, notably **#9** (direct connections with a password):
  P4 and everything downstream of it (P5 password field, P8, P11, P13a device password, P17) wait for it; the plan
  assumes the recommended defaults but does not decide them.

Not in this plan (landing repo, listed so it is not forgotten): `/install.ps1`
redirect in `spacingmind/spacing-mind-landing-page` `apps/web/next.config.ts`
`redirects()`, added after P1 is in a release; the relay privacy page on spacingmind.com. Not in this plan: built-in TLS
for direct connections (decision 10), relay-then-direct "one host, several connections" (decision 25), pairing-offer
v2 compaction (decision 18), GitHub login for the hosted relay, a status page, removing gRPC v2 after its deprecation
window (decision 31; a later PR), and any rename of smind's own "Workspace" concept (a separate question still pending
with the Human, deliberately not touched here).

## Acceptance Criteria

### P0 — relay "workspace" → "relay host" (terminology, no wire change)

1. `smind relay host new <id>`, `ls`, `rm <id>` and `rotate <id>` exist and operate on the relay data directory;
   `smind relay workspace new|ls` keep working with identical behaviour and exit codes and print a one-line
   deprecation note to stderr.
2. `smind relay --help`, `relay connect` and `relay offer` usage and closing messages say "relay host" and contain
   the sentence: "Connecting registers this daemon once with the relay; every paired client sees all of this
   daemon's workspaces, spaces and tasks; nothing is created in smind."
3. README, `docs/homelab.md` (when it exists) and every user-visible string in the CLI and desktop UI use "relay
   host"; no user-visible string calls the relay-side record a "workspace".
4. **No wire change:** the offer's `ws` key, `Offer.WorkspaceID`, `workspace_id` in `relay.proto` and the admission
   transcript are untouched (the pairing fixture test still passes byte-for-byte); the doc comments say "relay
   host id".
5. `rotate` generates a new secret, invalidates the old one for new admissions, and prints the new secret once.

**Paseo reference — deliberately different.** Paseo calls the daemon a "host" in its UI (`HostProfile`,
`packages/app/src/types/host-connection.ts`) and the relay session a `serverId`; it has no relay-side record that
needs a separate name. We adopt "host" for the same thing Paseo's users see — a daemon — and add "relay host" for
its mailbox on a relay.

### P1 — install scripts

1. `install.sh --version 0.11.1` installs that release; `SMIND_VERSION` still works;
   if both are given the flag wins; `--help` prints usage.
2. On `MINGW*|MSYS*|CYGWIN*` the script exits non-zero with the PowerShell
   one-liner, not "no native Windows build yet"; the header comment and `README.md`
   no longer claim there is no native Windows daemon or that the app uses WSL2.
3. Checksum verification is unchanged and mandatory (a tampered archive exits
   non-zero and installs nothing).
4. `install.ps1` resolves the OS architecture (amd64/arm64, including an x64
   PowerShell on an ARM64 OS → arm64), downloads `smind_<v>_windows_<arch>.zip` +
   `checksums.txt`, verifies SHA-256, extracts only `smind.exe`, installs to
   `%LOCALAPPDATA%\Programs\smind\`, adds it to the user `PATH` once (idempotent),
   and never writes under `%LOCALAPPDATA%\smind\`.
5. `install.ps1` honours `SMIND_VERSION` / `SMIND_INSTALL_DIR`, enforces TLS 1.2 on
   Windows PowerShell 5.1, warns (does not fail) when Git for Windows is missing, and
   leaves a previous install untouched on any failure.
6. README documents both one-liners and states that `install.ps1` is served from
   spacingmind.com only after the landing redirect exists (raw GitHub URL until then).

**Paseo reference — deliberately different.** Paseo has no install script; its CLI is
`npm install -g @getpaseo/cli` and its apps are installers (`refs/paseo/README.md:88-100`).
Nothing to port; the "install, start, then choose how clients connect" server shape
(`README.md:94-100`, `packages/cli/src/commands/onboard.ts`) is reflected in the closing
message of both scripts ("Next: `smind serve`, then see docs/homelab.md").

### P2 — `smind relay offer` + quickstart

1. `smind relay offer` prints the pairing URL on stdout exactly as today (a script
   capturing stdout still gets only the URL); with a TTY on stdout or `--qr` it also
   prints a terminal QR to **stderr**, so `$(smind relay offer)` stays clean.
2. `--png <file>` writes a PNG QR, mode `0600`, refusing to overwrite without `--force`.
3. A 432-byte offer (ADR-0031's realistic example) renders: QR encoding tries level M and
   falls back to level L; an offer that fits neither prints the URL plus "too long for a
   QR code" and exits 0.
4. Help text and `relay connect`'s closing message say "for a mobile device or the desktop
   app" and "treat this URL like a password: anyone with it can reach this daemon through
   the relay; to revoke, run `smind relay host rotate`", and
   carry Paseo-style consent wording ("traffic is end-to-end encrypted; the relay cannot
   read your code or messages").
5. `docs/homelab.md` documents **both** paths — direct connection (placeholder section
   completed by P4) and the relay (on a VPS or on the homelab; enrol → connect → offer →
   pair desktop → pair mobile) — naming the open ports (`7400/tcp` native + `7401/tcp`
   grpc-web, adjacent), the fingerprint check (printed by `smind relay`, pinned by
   `relay connect` on first use), the one-device limit **until P12/P13a–c land**, the SSH
   alternative, and when to prefer which transport.

**Paseo reference — adapted / different.** *Adapted:* relay is an explicit opt-in with a
plain-language E2EE consent (`packages/cli/src/commands/daemon/pair.ts:106-109`; relay off by
default, `packages/server/src/server/config.ts:302-307`); the pairing link is printed with a QR
(`pair.ts` `formatPairingInstructions`). *Different:* Paseo's offer is `{v, serverId,
daemonPublicKeyB64, relay}` with **no secret** (≈ 230 bytes, always QR-sized;
`packages/protocol/src/connection-offer.ts:10-17`) because its relay is public and open; ours
carries the relay host's admission secret and pinned fingerprint (432 bytes), hence the level-L
fallback instead of a smaller offer (v2 compaction deferred, decision 18). Paseo also exposes
a compact `relay://host:port/<serverId>?key=…` URI (`daemon-endpoints.ts`); not needed yet.

### P3 — desktop "Add via relay link"

1. Settings → Connections has **Add via relay link** (desktop build only): paste → preview →
   confirm → connection appears in the list and can be selected; the pasted string is cleared
   on success, on failure and on unmount, never logged, and the input does not autocomplete.
2. `connections_preview_relay(pairing_url)` returns only non-secret fields (relay host:port,
   relay host id, grouped daemon-key fingerprint, `pinned: bool` (false = CA-verified), and `kind: "newHost" |
   "changedConnection"` when a saved connection has the same relay host but a different key or
   relay); no field contains the admission secret or key bytes. It is added to `build.rs`,
   `capabilities/proxy.json` and the capability allowlist test.
3. The confirmation screen shows those values and requires an explicit **Add**; for
   `changedConnection` it says the key/relay changed and defaults to Cancel; Cancel adds and
   saves nothing.
4. The existing "Add a connection" URL form rejects any input containing `#offer=` (or whose
   fragment parses as an offer) with a message pointing to "Add via relay link", and never
   saves a connection for it.
5. A malformed link, a version-mismatched offer and an offer with no fingerprint each show the
   Rust error text inline (`role="alert"`) and add nothing.
6. Light and dark screenshots of the section, the preview, the changed-connection warning and an
   error state were looked at on the real desktop app.

**Paseo reference — ported.** The confirm-before-save step with key fingerprint and relay
endpoint, and the "new host" vs "changed connection" distinction, come from
`refs/paseo/packages/app/src/runtime/host-confirmation.ts` (`HostConfirmationRequest`,
`confirmLink`); paste-a-link as a first-class entry is `components/pair-link-modal.tsx` and the
Maestro flow `packages/app/maestro/relay-pairing.yaml`. Not ported: scanning a QR (no scanner on
desktop), deep-link auto-pair (decision 16).

### P4 — ★ direct connections with a password, `smind config`, `smind set-password`

1. **Defaults unchanged.** With no new config the daemon listens on `127.0.0.1:4648`, serves
   `/api/token` to loopback clients as today, and every existing client (web UI, desktop proxy,
   CLI, MCP) works without a password.
2. `smind config get [path]`, `set <path> <value>`, `unset <path>` read/write `config.yaml`
   (comments preserved), validate known keys (`server.port`, `server.listen`,
   `server.hostnames`), and print that a restart is needed; `smind serve` still takes no flags.
3. `smind set-password` prompts twice on a TTY (refuses non-TTY stdin without
   `--password-stdin`), rejects passwords under 12 characters, stores a **bcrypt hash** in
   `config.yaml` (`server.passwordHash`, file mode `0600`), never echoes or logs it.
   `SMIND_PASSWORD` (plain env, hashed in memory at start) and `SMIND_LISTEN`,
   `SMIND_HOSTNAMES` override config for containers.
4. **The daemon refuses to start** when `listen` is non-loopback and no password is
   configured, with a message naming `smind set-password`.
5. `GET /api/token` is served only when `RemoteAddr` is loopback **and** `Host` is a loopback
   name/IP **and** no `Forwarded`/`X-Forwarded-*`/`X-Real-IP` header is present; otherwise 403.
6. `POST /api/login {password}` returns an expiring, in-memory 256-bit session token
   (not the daemon token); wrong password → 401 with constant-time comparison; per-IP
   exponential backoff after repeated failures (429 with `Retry-After`).
7. `/ws` accepts the session token via `Sec-WebSocket-Protocol` bearer or `Authorization`,
   **not** via the URL query, for non-loopback clients; the loopback token query path keeps
   working for existing loopback clients.
8. `Host` is validated on every HTTP request and WS upgrade against `localhost`, `*.localhost`,
   any literal IP, and `server.hostnames` (leading `.` matches the domain and subdomains);
   anything else → 403 `Host not allowed`. `CheckOrigin` stays same-origin.
9. The embedded web UI shows a login form when the daemon reports a password is required,
   stores nothing in `localStorage` beyond the session token's lifetime policy chosen in the PR,
   and keeps working with no login on loopback.
10. `GET /healthz` stays unauthenticated and gains `authRequired: bool`.
11. Docs (`docs/homelab.md`) state the threat model from ADR-0031 D8, including plain-HTTP
    exposure on a LAN, the Host-rewriting reverse-proxy caveat, and "the password is an RCE
    credential".

**Paseo reference — ported (model), stricter (enforcement).** Ported: loopback default with a
configurable `listen` (`packages/server/src/server/config.ts:470-480`), bcrypt-hashed password
set by a CLI command (`packages/cli/src/commands/daemon/set-password.ts`, `auth.ts:35`) and
checked at admission (`session-admission-auth.ts`), Host-header allowlist with Vite-style
semantics (`hostnames.ts`, `SECURITY.md` "DNS rebinding protection"), `daemon config
get/set/unset` and removed launch flags (`cli/src/commands/daemon/{config,local-daemon}.ts:82-110`),
health endpoint exempt. Stricter: refuse non-loopback without a password (Paseo warns,
`docker/base/rootfs/usr/local/bin/paseo-docker-entrypoint:60-65`); expiring session tokens
rather than the standing credential; `/api/token` closed to non-loopback callers (Paseo has no
such endpoint). Different transport detail: Paseo sends the password in the WS `hello`; we log in
over HTTP first because our upgrade authenticates by token (`internal/wsapi`).

### P5 — ★ first-run and connection onboarding (macOS + Windows)

1. **Settings file.** `app-settings.json` (`onboardingCompleted`, `manageLocalDaemon`) is
   read/written by two new commands (`app_settings_get`, `app_settings_set`), both on the
   capability allowlist; a corrupt file falls back to "first run" without crashing.
2. **Upgrade rule.** Absent settings + existing `connections.json` or managed daemon record →
   `{true, true}` is written and no first-run appears; macOS auto-start behaves exactly as
   v0.11.1.
3. **No silent install.** On a fresh install nothing is installed or started until the user
   chooses; `auto_start` is a no-op while `onboardingCompleted == false`.
4. **Screen.** First-run shows (a) *Run smind on this computer*, (b) *Connect to a daemon*,
   (b2) *Connect over SSH*, (c) *Pair via relay link*, and (d) *Use the daemon already running
   here* when one is detected; "Skip for now" leaves first-run pending and installs nothing. On
   Linux (deb) (a) is replaced by guidance to run `smind service install`. The screen leaves by
   itself once a choice succeeds (Paseo's redirect-when-online).
5. **Pre-selection.** Port free → (a); a smind daemon answering and not managed by the app →
   (d) selected and (a) disabled with the reason; anything else answering or the port occupied →
   no pre-selection, (a) disabled with a port-conflict message.
6. **(a)** calls the same `daemon_install` path as Settings → Daemon → Install (no new install
   code), then sets `manageLocalDaemon=true`, selects `local`, and enters the app only after
   `/healthz` answers; an install error stays on the screen with Retry and the other choices
   usable.
7. **(b)** validates the URL, probes via `connections_probe(url)` (`{reachable, version,
   service, authRequired}`) and shows the result before saving; when `authRequired` a
   **password** field appears and the app logs in through P4's `/api/login` before saving
   (the password is never stored; the session token is held in Rust only); a non-smind endpoint
   is refused; with a loopback-bound daemon the screen explains the situation and points to (b2)
   and (c).
8. **(b2)** takes host (+ ssh port, daemon port), validates them (rejects empty, whitespace, and
   hosts starting with `-`; ports 1–65535), spawns the system `ssh -N -L
   127.0.0.1:<free-port>:127.0.0.1:<daemon-port> -- <host>` with `BatchMode=yes` and a connect
   timeout, saves a URL connection to the local end, shows ssh's stderr on failure, and tears the
   tunnel down when the connection is removed or the app quits; if `ssh` is missing it says so.
9. **(c)** uses P3's component unchanged (no second implementation).
10. **(d)** connects to the local daemon with `manageLocalDaemon=false`; the app never installs,
    starts, updates or takes over afterwards; Settings → Daemon shows read-only status and an
    "Enable app-managed daemon" action.
11. **No local daemon mode.** With `manageLocalDaemon=false` `auto_start` never runs, the Daemon
    section's mutating actions are disabled with an explanation, switching to `local` installs
    nothing, and the app is fully usable with only remote/relay/SSH connections.
12. **Settings → Connections** keeps add/switch/remove with multiple connections, gains one
    **Add connection** menu (URL / SSH / relay link, same components as first-run) and a **Manage
    this computer's daemon** toggle with a confirmation before turning it off while the app owns a
    running daemon (persist the setting *before* any stop). **Run setup again** is reachable from
    the Help/app menu and changes no saved connection.
13. **Take-over safeguard (bug fix).** On macOS, Take over management is refused unless the port
    owner's image is a `smind` executable (parity with Windows); an `ssh` tunnel owner is never
    adopted.
14. **Later launches.** `manageLocalDaemon=true` and `local` selected → `auto_start` as today on
    every launch; otherwise nothing starts. The daemon keeps running after the app quits.
15. Light and dark screenshots of the first-run screen (each pre-selection state, and the
    entry/exit transition frames) and of Settings → Connections were looked at on the real macOS app
    and on Windows 11, using whichever install path the bundled-daemon branch has merged by then.

**Paseo reference — ported (toggle, methods, SSH, confirmation), deliberately different
(first-run, quit).** Ported: the persisted `manageBuiltInDaemon` toggle with a confirm and
persist-before-stop ordering (`packages/app/src/desktop/settings/desktop-settings.ts:36-39`,
`desktop/daemon/daemon-management-toggle.ts`); the welcome methods direct / paste link / SSH
(`components/welcome-screen.tsx`, `add-host-modal.tsx`, `add-host-method-modal.tsx`); the
`remoteSsh` host that tunnels to an already-running daemon and never starts one
(`types/host-connection.ts`, `packages/cli/src/ssh/ssh-tunnel.ts`,
`packages/protocol/src/ssh-transport.ts` — host/port validation); redirect once a host is online
(`welcome-screen.tsx` `useAnyHostOnline`). Different: Paseo's desktop **silently auto-starts** the
built-in daemon (`app/_layout.tsx:362-370`) and shows Welcome only when no host exists; we show an
explicit first-run (decision 22, alternative = Paseo parity). Paseo **stops the daemon on quit**
(`keepRunningAfterQuit: false`, `packages/desktop/src/daemon/quit-lifecycle.ts:56`); ours keeps
running (ADR-0013). Ownership is the app-side `ManagedRecord` (`managed.rs:43-51`) plus `/healthz
managedBy`, where Paseo uses the daemon's lock file (`desktopManaged`, `pid-lock.ts`;
`daemon-manager.ts:189-194`).

### P6 — `smind update`

1. `smind update --check` prints current and latest and exits 0 whether or not an update exists;
   `--json` emits `{current, latest, updateAvailable}`.
2. `smind update` downloads `smind_<v>_<os>_<arch>.tar.gz|zip` and `checksums.txt`, verifies
   SHA-256, extracts only the `smind`/`smind.exe` member, runs the new binary's `version` and
   compares it to the target before swapping; any failure leaves the installed binary
   byte-identical.
3. Refuses (non-zero, one-line reason, nothing changed): runs running or a permission pending
   (override `--force`); current version is `dev`; the running binary is inside the desktop app
   bundle / managed-daemon directory; target older than current without `--force`; target
   directory not writable (names the cause; no self-elevation); running inside a container.
4. Active runs are detected via the local daemon's `run.list`; a daemon that is not running means
   "nothing to protect".
5. Unix swap is atomic (`rename`, `smind.old` kept until the post-swap check passes); Windows
   renames the running `smind.exe` aside to `smind.exe.old`, moves the new file in and removes
   `.old` on the next start or update; a failed post-swap check rolls back.
6. When a service-managed daemon exists the update restarts it through `smind service restart` and
   succeeds only after `/healthz` reports the new version within 30 s, otherwise rolls back and
   restarts the old binary; a hand-started daemon is never respawned (instructions printed).
   (Exercised through an injected restarter until P8 lands.)
7. `--version <v>` installs that release (downgrade needs `--force`); `--dry-run` performs
   everything except the swap; `SMIND_UPDATE_BASE_URL` (test seam) redirects downloads and is
   documented as test-only.
8. Latest-version resolution uses the `releases/latest` redirect, not the GitHub API, and honours
   `HTTPS_PROXY`.

**Paseo reference — deliberately different, lifecycle ideas ported.** Paseo has no updater (npm
upgrade; Docker by pulling a tag; `README.md:94-100`, `docs/docker.md`). Ported ideas: graceful,
in-band stop/restart rather than killing (`packages/cli/src/commands/daemon/{stop,restart}.ts`
→ ours `POST /api/shutdown`) and **confirming the replacement is a new identity that is serving**
before reporting success (`restart.ts:57-110` compares worker pid and serverId → ours: `/healthz`
version == target, with rollback).

### P7 — release signing + CLI verification

1. The `publish` job signs `checksums.txt` with the existing minisign key
   (`TAURI_SIGNING_PRIVATE_KEY`) and uploads `checksums.txt.minisig`; on a real release a missing
   key fails the job (ADR-0023 §4), on a dry run it is skipped with a warning.
2. `smind update` downloads the `.minisig` and verifies it against the embedded desktop-updater
   public key before trusting `checksums.txt`; a bad or missing signature aborts with nothing
   changed; there is no flag to skip it.
3. The embedded key equals `plugins.updater.pubkey` in `tauri.conf.json` (a test fails on
   divergence).
4. Releases published before P7 are not updatable *to* by a P7 binary (documented): the verifier
   reports "release has no signature" rather than falling back to checksum-only.

**Paseo reference — deliberately different.** Paseo relies on npm package integrity and signed
desktop updater bundles; it has no equivalent signature over CLI/daemon downloads. Nothing to
port; we reuse our own desktop-updater key (ADR-0023) so there is one rotation story.

### P8 — ★ `smind service` (systemd, launchd), `managedBy`, daemon log file

1. `smind service install|uninstall|status|start|stop|restart|logs` exist with the flags in
   ADR-0031 D2 (`--system`, `--home`, `--path`, `--no-start`, `-f/-n` for logs); there is no
   `--port`/`--listen` (configuration is `smind config`, P4).
2. Linux: user unit by default; the rendered unit has `ExecStart=<abs> serve`,
   `Restart=on-failure`, `RestartSec=5`, `TimeoutStopSec=30`, the captured `PATH`, `SMIND_HOME` only
   when set, `SMIND_SUPERVISOR=systemd`, no sandboxing directives; `--system` writes
   `User=<SUDO_USER>` and refuses plain root without `SUDO_USER`; install prints the linger command
   when lingering is off.
3. macOS: LaunchAgent plist has `RunAtLoad`, `KeepAlive={SuccessfulExit:false}`,
   `EnvironmentVariables` with the captured `PATH` and `SMIND_SUPERVISOR=launchd`; `smind stop`
   leaves the daemon stopped, `kill -9` restarts it.
4. `install` is idempotent; refuses to *start* (but still enables) when a foreign daemon answers on
   the port; never kills a process; `uninstall` never touches `SMIND_HOME`.
5. `status` output and exit codes (0 running / 3 stopped / 4 not installed) match the ADR and report
   daemon vs binary version.
6. `/healthz` gains `managedBy` (absent when `SMIND_SUPERVISOR` is unset); existing fields unchanged.
7. **Daemon log file:** `smind serve` also writes `$SMIND_HOME/daemon.log`, rotated at 10 MiB keeping
   5 files (configurable via `smind config`), mode `0600`, still logging to stderr; `smind service
   logs [-f] [-n N]` tails it on every OS; no per-OS log redirection exists in the units.
8. `install.sh --service` installs then runs `smind service install` (`--system` passes through); it
   fails clearly on an OS without a backend.
9. `docs/homelab.md` documents systemd user vs system, linger, the macOS LaunchAgent limit (needs a
   logged-in GUI session), where logs live, and how the desktop app treats a service-managed daemon.

**Paseo reference — deliberately different (service installer), ported (log file, status).**
Paseo has **no OS-service installer**: a background launch with an in-process **supervisor** that
restarts the worker (`packages/server/scripts/supervisor.ts`; `supervisor-entrypoint.ts:170`
`restartOnCrash: true`) and `daemon run` for foreground use under your own unit
(`packages/cli/src/commands/daemon/{start,local-daemon}.ts`). We keep `smind service` (Human asked
for boot persistence; the OS manager is our supervisor; `smind serve` = their `daemon run`).
Ported: the daemon-owned **rotating `daemon.log`** (`supervisor.ts:15-16,128-129`,
`supervisor-log-config.ts:28-30`, `daemon-instance.ts:51-55`) and status-as-observation
(`status.ts`). Adapted: daemon-recorded ownership (`pid-lock.ts` `desktopManaged`) → env-derived
`/healthz managedBy`. Not ported: `daemon start` (decision 26).

### P9 — `smind service` Windows + `install.ps1 -Service`

1. A per-user Scheduled Task `smind` runs a hidden `smind.exe serve` at logon (no console window),
   restarts on failure (3× at 1-minute intervals); output goes to the daemon's own `daemon.log`
   (P8), not a shell redirect.
2. `start`/`stop`/`restart`/`status`/`logs`/`uninstall` behave as in P8; `stop` uses `POST
   /api/shutdown` (ADR-0029) first and `schtasks /End` only if the daemon does not exit within the
   stop timeout.
3. Registration does not require elevation (verified on Windows 11); if it does on some setups the
   error says exactly which step needs it.
4. `SMIND_SUPERVISOR=task` is set; the hidden-launch wrapper works on Windows 10 and 11 (spike
   result recorded in Validation, with the fallback used if `conhost --headless` is unavailable).
5. `install.ps1 -Service` installs then registers the task.
6. Docs state plainly that the task starts at logon, not at boot.

**Paseo reference — deliberately different.** Paseo has no Windows service/task story either
(CLI via npm; the desktop app starts the daemon and its lock file marks `desktopManaged`,
`pid-lock.ts`, `daemon-manager.ts:83-84`). ADR-0030 already rejected a service for the app; the
per-user task is the minimal boot/logon-persistence answer for the CLI. Nothing to port.

### P10 — "older than app" notice, service recognition, `daemon.info`

1. For a non-managed connection (url, relay, or service-managed local) whose daemon is older than
   the app, the banner shows the update command in a code block with a **Copy** button: `smind
   update` when the daemon's version is at least the first release that has the command, otherwise
   the installer one-liners (`curl -fsSL https://spacingmind.com/install.sh | sh` and
   `irm https://spacingmind.com/install.ps1 | iex`); a Docker daemon (reported via `daemon.info`)
   is told to `docker pull`.
2. Copy puts exactly the command on the clipboard and the button confirms ("Copied") without moving
   focus.
3. `daemon.info` (WS) returns `{version, commit, image?}`; the UI reads it over `/ws` for relay
   connections, so the notice shows for relay daemons; a relay daemon that does not know the method
   shows "version unknown" plus the installer commands and never claims "up to date".
4. A managed local daemon keeps its **Update & restart** button and shows no copy block (no
   regression to the existing banner tests).
5. A service-managed local daemon (`/healthz managedBy`) shows "managed by <systemd|launchd|Task
   Scheduler>", hides **Take over management**, and (with P5's setting) never offers (a) while the
   service owns the port.
6. Light/dark screenshots of the banner in all four states were looked at.

**Paseo reference — adapted.** Paseo learns the daemon version over the existing client connection
(WS `server_info`, `getDaemonStatus`; `pair.ts` gates on `features.daemonStatusRpc` and answers
"Update the Paseo daemon…"), so relay hosts need no separate probe — our `daemon.info` is the same
idea. The copyable command is ours (Human's request).

### P11 — ★ Docker image

1. A `docker` job in `release-please.yml` runs after `publish` on real releases, builds
   `linux/amd64` + `linux/arm64` from the release's own linux binaries (no Go build in Docker), pushes
   `ghcr.io/spacingmind/smind:<version>`, `:<major>.<minor>`, `:latest` with provenance/SBOM
   attestations; on `workflow_dispatch` it builds without pushing. A separate `docker-check.yml` builds
   (no push) on pull requests that touch Docker files.
2. The image runs the daemon as non-root `smind` (uid/gid 1000) via a **root entrypoint that creates/
   chowns `/data` and `/home/smind` when root-owned, then drops privileges with `gosu`** (and just runs
   when started non-root); `tini` is PID 1; `HOME=/home/smind`, `SMIND_HOME=/data`, `VOLUME /data`;
   contains `git`, `openssh-client`, `ca-certificates`, `curl`, `gosu`, Node 22.
3. The image sets `SMIND_LISTEN=0.0.0.0:4648` and **exits with an error when `SMIND_PASSWORD` is
   unset**; with a password, `docker run -p 4648:4648` works and the web UI at `http://<host>:4648`
   shows the login screen.
4. `HEALTHCHECK` on `http://127.0.0.1:4648/healthz` turns `healthy` within 30 s of start.
5. State persists: stop/remove/recreate with the same `/data` volume keeps workspaces, tokens,
   accounts and the relay config.
6. `docker exec <c> smind relay connect …` persists the relay config in `/data` and a restarted
   container dials the relay again.
7. `smind update` inside the image refuses with "update by pulling a new image".
8. `docker/docker-compose.example.yml` and `docker/Dockerfile.agents.example` ship and are built/linted
   in `docker-check.yml`; `docs/homelab.md` Docker section states the limits (agent CLIs not included +
   the child-image example that was actually built, browser OAuth caveat as tested, `/workspaces`
   mount, no example password like `change-me`).

**Paseo reference — ported, with three deliberate differences.** Ported from `refs/paseo/docs/docker.md`,
`docker/base/Dockerfile`, `docker/base/rootfs/usr/local/bin/paseo-docker-entrypoint`,
`docker/docker-compose.example.yml`, `docker/Dockerfile.agents.example` and
`.github/workflows/docker.yml`: multi-arch ghcr.io image, `0.0.0.0` listen + published port +
password, bundled web UI, agent CLIs not baked in with a child-image example, state/home vs workspace
volumes, root-entrypoint ownership fix then `gosu`, `tini`, non-publishing PR build, publish on stable
tags. Different: (1) built from the release binaries, not from source in Docker; (2) refuses to start
non-loopback without a password (Paseo warns); (3) no `change-me` example.

### P12 — ★ relay protocol v3 (WebSocket): spec, conformance suite, Go relay, multi-device

1. `docs/relay-protocol-v3.md` is the normative spec (endpoints, roles, messages, admission, limits, errors,
   versioning) and matches ADR-0031 D9; it names the relay host (not "workspace") throughout.
2. `relay/protocol/` holds the shared definition (`relay_v3.proto`, golden vectors for SCRAM proofs, offers and
   tokens, and the language-neutral conformance scenarios); `relay/conformance/` is the ONE black-box runner
   (`go run ./relay/conformance --base <url> [--open-enrolment]`) that works against any v3 relay. A new
   `relay-ci.yml` runs it on pull requests that touch `relay/**`, `internal/relay/**` or `relay-worker/**`.
3. The Go relay serves v3 on one TLS listener; gRPC v2 and the grpc-web listener keep running unchanged beside
   it (flag `--v2`, default on) for the deprecation window (decision 31).
4. Admission v3: `POST /v3/admit/challenge` and `POST /v3/admit` (ADR-0011 SCRAM math, shared golden vectors
   with the Rust and TS implementations) return a stateless admission token valid 24 h; the token is accepted
   only in `Sec-WebSocket-Protocol`, never in the URL; challenge nonces are single-use with a 30 s TTL; a
   wrong proof and an unknown host id are indistinguishable.
5. Multi-device: `role=control` (one per host), `role=data` (one per device), `role=device` (many); the relay
   assigns `device_id`; control carries `device_connected`, `device_disconnected`, `sync{device_ids}` on every
   control (re)connect; a reconnecting device replaces only its own previous socket; closing one device closes
   only its data socket; frames are buffered until the daemon's data socket exists, bounded by 200 frames and
   4 MiB per side; ≤ 16 devices per host and ≤ 1 MiB per message (configurable on a self-hosted relay).
6. Enrolment on a self-hosted relay: `POST /v3/hosts` is **disabled by default** (403 with a hint to use
   `smind relay host new` or `--open-enrolment`); `POST /v3/hosts/<id>/rotate` (authenticated by an admission
   proof) replaces the secret and invalidates old offers and tokens; `DELETE /v3/hosts/<id>` removes a host.
7. TLS: a self-hosted relay keeps its self-signed certificate and prints its fingerprint; `--tls-cert/--tls-key`
   lets an operator supply a CA certificate.
8. `GET /v3/info` returns `{version, protocols}` and `GET /v3/healthz` returns 200; a path for an unsupported
   protocol version returns 404.
9. ADR-0007 (b)/(f) and ADR-0011 get a one-line pointer to ADR-0031 D9/D11.

**Paseo reference — ported (shape), adapted (admission, framing).** `refs/paseo/packages/relay/src/
cloudflare-adapter.ts:119-127,340-395`, `packages/server/src/server/relay-transport.ts:49-55,121-130`,
`cloudflare-adapter.test.ts:193-229` (`clt_multi`), `protocol/src/daemon-endpoints.ts`
(`buildRelayWebSocketUrl`): control/data/client roles, relay-assigned `connectionId`, `sync`/`connected`/
`disconnected`, buffering until the data socket exists. Adapted: SCRAM admission and enrolment in front, protobuf
frames, admission token in a subprotocol. New here: the conformance suite (Paseo's relay has a Worker
adapter and its own tests, not a cross-implementation suite).

### P13a — ★ relay v3 clients: Go daemon side (bridge, `relay connect`, offer)

1. The daemon bridge speaks v3: one `control` socket, one `data` socket and one E2EE session per announced
   device; the fixed `m1-default-*` ids are gone for v3 (v2 code path remains for the deprecation window).
2. **CA-verified mode:** an empty fingerprint means verify chain + hostname against the system roots; a
   non-empty fingerprint pins exactly that certificate; `relay.Dial`'s "empty fingerprint refused" rule
   (`internal/relay/client/client.go:37-40`) is replaced accordingly.
3. `smind relay connect <address> <host-id> <secret>` first tries normal verification (CA mode, stores an empty
   fingerprint); if that fails it shows the certificate fingerprint and asks for confirmation (or takes
   `--fingerprint <hex>`); silent trust-on-first-use is gone; the confirmation says what is being trusted.
4. `smind relay offer` emits a v3 offer with a single `wss://host[:port]` relay address (offer key `ws` still
   carries the host id; no port derivation); QR/PNG behaviour from P2 is unchanged.
5. When a daemon password is set (P4), a device must present it inside the E2EE hello; missing/wrong → rejected
   before any session data; with no password set the daemon logs a one-line warning per start.
6. A daemon with a v2 config keeps working until the window closes; `relay connect` always writes v3 config.

### P13b — ★ relay v3 clients: desktop (Rust)

1. The Rust relay client speaks v3 over WebSocket (rustls): pinned certificate or system/webpki roots per the
   offer; relay-assigned device id; resume after a transport drop; no eviction ping-pong with other devices.
2. `connections_add_relay` accepts v3 offers (single `wss://` address) and still accepts v2 offers during the
   window; `connections_preview_relay` (P3) shows `pinned` vs `caVerified`; the "native = grpc-web − 1" derivation
   is used only for v2 offers.
3. When the host requires the daemon password, "Add via relay link" asks for it (never stored, held in Rust only)
   and sends it in the E2EE hello.
4. Two desktop apps, or a desktop and a mobile device, stay attached to one relay host simultaneously.

### P13c — ★ relay v3 clients: mobile (TypeScript)

1. The mobile relay client speaks v3 over React Native's WebSocket with the `smind.relay.v3` subprotocol and the
   admission token; grpc-web framing is not used for v3; relay-assigned device id; password in the E2EE hello.
2. Pairing parses v3 offers and still parses v2 offers during the window; the unused `relayFingerprint` is
   documented as "CA-verified relays only on mobile" in the pairing screen help.

### P14 — ★ Cloudflare Worker relay (`relay-worker/`, in the monorepo)

1. `relay-worker/` is a self-contained TypeScript package (own `package.json`, `wrangler.toml`, tests; a bun
   workspace member) with a Worker, a `RelayHost` Durable Object (SQLite-backed, Hibernation API, per `host_id`)
   and a `Registry` object. It implements protocol v3 from `relay/protocol/` and **passes the same conformance
   suite as the Go relay**, run in CI against the Worker under `workerd`/Miniflare (`wrangler dev`), for every
   non-enrolment scenario.
2. The 30 s control ping is answered with `setWebSocketAutoResponse` (no wake); idle hosts hibernate; the host
   record and challenge nonces survive object eviction; the admission token (HMAC with `RELAY_SIGNING_KEY`)
   verifies after eviction; in-memory frame buffers are allowed to be lost on eviction and clients recover.
3. Message ≤ 1 MiB, ≤ 16 devices, buffer ≤ 200 frames and 4 MiB per side are enforced in the Worker too.
4. `GET /v3/info` reports `{version, commit, protocols: [3]}` (package version + commit SHA, **not** the smind
   release); `GET /v3/healthz` returns 200.
5. Unit tests and the conformance run happen in `relay-ci.yml` on pull requests; no Cloudflare credentials are
   needed for CI.
6. The directory is self-contained (no imports from outside `relay-worker/` and `relay/protocol/`) so it can be
   split into its own repository later; a test fails if an import escapes those two directories.

**Paseo reference — ported (Worker + Durable Object + hibernation), adapted (SCRAM admission, registry, caps).**
`refs/paseo/packages/relay/src/cloudflare-adapter.ts` (614 lines; `RelayDurableObject`, hibernation, v2 roles,
control nudging `:219-250`), `wrangler.toml` (DO binding, `new_sqlite_classes`, observability). The TypeScript
port is roughly that size plus admission and limits.

### P15 — ★ open enrolment and abuse limits (Worker + Go parity)

1. `POST /v3/hosts` creates a relay host and returns `{host_id, secret}` once; `host_id` is server-generated
   (`h_` + 26 base32 chars); only `StoredKey` is stored; the response is never cached or logged.
2. Per-IP limits: ≤ 5 enrolments/hour and ≤ 20/day, ≤ 8 concurrent connection attempts; over the limit → 429 with
   `Retry-After`. The client IP comes from the platform's trusted header only (`CF-Connecting-IP` on the Worker).
3. Global cap 50,000 hosts via the registry; at the cap enrolment returns 503 with a clear message.
4. GC by Durable Object alarm: host never admitted within 24 h → deleted; no admission for 90 days → deleted.
5. Per-host accounting: sustained > 1 MiB/s is throttled; a soft 20 GB/month cap rejects new frames with a
   clear close code; counters survive eviction.
6. `rotate` and `DELETE` work against the Worker; an operator ban (`RELAY_ADMIN_TOKEN`-protected admin endpoint)
   disables a host id immediately; banned/deleted hosts get the same response as unknown ones.
7. The Go relay's `--open-enrolment` implements the same endpoint and the same per-IP limits (configurable), so
   the conformance suite passes on both with `--open-enrolment`.

**Paseo reference — different.** Paseo's relay is open: no enrolment, no per-host secret, session id =
`serverId`, so none of this exists there (`SECURITY.md`, "Relay threat model"); enrolment and quotas are new
because our relay authenticates hosts (ADR-0011) and is now shared.

### P16 — Cloudflare deploy and ops (BLOCKED on Human)

1. `relay-worker/wrangler.toml` defines production (`relay.spacingmind.com`, `custom_domain = true`) and
   staging (`relay-staging.spacingmind.com`) environments, the Durable Object bindings, the `new_sqlite_classes`
   migration and observability.
2. `.github/workflows/deploy-relay.yml` is **separate from release-please** and triggers only on pushes to
   `develop` that touch `relay-worker/**` or `relay/protocol/**`, and on `workflow_dispatch`. The job runs unit
   tests and the conformance suite against `wrangler dev`, then declares `environment: relay-production` (required
   reviewers: Human approves each deploy) and runs `wrangler deploy`, syncing `RELAY_SIGNING_KEY` and
   `RELAY_ADMIN_TOKEN` with `wrangler secret put`. The Cloudflare token is available only inside that environment;
   no other workflow or PR build can read it.
3. A manual dispatch with `target=staging` deploys to `relay-staging.spacingmind.com` using the optional
   `relay-staging` environment; afterwards a smoke step runs the conformance suite against
   `wss://relay-staging.spacingmind.com`.
4. A deploy for which the environment/secrets are missing fails with a clear message and changes nothing; the
   repository builds, tests and merges without any Cloudflare credentials.
5. `docs/relay-ops.md` is the runbook: how to create the environments, reviewers and token (exact scopes), deploy,
   approve, `wrangler rollback`, secret rotation (`openssl rand -hex 32`), ban/unban, reading Workers logs,
   cost/usage alerts, and the Human-owned items (billing alert, uptime check, status page); and how to split
   `relay-worker/` into its own repository later.
6. `GET /v3/info` after a deploy reports the deployed commit.

**Blocked on Human (do not start without these):** the GitHub Environments `relay-production` (required
reviewers) and optionally `relay-staging`; environment secrets `CLOUDFLARE_API_TOKEN` (Account → Workers Scripts:
Edit, Account → Account Settings: Read; Zone `spacingmind.com` → Workers Routes: Edit, DNS: Edit — verify the list
in the dashboard), `CLOUDFLARE_ACCOUNT_ID`, `RELAY_SIGNING_KEY`, `RELAY_ADMIN_TOKEN`; upgrading the Cloudflare
account to **Workers Paid**; confirming the zone; confirming the repository plan supports required reviewers; a
billing usage alert (decision 27); optionally an uptime-check/status-page account.

**Paseo reference — ported (monorepo layout, workflow), adapted (gating).** `refs/paseo/packages/relay` lives in
Paseo's monorepo with `.github/workflows/deploy-relay.yml` (typecheck, then `wrangler deploy` with
`CLOUDFLARE_API_TOKEN`) and `packages/relay/wrangler.toml` (custom-domain route) — the precedent for keeping the
Worker beside the rest of the code. Different: ours is path-filtered, runs the conformance suite first, deploys
through a reviewer-approved GitHub Environment, and has a staging target; Paseo's workflow is manual-only
(`workflow_dispatch`) while its traffic is cut over to Fly.

### P17 — hosted relay as the default (`smind relay connect` with no address)

1. `smind relay connect` with no arguments shows the consent line, then enrols on `relay.spacingmind.com`
   (`POST /v3/hosts`), stores `{address, host_id, secret}` with an empty fingerprint (CA mode) at mode 0600, and
   prints "registered this daemon with the relay; every paired client sees all of your workspaces".
2. It **refuses to finish without a daemon password** (offers to run `smind set-password` first) because the
   offer is the secret (decision 14); it works with a self-hosted address exactly as before.
3. `smind relay host rotate` / `rm` operate on the hosted host (authenticated by an admission proof) and
   invalidate old offers.
4. `docs/homelab.md` documents the hosted path; `docs/relay-privacy.md` states what the relay sees (IPs, timing,
   host ids, sizes), retention, and that there are no accounts — the source text for the landing page.
5. The production default is switched on only after the privacy page is live (a landing-repo follow-up) and
   P16's first approved production deploy has run (blocked on Human).

**Paseo reference — adapted.** Paseo's `daemon pair --relay` enables its hosted relay after an explicit consent
prompt (`packages/cli/src/commands/daemon/pair.ts:106-109`; relay off by default,
`packages/server/src/server/config.ts:302-307`) and `onboard.ts` runs the same choice during first-time setup;
ours is the same opt-in plus the password requirement.

## Test Scenarios

Names are the exact test/case names the implementer must create (Go `TestX`, shell `test_x`, TS `it("…")`,
Rust `fn x`). A scenario marked **(real)** needs a real platform and is recorded in Validation, not in CI.

### P0

- `TestRelayHostNewAliasesWorkspaceNew`, `TestRelayWorkspaceAliasPrintsDeprecationToStderr`, `TestRelayHostLsMatchesWorkspaceLs`,
  `TestRelayHostRmRemovesStoreEntry`, `TestRelayHostRotateInvalidatesOldSecret`, `TestRelayHelpSaysRelayHostAndAllWorkspaces`
  (exact sentence), `TestNoUserVisibleStringCallsRelayRecordAWorkspace` (greps CLI help and UI strings),
  `TestOfferWireFormatUnchanged` (the existing pairing fixture, byte for byte).

### P1

- `test_version_flag_wins_over_env`, `test_help_prints_usage`, `test_msys_prints_powershell_oneliner` (fake
  `uname -s` = `MINGW64_NT`), `test_checksum_mismatch_installs_nothing`, `test_unknown_flag_exits_2`.
- Pester (or PowerShell script): `Select-Arch_x64PowerShellOnArm64Os_ReturnsArm64`,
  `Install_ChecksumMismatch_LeavesExistingExe`, `Install_ZipWithPathTraversal_Rejected`,
  `Install_AddsUserPathOnce`, `Install_NeverTouchesAppManagedDir`, `Install_WarnsWhenGitMissing`.
- **(real)** `install.sh` on Linux + macOS and `install.ps1` on Windows 11 against a real release;
  `smind --version` prints the version.

### P2

- `TestRelayOfferURLOnlyOnStdout` (golden; QR on stderr), `TestRelayOfferQRFallsBackToLevelL` (the 432-byte
  offer, decoded with the `qr_decode_test.go` helper), `TestRelayOfferTooLongPrintsNoteAndExitsZero`,
  `TestRelayOfferPNGMode0600AndNoOverwrite`, `TestRelayOfferWording` ("desktop app", "treat this URL like a
  password", E2EE consent line), `TestQRTextLevelFallbackBoundary` (412 and 413-byte inputs).

### P3

- UI: `it("previews a pasted relay link and shows host, workspace and fingerprint without the secret")`,
  `it("adds the connection only after Add is pressed")`, `it("Cancel adds nothing")`,
  `it("warns and defaults to Cancel when the same workspace returns with a different key or relay")`,
  `it("clears the pasted link after success, failure and unmount")`,
  `it("rejects a pairing link pasted into the URL form and points to Add via relay link")`,
  `it("shows the Rust error inline for a malformed link")`, `it("is not registered in a non-desktop build")`.
- Rust: `connections_preview_relay_returns_no_secret_fields`, `connections_preview_relay_rejects_wrong_version`,
  `connections_preview_relay_flags_changed_key`, capability test `proxy_capability_lists_preview_relay`.
- **(real)** local `smind relay` + `smind serve` + `relay connect/offer`, paste into the real desktop app, add,
  select, see the workspace list; light+dark screenshots.

### P4

- Go config/CLI: `TestConfigSetGetUnsetRoundTripKeepsComments`, `TestConfigSetRejectsUnknownKey`,
  `TestSetPasswordRejectsShort`, `TestSetPasswordWritesBcryptMode0600`, `TestSetPasswordNonTTYNeedsFlag`,
  `TestServeFlagsStillRejected`.
- Listen/auth: `TestServeLoopbackDefaultUnchanged`, `TestServeRefusesNonLoopbackWithoutPassword`,
  `TestEnvOverridesConfig` (`SMIND_LISTEN`, `SMIND_PASSWORD`, `SMIND_HOSTNAMES`).
- Token gating (adversarial): `TestAPITokenLoopbackOK`, `TestAPITokenForbiddenForNonLoopbackRemoteAddr`,
  `TestAPITokenForbiddenWhenHostIsExternal`, `TestAPITokenForbiddenWithForwardedHeaders`.
- Login: `TestLoginWrongPasswordConstantTime401`, `TestLoginRateLimitBackoff429`,
  `TestLoginReturnsSessionNotDaemonToken`, `TestSessionTokenExpires`, `TestWSAcceptsSubprotocolBearer`,
  `TestWSRejectsSessionTokenInQueryForNonLoopback`, `TestLoopbackQueryTokenStillWorks`.
- Host allowlist: `TestHostAllowlistDefaults`, `TestHostAllowlistLeadingDotMatchesSubdomains`,
  `TestHostNotAllowed403`, `TestCheckOriginSameOriginOnly`, `TestHealthzAuthRequiredField`.
- UI: `it("shows the login form when the daemon requires a password")`, `it("does not show it on loopback")`,
  `it("keeps the session token out of localStorage after the chosen lifetime")`.
- **(real)** from another machine on the LAN: login in a browser, run a task; wrong password lockout;
  `curl` through nginx on the same host cannot read `/api/token`.

### P5

- Rust: `app_settings_missing_file_means_first_run`, `app_settings_corrupt_file_falls_back_to_first_run`,
  `upgrade_rule_existing_connections_marks_onboarded`, `upgrade_rule_existing_managed_record_marks_onboarded`,
  `auto_start_noop_until_onboarded`, `auto_start_noop_when_manage_local_daemon_false`,
  `auto_start_runs_when_managed_and_local_selected` (extends the `launch_decision` table),
  `take_over_refused_for_non_smind_owner_on_macos`, `take_over_allowed_for_smind_owner_on_macos`,
  `connections_probe_reports_reachable_version_service_and_auth`, `connections_probe_rejects_non_smind_healthz`,
  `ssh_host_starting_with_dash_rejected`, `ssh_host_with_whitespace_rejected`, `ssh_port_out_of_range_rejected`,
  `ssh_tunnel_args_use_batchmode_and_double_dash`, `ssh_tunnel_closed_when_connection_removed`,
  `ssh_missing_binary_reports_clearly`, capability test for the new commands.
- UI (`first-run.test.tsx`): `it("preselects Run here when the port is free")`,
  `it("preselects Use the running daemon and disables Run here when a smind daemon answers")`,
  `it("disables Run here with a port-conflict message when something else owns the port")`,
  `it("Skip for now installs nothing and leaves first-run pending")`,
  `it("Run here calls daemonInstall once, waits for healthz, then enters the app")`,
  `it("an install failure keeps the screen and the other choices usable")`,
  `it("Connect by URL probes before saving and shows the version")`,
  `it("asks for a password when the probe says authRequired and never stores it")`,
  `it("refuses a URL that is not a smind daemon")`, `it("explains loopback-only daemons and points to SSH and relay")`,
  `it("Connect over SSH validates the host before spawning anything")`,
  `it("Pair via relay link uses the shared AddRelayLink component")`,
  `it("Use running daemon sets manageLocalDaemon=false and never calls daemonInstall")`,
  `it("shows guidance instead of Run here on Linux")`.
- UI settings: `it("Manage this computer's daemon toggle confirms before disabling while the app owns a running daemon")`,
  `it("persists the setting before stopping")`, `it("Run setup again leaves saved connections untouched")`,
  `it("with manageLocalDaemon=false the daemon section disables install/update/restart/take-over")`.
- **(real, macOS + Windows 11):** (1) wipe app data → first-run appears, nothing running; (2) (a) → daemon up, app
  usable; (3) quit/relaunch → auto-start per setting, daemon survives quit; (4) start `smind serve` by hand first →
  (d) pre-selected, Connect works, Settings shows "not managed", no take-over side effects; (5) `ssh -L 4648:…`
  tunnel → Take over refused on macOS; (6) manage=false → relaunch, nothing starts; (7) relay link via (c); (8) SSH
  (b2) to a real host, kill ssh → connection shows unreachable, retry works; (9) direct URL + password against a P4
  daemon; light+dark screenshots of every state and transition frame.

### P6

- `TestUpdateCheckReportsLatestAndExitsZero`, `TestUpdateVerifiesChecksumAndInstalls` (httptest fixture via
  `SMIND_UPDATE_BASE_URL`), `TestUpdateChecksumMismatchLeavesBinaryIdentical`,
  `TestUpdateRejectsArchiveWithPathTraversal`, `TestUpdateRejectsMissingBinaryMember`,
  `TestUpdateRejectsOversizeMember`, `TestUpdateRefusesWhileRunsActive`, `TestUpdateRefusesWhilePermissionPending`,
  `TestUpdateForceOverridesActiveRuns`, `TestUpdateNoDaemonRunningIsFine`, `TestUpdateRefusesDevBuild`,
  `TestUpdateRefusesDesktopManagedPath`, `TestUpdateRefusesInsideContainer`,
  `TestUpdateRefusesDowngradeWithoutForce`, `TestUpdateRefusesUnwritableDir`,
  `TestUpdateAtomicSwapKeepsOldUntilVerified`, `TestUpdateRollsBackWhenNewBinaryVersionMismatches`,
  `TestUpdateWindowsRenameAsideAndCleanup` (build-tagged `windows`, existing Windows CI job),
  `TestUpdateRestartsServiceAndWaitsForHealthzVersion` (fake restarter),
  `TestUpdateRollsBackWhenHealthzNeverReportsNewVersion`, `TestUpdateDoesNotRespawnManualDaemon`,
  `TestUpdateLatestUsesRedirectNotAPI`, `TestUpdateHonoursHTTPSProxy`, `TestUpdateDryRunChangesNothing`.
- **(real)** Linux, macOS, Windows 11: `smind update --version <older>` then `smind update` against a real published
  release; Windows with a running `smind.exe` daemon.

### P7

- `TestVerifyChecksumsSignatureAccepts`, `TestVerifyRejectsTamperedChecksums`, `TestVerifyRejectsMissingMinisig`,
  `TestVerifyRejectsWrongKey`, `TestEmbeddedPubkeyMatchesTauriConf`, `TestUpdateRefusesReleaseWithoutSignature`.
- CI: a `workflow_dispatch` dry run on the PR branch produces `checksums.txt.minisig` that `minisign -V` accepts with the
  committed public key (recorded in Validation).

### P8

- `TestSystemdUserUnitGolden`, `TestSystemdSystemUnitGoldenUsesSudoUser`, `TestSystemdSystemRefusesPlainRoot`,
  `TestSystemdUnitOmitsSMINDHOMEWhenUnset`, `TestSystemdUnitCapturesPATH`, `TestLaunchdPlistGolden`,
  `TestServiceInstallIdempotent`, `TestServiceInstallRefusesToStartWhenForeignDaemonAnswers`,
  `TestServiceUninstallKeepsSMINDHOME`, `TestServiceStatusExitCodes`, `TestLingerHintWhenDisabled`.
- Logging: `TestDaemonLogFileRotatesAtSize`, `TestDaemonLogFileMode0600`, `TestServiceLogsTailsDaemonLog`,
  `TestStderrStillReceivesLogs`.
- Healthz: `TestHealthzIncludesManagedByWhenEnvSet`, `TestHealthzOmitsManagedByWhenUnset`.
- `test_install_sh_service_flag_runs_service_install` (shim `smind`).
- **(real, Linux with systemd and a macOS box):** install → reboot/logout cycle (linger) → daemon is up; `smind stop`
  stays stopped (launchd); `kill -9` restarts; `smind update` restarts via the service; `smind service logs` shows
  output; the desktop app (macOS) shows the daemon as service-managed with no Take over.

### P9

- `TestWindowsTaskXMLGolden` (logon trigger, restart-on-failure, hidden action), `TestWindowsTaskStopUsesShutdownEndpointFirst`,
  `TestWindowsTaskStopFallsBackToEnd`, `TestWindowsServiceStatusExitCodes`, `Install_Service_RegistersTask` (Pester).
- **(real, Windows 11, a spike first):** register without elevation; no console window at logon; kill the process →
  restarts; `smind service stop` is graceful; `daemon.log` grows; Windows 10 result recorded or marked unverified.

### P10

- UI (`desktop-daemon-banner.test.tsx`): `it("shows smind update with a Copy button for a url connection whose daemon has the command")`,
  `it("shows the installer one-liners for a daemon older than the update command")`,
  `it("shows docker pull for a daemon that reports it runs from an image")`,
  `it("copies exactly the displayed command to the clipboard")`, `it("shows the notice for a relay connection using daemon.info")`,
  `it("shows version unknown plus installer commands when a relay daemon lacks daemon.info")`,
  `it("keeps Update & restart for the managed local daemon and shows no copy block")`,
  `it("shows managed-by-service and hides Take over for a service-managed local daemon")`.
- Go: `TestDaemonInfoReturnsVersionCommitAndImageFlag`, `TestDaemonInfoIsReadableOverRelayChannel`.
- Rust: `classify_unmanaged_service_when_healthz_managed_by_present`.
- **(real)** pair a desktop app to an older (v0.11.1) relay-connected daemon and to a newer-than-daemon fake; screenshots light/dark.

### P11

- CI (`docker-check.yml` and a `workflow_dispatch` dry run): `build amd64+arm64 succeeds`, `container becomes healthy within 30s`,
  `smind --version equals the release version`, `runs as uid 1000 after root entrypoint`, `refuses to start with SMIND_LISTEN=0.0.0.0:4648
  and no SMIND_PASSWORD`, `login works through a published port`, `compose example validates (docker compose config)`,
  `agents Dockerfile example builds`.
- `test_image_chowns_root_owned_volume.sh`, `test_image_state_survives_recreate.sh`, `test_image_relay_config_persists.sh`,
  `test_image_update_refused.sh`.
- **(real)** pull the published `:<version>` on an amd64 and an arm64 host; run the docs' quickstart verbatim; verify the child-image
  example builds and `claude --version` runs inside.

### P12

- Go (`go test ./internal/relay/...` and the runner): `TestV3ServerPassesConformance`, `TestV2GRPCStillWorks`
  (existing suites unchanged), `TestV3TokenNeverAcceptedInURL`.
- Conformance scenarios (`relay/protocol/scenarios/`, run against every implementation): `admit_challenge_nonce_single_use`,
  `admit_wrong_proof_and_unknown_host_indistinguishable`, `admit_token_expires`, `token_in_url_rejected`,
  `two_devices_attach_concurrently`, `device_reconnect_replaces_same_device_only`, `control_sync_on_reconnect`,
  `control_announces_connected_and_disconnected`, `daemon_data_socket_per_device`, `buffer_until_daemon_attaches`,
  `buffer_bounded_by_frames_and_bytes`, `device_cap_16`, `message_over_1mib_rejected`, `devices_cannot_read_each_others_frames`,
  `closing_one_device_keeps_others`, `enrolment_disabled_by_default`, `enrolment_open_returns_secret_once`,
  `rotate_invalidates_old_secret_and_tokens`, `delete_host`, `unsupported_protocol_path_404`, `ping_answered`, `info_and_healthz`.
- Shared vectors: `scram_proof_matches_golden_vectors` (Go, Rust and TS each assert the same file).

### P13a (Go daemon)

- `TestBridgeV3ServesTwoDevicesIndependently`, `TestBridgeV3ClosingOneDeviceKeepsOthers`, `TestDialEmptyFingerprintUsesSystemRoots`,
  `TestDialNonEmptyFingerprintPinsExactly`, `TestRelayConnectTriesSystemRootsFirst`, `TestRelayConnectAsksBeforePinning`,
  `TestRelayConnectFingerprintFlag`, `TestRelayOfferV3SingleWSSAddress`, `TestRelayDeviceWithoutPasswordRejectedWhenPasswordSet`,
  `TestRelayDeviceWrongPasswordRejected`, `TestRelayDeviceNoPasswordConfiguredWarnsOnce`, `TestV2ConfigStillDials`.

### P13b (desktop Rust)

- `client_v3_uses_relay_assigned_device_id`, `client_v3_resumes_after_transport_drop_keeps_device_id`, `client_v3_pinned_vs_webpki_roots`,
  `two_desktop_clients_stay_attached` (interop harness against the Go relay), `add_relay_accepts_v3_and_v2_offers`,
  `preview_relay_reports_pinned_or_ca_verified`, `add_relay_password_never_returned_to_webview`.
- UI: `it("asks for the daemon password when the relay host requires it and never stores it")`.

### P13c (mobile TS)

- `RelayConnection v3 uses the subprotocol and admission token`, `RelayConnection v3 takes the assigned device id`,
  `RelayConnection v3 sends the password in the E2EE hello`, `pairing parses v3 and v2 offers`.
- **(real, P13a–c together)** daemon + desktop app + mobile (or two desktop apps) on one relay host, first on the Go
  relay and then on the staging Worker, all live for ≥ 10 minutes with no eviction; kill one, the others stay up.

### P14

- Vitest: `relay_host_record_survives_eviction`, `challenge_nonce_survives_eviction_and_is_single_use`,
  `admission_token_verifies_after_eviction`, `control_ping_uses_auto_response_without_waking`,
  `sockets_survive_hibernation_via_attachments`, `buffers_lost_on_eviction_degrade_gracefully`, `caps_enforced_in_worker`,
  `no_import_escapes_relay_worker_and_protocol_dirs`.
- CI: the conformance runner against `wrangler dev` passes every non-enrolment scenario (`relay-ci.yml`).

### P15

- Conformance (enrolment scenarios, run on the Worker and on the Go relay with `--open-enrolment`): `enrol_returns_server_generated_id_and_secret_once`,
  `enrol_per_ip_hourly_limit_429`, `enrol_per_ip_daily_limit_429`, `enrol_global_cap_503`, `concurrent_attempts_per_ip_capped`.
- Vitest/Go: `unused_host_gc_after_24h`, `idle_host_gc_after_90d` (fake clock / alarm), `per_host_throughput_throttled`,
  `per_host_monthly_soft_cap_rejects`, `banned_host_looks_like_unknown_host`, `ip_taken_from_trusted_header_only`,
  `secret_never_logged_or_cached`.

### P16

- Workflow checks (`actionlint` + a dry run): `deploy_only_on_relay_paths_and_dispatch`, `job_declares_relay_production_environment`,
  `missing_secrets_fail_with_clear_message`, `ci_without_cloudflare_credentials_passes`.
- **(real, Human-gated)** approve a staging deploy, run the conformance suite against `wss://relay-staging.spacingmind.com`; approve a production
  deploy; `GET https://relay.spacingmind.com/v3/info` shows the deployed commit; `wrangler rollback` exercised on staging.

### P17

- `TestRelayConnectNoArgsShowsConsentThenEnrols`, `TestRelayConnectNoArgsRefusesWithoutPassword`,
  `TestRelayConnectNoArgsStoresEmptyFingerprint`, `TestRelayConnectStoresConfigMode0600`, `TestRelayConnectExplicitAddressUnchanged`,
  `TestRelayHostRotateAgainstHosted`, `TestRelayHostRmAgainstHosted`.
- **(real)** against the staging Worker: `smind relay connect`, `smind relay offer`, pair the desktop app, see all of the daemon's workspaces, tasks and
  runs (nothing new created); rotate and confirm the old offer stops working.

## Decisions

- Architecture and the 34 open/decided decisions live in
  [ADR-0031](../../decisions/0031-homelab-cli-distribution.md); this plan uses the recommended defaults. Any answer
  that differs reopens only the PRs listed in the table's "Blocking decisions" column.
- **Changed by the Paseo study** (see the ADR's reference table): direct connections with a password replace
  "loopback only" (P4); relay multi-device is scheduled in scope (P12, P13a–c) instead of deferred; Docker publishes a
  port behind a required password and uses a root-then-`gosu` entrypoint (P11); a daemon-owned rotating log replaces
  per-OS log redirection (P8); `smind config` replaces launch flags (P4); an SSH-tunnel connection type joins
  first-run (P5); relay devices present the daemon password (P13a–c).
- **Decided by Human, 2026-10-13:** the relay is hosted at `relay.spacingmind.com` on Cloudflare Workers +
  Durable Objects (decision 12) with the Go relay speaking the same WebSocket protocol v3 (P12); the Worker lives in
  this monorepo as `relay-worker/` with one shared `relay/protocol/` definition and ONE conformance suite run against
  both implementations (decision 34); `deploy-relay.yml` is separate from release-please, path-filtered, and gated by
  the `relay-production` GitHub Environment with required reviewers; the relay-side "workspace" is renamed "relay
  host" (P0, decision 33). Still pending with the Human: the other open decisions (notably #9) and a separate naming
  question about smind's own "Workspace" (not acted on).
- **Terminology (P0):** a *relay host* is one daemon's mailbox on a relay (host id + admission secret). Connecting
  registers this daemon once; it creates nothing in smind's data; every paired client sees all of the daemon's
  workspaces, spaces and tasks.
- **Deliberately not ported:** OS-service-less supervision (`daemon start`), hosted relay, silent auto-start as the
  default, stop-on-quit, secret-less offers — each with its reason in the ADR.
- PRs are sequenced, not stacked; shared files: `scripts/install.sh` (P1 → P8), `scripts/install.ps1` (P1 → P9),
  `docs/homelab.md` (P2 → P4 → P8 → P9 → P11 → P17, section-owned), `release-please.yml` (P7 → P11),
  `cmd/smind/relay.go` (P0 → P2 → P13a → P17), `desktop/.../relay/*` and `add-relay-link.tsx` (P3 → P13b),
  `internal/server/server.go`/`serve.go`/`config.go` (P4 → P8), `cmd/smind/main.go` (P4, P6, P8: one-line additions,
  rebase), `platform.ts`/`connections-section.tsx`/`daemon_manager.rs`/banner (P3 → P5 → P10).
- P5 must not edit the install/update path the Windows bundled-daemon branch is changing; it only gates it and calls
  the existing `daemon_install`.
- New IPC commands (`connections_preview_relay`, `connections_probe`, `app_settings_*`, SSH tunnel start/stop) extend
  the allowlist; each is minimal and returns no key material (ADR-0013 decision 2, ADR-0023 §5).

## Progress

- 2026-10-13: ADR-0031 and this plan written (docs only), then revised after studying Paseo (CLI lifecycle, pairing,
  relay multi-client, direct+password, Docker, onboarding), then again for the Human's hosted-relay answers
  (Cloudflare Workers + Durable Objects, one WebSocket protocol, "relay host" rename, monorepo layout, gated deploy).
  Nothing implemented. Waiting on Human for the remaining open decisions; wave 1 (P0, P1, P2, P3) is unblocked by
  decisions 15, 16, 18, 21 only. P16 is blocked on Human-provided GitHub Environments and Cloudflare credentials.

## Validation

To be filled per PR as work lands: for each Acceptance Criterion, the test name or the real-platform check that
confirmed it, the exact command run, and what was **not** verified (for example Windows 10, an arm64 Docker host, a
real signed release, an actual mobile device). Items marked **(real)** above are not satisfied by CI alone.
