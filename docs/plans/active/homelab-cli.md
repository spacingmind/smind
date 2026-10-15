# Homelab distribution: update, service, install scripts, Docker, direct + relay connections, desktop first-run

Implements [ADR-0031](../../decisions/0031-homelab-cli-distribution.md)
(**Accepted 2026-10-14**, except decisions 9, 12, 27–32 and 36, which remain **Open**; the PRs they gate stay BLOCKED
below. Revised after a study of Paseo and the Human's answers: hosted-relay hosting is pending, multi-device is designed on
gRPC). Every PR below that depends on an
open decision says so; the plan is written against the ADR's recommended defaults
(★ = default changed or new because of Paseo), and a PR must not start before its
blocking decisions are answered (AGENTS.md rule d).

Size (whole plan): **XL (10)** — scope 2, PRs 2, risk 2, decisions 2, verification 2
→ split into twenty-one PRs (P0–P16; P12 and P13 are split), each sized below (WORKSPACE_PROTOCOL.md). All PRs branch
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
| **P12a** ★ multi-device relay (gRPC): relay server, additive proto, 3-device integration harness | 6 | `internal/relay/relaypb/relay.proto` (+ regenerated), `internal/relay/server/*`, `internal/relay/e2ee` (read-only), `internal/relay/bridge/harness/*`, `cmd/smind/relay.go` (`--max-devices-per-host`) | **L (7)** — scope 1, PRs 1, risk 2, decisions 1, verify 2 | 13 (accepted) | **P0** (shared `cmd/smind/relay.go`; independent of hosting) |
| **P12b** ★ multi-device relay (gRPC): bridge device hub, device identity, pairing window, `smind relay devices` | 6 | `internal/relay/bridge/*`, `internal/relay/devices/*` (new registry), `internal/wsapi/*` (`relay.device.*`), `cmd/smind/relay.go`, `cmd/smind/relay_devices.go` | **L (8)** — scope 2, PRs 1, risk 2, decisions 2, verify 1 | 13, 35 | **P12a**, **P0**, **P2** (same `relay.go`) |
| **P12c** CA-verified TLS mode (Go) and no silent TOFU | 6 | `internal/relay/client/*`, `internal/relay/pairing/*`, `cmd/smind/relay.go`, `internal/relay/server/run.go` (`--tls-cert/--tls-key`) | **M (4)** — scope 1, PRs 0, risk 2, decisions 0, verify 1 | — | **P12b** (same `relay.go`) |
| **P13a** ★ multi-device clients: desktop (Rust) | 6 | `desktop/daemon-client/src/relay/*`, `desktop/src-tauri/src/commands.rs` (key/offer/preview plumbing), `web/packages/ui/src/components/settings/add-relay-link.tsx` | **L (7)** — scope 2, PRs 0, risk 2, decisions 1, verify 2 | 35 | **P12b**, **P3** (same files) |
| **P13b** ★ multi-device clients: mobile (TypeScript) | 6 | `mobile/src/relay/*`, `mobile/src/screens/PairingScreen.tsx`, `mobile/package.json` (secure store) | **M (5)** — scope 1, PRs 0, risk 2, decisions 1, verify 2 (real device) | 35 | **P12b** |
| **P13c** relay device password | 6 | `internal/relay/bridge/*` (hello check), `desktop/…`, `mobile/…` prompts | **S (3)** — scope 1, PRs 0, risk 1, decisions 1, verify 1 | **9**, 14 | **P12b**, **P4**, P13a, P13b |
| **P14** hosted relay: deploy + ops (**BLOCKED on decision 12**) | 6 | `internal/relay/server/run.go` (`--listen-mux`), `deploy/relay/*`, `.github/workflows/deploy-relay.yml`, `docs/relay-ops.md` | **M (5)** — scope 1, PRs 0, risk 2, decisions 1, verify 2 (real proxy + approved deploy) | **12**, 27, 29, 34, 36 | **P12a–c**; Human-provided environments/secrets |
| **P15** hosted relay: open enrolment + abuse limits (**BLOCKED on decision 12**) | 6 | `internal/relay/relaypb/relay.proto` (`Enrol`), `internal/relay/server/*`, `internal/relay/admission/*` | **L (7)** — scope 1, PRs 1, risk 2, decisions 2, verify 1 | **12**, 28, 32 | **P12a**, **P14** |
| **P16** hosted relay: `relay connect` with no address (**BLOCKED on decision 12**) | 6 | `cmd/smind/relay.go`, `internal/relay/client/*`, `docs/homelab.md`, `docs/relay-privacy.md` | **M (5)** — scope 1, PRs 0, risk 1, decisions 2, verify 2 (against staging) | **12**, 14, 30, 32 | **P12c**, **P13c**, **P15**, **P14**, **P4**, **P0** |

**Status (2026-10-14, after the Human's approval)**

- **READY TO START** (decisions accepted; no Human input needed): **P0, P1, P2, P3** (wave 1); **P6, P7** (`smind update` +
  signing); the multi-device track **P12a, P12b, P12c, P13a, P13b**.
  **P0 must merge before P2 and P12a** (shared `cmd/smind/relay.go`); P12b follows P12a, P0 and P2; P12c follows P12b;
  P13a follows P12b and P3; P13b follows P12b; P7 follows P6.
- **BLOCKED on decision 9** (direct connections with a password — still Open): **P4**, and what depends on it: **P5, P8, P9,
  P10, P11, P13c**.
- **BLOCKED on decision 12** (hosted-relay hosting — still Open; also 27–32, 36): **P14, P15, P16**.

**Suggested order**

1. *Wave 1:* **P0** (relay-host rename; first, because `cmd/smind/relay.go` is shared) in parallel with **P1** (install
   scripts); then **P2** (offer QR) and **P3** (Add via relay link). **P12a** may start as soon as **P0** has merged.
2. *Multi-device (ready):* **P12a** (relay server + proto + 3-device harness), then **P12b** (after P12a, P0, P2), **P12c**
   (after P12b), and the clients **P13a** desktop (after P12b and P3) and **P13b** mobile (after P12b), in parallel.
3. *Independent (ready):* **P6** (`smind update`) and then **P7** (signing).
4. *Waiting for the Human's answer on decision 9:* **P4**, then **P5** (first-run), **P8** (service), **P9**, **P10**,
   **P11** (Docker; also after P7) and **P13c** (relay device password; also after P12b). Written against the recommended
   defaults, not started.
5. *BLOCKED on decision 12:* **P14** (deploy and ops), **P15** (open enrolment), **P16** (default address) — in that order,
   after P12a–c; nothing here starts until the Human chooses between (D) Cloudflare gRPC proxy + Go relay, (C) Cloudflare
   Containers behind a Worker, or the withdrawn (B).

`cmd/smind/main.go` gets one-line dispatcher cases from P4 and P6: rebase the later one, do not stack.

**Blocked on Human** (nothing below can start or finish without them; the other PRs do not need them):

- **Decision 12 — hosted relay hosting (D / C / B).** Blocks P14, P15, P16. (C) additionally needs the Cloudflare private-beta
  access the Human has requested and a plan that includes Spectrum (not stated in Cloudflare's post — verify).
- **P14 / production:** the GitHub Environment `relay-production` (required reviewers) and its secrets — which ones depends on
  the option (Cloudflare token with DNS + zone-settings scope; a GCP service account for Cloud Run or a deploy key for a VPS) —
  confirmation that the repository plan supports required reviewers, and a billing alert.
- **P16 / switching the default on:** the privacy page on spacingmind.com (landing-repo follow-up) and the first approved
  production deploy.
- **Decision 9** (direct connections with a password): P4 and everything after it in item 4 waits.
- **Open decisions** (ADR-0031 "Open decisions for Human"): 9, 12, 27–32 and 36 (single-port mode, part of P14). Decision 35 (device
  identity + pairing window + pending approval) is **accepted**.

Not in this plan (landing repo, listed so it is not forgotten): `/install.ps1`
redirect in `spacingmind/spacing-mind-landing-page` `apps/web/next.config.ts`
`redirects()`, added after P1 is in a release; the relay privacy page on spacingmind.com. Not in this plan: built-in TLS
for direct connections (decision 10), relay-then-direct "one host, several connections" (decision 25), pairing-offer
v2 compaction (decision 18), GitHub login for the hosted relay, a status page, a WebSocket relay protocol (option B, withdrawn
as preferred). gRPC stays the relay protocol. smind's own "Workspace" concept is **not** renamed (Human, 2026-10-13): only the
relay-side "workspace" becomes "relay host".

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
   `relay connect` on first use), the one-device limit **until P12a–b and P13a–b land**, the SSH
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

### P12a — ★ multi-device relay (gRPC): relay server, proto, integration harness

*Independent of hosting; may start as soon as ADR-0031 is accepted.* Design: ADR-0031 D9.

1. `relay.proto` gains, additively: `ControlFrame.DeviceAttach.session_id = 3`, `DeviceDetach.session_id = 2`,
   `ControlFrame.Sync{repeated DeviceRef{device_id, session_id}}` (body field 6), `ControlFrame.Revoke{device_id}`
   (field 7) and `Frame.keepalive = 7`; Go stubs regenerated; a frame encoded with the new definition still decodes
   under the previous one (golden bytes test).
2. When a **device-side** stream attaches to a route that has no daemon side, the relay sends `DeviceAttach{device_id,
   session_id}` on that host's control stream; when the route is garbage-collected or its device side ends with no daemon
   side, it sends `DeviceDetach`; on every accepted control stream it sends `Sync` listing routes with a live device side and
   no daemon side.
3. The relay keeps one control stream per host; a new one supersedes the old (the old ends with `Aborted: superseded`).
4. Limits: ≤ 16 attached devices per host (configurable; the 17th gets `ResourceExhausted`), per-side buffer ≤ 200 frames
   **and** ≤ 4 MiB, route grace unchanged.
5. `Frame{keepalive}` is consumed by the relay and answered on the same stream; it is never forwarded or buffered and does
   not need routing metadata beyond the binding; control `Ping`/`Pong` is unchanged.
6. `Revoke{device_id}` closes that device's streams and refuses new attaches for it until the control stream ends.
7. Backward compatibility: the existing suites (`server_test`, `fanout_test`, `bounds_test`, the bridge integration test)
   pass unchanged; a daemon that ignores the new control messages (today's bridge) and a client using the fixed ids still
   work exactly as before.
8. A Go **integration harness** runs a relay, a bridge-like daemon and **three simultaneous devices** (two native-gRPC
   "desktops" and one grpc-web "mobile") and is reused by P12b, P13a and P13b.

**Paseo reference — ported (shape).** Control socket + per-connection data socket + the relay telling the daemon about each
connection + `sync` on control (re)connect + buffering until the daemon side exists
(`refs/paseo/packages/relay/src/cloudflare-adapter.ts:119-127,340-395`, `relay-transport.ts:49-55,121-130`, test `clt_multi`
`cloudflare-adapter.test.ts:193-229`, stale-control handling `:219-250`). Adapted: gRPC streams, SCRAM admission in front,
the existing `DeviceAttach`/`DeviceDetach` slots (`relay.proto:121-148`) instead of new JSON control messages.

### P12b — ★ multi-device relay (gRPC): daemon bridge, device identity, `smind relay devices`

1. **Device hub:** the bridge no longer holds one `DataConn`; it reads the control stream and, for each `DeviceAttach` /
   `Sync` entry, opens a daemon-side `OpenData` for `(host, session, device)`, completes the E2EE handshake as responder and
   runs `api.ServeTransport` on that device's own transport — so every device sees **all** of the daemon's workspaces, spaces
   and tasks, with its own subscriptions and permission prompts. `DeviceDetach` or a revoke cancels only that device. A failure
   in one device session never affects another. ≤ 16 concurrent device sessions (configurable).
2. **Keepalive and reconnect:** data and control streams send keepalives every 30 s; a stream with no frame from the relay for
   75 s is treated as dead and re-established with jittered backoff; a dropped device stream resumes in place
   (`DataConn.Resume`); after a daemon restart the `Sync` re-announces waiting devices; HTTP/2 PING keepalive parameters are
   also set.
3. **Device identity:** the first application message must be `device_hello{v, device_pub (Ed25519), label, signature,
   [password]}` (**v2**, see ADR-0031 D9: the daemon sends a fresh challenge first) where `signature` signs `"smind-device-hello-v2" ‖ SHA-256(handshake transcript) ‖ daemon_pub ‖ session_id ‖ challenge`; the frame
   `device_id` must equal `base32(SHA-256(device_pub))[:26]`; a mismatch or a bad signature drops the session.
4. **Registry:** `$SMIND_HOME/relay-devices.json` (mode 0600) holds `{device_id, device_pub, label, first_seen, last_seen,
   state: approved | pending | revoked}`; revoked devices are refused with a clear reason; atomic writes.
5. **Pairing window:** `smind relay offer` opens a window (default 10 minutes or one new device; `--window <d>`,
   `--max-devices <n>`) during which an unknown device is approved automatically; outside it the device is recorded `pending`
   and refused ("awaiting approval").
6. **CLI + API:** `smind relay devices ls [--pending]`, `approve <id>`, `revoke <id>`, `rename <id> <label>` call new
   `relay.device.list|approve|revoke|rename|window` WS methods on the running daemon (loopback token) so a revoke closes the
   device's stream immediately and sends `Revoke` to the relay; with no daemon running they edit the registry file directly.
7. **Legacy clients:** a client that sends no `device_hello` within 5 s is served as a single unlisted `legacy` device while
   config `relay.legacyDevices` is true (default **false — Human decision 2026-10-15**, was "true for one minor release"); the daemon logs a warning at start and per legacy
   connection. With the default false, an already-installed desktop/mobile build cannot connect through the relay until P13a/b
   ship (it is refused with `device_hello_required`); a user may opt in with `relay.legacyDevices: true`, knowing legacy access is
   served without approval and cannot be revoked (`docs/homelab.md`, ADR-0031 D9).
8. No change to the E2EE core (`internal/relay/e2ee`): the handshake, HKDF salt and counters are untouched.

**Paseo reference — different (device key).** Paseo's clients use an ephemeral key per connection and the daemon keeps no device
identity ("there is no persistent phone-side secret to steal", `refs/paseo/SECURITY.md`, "How it works"); smind adds a persistent
device key and a pairing window to support listing and revocation (decision 35; the alternative keeps Paseo's stance). Paseo's
optional daemon password inside the encrypted hello is the model for `device_hello.password` (P13c).

### P12c — CA-verified TLS mode for relay clients (Go) and no silent trust-on-first-use

1. `internal/relay/client.Dial`: an **empty fingerprint verifies the certificate chain and hostname against the system
   roots**; a non-empty fingerprint pins exactly that certificate (the "empty refused" rule at `client.go:37-40` goes).
2. `smind relay connect <address> …` first tries normal verification and, if it verifies, stores an empty fingerprint; if
   not, it shows the certificate fingerprint and asks for confirmation (or takes `--fingerprint <hex>`); there is no silent
   pinning of whatever the relay presents (`bridge.FetchFingerprint` becomes the confirmation's data source).
3. `smind relay offer` omits `fp` for a CA-verified relay (`Offer` already documents "empty when the relay uses a public CA").
4. `smind relay --tls-cert/--tls-key` lets a self-hoster serve a CA certificate (Let's Encrypt) instead of the self-signed one.

### P13a — ★ multi-device clients: desktop (Rust)

1. A persistent Ed25519 device key is generated on first relay pairing and stored in the app data dir at mode 0600 next to the
   pairing file (new `ed25519-dalek` dependency); it is deleted when the connection is removed or unpaired and never returned
   by any IPC command.
2. Each connection uses its own session id and a `device_id` derived from the key, sends `device_hello` after the E2EE
   handshake, sends 30 s keepalives, applies the 75 s dead-stream rule and reconnects with jitter and resume.
3. **CA-verified mode:** an offer with an empty fingerprint is verified against the system/webpki roots; a non-empty one is
   pinned; `connections_add_relay` and `connections_preview_relay` (P3) report `pinned` vs CA-verified.
4. Two desktop apps, or one desktop and one mobile device, stay attached to one relay host at the same time with no eviction
   or reconnect ping-pong; the existing single-device behaviour is unchanged for a daemon that has not yet been updated
   (legacy path).
5. The interop test runs the Go harness (relay + bridge) with **two Rust clients and one TypeScript client at once**.

### P13b — ★ multi-device clients: mobile (TypeScript)

1. A persistent Ed25519 device key (`@noble/curves`, already a dependency) is generated at first pairing and kept in the
   platform secure store (a secure-store dependency such as `expo-secure-store` is added and verified on a device).
2. The grpc-web client sends `device_hello`, keepalives on the WebSocket stream, applies the 75 s dead-stream rule and resumes
   after drops; the fixed `m1-default-*` ids are replaced by a per-pairing session id and the key-derived device id.
3. The pairing screen help says a self-signed relay needs a CA-verified certificate on a real device (mobile does not pin).
4. The node integration test (`integration.persistent.node.test.ts` style) joins the three-device interop run of P13a.

### P13c — relay device password (after P4)

1. When a daemon password is set (P4), `device_hello.password` must be present and correct before any session data;
   missing/wrong → rejected and not recorded as pending; with no password set the daemon logs a one-line warning per start.
2. Desktop and mobile prompt for the password when the daemon requires it (never stored; held in memory/Rust only).

**Paseo reference — ported.** The password inside the encrypted hello (`refs/paseo/SECURITY.md` "Trust model";
`packages/server/src/server/session-admission-auth.ts` → `password_required`/`incorrect_password`). Different: Paseo currently
admits relay clients with no credential for old mobile builds; we enforce once the legacy window closes.

### P14 — hosted relay: deploy and ops (**BLOCKED on decision 12**)

*Cannot start until the Human chooses the hosting option (D11): (D) Cloudflare gRPC proxy + Go relay on Cloud Run or a VPS,
(C) Cloudflare Containers behind a Worker, or (B) the withdrawn WebSocket rewrite.* Scope once unblocked:

1. A single-port mode (`smind relay --listen-mux`) serving native gRPC and grpc-web on one TLS/h2 listener (Cloudflare
   proxies gRPC on 443 only), with CA/origin-certificate support from P12c and a health endpoint.
2. Persistent storage for the relay-host store for the chosen origin, and tolerance for losing in-memory route state
   (clients resume or re-handshake — D9 flows 5–6).
3. The deployment definition (`deploy/relay/`), `.github/workflows/deploy-relay.yml` (separate from release-please,
   path-filtered, manual dispatch, deploying through the `relay-production` GitHub Environment with required reviewers),
   zone settings for the chosen option (proxied `relay` record, SSL/TLS Full(strict), Network → gRPC on for (D)), and
   `docs/relay-ops.md` (deploy, approve, rollback, secret rotation, ban/unban, logs, cost alerts, how to split the directory
   into its own repository).
4. A soak test: a stream held idle with only keepalives survives ≥ 10 minutes through the real proxy (the 125 s read-timeout
   case) and a Cloud Run-style 60-minute stream cap is handled by reconnect.

**Blocked on Human:** decision 12; the GitHub Environment `relay-production` (required reviewers) and its secrets (which ones
depend on the option: Cloudflare token with DNS + zone-settings scope, and a GCP service account or deploy key); Cloudflare
beta access for (C) (requested); confirmation that the repository plan supports required reviewers; a billing alert.

### P15 — hosted relay: open enrolment and abuse limits (**BLOCKED on decision 12**)

1. An additive `Enrol` RPC returns a server-generated host id (`h_` + 26 base32 chars) and a 256-bit secret once; only the
   `StoredKey` is stored; the response is never cached or logged; `smind relay --open-enrolment` turns it on (off by default
   on a self-hosted relay).
2. Per-IP limits (≤ 5 enrolments/hour, ≤ 20/day, ≤ 8 concurrent connection attempts → `ResourceExhausted` with a retry hint), a
   global host cap, GC of hosts never admitted within 24 h or idle for 90 days, per-host throughput and monthly soft caps, an
   operator ban list, and `host rotate` / delete authenticated by an admission proof. The client IP comes from the trusted
   proxy header only.

### P16 — hosted relay: `smind relay connect` with no address (**BLOCKED on decision 12**)

1. `smind relay connect` with no arguments shows the consent line, enrols on `relay.spacingmind.com`, stores
   `{address, host id, secret}` with an empty fingerprint (CA mode) at mode 0600, and prints "registered this daemon with the
   relay; every paired client sees all of your workspaces".
2. It refuses to finish without a daemon password (offers `smind set-password`); a self-hosted address works as before.
3. `docs/homelab.md` documents the hosted path and `docs/relay-privacy.md` states what the relay sees, retention and that there
   are no accounts (source for the landing page). The production default is switched on only after the privacy page is live and
   the first approved production deploy has run.

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

### P12a

- Go: `TestProtoNewFramesDecodeUnderOldDefinition` (golden bytes), `TestDeviceAttachSentWhenDeviceAttachesWithoutDaemonSide`,
  `TestDeviceDetachSentWhenRouteGarbageCollected`, `TestSyncListsWaitingRoutesOnControlAccept`,
  `TestNewControlStreamSupersedesOld`, `TestDeviceCapPerHostIs16AndConfigurable`, `TestBufferBoundedByFramesAndBytes`,
  `TestKeepaliveEchoedNotForwardedNotBuffered`, `TestRevokeClosesDeviceStreamsAndBlocksAttach`,
  `TestDeviceReconnectReplacesOnlyItsOwnRoute`, `TestExistingFixedIDClientAndDaemonStillWork`,
  `TestIdleStreamSurvivesProxyIdleTimeout` (an h2 shim closes any stream with no application bytes for 1 s; keepalives every
  0.2 s keep it open — the scaled-down 125 s case).
- **Three-device integration (harness):** `TestThreeDevicesConcurrentViaHarness` — two native-gRPC devices and one grpc-web
  device attach to one host at once and exchange frames with a daemon-side stub in both directions; then `…ThenOneReconnects`
  (only that device's route is replaced), `…ThenDaemonControlReconnects` (Sync re-announces all three), `…ThenOneRevoked`.

### P12b

- Hub: `TestBridgeServesThreeDevicesIndependently`, `TestBridgeDeviceFailureIsolated`, `TestBridgeDeviceDetachCancelsOnlyThatDevice`,
  `TestBridgeResumesDeviceAfterTransportDrop`, `TestBridgeReannouncesAfterDaemonRestartViaSync`,
  `TestBridgeDeadStreamAfter75sReconnectsWithJitter` (fake clock), `TestBridgeDeviceCapRejectsSeventeenth`,
  `TestEveryDeviceSeesAllWorkspaces` (a workspace created by one device's RPC is listed to the others).
- Identity: `TestDeviceHelloValidSignatureAccepted`, `TestDeviceHelloBadSignatureDropped`,
  `TestDeviceHelloReplayInAnotherSessionRejected`, `TestDeviceIDMustMatchPublicKeyHash`,
  `TestE2EECoreUnchanged` (existing `e2ee` fixtures byte-for-byte).
- Registry and window: `TestRegistryFileMode0600AndAtomicWrite`, `TestRevokedDeviceRefusedWithReason`,
  `TestUnknownDeviceOutsideWindowIsPending`, `TestOfferOpensWindowAndApprovesExactlyOneDevice`, `TestPairingWindowExpires`,
  `TestPairingWindowFlags`.
- CLI/API: `TestDevicesLsAndPendingFlag`, `TestDevicesApproveRevokeRename`, `TestDevicesRevokeClosesStreamAndSendsRevoke`,
  `TestDevicesCLIEditsFileWhenNoDaemon`, `TestRelayDeviceWSMethodsRequireToken`.
- Legacy: `TestLegacyClientServedWhileLegacyDevicesTrue`, `TestLegacyClientRefusedWhenLegacyDevicesFalse`,
  `TestLegacyWarningLogged`.

### P12c

- `TestDialEmptyFingerprintUsesSystemRoots`, `TestDialNonEmptyFingerprintPinsExactly`, `TestRelayConnectTriesSystemRootsFirst`,
  `TestRelayConnectAsksBeforePinning`, `TestRelayConnectFingerprintFlag`, `TestOfferOmitsFPForCAVerifiedRelay`,
  `TestRelayTLSCertAndKeyFlags`.

### P13a

- Rust: `device_key_generated_once_mode_0600`, `device_key_deleted_when_connection_removed`, `device_key_never_returned_by_ipc`,
  `device_id_matches_public_key_hash`, `device_hello_signature_verifies_against_go_vector` (shared vector file),
  `client_sends_keepalive_every_30s`, `client_reconnects_after_75s_silence_with_jitter`,
  `client_resumes_after_transport_drop_keeping_ids`, `client_pinned_vs_webpki_roots`,
  `add_relay_accepts_empty_fingerprint_offer`, `preview_relay_reports_pinned_or_ca_verified`, `legacy_daemon_still_works`.
- Interop (Go harness): `three_devices_two_rust_one_ts_stay_attached`, `…_after_one_reconnects`, `…_after_daemon_restart`,
  `…_after_revoke_only_that_device_is_refused`.

### P13b

- TS: `device key persists in the secure store`, `device_hello signature verifies against the Go vector`,
  `keepalive is sent on the websocket stream`, `dead stream after 75s reconnects`, `resumes after a drop keeping ids`,
  `pairing parses offers with and without a fingerprint`; the node integration test joins the three-device interop run.
- **(real, P12a–P13b together):** daemon + **two desktop apps + one mobile device** on one self-hosted relay for ≥ 10 minutes
  (first directly, then through a proxy shim with a 125 s read timeout): all three stay live; kill one, the others stay; revoke
  one with `smind relay devices revoke`, it is refused and the others are unaffected; restart the daemon, all three return.

### P13c

- `TestDeviceHelloPasswordRequiredWhenSet`, `TestWrongPasswordRejectedAndNotRecordedPending`, `TestNoPasswordConfiguredWarnsOnce`;
  UI: `it("asks for the daemon password when the relay host requires it and never stores it")`.

### P14 (blocked on decision 12)

- `TestListenMuxServesGRPCAndGRPCWebOnOnePort`, `TestHealthEndpointOK`, workflow checks
  (`deploy_only_on_relay_paths_and_dispatch`, `job_declares_relay_production_environment`,
  `missing_secrets_fail_with_clear_message`, `ci_without_cloudflare_credentials_passes`).
- **(real, Human-gated)** an idle keepalive-only stream survives ≥ 10 minutes through the real Cloudflare proxy; an approved
  staging then production deploy; the three-device test against the hosted relay.

### P15 (blocked on decision 12)

- `TestEnrolReturnsServerGeneratedIDAndSecretOnce`, `TestEnrolPerIPHourlyLimit`, `TestEnrolPerIPDailyLimit`,
  `TestEnrolGlobalCap`, `TestConcurrentAttemptsPerIPCapped`, `TestUnusedHostGCAfter24h`, `TestIdleHostGCAfter90d`,
  `TestPerHostThroughputThrottled`, `TestPerHostMonthlySoftCap`, `TestBannedHostLooksLikeUnknownHost`,
  `TestIPFromTrustedHeaderOnly`, `TestEnrolSecretNeverLoggedOrCached`, `TestOpenEnrolmentOffByDefault`.

### P16 (blocked on decision 12)

- `TestRelayConnectNoArgsShowsConsentThenEnrols`, `TestRelayConnectNoArgsRefusesWithoutPassword`,
  `TestRelayConnectNoArgsStoresEmptyFingerprint`, `TestRelayConnectStoresConfigMode0600`, `TestRelayConnectExplicitAddressUnchanged`.
- **(real)** against staging: `smind relay connect`, `smind relay offer`, pair the desktop app, see all of the daemon's workspaces,
  tasks and runs (nothing new created); rotate and confirm the old offer stops working.

## Decisions

- Architecture and the 36 decisions live in [ADR-0031](../../decisions/0031-homelab-cli-distribution.md): 27 are **accepted**
  (2026-10-14, with their recommended defaults, including 35), 9 remain **Open** (9, 12, 27–32, 36). The PRs gated by an Open
  decision stay BLOCKED; an answer that differs from the recommended default reopens only the PRs listed in the table's
  "Blocking decisions" column.
- **Changed by the Paseo study** (see the ADR's reference table): direct connections with a password replace
  "loopback only" (P4); relay multi-device is scheduled in scope (P12a–b, P13a–b) instead of deferred; Docker publishes a
  port behind a required password and uses a root-then-`gosu` entrypoint (P11); a daemon-owned rotating log replaces
  per-OS log redirection (P8); `smind config` replaces launch flags (P4); an SSH-tunnel connection type joins
  first-run (P5); relay devices present the daemon password (P13c).
- **Decided by Human, 2026-10-13/14:** the relay-side "workspace" is renamed "relay host" (P0, decision 33) and smind's own
  "Workspace" stays as is; the hosted relay is named `relay.spacingmind.com`, DNS on Cloudflare, with the trust model accepted
  and production deploys gated by a `relay-production` GitHub Environment with required reviewers, code kept in the monorepo
  (decision 34). **2026-10-14:** the hosted relay's **hosting is PENDING** (decision 12: Cloudflare gRPC proxy + Go relay on
  Cloud Run or a VPS; Go relay in Cloudflare Containers behind a Worker via inbound gRPC (private beta, access requested); the
  Workers + Durable Objects WebSocket rewrite is no longer preferred) — **P14–P16 are BLOCKED on it**; **multi-device is designed
  now on the existing gRPC protocol** (ADR-0031 D9; P12a–b, P13a–b), independent of hosting. Still pending with the Human: the
  other open decisions (notably #9, which blocks P4 and its dependants, and the new #35).
- **Terminology (P0):** a *relay host* is one daemon's mailbox on a relay (host id + admission secret). Connecting
  registers this daemon once; it creates nothing in smind's data; every paired client sees all of the daemon's
  workspaces, spaces and tasks.
- **Deliberately not ported:** OS-service-less supervision (`daemon start`), silent auto-start as the default,
  stop-on-quit, secret-less offers, ephemeral-only device keys (decision 35's alternative) — each with its reason in the ADR.
- PRs are sequenced, not stacked; shared files: `scripts/install.sh` (P1 → P8), `scripts/install.ps1` (P1 → P9),
  `docs/homelab.md` (P2 → P4 → P8 → P9 → P11 → P16, section-owned), `release-please.yml` (P7 → P11),
  `cmd/smind/relay.go` (P0 → P2 → P12b → P12c → P16), `internal/relay/server/*` (P12a → P14 → P15),
  `desktop/.../relay/*` and `add-relay-link.tsx` (P3 → P13a),
  `internal/server/server.go`/`serve.go`/`config.go` (P4 → P8), `cmd/smind/main.go` (P4, P6, P8: one-line additions,
  rebase), `platform.ts`/`connections-section.tsx`/`daemon_manager.rs`/banner (P3 → P5 → P10).
- P5 must not edit the install/update path the Windows bundled-daemon branch is changing; it only gates it and calls
  the existing `daemon_install`.
- New IPC commands (`connections_preview_relay`, `connections_probe`, `app_settings_*`, SSH tunnel start/stop) extend
  the allowlist; each is minimal and returns no key material (ADR-0013 decision 2, ADR-0023 §5).

## Progress

- 2026-10-13: ADR-0031 and this plan written (docs only), revised after studying Paseo, then for the Human's first
  hosted-relay answers.
- 2026-10-14: revised for the Human's new decisions: hosted-relay hosting is **pending** (options D/C/B recorded, B no longer
  preferred; P14–P16 blocked); multi-device designed now on gRPC (ADR-0031 D9: control-stream announcements, per-device E2EE
  sessions, device identity + pairing window, `smind relay devices ls|approve|revoke|rename`, 30 s keepalive for the 125 s proxy
  timeout, a ≥ 3-device test) as P12a–b and P13a–b; plan re-sequenced (wave 1 = P0, P1, P2, P3; multi-device may start on ADR
  acceptance). Nothing implemented.
- 2026-10-14 (later): the Human approved ADR-0031 — **Accepted, except decisions 9, 12, 27–32 and 36 (Open)**; decision 35 accepted
  as proposed. Ready to start: P0–P3, P6, P7, P12a–c, P13a–b (P0 merges before P2 and P12a). Blocked: P4 and dependants (decision 9);
  P14–P16 (decision 12). Nothing implemented yet.
- 2026-10-14: **P6 implemented** (`smind update`, checksum verification; PR "feat(cli): smind update with checksum verification
  (ADR-0031 P6)"): `internal/selfupdate/*`, `cmd/smind/update.go`, dispatcher case + startup `.old` cleanup in `main.go`. Signature
  verification is P7 (nil `ChecksumVerifier` hook in `update.go`); the service restart path is exercised through an injected
  `Daemon` until P8 lands (`/healthz managedBy` is read but nothing emits it yet).
- 2026-10-14: **P7 implemented** (release signing + CLI verification; PR "feat(release): sign checksums and verify them in smind
  update (ADR-0031 P7)"): `scripts/sign-checksums.sh` + `publish` job steps sign `checksums.txt` with the desktop-updater key
  (Tauri CLI 2.11.5 from `desktop/package-lock.json`, `--app-version` binds the version into the signed comment) and upload
  `checksums.txt.minisig` (before `checksums.txt`); `internal/selfupdate/verify.go` is a minisign verifier (prehashed `ED` only) with
  `UpdaterPublicKey` embedded; `smind update` has no bypass (not even `--force`), and `Updater.Update` refuses without a verifier.
  P1 has merged, so the README now says that releases before the first signed one cannot be updated *to* with `smind update`
  (reinstall with the install script); install scripts stay checksum-only per decision 1. **Accepted limit:** the signature binds
  a release's checksums to its version, but not "latest" — a host that serves an older (still newer-than-installed) genuine
  release as latest can hold a client back; bounding that needs a signed latest pointer or timestamp window (not in P7).

- 2026-10-14 (P12a): relay server, additive proto and 3-device harness implemented on `feat/relay-multi-device-server` (one PR).
  Decisions taken inside the spec: `Sync` is always sent on control accept (even empty) so a daemon has an unambiguous "initial
  state complete" marker, which changed the one existing grpc-web test that assumed strict Pong/Pong alternation (it now reads
  until the Pong); the relay does **not** re-announce `DeviceAttach` when a daemon side ends (a daemon that rejects a device
  would reconnect to it in a loop; `Sync` on control accept covers a lost daemon); `Revoke` also closes the daemon side of that
  device's routes and is scoped to the control stream that sent it; `--max-devices-per-host` counts distinct device ids with a live
  device side; the server enforces a 20 s HTTP/2 PING floor (`keepalive.EnforcementPolicy`) so clients may later enable
  `keepalive.ClientParameters` at 30 s; the Go client gained an opt-in `WithKeepalive` (30 s probe, 75 s dead-stream rule, probe
  negotiated: the probe carries a per-connection nonce that the relay echoes, and the first echo must arrive within 10 s or
  keepalive stays off for that stream, so a relay from before this PR is sent one probe per stream, not a stream of them).
  An independent review (concurrency, compatibility, E2EE) drove: nonce-matched echoes, per-stream probe verdict, non-blocking
  probe writes, `DeviceDetach` on route GC, route cap and id-length bound, length-prefixed route keys, server HTTP/2 pings, and the
  ADR-0031 D9 "trust model of the control stream" paragraph (any host-secret holder can supersede control — follow-up, decision 12/28). Client HTTP/2 PINGs are deliberately **not** enabled: an older relay's default policy
  answers a ping more often than every 5 minutes with GOAWAY.
- 2026-10-15 (P12b): device hub, device identity, registry, pairing window and `smind relay devices` implemented on
  `feat/relay-device-hub`. Decisions inside the spec: `device_hello` is a wsapi request (`relay.device_hello`) so an old daemon
  answers "method not found"; the signed transcript is the e2ee HKDF salt (`e2ee.TranscriptHash`, additive, pinned by
  `TestE2EECoreUnchanged`); sessions survive a relay reconnect and are resumed from `Sync`, absence from a `Sync` does not drop one;
  `Revoke` is sent only after a `Sync` proves the relay speaks it and is re-sent on every reconnect; `relay.device.*` answer
  `forbidden` over the relay. An independent review drove: legacy fallback only on the fixed route id and one session; a pairing
  window slot only for devices first seen after it opened; the hub never drops a session because a `Sync` omitted it; same-device
  new session replaces the old; room checked before a device is recorded; 128-pending cap; CLI edits the registry file only when
  nothing listens on the daemon port; label hygiene (bidi/zero-width/separator characters); registry durability (dir fsync, rename
  retry). **Decided by the Human (2026-10-15): `relay.legacyDevices` defaults to false** (with it true, revoke, pending and the window do
  not bind legacy access); `docs/homelab.md` says that relay pairing needs the updated clients (P13a/b) until then and warns about
  the opt-in. Still open: a per-offer one-time token (offer format change) is the stronger pairing-window fix.
- 2026-10-15 (fix, before #350/#351 merge): the P13b review found two defects in what P12b shipped. (1) `device_hello` v1 signed only
  (daemon key, client ephemeral key), both fixed by a captured handshake, so a captured session replays into a new route and is
  admitted as that device (and `supersede` drops the real session): hello **v2** — the daemon sends a fresh challenge first and the
  signature covers the route session id and that challenge; v1 is refused (no released client speaks it); vector updated (v1 kept as
  history). (2) wsapi answered an unknown method with nothing (the error was sent before the request was registered in flight):
  fixed, tested.
- 2026-10-15 (P13b): mobile device identity, keepalive, resume and multi-device implemented on
  `feat/mobile-relay-device-identity`. Decisions inside the spec: the daemon must be v0.12+ (the client uses a per-pairing
  session id and the key-derived device id, which a pre-P12b daemon never opens a route for, so there is no legacy fallback on mobile —
  a daemon that does not answer `device_hello` within 20 s is reported as "may be older than v0.12"); a device the daemon has not
  approved is `pending` and **retried every 5 s** with a fresh session until approved (revoked and bad-hello are final, anything
  else backs off); after a drop the client first **resumes** the same session (same route, E2EE counters and frame sequence) and
  proves the daemon still holds it with one `workspace.list` (any answer counts; 10 s), else opens a **fresh session** (new session id,
  new ephemeral key, new hello) and re-subscribes; requests in flight across a drop reject (at-most-once, as P12b says); the
  key lives in the platform secure store (`expo-secure-store`, device-bound, after-first-unlock) and in memory only on Expo web;
  a revoked device offers **Reset device identity** (new key = new pending device) instead of retrying; the hand-written proto
  gained `Frame.keepalive` only (a device never opens the control stream, so none of the `ControlFrame` messages are needed).
  **Daemon finding (not fixed here):** `internal/wsapi/conn.go` `dispatch` calls `sendError` for an unknown method *before* the
  request is registered in-flight, so `terminate` rejects it and the daemon sends **no answer at all** — the ADR/P12b text that
  "a daemon from before P12b answers method not found" is not what the code does; a client cannot use an unknown method as a
  probe (the first cut of this client did, and a resumed session was never confirmed).
  An independent review (identity, reconnect state machine, protocol agreement) drove: `close()` during a fresh session's
  handshake ends its stream (it had left an admitted ghost device); an **end-to-end liveness watchdog** (a request unanswered for 15 s
  or 90 s of silence is followed by one cheap question; no answer in 10 s means the daemon lost the session and a fresh one is
  opened — keepalive only proves the *relay*, so a daemon restarted behind a live stream used to hang every call); pending polls grow
  5 s → 30 s (each ask is a fresh route and the relay keeps an abandoned route 5 minutes against its 128-route host cap) and a blip
  while pending, a full daemon (`too_many_*`) or a full relay (status 8) are retried, not final; a link that dies within 2 s of
  connecting backs off (the hub's min-stable rule); the Ed25519 seed is a closure variable, not a property of the identity object;
  Connect is single-flight and an identity reset waits for the key removal. **Not changed, for a decision (E2EE core / ADR text):**
  the signed hello is bound to `(daemon_pub, client ephemeral pub)` only and the daemon adds no per-session value, so whoever can open
  a route under a device's id (the relay operator, a holder of the host secret) can replay a captured hello + ciphertext into a new
  route, be admitted as that device and make the daemon's `supersede` drop the real session (no forgery, no reads; availability and
  impersonation of a session the attacker cannot extend) — "so it cannot be replayed" in D9 holds against a different ephemeral key
  but not against that party; fixing it needs the daemon to contribute a nonce or the session id to the signed bytes (Go + vector
  + desktop + mobile together).

## Validation

To be filled per PR as work lands: for each Acceptance Criterion, the test name or the real-platform check that
confirmed it, the exact command run, and what was **not** verified (for example Windows 10, an arm64 Docker host, a
real signed release, an actual mobile device). Items marked **(real)** above are not satisfied by CI alone.

### P6 (2026-10-14)

Commands: `task test`, `task lint`, `GOOS=windows go vet ./...`, `go test -race ./internal/selfupdate/ ./cmd/smind -run Update`.

- AC1 `--check`/`--json`: `TestUpdateCheckReportsLatestAndExitsZero`, `TestUpdateCheckJSON` (cmd), `TestUpdateCheckReportsLatest`.
- AC2 download/verify/extract/version-check, byte-identical on failure: `TestUpdateVerifiesChecksumAndInstalls`,
  `…ChecksumMismatchLeavesBinaryIdentical`, `…MissingAssetLeavesBinaryIdentical`, `…UnknownReleaseIs404`, `…FollowsDownloadRedirects`,
  `…RejectsArchiveWithPathTraversal`, `…RejectsMissingBinaryMember`, `…RejectsOversizeMember`, `TestExtractMemberEdgeCases`,
  `…StagedBinaryVersionMismatchIsNeverInstalled`.
- AC3/4 refusals + active-run detection via `run.list`: `TestUpdateRefusesWhileRunsActive`, `…WhilePermissionPending` (a pending
  permission is a *running* run; `run.list` has no separate state), `…ForceOverridesActiveRuns`, `…NoDaemonRunningIsFine`,
  `…RefusesWhenTheDaemonCannotBeAsked`, `…RechecksActiveRunsJustBeforeSwap`, `…RefusesDevBuild` (incl. `task build`'s
  `-dev+sha[.dirty]`), `…RefusesDesktopManagedPath` + `TestDesktopOwned`, `…RefusesInsideContainer`, `…RefusesDowngradeWithoutForce`,
  `…RefusesUnwritableDir`.
- AC5 swap/rollback: `…AtomicSwapKeepsOldUntilVerified`, `…RollsBackWhenNewBinaryVersionMismatches` (atomic + rename-aside),
  `TestSwapAndRestoreBothStrategies`, `TestPreserveKeepsRollbackCopyOutOfCleanupsReach`; Windows lock semantics:
  `TestUpdateWindowsRenameAsideAndCleanup` (build-tagged; **compiled by `GOOS=windows go vet`, not run — no Windows host**).
  `TestUpdateEndToEndReplacesTheRunningBinary` builds two real binaries and runs the old one's own `update` (macOS here).
- AC6 restart: `…RestartsServiceAndWaitsForHealthzVersion`, `…RollsBackWhenHealthzNeverReportsNewVersion`,
  `…DoesNotRespawnManualDaemon` (fake `Daemon`; no real service until P8).
- AC7/8: `…InstallsExplicitVersion`, `…DryRunChangesNothing`, `…LatestUsesRedirectNotAPI`, `…HonoursHTTPSProxy` (child process, CONNECT seen).
- **Not verified:** **(real)** Linux/Windows 11 against a real published release (none newer than the running build exists to
  update to from this branch), Windows with a running `smind.exe` daemon, real `smind service restart` (P8).

### P7 (2026-10-14)

Commands: `task test`, `task lint`, `GOOS=windows go vet ./...`, `actionlint`, `shellcheck scripts/sign-checksums.sh`,
`scripts/sign-checksums.sh` run locally with a throwaway key + the locked Tauri CLI 2.11.5 + `minisign` 0.12.

- AC1 signing in `publish`: `scripts/sign-checksums.sh` (Tauri sign → base64-decode → `checksums.txt.minisig`; re-verified with
  `minisign -V` **and** smind's own verifier). Real release: missing secret fails the job; dry run: throwaway key, the real secret
  is not referenced. CI evidence: workflow_dispatch run recorded below.
- AC2 `TestVerifyChecksumsSignatureAccepts`, `TestVerifyRejectsTamperedChecksums`, `TestVerifyRejectsMissingMinisig`,
  `TestVerifyRejectsWrongKey` (incl. same key-id forgery), `TestVerifyRejectsLegacyEdAlgorithm`,
  `TestVerifyRejectsAlteredTrustedCommentAndReplays`, `TestVerifyRejectsDuplicateTrustedCommentFields`,
  `TestVerifyRejectsMalformedSignatures`, `TestVerifyRejectsOversizeSignatureFile`, `TestUpdateRefusesReleaseWithoutSignature`
  (selfupdate + cmd), `TestUpdateRefusesBadSignatureAndNeverDownloadsTheArchive`,
  `TestUpdateRefusesSwappedChecksumsEvenIfTheArchiveMatchesThem`, `TestUpdateRefusesBadSignature` (cmd; `--force` does not
  bypass), `TestUpdateHasNoSignatureBypassFlag`, `TestUpdateRefusesWithoutAVerifier`, `TestUpdateUsesTheEmbeddedReleaseKeyByDefault`,
  and the real-binary `TestUpdateEndToEndReplacesTheRunningBinary/{signed,unsigned,other key}`. Mutation-checked: deleting the
  `VerifyChecksums:` wiring in `cmdUpdate` fails the e2e and the embedded-key tests.
- Real-format compatibility: `TestVerifyAcceptsRealTauriSignerOutput` (committed fixture from the real `tauri signer sign`, throwaway
  key); one-off, not committed: the real v0.11.1 `smind_0.11.1_x64-setup.exe.sig` verifies against the embedded production key.
- AC3 `TestEmbeddedPubkeyMatchesTauriConf`.
- AC4 releases without a signature: `ErrNoSignature` ("release has no signature"), no checksum-only fallback.
- **Not verified:** a real signed release with the production secret (needs the next release; the job fails closed if the secret is
  absent), the password path with an encrypted key (the key has no password, ADR-0023), Windows runs of the tests.

### P12a (2026-10-14, branch `feat/relay-multi-device-server`)

Commands: `go test -race -count=20 ./internal/relay/...` (all packages ok; client 440 s, relaytest 95 s, server 106 s),
`task lint`, `task test` (exit 0), `GOOS=windows go vet ./internal/relay/... ./cmd/...`.

| AC | Evidence |
|---|---|
| 1 proto additive + golden | `relay.proto` regenerated with protoc 36.2 / protoc-gen-go 1.36.12 / go-grpc 1.6.2 (not hand-edited); `compat_test.go`: `TestGoldenBytesCurrentEncoding`, `TestNewFrameDecodesUnderPreviousDefinition`, `TestNewControlFramesDecodeUnderPreviousDefinition`, `TestPreviousEncodingDecodesUnderCurrentDefinition` against `testdata/relay_v0.desc` (the previous definition; plan name `TestProtoNewFramesDecodeUnderOldDefinition` is covered by these) |
| 2 Attach / Detach / Sync | `TestDeviceAttachSentWhenDeviceAttachesWithoutDaemonSide`, `TestDeviceDetachSentWhenRouteGarbageCollected` (device-side end and GC), `TestSyncListsWaitingRoutesOnControlAccept` |
| 3 one control stream per host | `TestNewControlStreamSupersedesOld` (`Aborted: superseded`) |
| 4 limits | `TestDeviceCapPerHostIs16AndConfigurable` (17th `ResourceExhausted`), `TestBufferBoundedByFramesAndBytes`, plus review additions `TestRouteCapPerHostBoundsDaemonSideRoutes`, `TestOverlongIDsAreRejected`; route grace unchanged (existing `bounds_test`) |
| 5 keepalive | `TestKeepaliveEchoedNotForwardedNotBuffered`; client rule with a fake clock: `TestKeepaliveSentEvery30s`, `TestDeadStreamAfter75sOfSilence`, `TestAnyRelayFrameResetsTheDeadStreamTimer`, `TestUnansweredProbeTurnsKeepaliveOff`, `TestForwardedPeerProbeIsNotAnEcho`, `TestKeepaliveReprobesAfterResumeWhenFirstEchoWasLate`, `TestBlockedProbeWriteDoesNotDisableTheDeadStreamRule`, `TestKeepaliveFollowsResumeToTheNewStream`; proxy case `TestIdleStreamSurvivesProxyIdleTimeout` (idle shim closes a stream after 1 s, keepalives every 0.2 s keep it; `TestIdleStreamWithoutKeepaliveIsClosedByTheProxy` is the control) |
| 6 Revoke | `TestRevokeClosesDeviceStreamsAndBlocksAttach`, harness `…ThenOneRevoked` |
| 7 backward compatibility | existing `server_test`, `fanout_test`, `bounds_test`, `bridge` suites pass; **one existing test edited**: `TestGRPCWeb_OpenControlPingPongOverWebsocket` now reads until the Pong because `Sync` follows the first `Pong`; `TestExistingFixedIDClientAndDaemonStillWork`; `TestControlStreamsOfDifferentHostsAreIsolated` |
| 8 3-device harness | `internal/relay/relaytest` (real `server.Run`, native + grpc-web clients over one `relaypb.RelayClient` interface, daemon stub, devices): `TestThreeDevicesConcurrentViaHarness` (+ `…ThenOneReconnects`, `…ThenDaemonControlReconnects`, `…ThenOneRevoked`), `TestNoPlaintextVisibleToRelay` |

Not verified: a real proxy (Cloudflare) in front of the relay, a real mobile/desktop client, the cross-language (Rust/TS)
interop run named in the ADR test bar (P13a/b), a daemon restart through the real bridge (P12b), grpc-web keepalive
through a browser WebSocket.

### P12b (2026-10-15, branch `feat/relay-device-hub`)

Commands: `go test -race -count=20 ./internal/relay/... ./internal/wsapi/ ./internal/config/`, `task lint`, `task test`,
`GOOS=windows go vet ./internal/relay/... ./internal/wsapi/... ./cmd/...`, and a live smoke (`smind serve` in a temp
`SMIND_HOME`, `smind relay devices ls` → "(no devices)", `approve nope` → "no such device", exit 1).

| AC | Evidence |
|---|---|
| 1 hub | `internal/relay/bridge`: `TestBridgeServesThreeDevicesIndependently` (2 native + 1 grpc-web through the real relay and real wsapi), `TestEveryDeviceSeesAllWorkspaces`, `TestBridgeDeviceFailureIsolated`, `TestBridgeDeviceDetachCancelsOnlyThatDevice`, `TestBridgeDeviceCapRejectsSeventeenth`, `TestSameDeviceNewSessionReplacesItsOldSession` |
| 2 keepalive/reconnect | `TestBridgeResumesDeviceAfterTransportDrop`, `TestBridgeReannouncesAfterDaemonRestartViaSync`, `TestBridgeDeadStreamAfter75sReconnectsWithJitter` and `TestBridgeControlStreamSilentFor75sReconnects` (fake clock, blackholing proxy); HTTP/2 PING parameters were set in P12a |
| 3 identity | `devices`: `TestDeviceIDMustMatchPublicKeyHash`, `TestDeviceHelloValidSignatureAccepted`, `TestDeviceHelloBadSignatureDropped`, `TestDeviceHelloReplayInAnotherSessionRejected`, `TestHelloVectorMatchesFile` (cross-language vector); hub level `TestDeviceHelloReplayInAnotherSessionRefused` |
| 4 registry | `TestRegistryFileMode0600AndAtomicWrite`, `TestRevokedDeviceRefusedWithReason` (+ `…AtHello` at the hub), `TestRegistryReloadSeesExternalEdit`, `TestRegistryCorruptFileIsAnError`, `TestRegistryConcurrentAdmit` |
| 5 window | `TestUnknownDeviceOutsideWindowIsPending`, `TestOfferOpensWindowAndApprovesExactlyOneDevice`, `TestPairingWindowExpires`, `TestPairingWindowFlags`, `TestPendingDeviceFromBeforeTheWindowDoesNotTakeASlot`, `TestTooManyPendingDevicesAreRefused` |
| 6 CLI + API | `cmd/smind`: `TestDevicesLsAndPendingFlag`, `TestDevicesApproveRevokeRename`, `TestDevicesCLIEditsFileWhenNoDaemon`, `TestDevicesCLIDoesNotEditFileWhenDaemonIsUnreachableForOtherReasons`, `TestRelayOfferWindowFlags`; `wsapi`: `TestRelayDeviceWSMethodsRequireToken`, handler tests; hub: `TestDevicesRevokeClosesStreamAndSendsRevoke` (stream closed, relay refuses the re-attach with PermissionDenied, revocation re-sent after a reconnect) |
| 7 legacy | `TestConfig_RelayDefaultsAndValidation` (default false, opt-in true), `TestLegacyClientServedWhileLegacyDevicesTrue`, `TestLegacyClientRefusedWhenLegacyDevicesFalse`, `TestLegacyWarningLogged`, `TestLegacyClientSilentForHelloTimeoutIsStillServed`, `TestSilentClientRefusedWhenLegacyDevicesFalse`, `TestLegacyFallbackOnlyOnTheFixedRoute`, `TestNewestLegacySessionReplacesTheOlder`; the pre-existing bridge tests (fixed ids, no hello) pass unchanged |
| 8 e2ee unchanged | `TestE2EECoreUnchanged` (fixed identities, golden keys), `TestTranscriptHashIsTheHKDFSalt`, the existing e2ee fixtures; `deriveKeys` is byte-identical to before |

Not verified: a real desktop/mobile client (P13a/b), the cross-language hello vector in Rust/TS, Windows at runtime (registry
rename retry, `Restrict`), the hub against a pre-P12a relay (the `Sync`-less fallback to the fixed legacy route has no test — it is reasoned
from the old server code only), a real proxy.

### P8 (2026-10-15, branch `feat/smind-service-unix`)

Commands: `go test ./...`, `go test -race ./internal/service ./internal/logging`, `task lint`, `sh scripts/install_test.sh` (sh, dash, bash),
`GOOS=windows go vet ./cmd/... ./internal/service/... ./internal/logging/... ./internal/config/... ./internal/server/...`, `actionlint`,
`shellcheck scripts/install.sh scripts/install_test.sh scripts/service_smoke.sh`, and the `Service (systemd, launchd)` workflow
(`scripts/service_smoke.sh` against the real service managers on `ubuntu-latest` and `macos-latest`).

| AC | Evidence |
|---|---|
| 1 commands/flags | `cmd/smind`: `TestServiceUsageErrorsExitTwo` (no `--port`; `--no-start` is install-only), `TestServiceStatusExitCodes`, `TestServiceOnUnsupportedOS`; `TestServiceCLIWithFakeServiceManager` runs the real binary against a fake `systemctl`/`launchctl` on `PATH` and a temp `HOME` (install, status, re-install, uninstall) |
| 2 systemd | `internal/service`: `TestSystemdUserUnitGolden`, `TestSystemdUserUnitWithSMINDHOMEGolden`, `TestSystemdSystemUnitGoldenUsesSudoUser`, `TestSystemdSystemRefusesPlainRoot` (no `SUDO_USER` and `SUDO_USER=root`), `TestSystemdSystemNeedsRoot`, `TestSystemdUserInstallUnderSudoIsRefused`, `TestSystemdUnitOmitsSMINDHOMEWhenUnset`, `TestSystemdUnitCapturesPATH` (+ `--path`), `TestSystemdUnitDirectives` (no sandboxing directives), `TestSystemdUnitQuoting`, `TestSystemdInstallRefusesNewlinesInPaths`, `TestLingerHintWhenDisabled` |
| 3 launchd | `TestLaunchdPlistGolden` (`RunAtLoad`, `KeepAlive={SuccessfulExit:false}`, `PATH`, `SMIND_SUPERVISOR=launchd`; `plutil -lint` on macOS), `TestLaunchdPlistIsWellFormedAndEscaped`, `TestServiceStartStopRestart/launchd`; **real:** the macOS runner smoke test: graceful `smind stop` stays stopped for 12 s, `kill -9` restarts |
| 4 install | `TestServiceInstallIdempotent` (both backends), `TestServiceInstallDoesNotRestartARunningDaemonOnAChangedUnit`, `TestServiceInstallNoStartStillEnables`, `TestServiceInstallRefusesToStartWhenForeignDaemonAnswers` (hand-started smind, an app-managed one, a non-smind listener; both backends; the foreign daemon stays up, the unit/plist is still installed and enabled), `TestServiceStartRefusesWhenForeignDaemonAnswers`, `TestServiceUninstallKeepsSMINDHOME` |
| 5 status | `TestServiceStatusExitCodes` (both backends, and through the CLI), `TestServiceStatusReportsDaemonVersusBinaryVersion`, `TestServiceStatusReportsAForeignListener`, `TestServiceStatusReadsThePortFromConfig`, `TestSystemdScopeAutoDetectsASystemUnit` |
| 6 healthz | `internal/server`: `TestHealthzIncludesManagedByWhenEnvSet`, `TestHealthzOmitsManagedByWhenUnset`; `cmd/smind`: `TestServe_DaemonLogAndManagedBy` (real daemon with and without `SMIND_SUPERVISOR`; the variable is removed from the daemon's own environment so spawned agents do not inherit it) |
| 7 daemon.log | `internal/logging`: `TestDaemonLogFileRotatesAtSize`, `TestDaemonLogFileMode0600`, `TestRotatingKeepsOnlyTheActiveFileWhenMaxFilesIsOne`, `TestRotatingOversizedSingleWriteIsKeptWhole`, `TestRotatingConcurrentWritersLoseNoWholeLine`, `TestTailFile`, `TestFollowSeesAppendsAndSurvivesRotation`; `cmd/smind`: `TestStderrStillReceivesLogs`, `TestDaemonLogFailureFallsBackToStderr`, `TestServiceLogsTailsDaemonLog`, `TestServiceLogsFollowStopsOnContextAndSeesAppends`; `internal/config`: `TestDefault_LogRotation`, `TestLoad_LogRotationOverrideAndValidation` |
| 8 install.sh | `scripts/install_test.sh`: `test_install_sh_service_flag_runs_service_install` (shim `smind`), `test_service_system_flag_passes_through`, `test_system_without_service_exits_2`, `test_service_install_failure_fails_the_script_but_keeps_the_binary`, `test_no_service_flag_never_runs_service_install`, `test_service_flag_on_msys_still_points_at_powershell` |
| review round | independent adversarial review (11 findings, 9 acted on): `TestStatusStartRestartUseTheInstalledUnitsHome` (the daemon's own `SMIND_HOME`/port after `install --home`, both backends), `TestInstalledHomeDefaultsWhenTheUnitHasNoSMINDHOME`, `TestInstallRejectsARelativeHome`, `TestUnitHomeRoundTripsThroughQuoting`, `TestLaunchdRefusesRootAndSudo`, `TestSystemdActingCommandsRefuseSudoOnAUserUnitAndNeedRootForASystemUnit`, `TestSystemInstallWarnsThatSudoPATHIsCaptured`, `TestSystemdBusFailureWordingVariants`, `TestSystemdStatusToolFailureIsAnErrorNotNotInstalled`, `TestSystemdUninstallStopsWhenSystemdCannotBeReached`, `TestLaunchdUninstallKeepsThePlistWhenBootoutFails`, `TestRestartRefusesWhenAStoppedServiceFacesAForeignDaemon`, `TestStartWaitsForOurOwnDaemonNotAnyoneElses`, `TestLaunchdStartRefusesWhenForeignDaemonAnswers`, `TestLaunchdRestartWaitsForThePortToBeFree`, `TestInstallSaysWhenTheRunningDaemonIsOlderThanThisBinary`, `TestRotatingRecoversWhenTheLogCannotBeReopened`, `TestDaemonLogSurvivesABrokenStderr`, `TestTakeSupervisorEnvRemovesItFromTheEnvironment`, `TestServiceLogsReadTheInstalledServicesHome`; mutation-checked (dropping the managedBy match, the launchd `waitDown`, the uninstall stop check each fails a test) |
| 9 docs | `docs/homelab.md` "Run the daemon as a service" (systemd user vs `--system`, linger, the LaunchAgent GUI-session limit, log location and rotation, desktop-app coexistence); README pointer |

`smind update`'s restart path (P6) needed no change: `HealthzDaemon.Restart` runs `<exe> service restart` and `Status` reads `managedBy`; both now have a real
counterpart. Coexistence with the desktop app needed **no app change**: a service-managed daemon already shows as `unmanaged` to the app, which never touches it
automatically (`launch_decision` → `UnmanagedBanner`) and offers *take over management* only on an explicit confirmation; hiding that button for a daemon
whose `/healthz` has `managedBy` is the P10 work (noted in `docs/homelab.md`).

Known gaps left: launchd `status` does not print enabled/disabled; a Go crash trace goes to stderr (journal on Linux, nowhere on macOS), not `daemon.log`; on Windows `logs -f` will hold the log open without FILE_SHARE_DELETE (P9).

Not verified: a real **reboot/logout cycle with lingering** and a real **macOS login cycle** (the runners prove install, stop-stays-stopped, crash restart and
restart, not boot persistence); `--system` against a real `/etc/systemd/system` (unit rendering and the `SUDO_USER` rules are unit-tested, the real run
is not); `smind update` driving a real `service restart` end to end; the desktop app's behaviour next to a real service.

### Fix: device_hello v2 + unknown-method answer (2026-10-15, `fix/device-hello-replay`)

`devices`: `TestDeviceHelloReplayInAnotherSessionRejected` (other client key / daemon key / session id / challenge each fail),
`TestV1HelloIsRefused`, `TestChallengeMessageRoundTrip`, `TestHelloVectorMatchesFile` (v2 vector incl. the challenge message and
three negatives), `TestV1VectorIsHistorical`; hub (real relay + hub): `TestCapturedHelloSessionCannotBeReplayedIntoANewRoute`
(a captured transcript — handshake frames + hello ciphertext — replayed into a new route is rejected and the real session
untouched; the test fails when the challenge/session binding is removed), `TestHelloBoundToTheChallengeAndTheSession`,
`TestEachSessionGetsAFreshChallenge`, `TestHelloVersion1IsRefusedWithAClearReason`; `wsapi`:
`TestUnknownMethodAlwaysGetsAMethodNotFoundResponse` (fails on the old ordering).

### P13b (2026-10-15, branch `feat/mobile-relay-device-identity`)

Commands: `cd mobile && npx tsc --noEmit && npx vitest run --exclude "**/*.node.test.ts"` (19 files, 119 tests),
`npm run test:integration` (3 files, 10 tests; builds `internal/relay/bridge/devharness`, a real relay + a real daemon hub with a
device registry + two Go desktop peers), `npm run smoke:web:full` (Expo web in headless Chromium against the same harness),
`go test ./...`, `go test -race -count=3 ./internal/relay/bridge/devharness/`, `task lint`.

| AC | Evidence |
|---|---|
| 1 key in the secure store | `deviceIdentity.test.ts`: `device key persists in the secure store`, `is generated once and differs between devices`, `replaces a corrupt stored value with a fresh key`, `never puts the seed in the hello, nor on the identity object`; `platformKeyStore.ts` wires `expo-secure-store` (device-bound, after-first-unlock; in memory on web) |
| shared vector | `deviceIdentity.test.ts`: `device_hello signature verifies against the Go vector (and matches it byte for byte)`, `derives the vector public key and device id`, `builds the vector signed bytes`, negative case `a hello signed for a different session does not verify against this one` — reads `internal/relay/devices/testdata/device_hello_vector.json` |
| 2 hello, keepalive, resume, ids | `RelayConnection.device.test.ts` (fake relay + daemon that verifies the hello with the real signed-bytes construction): `sends a device_hello that the daemon verifies, under a key-derived device id and a per-pairing session id`, `keepalive is sent on the websocket stream`, `dead stream after the silence window reconnects (and resumes the same session)`, `resumes after a drop keeping ids, sequence and the E2EE session`, `opens a fresh session … when the daemon lost the old one, and re-subscribes`, `rejects an in-flight request when the stream drops (at-most-once)`; `keepalive.test.ts` pins 30 s / 75 s / 10 s and the nonce-echo rule; `proto.test.ts`: `encodes Frame.keepalive (field 7) exactly as the Go relaypb does` (Go-computed literal), round trips |
| pending / revoked UI states | `RelayConnection.device.test.ts`: `a device the daemon has not approved is pending, then connects by itself once approved`, `a revoked device is revoked and stops trying`, `revocation mid-session …`, `… noticed only at the next hello …`; `connectionStatus.test.ts` (copy incl. the exact `smind relay devices approve <id>`); screenshots (below) |
| 3 help text | `CERTIFICATE_HELP` on the pairing screen (screenshot) and `docs/homelab.md` §6 |
| 4 interop, 3 devices | `integration.devices.node.test.ts` (real TS client ↔ real Go relay + daemon hub + 2 Go native desktop peers): pending → `approve` → connected by itself; peers join while the mobile client stays attached (3 approved + connected devices); real-relay keepalive echoes; resume after a cut stream with the peers unaffected; fresh session + re-subscribe after `daemon-restart` **with the stream left up** (liveness watchdog); relay restart; `revoke` refuses only the mobile device and the peers keep answering; a revoked key stays revoked and a new key is a new pending device. `integration.node.test.ts` / `integration.persistent.node.test.ts` now run through `device_hello` against the same hub |
| review hardening | `RelayConnection.device.test.ts` "review hardening": close during the handshake, daemon restart behind a live stream (pending call rejected, fresh session), idle probe, transient error while pending, `too_many_devices` retried, status 8 retried, flap backoff, daemon that never answers `device_hello` |

Screenshots (Expo web, light and dark, read by the Lead): `mobile/.smoke/{pairing,pending,tasks,revoked}-{light,dark}.png` from
`npm run smoke:web:full` ("Waiting for approval" with the approve command naming the device, the task list after approval, "This
device was revoked" with *Reset device identity*); not committed (`.smoke/` is gitignored).

`task test`: the Go suite passes (`go test ./...`) and so do the mobile suites; the **web UI suite did not pass cleanly on this
machine** (38 timeouts/failures in `App.test.tsx`, `app-menu`, `open-in-editor`, `desktop-chrome`, `App.responsive` with the load
average at ~150 from other agents; `App.test.tsx -t TB-AC4` passes alone) — no file under `web/` or `desktop/` is touched by this
branch.

Not verified: a real phone or simulator (Hermes, `crypto.getRandomValues`, `expo-secure-store` on iOS/Android — including whether
Android returns null instead of throwing on a Keystore mismatch, which would silently make the device a new pending device —, React
Native's `WebSocket`); the ≥ 10-minute run with two real desktop apps and a 125 s proxy shim; self-signed relay refusal on a device;
a Rust client in the three-device run (P13a). Labels with exotic Unicode format characters (`devices.ValidateLabel` follows Go's
`unicode.Cf`; the client's table covers the common ones and the label is app-generated today).
