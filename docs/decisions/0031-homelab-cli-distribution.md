# 0031: Homelab distribution — `smind update`, `smind service`, install scripts, Docker, direct and relay connections, desktop first-run

## Status

**Accepted (2026-10-14), except decisions 9, 12, 27–32 and 36, which remain Open** (direct connections with a password,
and the hosted relay with everything it gates). The accepted decisions are listed under "Decisions (accepted 2026-10-14)"
with the default the Human approved; the Open ones stay under "Open decisions for Human", and **every PR they gate stays
BLOCKED** in the plan
([`docs/plans/active/homelab-cli.md`](../plans/active/homelab-cli.md)). Decisions whose default changed *because of* the Paseo
study are marked **★**. Builds on ADR-0007/0011 (relay, admission),
ADR-0013/0023/0030 (desktop app, updater, native Windows daemon) and ADR-0029
(`POST /api/shutdown`). Several decisions here **amend** earlier ones (loopback
only — pending decision 9 —, one relay device per relay host, ephemeral device keys); each says so
and, once accepted, the amended ADR gets a one-line pointer here.

**Human answers recorded 2026-10-13/14:** the relay-side "workspace" is renamed **relay host** and smind's own
"Workspace" stays as is (D12); the hosted relay will be called `relay.spacingmind.com`, with DNS on Cloudflare, an
accepted trust model, and production deploys gated by a GitHub Environment with required reviewers (D11).
**The hosting choice is PENDING** (2026-10-14): Cloudflare Workers + Durable Objects (a WebSocket rewrite) is **not**
preferred any more; the options under study keep gRPC — Cloudflare's gRPC proxy in front of the unchanged Go relay,
or the Go relay in Cloudflare Containers behind a Worker (private beta, access requested) — and every hosted-relay PR
is **blocked on that decision**. **Multi-device (D9) is designed now on the existing gRPC protocol, independent of
hosting.** All other decisions were accepted with their recommended defaults on 2026-10-14, including 35 (device identity:
Ed25519 `device_hello` + pairing window + pending approval).

## Terminology: relay host, not workspace

smind has **workspaces** (a registered git repository, with spaces and tasks). The relay has
used the same word for something unrelated: the per-daemon record on a relay
(`smind relay workspace new`, `workspace_id` in `relay.proto` and in pairing offers, the
`ws` key of an offer). That caused a real misreading ("connecting creates a new workspace?").
It does not. In this ADR and plan the relay-side concept is called a **relay host** (id: *host
id*): **one daemon's mailbox on a relay**, made of a host id plus an admission secret.

> Connecting a daemon to a relay registers **this daemon once** with that relay (a relay host
> record and its admission secret). Nothing is created in smind's data model. Every client paired
> with that daemon then sees **all** of the daemon's workspaces, spaces and tasks, exactly as it
> would on a direct connection.

Where this ADR quotes today's code it keeps the code's word ("workspace") with the cited
file:line; everywhere else it says "relay host". The rename and its compatibility rules are D12.

## Reference: what Paseo does (studied 2026-10-13)

`refs/paseo` @ `8d9e7d3a`. Paseo is the product smind is meant to replace for
daily use, and it already ships a server/homelab story. Paths below are
relative to `refs/paseo/`. Every item section further down ends with a **Paseo
reference** block stating *ported / adapted / deliberately different (why)*.

| Area | What Paseo does | Source |
|---|---|---|
| Daemon lifecycle | `paseo daemon start` (background, "managed"), `run` (foreground, "deployment" for systemd/Docker), `stop`, `restart`, `status`, `reload`, `config get/set/unset`, `set-password`, `pair`. A **supervisor** process spawns a worker, restarts it on crash or on an in-band restart request, and rotates `daemon.log`. **No OS-service installer and no self-update.** Installed with `npm install -g @getpaseo/cli`. | `packages/cli/src/commands/daemon/{index,start,stop,restart,status,reload,config,local-daemon,set-password,pair}.ts`, `packages/server/scripts/supervisor.ts` (restart support `:58,179`, enabled by `supervisor-entrypoint.ts:170`; log rotation `:15-16,128-129`, defaults in `supervisor-log-config.ts:28-30`), `README.md:94-100` |
| State | `$PASEO_HOME`: `config.json`, `paseo.pid` (pid, startedAt, listen, `desktopManaged`), `daemon.log`, `daemon-keypair.json` (0600), `local-credential` (0600, rewritten each run, removed on exit) | `packages/server/src/server/{pid-lock,daemon-instance,daemon-keypair,local-credential}.ts`, `SECURITY.md` |
| Launch flags | `--port/--listen/--relay/--hostnames/…` were **removed**; the CLI rejects them and says "use `daemon config set <path> <value>`, then restart" | `packages/cli/src/commands/daemon/local-daemon.ts:82-110` |
| Stop / restart | In-band RPCs (`shutdownServer`, `restartServer`), never signals; `--force` only for a local home | `stop.ts:25`, `restart.ts:57` |
| Direct connections | Default `127.0.0.1:6767`; `daemon.listen` / `PASEO_LISTEN` can bind anywhere; optional **bcrypt-hashed password** (`daemon set-password`, `PASEO_PASSWORD`) sent in the WS `hello`; **Host-header allowlist** against DNS rebinding (`PASEO_HOSTNAMES`) | `packages/server/src/server/config.ts:470-480`, `auth.ts:35`, `session-admission-auth.ts`, `hostnames.ts`, `SECURITY.md` ("Local daemon trust boundary", "DNS rebinding protection") |
| Relay | **Hosted** default `relay.paseo.sh:443`, off by default and enabled with a consent prompt (`daemon pair --relay`); multi-client: one Durable Object per `serverId`, a daemon **control socket** plus one daemon **data socket per client `connectionId`**, many client sockets per connection, a separate E2EE handshake for each | `packages/relay/src/cloudflare-adapter.ts:119-127,340-395`, `packages/server/src/server/relay-transport.ts:49-55`, `config.ts:39,302-307`, `.github/workflows/deploy-relay.yml` |
| Pairing offer | `{v:2, serverId, daemonPublicKeyB64, relay:{endpoint,useTls}}` in a `#offer=` fragment — **no secret**; ≈ 230-byte link that fits a QR. Also a compact `relay://host:port/<serverId>?key=…&ssl=true[&password=…]` URI | `packages/protocol/src/connection-offer.ts:10-17`, `daemon-endpoints.ts` (`serializeRelayConnectionUri`), `packages/cli/src/commands/daemon/pair.ts` |
| Docker | Official multi-arch image on ghcr.io: daemon listens `0.0.0.0:6767`, serves the bundled web UI, `-p 6767:6767` works, `PASEO_PASSWORD` recommended (warning if unset), agent CLIs **not** baked in (child-image example), root entrypoint chowns volumes then drops to uid 1000 with `gosu`, `tini`; non-publishing build on PRs, publish on stable tags | `docs/docker.md`, `docker/base/Dockerfile`, `docker/base/rootfs/usr/local/bin/paseo-docker-entrypoint:60-65`, `docker/{README.md,docker-compose.example.yml,Dockerfile.agents.example}`, `.github/workflows/docker.yml` |
| Desktop daemon | Built-in daemon **auto-started silently** when `manageBuiltInDaemon` (default true) and no local override; toggled in settings with a confirm; the daemon **stops on quit** (`keepRunningAfterQuit` default false); the daemon's own lock file records `desktopManaged`, and the app stops only a daemon it "owns" (pid + startedAt match) | `packages/app/src/desktop/settings/desktop-settings.ts:36-39`, `packages/app/src/app/_layout.tsx:362-370`, `packages/app/src/desktop/daemon/daemon-management-toggle.ts`, `packages/desktop/src/daemon/{daemon-manager.ts:83-84,189-194,quit-lifecycle.ts:56}` |
| Onboarding / hosts | `WelcomeScreen` (scan QR / direct connection / paste pairing link / remote SSH), redirects once any host is online; a host has several connections (relay, direct TCP, **remote SSH**) with a preferred one — Maestro flow `relay-then-direct` pairs over the relay then adds a direct endpoint and expects "Connected via <direct>"; **host confirmation** (key fingerprint + relay endpoint) before saving a host from a pairing link | `packages/app/src/components/{welcome-screen,add-host-modal,pair-link-modal,add-host-method-modal}.tsx`, `src/types/host-connection.ts`, `src/runtime/host-confirmation.ts`, `packages/app/maestro/{relay-pairing,relay-then-direct}.yaml`, `packages/{app,desktop}/e2e/…pair-device-relay.spec.ts`, `packages/cli/src/ssh/ssh-tunnel.ts`, `packages/protocol/src/ssh-transport.ts` |

## Context: smind today

**CLI and install**

- `smind` has no `update` and no `service` command. The dispatcher knows
  `serve stop relay workspace account space task profile usage mcp skill
  version` (`cmd/smind/main.go:40-66`; usage text `:75-132`). `smind serve`
  takes no arguments (`cmd/smind/serve.go:34-37`): the port comes from
  `~/.smind/config.yaml` `server.port` (`internal/config/config.go:90-93`) and
  is read the same way by every CLI client (`cmd/smind/client.go:41-49`,
  `cmd/smind/stop.go:52`).
- `scripts/install.sh` installs `smind_<v>_<os>_<arch>.tar.gz`, verifying only
  the SHA-256 in the `checksums.txt` fetched from the same release
  (`scripts/install.sh:69-82`). Version pinning is the `SMIND_VERSION` env only
  (`:7`, `:56`). It still says "no native Windows build yet" (`:13-14`,
  `:30-32`) although v0.11 ships `smind_<v>_windows_<arch>.zip`
  (`.github/workflows/release-please.yml:139-154`); README `:123-126` is stale
  the same way (and says the app manages the daemon "via WSL2", which ADR-0030
  removed).
- `https://spacingmind.com/install.sh` is a **307 redirect** to
  `raw.githubusercontent.com/spacingmind/smind/master/scripts/install.sh`
  (`curl -I`, 2026-10-13). The redirect lives in the landing repo
  (`spacingmind/spacing-mind-landing-page`, `apps/web/next.config.ts`
  `redirects()`, destination `siteConfig.links.installScript`; comment: "served
  from smind's master branch, which only moves on release"). `/install.ps1` is
  404. A new script is live at spacingmind.com only after a release moves
  `master` *and* a landing PR adds the redirect.
- Release assets: per-OS archives plus one `checksums.txt` built in the same
  `publish` job that uploads them (`release-please.yml:307-317`, `:332-342`).
  `.sig` files exist only for the desktop updater bundles; the minisign public
  key is `desktop/src-tauri/tauri.conf.json:25`, the private key is the
  `TAURI_SIGNING_PRIVATE_KEY` secret (ADR-0023). A compromised release could
  swap an archive *and* `checksums.txt`; the checksum only detects corruption.
  No Docker image is built anywhere.
- Version signal exists: `/healthz` returns `{status, service, version}`
  (`internal/server/server.go:148-153`), `smind --version` (`main.go:63`).
- **The daemon binds `127.0.0.1` only** (`Server.Addr`,
  `internal/server/server.go:139-146`) and serves `GET /api/token`
  **unauthenticated** (`:170-174`; its doc comment: "scoped to smind's current
  single-user-localhost posture; revisit if remote access is ever added"). The
  WebSocket takes that token as a query parameter (`internal/wsapi`). So today
  `-p 4648:4648` on a container cannot work, and a same-host reverse proxy
  would publish the token. Remote access today is the relay or an SSH/Tailscale
  tunnel to loopback. **There is no password, no Host-header allowlist and no
  way to listen elsewhere.**
- Graceful stop is SIGINT/SIGTERM *or* `POST /api/shutdown`, one shutdown path
  (`cmd/smind/serve.go:118`, `:130-147`; ADR-0029 D2;
  `internal/server/server.go:183-186`); `smind stop` waits up to 30 s
  (`stop.go:17-22`). A clean shutdown exits 0 (`serve.go:147`), which the
  launchd `KeepAlive` design below relies on.
- The daemon logs with the standard logger to stderr; there is no log file
  option or rotation.
- Agent providers are external processes: GLM runs `npx -y glm-acp-agent@1.14.0`
  (`internal/acp/glm.go:32`) and the Claude runner finds `claude` via
  `exec.LookPath` (`internal/claudecli/resolve.go:45`). A service or container
  environment must provide `PATH` with `git`, `node`/`npx` and the agent CLIs.

**Relay pairing (re-verified)**

- The relay server is `smind relay` (`cmd/smind/relay.go:34-74`; defaults
  `:7400` native gRPC and grpc-web `:7401`, `internal/relay/server/run.go:51-54`).
  There is **no hosted relay**: `docs/ROADMAP.md:98` still lists "Deploy relay at
  `relay.spacingmind.sh`" as unchecked, and that host does not resolve.
  (In the next bullets "workspace" is today's code word for what this ADR calls a
  **relay host**; see Terminology.) `smind relay workspace new <id>` prints the host secret once
  (`relay.go:83-93`); `smind relay connect <addr> <workspace-id> <secret>`
  stores the config and pins the relay's TLS fingerprint **trust-on-first-use**
  (`relay.go:130-155`); `smind serve` then dials the relay on every start
  (`serve.go:128`, `:150-176`).
- `smind relay offer` prints **only a URL** (`relay.go:213`), takes no flags
  (`:176-178`), and says "for a mobile device" (`:153`, `:252`).
  `Offer.QRText`/`QRPNG` exist (`internal/relay/pairing/qr.go:218-275`) but
  **nothing calls them** outside the package.
- **A realistic offer does not fit the QR encoder.** The in-tree encoder stops
  at version 15; at level M that is 412 bytes (`qr.go:32`, `:86`). An offer for
  `https://homelab-relay.example.com:7401`, workspace `homelab`, a pinned
  fingerprint and a 32-byte secret is a **432-byte URL** (throw-away test,
  2026-10-13) and returns `ErrContentTooLong` at level M; level L is supported
  (`qr.go:72`) but unused. Paseo's offer is ≈ 230 bytes because it carries no
  secret and no fingerprint (see "Paseo reference" in D6). Our offer also
  repeats the key: `id` is the daemon's base64 public key
  (`internal/relay/bridge/bridge.go:57-59`) and `pk` is the same key again.
- The offer is the **workspace master secret** plus the daemon public key and the
  relay's grpc-web address (`internal/relay/pairing/offer.go:64-82`; the address
  is derived as native port + 1, `relay.go:163-174`,
  `desktop/daemon-client/src/relay/client.rs:45-68`). It has **no expiry, is not
  single-use, and is identical every time it is printed**; there is no
  per-device credential (`offer.go:71-81` calls this "a known milestone-1
  simplification"; ADR-0011 defers per-device identity). The payload lives in the
  URL fragment (`offer.go:173-211`).
- `DefaultPairURL` is `https://spacingmind.sh/pair` (`offer.go:28-31`);
  `spacingmind.sh` does not resolve (2026-10-13). Harmless — clients parse only the
  fragment — but it can never be a universal link.
- The mobile app is **paste-only** (`mobile/src/screens/PairingScreen.tsx:3`,
  `:50`; no camera/barcode dependency under `mobile/`). A QR is not scannable by
  any smind client today.
- **One attached device per workspace.** The bridge hardcodes a single
  `(session, device)` pair (`internal/relay/bridge/bridge.go:12-35`) and the
  relay's `attachRoute` cancels the previous stream for the same route key and
  replaces it (`internal/relay/server/server.go:576-604`); the control stream
  rejects everything but `Ping` with `Unimplemented` (`:388-395`) although
  `relay.proto` already declares `ControlFrame.DeviceAttach`/`DeviceDetach`
  (`internal/relay/relaypb/relay.proto:121-148`).
  `docs/plans/completed/desktop-relay-transport.md` records the consequence: a
  mobile device and a desktop connection to the same workspace cannot both be
  attached; connecting one disconnects the other. The desktop client reconnects
  automatically with backoff capped at 30 s
  (`desktop/daemon-client/src/relay/client.rs:39`, `:485-491`), so two clients
  will likely evict each other repeatedly (unverified; test scenario in the
  plan). **Paseo solves exactly this** (see D9).
- Desktop side: Rust supports relay connections and exposes
  `connections_add_relay(label, pairing_url)`
  (`desktop/src-tauri/src/commands.rs:76-120`, registered `lib.rs:105`,
  `build.rs:34`, allowed by `capabilities/proxy.json:13`). The bundled UI never
  calls it: `DesktopApi` has only `addConnection(label, url)`
  (`web/packages/ui/src/lib/platform.ts:98-102`, `:196-202`, `:257-263`) and
  Settings → Connections is a label + URL form
  (`web/packages/ui/src/components/settings/connections-section.tsx:53`,
  `:128-147`). **Footgun:** pasting a pairing link there goes to
  `connections_add`, whose `validate_base_url` accepts any http(s) URL and strips
  path and fragment (`desktop/daemon-client/src/proxy/connections.rs:41-62`), so
  it silently saves a bogus direct connection to `https://spacingmind.sh`.
- A relay connection's `base_url` is `relay://<workspace>@<native-addr>`
  (`connections.rs:157-173`); `connection_version` probes `GET <base_url>/healthz`
  with `reqwest` (`desktop/src-tauri/src/daemon_manager.rs:149-165`,
  `:1008-1024`), which cannot work for `relay://`, so the existing "daemon is
  older than this app" banner
  (`web/packages/ui/src/components/desktop-daemon-banner.tsx`) **can never show
  for a relay connection**. No WS RPC returns the daemon version.
- `smind://` deep links exist but only navigate to tasks
  (`desktop/src-tauri/src/deeplink.rs:1-5`; scheme `tauri.conf.json:30-35`).

**Desktop first launch (audit, 2026-10-13)**

- The registry always contains a built-in, non-removable `local` entry at index 0,
  selected on a fresh registry
  (`desktop/daemon-client/src/proxy/connections.rs:1-3`, `:100-107`, `:216-248`).
  No "first run completed" flag, no onboarding screen, no setting to stop the app
  managing a local daemon.
- **macOS:** `auto_start` runs on every launch (`lib.rs:303-307`,
  `daemon_manager.rs:313-345`): with `local` selected and nothing answering it
  **silently installs the bundled daemon and starts it**; an older managed daemon
  is offered an update; an unmanaged owner of the port is never touched
  (`launch_decision`, `:295-310`).
- **Windows:** no auto-start (`#[cfg(target_os = "macos")]`, `lib.rs:303`). A fresh
  launch shows "Can't reach Local … Start the daemon (`smind serve`), or switch to
  a different connection"
  (`web/packages/ui/src/components/desktop-unreachable.tsx`); the only route is
  Settings → Daemon → Install. A parallel branch
  (`feat/windows-bundled-daemon`) is changing Install/Update to copy a bundled
  `smind.exe`.
- **Linux (deb):** `Platform::Unsupported` (`daemon_manager.rs:94-108`): no daemon
  management at all.
- **Unmanaged port owner:** `classify` = Managed only if the recorded pid owns the
  port, else Unmanaged (`desktop/daemon-client/src/daemon_manager/managed.rs:43-51`);
  install is refused (`:64-70`); *Take over management* is offered with a confirm.
  Windows refuses take-over unless the owner image is `smind.exe`
  (`daemon_manager.rs:389-397`); on **macOS it is never refused** (doc at `:389`,
  test `:1215`), so Take over can adopt *any* port owner (e.g. the `ssh` process of
  an `ssh -L 4648:…` tunnel) and a later Restart/Update treats that executable as
  "ours".
- URL connections need no token entry: the proxy forwards the remote's
  unauthenticated `GET /api/token` (ADR-0013 decision 1).
- `desktop/ui/offline.html` is the Tauri `frontendDist` (`tauri.conf.json:7`)
  fallback; `desktop-unreachable.tsx` took over its job (ADR-0013 Consequences).
- ZCode's first-run screen is an account login with a "skip" escape
  (`refs/zcode/packages/ui/src/WelcomeScreen.tsx`); it has no connection
  onboarding, so it informs only visual style.

## Decision

### D1. `smind update`

`smind update [--check] [--version <v>] [--force] [--dry-run] [--json]`.

- Source: GitHub Releases of `spacingmind/smind`; "latest" is resolved like
  `install.sh` (the `releases/latest` redirect, not the rate-limited API;
  `install.sh:58-62`). Stable channel only — `release-please-config.json` sets
  `prerelease: false` and the desktop updater relies on it (ADR-0023 §3).
- Verification: download `smind_<v>_<os>_<arch>.tar.gz|zip` and `checksums.txt`,
  verify the SHA-256 like `install.sh` (`:79-82`) and ADR-0030's Windows install;
  extract **only** the `smind`/`smind.exe` member (reject path traversal and
  oversize members); run the new binary's `version` to confirm it matches the
  target before swapping. Signature verification is open decision 1.
- Refusals (non-zero, one-line reason, nothing changed): running agent work exists
  (open decision 4); the current binary is a `dev` build; the running binary lives
  inside the desktop app's bundle or managed-daemon directory (ADR-0013/0030: that
  daemon belongs to the app); the target is older than current unless `--force`
  (the store may have migrated forward); the binary's directory is not writable
  (message names the cause; never self-elevates); running inside a container.
- Active-run detection: dial the local daemon (`dialDaemon`,
  `cmd/smind/client.go:25`) and call `run.list` (`internal/wsapi/handlers.go:67`);
  a daemon that is not running means "nothing to protect".
- Atomic replace: write `<dir>/.smind.new.<pid>`, `chmod 755`, keep the old binary
  as `smind.old`, `rename` over the target (same pattern as `install.sh:97-100`).
  **Windows** cannot overwrite a running `smind.exe` (and the daemon re-executes
  `smind.exe` as the Claude launcher, `cmd/smind/main.go:24`) but can rename it:
  rename `smind.exe` → `smind.exe.old`, move the new file in, delete `.old` on the
  next start or update; roll back from `.old` if the post-swap check fails.
- Restart: only a **service-managed** daemon is restarted (D2's `service restart`,
  then poll `/healthz` until `version == target`, rolling back on timeout). A
  manually started daemon is never respawned silently; the command prints
  "restart it with `smind stop && smind serve`" (open decision 5).
- `--check` prints current/latest and exits 0 either way (`--json` for scripts);
  `--dry-run` does everything except the swap. `SMIND_UPDATE_BASE_URL` redirects
  downloads to an `httptest` server (test seam, unset in production).

**Paseo reference — deliberately different.** Paseo has no `update` command: it
is an npm package, installed and upgraded with `npm install -g @getpaseo/cli`
(`README.md:94-100`), and its Docker image is replaced by pulling a new tag. smind
is a single static binary with no package manager in the loop, so a self-updater
is the equivalent of `npm update`; verification therefore has to be ours (npm's
registry integrity has no counterpart here). Adopted from Paseo's lifecycle
style: the **graceful, in-band stop/restart before and after the swap** rather
than killing processes (`stop.ts:25`, `restart.ts:57`; ours is
`POST /api/shutdown`, ADR-0029), and the post-restart **confirmation that a
replacement with a new identity is serving** (`restart.ts:57-110` compares worker
pid and serverId) — ours is "healthz reports the target version".

### D2. `smind service install|uninstall|status|start|stop|restart|logs`

One `internal/service` interface with three backends; the CLI is identical on
every OS.

| | Linux | macOS | Windows |
|---|---|---|---|
| unit | systemd **user** unit `~/.config/systemd/user/smind.service` (default) or `--system` unit `/etc/systemd/system/smind.service` | LaunchAgent `~/Library/LaunchAgents/com.spacingmind.smind.plist` | per-user **Scheduled Task** `smind` at logon |
| start/stop | `systemctl [--user]` | `launchctl bootstrap/bootout/kickstart gui/<uid>` | `schtasks /Run`, `smind stop`, `schtasks /End` fallback |
| restart policy | `Restart=on-failure`, `RestartSec=5`, `TimeoutStopSec=30` | `KeepAlive={SuccessfulExit:false}` | task restart-on-failure (3× / 1 min) |
| logs | the daemon's own `daemon.log` (decision 7); journald also available | same | same |

Rules common to all backends:

- `ExecStart` is the absolute, symlink-resolved path of the running `smind`
  followed by `serve`; install warns if that path looks temporary.
- **Environment.** Service managers start with a minimal `PATH`, so the unit
  captures the installing shell's `PATH` (override `--path`) so `git`, `node`,
  `npx` and agent CLIs resolve. `SMIND_HOME` is written only if set (override
  `--home`). There is **no `--port`/`--listen`**: configuration is in
  `config.yaml`, edited with `smind config set` (decision 8), because CLI and
  daemon must agree.
- A graceful stop means exit 0: systemd `KillSignal=SIGTERM` reaches `serve.go:118`;
  launchd `SuccessfulExit:false` does *not* resurrect a daemon stopped with
  `smind stop`; a crash still restarts.
- No sandboxing directives (`ProtectHome`, …): agents must write to the user's
  repositories.
- `install` is idempotent, enables the unit and starts it unless `--no-start`. If
  something already answers `/healthz` on the configured port that this service did
  not start, install enables the unit but refuses to start it and says why; it
  never kills a foreign daemon.
- `uninstall` stops and removes the unit; it never deletes `SMIND_HOME`.
- `status` reports installed/enabled/active, pid, port, `/healthz` version vs the
  binary's, and the backend; exit codes follow `systemctl` (0 running, 3 not
  running, 4 not installed).
- Linux `--user`: the unit only runs at boot if lingering is enabled; `install`
  checks `loginctl show-user` and prints `sudo loginctl enable-linger $USER` when
  off. `--system` writes `User=<invoking user>` (from `SUDO_USER`), never root.
- Windows: the task registers via XML (`schtasks /Create /XML`) so restart settings
  can be expressed and runs a hidden launch of `smind.exe serve` (the daemon writes
  its own `daemon.log`, so no shell redirection is needed). Whether registering a
  logon task needs no elevation and whether the hidden-launch wrapper behaves on
  Windows 10/11 are **to be proven on a real machine** (plan spike). A task runs
  only after login; a headless Windows box needs auto-logon — documented, not
  solved here.
  **Measured in P9 (2026-10-17, `windows-latest` = Windows Server 2025; Windows 10/11 client unverified):**
  a standard local user (Medium integrity, not in Administrators) registers, runs, ends and deletes the logon task
  with `schtasks` — no elevation; `conhost.exe --headless smind.exe …` gives the daemon a console but no window, in
  the interactive session; **but** `conhost --headless` exits 0 whatever the child's exit code, so the task's
  `RestartOnFailure` never fires for a crashed daemon, and `schtasks /End` ends only conhost and leaves the daemon
  running. Hence the task runs `smind.exe service run` (a small supervisor inside a kill-on-close Job Object that
  restarts a crashed daemon 3× at 1-minute intervals, as `RestartOnFailure` would) and `service stop` kills what `/End`
  leaves. The task XML must be UTF-16 (UTF-8 is rejected: "unable to switch the encoding").

**Coexistence with the desktop app.** The app already refuses to install over an
unmanaged owner (`managed.rs:64-70`) and starts nothing when the port is held
(`launch_decision`), so it will not fight a service. What must change:
(1) a service-managed daemon must be recognised as *service-managed*, so the UI
hides **Take over** and says "managed by systemd/launchd/Task Scheduler — stop
the service to hand it to the app" (taking over a supervised daemon would make the
supervisor restart it); the signal is an additive `/healthz` field `managedBy`
set from an env var the unit defines (`SMIND_SUPERVISOR=systemd|launchd|taskscheduler`; the Windows value was `task` until P9, changed on the Human's instruction 2026-10-17)
(open decision 6); (2) macOS Take over must be refused unless the port owner is a
`smind` image, closing the `ssh -L` footgun (a bug fix, no decision needed);
(3) "Run smind on this computer" is disabled with an explanation while a service
owns the port, and `smind service install` refuses to start while the app's managed
daemon owns it.

**Paseo reference — deliberately different, plus two ports.** Paseo ships **no OS
service installer**: `daemon start` is a one-shot background launch whose
*supervisor* process restarts the worker (`supervisor-entrypoint.ts:170` sets `restartOnCrash: true`), and for servers
it documents running `daemon run` (foreground, "deployment" mode) under your own
systemd/Docker (`start.ts:36-55`, `local-daemon.ts:59`). smind keeps a first-class
`service` command because Human asked for boot persistence on a homelab and
because a Go single binary has no Node supervisor to lean on; the OS service
manager *is* our supervisor. `smind serve` already equals Paseo's `daemon run`.
**Ported:** (a) the daemon owns a **rotating `daemon.log`** in its home
(`supervisor.ts:15-16,128-129`, `daemon-instance.ts:51-55`), which removes the
shell-redirect and per-OS log-location code I had planned (decision 7);
(b) **status as an observation of the daemon's published endpoint**
(`status.ts`), which our `status` follows. **Adapted:** Paseo marks ownership
*inside the daemon's own lock file* (`pid-lock.ts` `desktopManaged`; the app
stops a daemon only if pid + startedAt match, `daemon-manager.ts:189-194`); we use
an env-derived `/healthz managedBy` because the app already reads `/healthz`
(`probe_healthz`) and the service may not be running when it looks. We do **not**
port `daemon start` (a detached background launch without a service manager;
decision 26).

### D3. Install scripts

- `scripts/install.sh`: add `--version <v>` (alongside `SMIND_VERSION`),
  `--service` (runs `smind service install` after installing; `--system` passes
  through) and `--help`; document `curl -fsSL … | sh -s -- --service`. For
  `MINGW*|MSYS*|CYGWIN*` print the PowerShell one-liner instead of the stale
  message (`:30-32`); fix the header comment (`:11-14`) and README `:123-126`.
  Checksum stays mandatory.
- New `scripts/install.ps1` for `irm https://spacingmind.com/install.ps1 | iex`:
  OS architecture (not process architecture, as ADR-0030) → asset name, TLS 1.2
  for Windows PowerShell 5.1, `Get-FileHash` SHA-256 against `checksums.txt`,
  extract only `smind.exe` from the zip (zip-slip safe), install to
  `%LOCALAPPDATA%\Programs\smind\`, add it to the user `PATH`. **It must not
  install into `%LOCALAPPDATA%\smind\bin\`**: that is the desktop app's
  managed-daemon directory (ADR-0030). Warn if Git for Windows is missing
  (ADR-0029 D1). Parameters arrive via env (`SMIND_VERSION`, `SMIND_INSTALL_DIR`)
  since `irm | iex` cannot take switches; `-Service` works when invoked as a script
  block.
- Serving: `install.ps1` needs a landing redirect identical to `install.sh` (a
  follow-up PR in the landing repo, **not part of this repo's PRs**) and goes live
  only after a release moves `master`; until then the documented URL is the raw
  GitHub one.

**Paseo reference — deliberately different.** Paseo has no install script (npm for
the CLI; app installers from GitHub releases and paseo.sh/download,
`README.md:88-100`). The curl|sh and `irm|iex` one-liners are the smind equivalent
of `npm install -g`; there is nothing to port. The "server" quickstart shape *is*
ported: install the CLI, start the daemon, then choose how clients connect
(`README.md:94-100`, `onboard.ts`) — see D6/D8.

### D4. "Daemon is older than this app" notice with a copyable command

Extend the existing banner for non-managed connections (remote/url/relay and
service-managed local) with the exact command in a copy-to-clipboard block. Which
command depends on the daemon's version: a daemon older than the first release
that contains `smind update` lacks the command, so the banner shows the installer
one-liners (both `curl … | sh` and `irm … | iex`, since the app cannot know the
remote OS) and `smind update` once the version has it; a Docker daemon is told to
`docker pull`. **For relay connections the version must come from a new additive
WS RPC `daemon.info`** (open decision 17), because `/healthz` is unreachable
through `relay://`. A relay daemon too old to answer shows "version unknown" with
the installer commands, never a false "up to date".

**Paseo reference — adapted.** Paseo reads the daemon version over the normal
client connection (the WS `server_info` message and `getDaemonStatus` RPC,
`packages/cli/src/commands/daemon/pair.ts` feature-checks `daemonStatusRpc`),
which is why its version-skew UX works for relay hosts with no extra probe — and it
tells the user "Update the Paseo daemon before pairing from this command" rather
than offering a command. Our `daemon.info` is the same idea (version over the
existing channel); the copyable *command* is ours, because Human asked for it.

### D5. Docker image

`ghcr.io/spacingmind/smind`, `linux/amd64` + `linux/arm64`, built by a new job in
`release-please.yml` that runs after `publish` and **reuses the release's own linux
binaries** (the Dockerfile `COPY`s the per-arch binary via `TARGETARCH`; no Go build
inside Docker, no QEMU compilation, byte-identical to the checksummed release).
Tags: `<version>`, `<major>.<minor>`, `latest` — stable releases only. Pull
requests that touch Docker files build the image **without pushing** (Paseo's
non-publishing check); `workflow_dispatch` can republish.

- Base `debian:bookworm-slim`. Contents: `smind`, `git`, `openssh-client`,
  `ca-certificates`, `curl` (healthcheck), `gosu`, `tini` (PID 1; zombie reaping),
  Node 22 LTS (GLM's `npx`).
- **Entrypoint (ported from Paseo):** starts as root only to create/chown the
  mounted `/data` and `/home/smind` if they are root-owned, then drops to the
  non-root `smind` user (uid/gid 1000, matching most host users) with `gosu`; if
  started as non-root it just runs. This fixes the bind-mount permission pain that
  `--user` workarounds only paper over.
- **Listening and auth (depends on decision 9):** the image sets
  `SMIND_LISTEN=0.0.0.0:4648` and **refuses to start without `SMIND_PASSWORD`**
  (Paseo only warns, `paseo-docker-entrypoint:60-65`), serves the web UI on the same
  origin, and `-p 4648:4648` works. Until decision 9 lands the image is relay-only
  (loopback bind, do not publish 4648).
- State: `SMIND_HOME=/data` (`VOLUME`), repositories under a second mount
  (`/workspaces`); stored paths are container paths. Agent credentials persist under
  `/home/smind` (`.claude`, `.codex`), as in Paseo.
- `HEALTHCHECK curl -fsS http://127.0.0.1:4648/healthz`.
- Shipped files: `docker/docker-compose.example.yml`,
  `docker/Dockerfile.agents.example`, `docs/homelab.md` Docker section.
- **Honest limits:** (1) agent CLIs (`claude`, `codex`) are **not** baked in:
  third-party binaries with their own licences, versions and credentials; the docs
  give a `FROM ghcr.io/spacingmind/smind:<v>` + `npm i -g …` child image, and
  `docker exec -it --user smind <c> claude` for first login. GLM works out of the box
  except it downloads its agent with `npx` on first use. (2) Browser OAuth
  (`smind account login`) wants a localhost callback and is awkward in a container;
  use `smind account add` (credential on stdin), provider env vars, or a mounted
  credential directory — to be verified in the Docker PR. (3) `smind update` inside
  a container refuses ("pull a new image tag"). (4) The container is the isolation
  boundary: agents can read/write whatever is mounted.

**Paseo reference — ported, with three deliberate differences.** Ported:
multi-arch ghcr.io image, `0.0.0.0` listen + published port + password for
network-reachable use, bundled web UI on the same origin, agent CLIs **not** baked
in with a child-image example, volumes split into state/home and workspace, root
entrypoint that fixes ownership then `gosu`s to a non-root user, `tini`, non-publishing
build on PRs and publish on stable tags
(`docs/docker.md`, `docker/base/Dockerfile`, `paseo-docker-entrypoint`,
`.github/workflows/docker.yml:37,129`). Different: (1) **built from the release
binaries**, not from source inside Docker (Paseo packs npm workspaces; ours is a
checksummed Go binary); (2) **no start without a password** when the bind is
non-loopback (Paseo warns and continues); (3) no example `PASEO_PASSWORD=change-me`
in the docs — examples read the password from an env file or a generated value.

### D6. Relay pairing for desktop clients

**What connecting does.** `smind relay connect` registers **this daemon once** with the relay (a
relay host + admission secret). It creates nothing in smind's data: every client paired afterwards sees
**all** of the daemon's workspaces, spaces and tasks (see Terminology).

- `smind relay offer` prints the URL (stdout, unchanged for scripts) and, with a TTY
  on stdout or `--qr`, a terminal QR on stderr; `--png <file>` writes a PNG at mode
  0600. QR encoding tries level M, falls back to level L when M does not fit, and
  if the offer still does not fit prints the URL plus a one-line "too long for a QR
  code" note instead of failing. Wording becomes "for a mobile device or the desktop
  app". The paste-able URL is what works today for both clients; the QR is
  convenience for a future mobile scanner.
- Offer semantics (open decision 15): reusable, non-expiring, equivalent every time,
  because it *is* the relay host's secret and the relay has no per-device credential;
  the CLI says so ("treat this URL like a password; to revoke, run `smind relay host
  rotate`"). Optionally a **daemon password** (decision
  14) becomes the second factor so a leaked offer alone is not enough.
- Desktop UI: Settings → Connections gains **Add via relay link**: paste → Rust
  parses (new `connections_preview_relay` returns only non-secret fields: relay
  host:port, relay host id, grouped daemon-key fingerprint, whether the relay is
  fingerprint-pinned or CA-verified) → the user confirms those values → `connections_add_relay`.
  The pasted string is held in component state only until submit and never logged.
  The existing URL form rejects any input containing `#offer=` and points to the new
  action (fixing the footgun). E2EE and fingerprint pinning are unchanged
  (ADR-0007/0011).
- Deep link `smind://pair#offer=…`: not in v1 (decision 16); when added it opens the
  same confirmation screen and never adds a connection from a click alone, because a
  web page can trigger `smind://`.
- Homelab quickstart `docs/homelab.md` documents the paths: direct connection (D8) for
  LAN/VPN; the **hosted relay** (`smind relay connect` with no address, D11; **pending**) for NAT/mobile; and a
  self-hosted relay (a VPS or the homelab with `7400/tcp` + `7401/tcp` open — the desktop derives the
  native port as grpc-web − 1, `client.rs:45-68`; compare the fingerprint printed by `smind relay` with the one
  `relay connect` pins).

**Paseo reference — adapted (offer, confirmation, relay opt-in) and deliberately
different (offer contents, hosting).**
- *Ported:* the confirmation step before saving a host from a pairing link, showing
  the daemon key fingerprint and the relay endpoint
  (`packages/app/src/runtime/host-confirmation.ts`; `kind: "newHost" |
  "changedConnection"` — we also confirm when the same workspace comes back with a
  **different key or relay**); paste-a-link as a first-class path
  (`pair-link-modal.tsx`, Maestro `relay-pairing.yaml`); relay as an explicit,
  **opt-in** choice with a plain-language consent ("Your connection is end-to-end
  encrypted. Paseo cannot read your code or messages.", `pair.ts:106-109`;
  `config.ts:302-307` keeps relay off by default) — our `relay connect` is already
  opt-in, and the docs/CLI text should carry the same consent wording.
- *Different, offer contents:* Paseo's offer is `{v, serverId, daemonPublicKeyB64,
  relay}` and **contains no secret** (`connection-offer.ts:10-17`): the relay is a
  public service with no admission, the E2EE handshake with the daemon's key is the
  trust anchor, and an optional daemon password is sent *inside* the encrypted
  hello (`SECURITY.md`, "Trust model"). smind's relay is private and gated by a
  relay host admission secret (ADR-0011), so our offer has to carry that secret
  — which is why it is a "password-like" link and why it does not fit a QR.
  Shrinking it (drop the duplicate `id`, base64url the fingerprint) is possible but
  needs a v2 offer decoded by Go, Rust and TS; deferred, level-L fallback instead.
  Paseo also offers a compact `relay://host:port/<serverId>?key=…[&password=…]` URI
  (`daemon-endpoints.ts`) for typing/pasting; we do not need a second format yet.
- *Hosting:* Paseo operates `relay.paseo.sh` (a Cloudflare Worker + Durable Object that currently forwards to a Fly
  deployment); smind's hosting for `relay.spacingmind.com` is **pending** (D11).
- *Not ported yet, recorded:* a host with **several connections and a preferred one**
  (relay now, direct when reachable; Maestro `relay-then-direct.yaml`; Paseo's
  `HostProfile.connections` + `preferredConnectionId`, `host-connection.ts`) — smind's
  registry has one URL per connection; keep connection ids derived from the daemon
  identity so this can be added without migration (decision 25).

### D7. Desktop first-run and connection onboarding (macOS + Windows)

**Settings model.** A small Rust-owned `app-settings.json` next to `connections.json`
(not inside the registry, whose load path deliberately tolerates and rewrites corrupt
files, `connections.rs:216-248`): `{ onboardingCompleted: bool, manageLocalDaemon: bool
}`. `manageLocalDaemon = false` is the **"no local daemon" mode**: the app never
installs, starts, updates or takes over a daemon, `auto_start` is a no-op and
Settings → Daemon's mutating actions are disabled with an explanation. The `local`
entry stays in the list as a plain "This computer (127.0.0.1:4648)" connection that
works if anything answers there — also how (d) connects to a CLI/service daemon
without managing it.

**Upgrade rule.** Existing installs never see first-run: when `app-settings.json` is
absent but `connections.json` or the managed-daemon record exists, write
`{onboardingCompleted: true, manageLocalDaemon: true}` (preserving today's macOS
auto-start) and show nothing.

**First-run screen.** Shown by the bundled UI before the workspace UI when
`onboardingCompleted == false`; full window, theme-aware, "Skip for now" leaves
first-run pending (nothing installed or started; Settings keeps a "Finish setup"
entry). Choices:

- **(a) Run smind on this computer** — calls the existing install path
  (`desktop.daemonInstall()` → `daemon_install`, whatever the Windows bundled-daemon
  branch makes of it), sets `manageLocalDaemon = true`, selects `local`, waits for
  `/healthz`. macOS/Windows only; on Linux (deb, `Platform::Unsupported`) it is
  replaced by guidance to run `smind service install`.
- **(b) Connect to a daemon** — a URL (validated, then probed through a new
  `connections_probe(url)` returning `{reachable, version, service, authRequired}`
  so the screen shows "reachable, v0.11.1" before saving), with an optional
  **password** field shown when the probe says `authRequired` (decision 24; only
  meaningful once decision 9 lands). Until then (or for a loopback daemon) the screen
  explains that smind daemons listen on loopback and offers (b2).
- **(b2) Connect over SSH** — host (+ ssh port, daemon port); the app shells out to
  the system `ssh -N -L <free-local-port>:127.0.0.1:<daemon-port> <host>` and saves a
  URL connection to the local end; no daemon change, no password (decision 11).
- **(c) Pair via relay link** — the same paste → confirm → add component as D6.
- **(d) Use the daemon already running here** — shown first and pre-selected when
  `daemon_status` finds something answering on the local port that the app does not
  manage. It names what it can ("smind v0.11.1, managed by systemd" when `managedBy`
  is present, "started outside the app" otherwise, "something that is not smind"
  when `/healthz` does not identify smind) and offers **Connect** only:
  `manageLocalDaemon = false`, never install, never take over. If the port is held
  by a non-smind or non-HTTP process, (a) is disabled with the port-conflict
  explanation (the app does not choose the port; ADR-0030).

Pre-selection: nothing on the port → (a); a smind daemon answering → (d); otherwise
none. macOS **no longer installs silently on first launch** (decision 22).

**After first-run (Settings → Connections / Daemon).** One **Add connection** menu
offers (b)/(b2)/(c) (same components as first-run); the list keeps add/switch/remove
with multiple connections; a **Local daemon** row has a "Manage this computer's
daemon" toggle (`manageLocalDaemon`) with a confirm before turning it off while the
app owns a running daemon, and turning it off never stops a daemon without asking.
Startup: when `manageLocalDaemon` is true and `local` is selected, `auto_start` runs
as today on every launch (macOS; Windows when the bundled-daemon change enables it)
and never otherwise (decision 23). The "default connection" stays "last selected"
(decision 25). **Run setup again** in the Help/app menu reopens first-run without
touching saved connections.

**Compatibility with the Windows bundled-daemon work.** This ADR only gates
*whether* the app installs/starts a daemon; the *how* stays in `daemon_manager.rs`'s
install/update path, called by (a) through `daemon_install`. No new install code.

**Paseo reference — ported (settings toggle, welcome methods, SSH host, host
confirmation); deliberately different (first-run, quit behaviour).**
- *Ported:* the persisted **`manageBuiltInDaemon`** toggle with a confirm before
  turning it off (`desktop-settings.ts:36-39`, `daemon-management-toggle.ts`; ours is
  `manageLocalDaemon`, and it persists *before* any stop so a failed stop leaves
  truthful state, same ordering); the welcome methods — direct connection, paste
  pairing link, scan QR, **remote SSH** (`welcome-screen.tsx`); the **remote-SSH host
  type** that tunnels to an already-running daemon (`host-connection.ts`
  `RemoteSshHostConnection`, `cli/src/ssh/ssh-tunnel.ts`, `protocol/src/ssh-transport.ts`
  — validates host/port, rejects hosts starting with `-`; we port those validations);
  the relay-link confirmation (D6); the redirect into the app as soon as a host is
  online (the first-run screen disappears itself once (a)/(d)/(b)/(c) succeeds).
- *Different, first-run:* Paseo's desktop **auto-starts the built-in daemon silently**
  by default (`_layout.tsx:362-370`) and shows its Welcome screen only to
  web/mobile (or desktop with management off and no hosts); the toggle is the escape
  hatch. Human asked for an explicit first-run choice including "no local daemon" and
  "use the one already running", so we ask once, pre-selecting the sensible answer, and
  keep Paseo's toggle for later. If Human prefers Paseo's behaviour, decision 22's
  alternative is "silent start + dismissible first-run banner".
- *Different, quit:* Paseo stops its built-in daemon on quit by default
  (`keepRunningAfterQuit: false`, `quit-lifecycle.ts:56`); smind's daemon survives the
  app quitting (ADR-0013) so agent work continues. Not changed here; a
  "stop daemon when quitting" option could be added to the same settings file.
- *Adapted, ownership:* Paseo's app knows it "owns" a daemon by pid + startedAt
  recorded in the daemon's lock file (`daemon-manager.ts:189-194`); ours stays the
  app-side `ManagedRecord` (`managed.rs:43-51`) plus `managedBy` from `/healthz`.

### D8. Direct connections with a password (amends "loopback only")

**Status: OPEN — decision 9 is not accepted; everything that depends on it (P4, P5's password field, P8, P11, P13c) stays blocked.**

**Proposal.** Let the daemon listen on a non-loopback address, protected by a password,
as Paseo does — making a LAN/Tailscale/VPN homelab work with **no relay at all** — while
keeping loopback-only as the default and refusing unsafe combinations.

- Config (`config.yaml`, edited via `smind config`, decision 8): `server.listen`
  (default `127.0.0.1:4648`; `server.port` kept for compatibility),
  `server.passwordHash` (bcrypt, written by `smind set-password`, file mode 0600),
  `server.hostnames` (extra allowed Host names; IPs and `localhost` always allowed).
  Env overrides for containers: `SMIND_LISTEN`, `SMIND_PASSWORD`, `SMIND_HOSTNAMES`.
- **The daemon refuses to start with a non-loopback `listen` and no password.**
  (Paseo only warns.) A weak password is rejected at `set-password` (minimum length 12;
  no default, no `change-me` in docs): anyone authenticated can run a shell through the
  terminal tab and every agent, so the password is an RCE credential.
- **`GET /api/token` is only served to a loopback client talking to a loopback Host with
  no forwarding headers** (`X-Forwarded-*`, `Forwarded`, `X-Real-IP`). Otherwise it is
  403. This keeps the existing loopback UX (bundled UI, desktop proxy) unchanged while
  closing the same-host-reverse-proxy leak: an external request carries an external
  Host, so it is refused even though `RemoteAddr` is loopback (caveat: a proxy that
  rewrites `Host` to `localhost` defeats this; documented).
- **Non-loopback clients log in:** `POST /api/login {password}` → a **revocable,
  expiring session token** (random 256-bit, held in memory, not the daemon's long-lived
  token), accepted by `/ws` via the `Sec-WebSocket-Protocol` bearer and the
  `Authorization` header — not the URL query, which proxies log. Login is rate-limited
  per client IP with exponential backoff and constant-time compare. The embedded web
  UI shows a login form when the daemon says a password is required (it can still use
  `/api/token` on loopback).
- **DNS rebinding / CSRF:** `Host` validated on every HTTP request and WS upgrade
  against the allowlist (403 otherwise), `CheckOrigin` stays same-origin (ADR-0013),
  no CORS.
- **Transport:** plain HTTP/WS on a LAN exposes the password to anyone who can sniff it;
  the docs say "LAN only if you trust it; otherwise Tailscale/WireGuard, an SSH tunnel,
  or a TLS-terminating reverse proxy" (Paseo's own guidance, `docs/docker.md`
  "Security"). Built-in TLS is decision 10 (default: not in v1).
- **Residual risk, stated:** a single shared password, no per-device revocation (a
  restart or `smind session revoke-all` invalidates sessions), no MFA, no audit log,
  and a same-user local process can still read the token file (`SECURITY.md` makes the
  same statement about Paseo's `local-credential`).

**When to use which:** *direct+password* for LAN/Tailscale/Docker-on-a-NAS (no extra
server, works with the web UI in any browser); *SSH tunnel* (b2) when the user already
has SSH and wants zero daemon exposure; *relay* for reaching a daemon behind NAT from
mobile/cellular networks (D6, D9).

**Paseo reference — ported (model), stricter (enforcement).** Ported from
`SECURITY.md` "Local daemon trust boundary" and `config.ts:470-480`: loopback by default,
`listen` configurable, a bcrypt-hashed password (`auth.ts:35`, `set-password.ts`) checked
at admission before any session data (`session-admission-auth.ts`), the Host-header
allowlist with the same Vite-style semantics (`hostnames.ts`; leading `.` matches a
domain and subdomains), health endpoint exempt, and the same explicit statement that
network exposure is the operator's responsibility. Stricter than Paseo: refuse to start
non-loopback without a password; sessions are expiring tokens, not the standing
credential; `/api/token` is closed to anything but a true loopback caller (Paseo has no
unauthenticated token endpoint to close). Paseo's password-in-WS-`hello` is what we use
for relay devices (decision 14); for HTTP+WS direct connections we choose login-then-token
because our WS handshake today authenticates by token (`internal/wsapi`).

### D9. Multi-device relay (gRPC): several clients attached to one daemon at once (amends ADR-0007 (g) and ADR-0011 "one device")

**Status: designed now, independent of hosting.** It builds on the **existing gRPC protocol** (no WebSocket
rewrite) and works against a self-hosted relay and against any hosted option in D11. It can start as soon as
this ADR is accepted.

**What is wrong today (verified).** The relay already routes per `(workspace, session, device)`
(`internal/relay/server/server.go:576-604`; "workspace" = relay host, see Terminology); what limits it to one
client is the two ends: the daemon bridge opens exactly one data stream with the hardcoded pair
`m1-default-session`/`m1-default-device` (`internal/relay/bridge/bridge.go:12-35`, `Run` keeps one `DataConn`,
`:62-170`), every client uses the same pair, the relay's control stream answers only `Ping` and rejects every
other body with `Unimplemented` (`server.go:388-395`), and the declared `DeviceAttach`/`DeviceDetach` messages are
never produced or consumed (`relay.proto:121-148`). A second client therefore attaches to the same route key and
**replaces** the first; the daemon never learns that devices exist.

**Design.** Per-device routes and E2EE sessions, announced to the daemon over the control stream.

Protocol additions (all additive; older peers ignore unknown fields):

| Message | Change |
|---|---|
| `ControlFrame.DeviceAttach` (relay → daemon) | add `bytes session_id = 3`; `device_id` as today; `pairing_payload` unused (kept) |
| `ControlFrame.DeviceDetach` (relay → daemon) | add `bytes session_id = 2` |
| `ControlFrame.Sync` (relay → daemon, new body field 6) | `repeated DeviceRef {device_id, session_id}`: every route that has a live device side and no daemon side; sent right after a control stream is accepted |
| `ControlFrame.Revoke` (daemon → relay, new body field 7) | `device_id`: close that device's streams and refuse new attaches for it until the control stream ends (best effort; the daemon is authoritative) |
| `Frame.keepalive` (new `bool keepalive = 7`) | a frame with this set is consumed by the relay and answered with a keepalive frame on the same stream; never forwarded, never buffered |

Message flows (D = daemon, R = relay, C1…C3 = clients; "route" = `(host, session, device)`):

```
1. Daemon start
   D → R  AdmitChallenge, Admit                      (unchanged, ADR-0011)
   D → R  OpenControl: Ping(0)                       (unchanged first frame)
   R → D  Pong(0), Sync{devices that are waiting}    (new)
   D      for each DeviceRef: spawn device session    (flow 3, steps b–e)

2. First client pairs (within the pairing window, see "Device identity")
   C1 → R  Admit (host id + secret from the offer)
   C1 → R  OpenData: Frame{host, session s1, device d1, DEVICE_TO_DAEMON, E2EE hello}
   R       creates route (host,s1,d1); no daemon side yet → buffers the hello
   R → D   control: DeviceAttach{d1, s1}
   (flow 3 continues)

3. Daemon spawns a device session  (one goroutine tree per device)
   b. D → R  OpenData: Frame{host, s1, d1, DAEMON_TO_DEVICE}      (attaches the daemon side of the route)
   c. R → D  buffered E2EE hello flushed; E2EE handshake completes (responder role, unchanged)
   d. C1 → D (first encrypted app message) device_hello{device_pub, label, signature, [password]}
      D      verifies the signature, derives device id, checks the device registry (see "Device identity")
   e. D      api.ServeTransport(ctx, dc)  — the same call a browser WebSocket gets, so C1 sees ALL of the
             daemon's workspaces, spaces and tasks; subscriptions and permission prompts are per connection

4. More clients at once
   C2, C3 repeat flow 2 with their own d2/d3 (and their own session ids). The relay holds three independent
   routes; the daemon holds three device sessions; nothing is shared, so no client evicts another.

5. Reconnect (client side)
   C2's transport drops → C2 re-opens OpenData with the SAME (s2, d2) and resumes the E2EE session in place
   (existing `DataConn.Resume`); only that route's stream is replaced. If the daemon side was also lost, the
   relay re-announces with DeviceAttach/Sync.

6. Daemon restart
   D reconnects control → R sends Sync{d1,d2,d3 still waiting} → D re-opens each route; clients whose E2EE
   session cannot resume (the daemon lost its counters) re-handshake with a fresh session id.

7. Revoke
   `smind relay devices revoke <id>` → D marks the device revoked, closes its transport, sends
   Revoke{d2} on control → R closes d2's streams and refuses further attaches; a reconnecting C2 is
   rejected by the daemon at step d.

8. Keepalive (every 30 s, both directions)
   D ↔ R control: Ping/Pong (exists).   C/D ↔ R data: Frame{keepalive} echoed by the relay.
   A side that sees no frame from the relay for 75 s treats the stream as dead and reconnects with jittered backoff.
```

**Limits (relay):** ≤ 16 attached devices per host (configurable), the existing per-side frame buffer (200
frames; ADR-0007 (a)) plus a new byte cap (4 MiB), idle-route grace unchanged (5 min,
`DefaultRouteGrace`). The relay keeps **one control stream per host**; a new one supersedes the old (the old
ends with `Aborted: superseded`).

**Implementation notes (P12a, as built).** (1) `Sync` is always sent after the first `Pong`, even when empty, and lists every route that has a live device side and no
daemon side: the daemon opens each listed route. Absence from a `Sync` is **not** proof that a device is gone (a route whose old
daemon side the relay has not yet noticed dead is not listed either), so a daemon keeps a session it holds until a
`DeviceDetach` or its own timeout. (2) The relay does **not** re-announce `DeviceAttach` when a daemon side ends
(a daemon that rejects a device would otherwise reconnect to it in a loop); a lost daemon recovers through `Sync`.
(3) `DeviceDetach` is sent when a device side ends with no daemon side and again, idempotently, when a route is
garbage-collected; the daemon treats a detach for an unknown route as a no-op and an attach for a known one as a no-op.
(4) `Revoke` also closes the daemon side of that device's routes and lives only as long as the control stream that sent it:
the daemon re-sends its revocations on every control (re)connect. (5) Besides the 16-device cap the relay bounds routes per
host (8 per device slot) and id length (128 bytes), and pings idle peers over HTTP/2 so a vanished phone frees its slot.
(6) Keepalive probes carry a per-connection random nonce in `Frame.sequence` and the relay echoes it: only an echo with
the matching nonce proves the relay supports keepalive (a relay without it forwards the probe, a keepalive frame with
somebody else's nonce, which only counts as liveness); each (re)opened stream probes once.

**Implementation notes (P12b, as built — the contract P13a/b implement).**
- *`device_hello` is an ordinary wsapi request*: `{"id":"…","method":"relay.device_hello","params":{"v":1,"device_pub":b64,
  "label":"…","signature":b64,"password"?:"…"}}`, the first message after the E2EE handshake. Success is an ordinary response
  `{"id","result":{"v":1,"device_id","state":"approved"}}`; a refusal is `{"id","error":{"message","code"}}` with code
  `awaiting_approval` (device recorded `pending`), `revoked`, `bad_hello` (bad signature / id mismatch / version / label),
  `too_many_devices`, `device_hello_required` (legacy clients refused), `device_hello_unsupported` or `internal`; the daemon
  then ends the route. A daemon from before P12b answers `method not found`, which is how a client detects it (legacy daemon).
- *What is signed:* `"smind-device-hello" ‖ e2ee.TranscriptHash(daemon_pub, client_ephemeral_pub) ‖ daemon_pub`, where
  `TranscriptHash` is the HKDF salt the session keys already use, `SHA-256("smind relay e2ee v1" ‖ daemon_pub ‖
  client_pub)`; the E2EE core is unchanged (a test pins the equality). `device_id` = lowercase unpadded RFC 4648 base32 of
  `SHA-256(device_pub)`, first 26 characters. Shared test vector: `internal/relay/devices/testdata/device_hello_vector.json`.
  `device_hello.password` is not covered by the signature and is checked (P13c) only over the encrypted channel.
- *Daemon behaviour:* one session per announced `(device_id, session_id)`; sessions survive a relay reconnect (orphaned, E2EE
  state kept up to 6 minutes) and are resumed in place when the next `Sync` lists their route, dropped when it does not;
  a client that lost its daemon (restart) must re-handshake with a **fresh session id** — it cannot tell by itself, so after
  any resume a client should confirm liveness with a request and fall back to a fresh session if it is not answered. The relay
  has no acknowledgements, so a request handed to the daemon's stream at the instant that connection dies can be lost in flight
  (and the E2EE counters then no longer line up: the session is wedged until a fresh one) — treat requests as at-most-once across a
  hard drop: time out, and open a fresh session rather than waiting. Data and
  control streams ping every 30 s and are re-established after 75 s of silence with ±20 % jittered backoff. Behind a relay that
  sends no `Sync` (older than P12a) the hub falls back to opening the fixed legacy route, and never sends `Revoke`.
- *Legacy clients* (no `device_hello`: first message is anything else, or silence for 5 s) are served as one unlisted device
  **only while `relay.legacyDevices` is true** — and only on the fixed legacy route (`m1-default-device`), one at a time (the
  newest replaces the older); a client on any other route id must identify itself. **DECIDED by Human (2026-10-15): the default
  is `false` starting with P12b / v0.12.0** (the plan had asked for "true for one minor release"). With it false the daemon
  refuses a client without a device identity with `device_hello_required`, so an installed desktop/mobile build cannot connect
  through the relay until it ships the multi-device client (P13a/b); a user can opt in explicitly with
  `relay.legacyDevices: true`, with a loud warning in the config docs (`docs/homelab.md`, `internal/config`) and in the log at
  start and per legacy connection. **Why:** while it is true, anyone holding the relay host secret and the daemon's public key
  (a leaked offer, a stolen paired device, or the relay operator) can open the fixed legacy route and be served without
  approval; `devices revoke`, the `pending` state and the pairing window do not bind legacy access.
- *Pairing window rule:* a device takes a slot only if it was first seen at or after the window opened (an attacker that
  registered early as `pending` is not first in line); first-come among devices that appear after `smind relay offer` remains —
  a one-time token in the offer link that the signed hello must carry is the stronger fix and is left to a follow-up
  (offer format change, P2/P13). The registry refuses new unknown devices beyond 128 pending.
- *Admin API* (local `/ws` only — over the relay these methods answer `forbidden`, so a paired device can never approve or
  revoke devices): `relay.device.list {}` → `{"devices":[{device_id,device_pub,label,first_seen,last_seen,state,connected}],
  "window":null|{expires_at,remaining}}`; `relay.device.approve|revoke {id}` and `relay.device.rename {id,label}` → the device
  (ids may be unique prefixes; errors `not_found`, `ambiguous`); `relay.device.window {duration_seconds?,max_devices?}`
  (defaults 10 min / 1) → the window. `smind relay offer` opens the window (`--window`, `--max-devices`).

**Trust model of the control stream (unchanged secret, new reach).** Both ends of a relay host authenticate with the same
host secret (ADR-0011), so the relay cannot tell the daemon from a client holding the offer. Before this decision that
mattered little (control carried only `Ping`/`Pong`); now whoever holds the secret can supersede the daemon's control
stream, read the `(device_id, session_id)` of waiting routes, and `Revoke` another device's route (availability only: the
payloads stay end-to-end encrypted and the daemon stays authoritative). The pairing window and the daemon's device
registry (below) limit what a leaked offer buys, but a daemon-only control credential — and rate-limiting supersede — is
the follow-up that closes this, to be decided with the hosted relay (decisions 12 and 28).

**Device identity, listing and revocation (needed for `smind relay devices ls|revoke`).** Today a device's key is
a fresh ephemeral X25519 key per session and the daemon cannot tell clients apart. Persistent device keys
**cannot** be fed to the existing handshake (its HKDF salt is the transcript of public keys,
`internal/relay/e2ee/session.go:57-102`; two static keys would repeat the derived key and reuse the nonce
space). So the E2EE core stays untouched and identity is an **inner layer**:

- Each client creates a persistent **Ed25519 device key** (desktop: app data dir, mode 0600 like
  `pairing_store`; mobile: the platform secure store) and sends, as its first encrypted application message,
  `device_hello{v, device_pub, label, signature, [password]}` where `signature` signs
  `"smind-device-hello" ‖ SHA-256(handshake transcript) ‖ daemon_pub` — proof of possession bound to this session,
  so it cannot be replayed.
- `device_id` in the frame metadata **must equal** `base32(SHA-256(device_pub))[:26]`; the daemon drops a route
  whose id does not match, so a client cannot claim another's id.
- The daemon keeps a registry `$SMIND_HOME/relay-devices.json` (mode 0600): `{device_id, device_pub, label,
  first_seen, last_seen, state: approved | pending | revoked}`.
- **Pairing window.** `smind relay offer` opens a window (default 10 minutes or one new device, `--window`,
  `--max-devices`) in which an unknown device is approved automatically; outside it an unknown device is recorded
  as `pending` and refused ("awaiting approval"; `smind relay devices approve <id>`). A leaked old offer is
  therefore useless once the window closes **(true since the 2026-10-15 decision made `relay.legacyDevices` default to false — see the P12b implementation notes)** — the offer stops being a standing credential. (The host secret still
  gates the relay; the daemon password, decision 14, is an additional factor inside `device_hello`.)
- `smind relay devices ls [--pending]` (id, label, state, connected?, first/last seen), `approve <id>`,
  `revoke <id>`, `rename <id> <label>` talk to the **running daemon** over its authenticated WS API (new
  `relay.device.*` methods) so a revoke takes effect immediately; with no daemon running they edit the registry
  file directly. `smind relay host rotate` remains the hard reset (new host secret; every old offer and token dies).
- This **amends ADR-0007 (g)**, which chose an ephemeral device key and "no persistent device secret". Paseo made
  the same choice on purpose ("the phone's keypair is ephemeral per connection, so there is no persistent
  phone-side secret to steal", `refs/paseo/SECURITY.md`). We deviate because per-device listing and revocation
  need an identity; the cost is one stored private key per device. If Human prefers Paseo's stance, decision 35's
  alternative keeps ephemeral keys: `devices ls` shows only *connected* devices (relay-assigned ids), `revoke`
  closes the connection, and the hard revoke is `host rotate`.

**Keepalive and proxies.** Cloudflare's proxy has a 125 s read timeout and a 30 s write timeout on non-Enterprise
zones (D11); idle WebSockets/streams can also be dropped. The 30 s application keepalive on control **and data**
streams (flow 8) and the 75 s dead-stream rule keep a stream alive through any such proxy, and client reconnect
with resume (flow 5) makes a dropped stream a non-event. gRPC's own HTTP/2 PING (`keepalive.ClientParameters`)
is also enabled but is not relied on, because an intermediary may answer pings itself.

**Clients.** Go daemon bridge (`internal/relay/bridge`): a device hub replaces the single `DataConn`. Desktop
Rust client (`desktop/daemon-client/src/relay/*`): generates and stores the device key, uses a per-connection
device id and session id, sends `device_hello` and keepalives, never evicts others. Mobile TS client
(`mobile/src/relay/*`): the same through grpc-web, stored key in secure storage. The relay's grpc-web listener
already maps to the same `Server`, so mobile devices get the same routes.

**Test bar (spelled out in the plan): ≥ 3 simultaneous devices — 2 desktop clients and 1 mobile client — plus
reconnect, daemon restart and revoke, in a Go integration test and in a cross-language interop run.**

**Paseo reference — ported (shape), adapted (frames, identity), different (device key).** Ported:
control socket + per-connection data socket + the relay telling the daemon about each connection + per-connection
E2EE + `sync` on control (re)connect + buffering until the daemon's data side exists
(`refs/paseo/packages/relay/src/cloudflare-adapter.ts:119-127,340-395`,
`packages/server/src/server/relay-transport.ts:49-55,121-130`, test `clt_multi` at
`cloudflare-adapter.test.ts:193-229`; control liveness `cloudflare-adapter.ts:219-250`). Adapted: gRPC streams
instead of WebSockets (ADR-0007 (f) unchanged), SCRAM admission in front (ADR-0011), the new frames above.
Different: Paseo's relay assigns `connectionId`s and keeps no device identity; smind adds a persistent device key
for listing/revocation and a pairing window (decision 35).

### D10. `smind config get|set|unset` and no launch flags

`smind config get [path]`, `set <path> <value>`, `unset <path>` read and write
`config.yaml` (comments preserved by a node-level YAML edit) and print "restart the
daemon to apply" or apply live where trivially possible (decision 8). `smind serve`
keeps taking no flags. This gives `service install` and the Docker image one documented
way to change `server.port`/`server.listen`/`server.hostnames` without per-OS unit edits.

**Paseo reference — ported.** Paseo removed `--port/--listen/--relay/--hostnames` from
`daemon start` and rejects them with "use `daemon config set <path> <value>`, then
restart" (`local-daemon.ts:82-110`), and provides `daemon config get/set/unset` and
`daemon reload` (`config.ts:75-110`, `reload.ts`). We port `config get/set/unset`; we do
not port `reload` (hot reload) yet.

### D11. Hosted relay at `relay.spacingmind.com` — PENDING (hosting not decided)

**Decided so far (Human, 2026-10-13/14):** the hosted relay's name is `relay.spacingmind.com`; DNS is on
Cloudflare (spacingmind.com is on the same account that deploys the landing site); the trust model below is
accepted; deploys are gated by a GitHub Environment with required reviewers; the relay-side "workspace" is renamed
"relay host" (D12). **Not decided: where and how the relay runs.** On 2026-10-14 the Human **withdrew the
Cloudflare Workers + Durable Objects choice** (option B below) in favour of keeping gRPC, and asked that the
options be recorded. Every hosted-relay PR (deploy/ops, open enrolment, default address) is **BLOCKED on this
decision**. Multi-device (D9) does **not** depend on it.

**Options (researched 2026-10-13/14; sources linked; anything not in a source is marked unverified).**

**(D) Cloudflare's gRPC proxy in front of the unchanged Go relay** (origin on Cloud Run or a small VPS).

- Cloudflare requirements ([gRPC connections](https://developers.cloudflare.com/network/grpc-connections/)): available
  on all plans; the endpoint must listen on **port 443**, support TLS and **HTTP/2 negotiated via ALPN**; requests
  carry `Content-Type: application/grpc`; the hostname must be **proxied** with SSL/TLS mode **Full** or stricter;
  **Network → gRPC must be switched on** for the zone, otherwise Cloudflare returns 403. Limits: WAF only inspects
  headers; **Cloudflare Access ignores gRPC**; Tunnel supports gRPC only with private subnet routing.
- **Timeouts that shape the protocol** ([connection limits](https://developers.cloudflare.com/fundamentals/reference/connection-limits/)):
  proxy **read timeout 125 s** (HTTP 524; changeable only on Enterprise zones), proxy **write timeout 30 s**, proxy idle
  timeout 900 s (520), idle client connections 400 s, HTTP/2 pings to origin off by default. Hence **application
  keepalive every 30–60 s on every stream and client reconnect-with-resume** — both are built into D9 (flow 8, flow 5),
  which is why D9 is useful whichever option wins.
- What the relay needs for this option: (1) a **single-port mode** serving native gRPC and grpc-web on one
  TLS/h2 listener (today two ports, `internal/relay/server/run.go:51-54`, deliberately separate listeners in
  `grpcweb.go`) because Cloudflare proxies gRPC only on 443 and mobile uses grpc-web over WebSocket; (2) a CA/origin
  certificate (a Cloudflare Origin CA certificate with *Full (strict)*, or Let's Encrypt) instead of the self-signed
  one; (3) persistent storage for the relay-host store (`workspaces.json`, `internal/relay/server/workspaces.go`).
- **Origin A — Cloud Run:** documented support for gRPC bidirectional streaming
  ([announcement](https://cloud.google.com/blog/products/serverless/cloud-run-gets-websockets-http-2-and-grpc-bidirectional-streams));
  requests (including streams) are capped by the request timeout, **at most 60 minutes**, so every stream is
  re-established at least hourly; instance state is in memory and Cloud Run can briefly exceed a `max-instances` cap
  and offers only best-effort session affinity, so a one-instance relay must tolerate losing route state (clients
  resume or re-handshake; D9 flows 5–6) and keep hosts in external storage (a volume or a database). Pricing and the
  Cloudflare-proxy-to-`*.run.app` Host-header handling are **unverified**.
- **Origin B — a small VPS** (Fly.io, Hetzner, …): no stream-duration cap, a persistent disk, one process, no autoscaling
  and a single point of failure; search results put Fly's `shared-cpu-1x` 256 MB near $1.94/month and Hetzner's smallest
  plan near €4.5–5.5/month (**unverified, check before budgeting**).
- Operator-visible traffic includes Cloudflare as a TLS-terminating processor (see "Trust model").

**(C) The unchanged Go relay in Cloudflare Containers behind a Worker, via inbound gRPC/TCP.**

- Announced 2026-08-03 ([Workers and Containers now support inbound TCP and gRPC](https://blog.cloudflare.com/grpc-workers/)):
  **private beta** (sign-up form; **Human has requested beta access**), ingress through a new **Spectrum** application type
  naming a Worker whose `connect()` handler can forward the socket to a Durable Object and on to a Container;
  **Containers support full-duplex bidirectional gRPC** (the post's example is a Go server); **Workers alone** get only
  gRPC-web translation — **unary and server-streaming, not bidirectional**; no GA date or pricing is given, and the post
  does not say which plans can use Spectrum (**verify before committing**). Cloudflare Containers themselves have been GA
  since 2026-04-13 and bill on active CPU on the Workers Paid plan
  ([changelog](https://developers.cloudflare.com/changelog/2026-04-13-containers-sandbox-ga)).
- It would keep the Go relay and its gRPC protocol byte-for-byte and put everything on the Cloudflare account that already
  hosts the site, but depends on a private beta, a container's ephemeral disk needs an answer for the host store, and a
  long-lived stream keeps its object pinned and billed.

**(B) WebSocket protocol v3 on Workers + Durable Objects, like Paseo — withdrawn as the preferred path.** Workers and
Durable Objects cannot terminate native gRPC bidirectional streams today (the post above), so this option replaces gRPC
with a WebSocket protocol implemented twice (the Go relay and a TypeScript Worker, kept honest by one conformance suite) and
migrates the Go, Rust and TypeScript clients. Kept only as notes: Durable Object pricing, limits and hibernation behaviour
are in Cloudflare's docs ([pricing](https://developers.cloudflare.com/durable-objects/platform/pricing/),
[limits](https://developers.cloudflare.com/durable-objects/platform/limits/)); Paseo runs this design
(`refs/paseo/packages/relay/src/cloudflare-adapter.ts`, `wrangler.toml`, `.github/workflows/deploy-relay.yml`) but its
current `wrangler.toml` sets `PASEO_RELAY_UPSTREAM = "https://paseo-relay-next.fly.dev"` and `cutover-proxy.ts` forwards
every request to a Fly deployment while `deploy-relay.yml` is "manual-only while relay.paseo.sh bridges traffic to the Fly
deployment" (the repository does not say why; I did not verify a reason).

| | (D) CF gRPC proxy + Go relay | (C) CF Containers + Worker gRPC ingress | (B) Workers + DO, WebSocket v3 |
|---|---|---|---|
| Protocol change | none | none | new protocol, 4 implementations |
| Runnable today | yes (VPS or Cloud Run) | no — private beta | yes |
| Hard constraints | 443 + ALPN h2, Full TLS, gRPC toggle, 125 s / 30 s timeouts | beta access, Spectrum plan, container disk | no native gRPC; DO 32 MiB message, 1,000 req/s per object |
| Ops | one small service + Cloudflare settings | all on Cloudflare, beta risk | all on Cloudflare, most code |
| Cost | VPS ≈ a few $/month (unverified) or Cloud Run | Workers Paid + Containers active CPU (unpriced in the post) | Workers Paid + DO requests/duration |
| Exit/fallback | n/a | fall back to (D) with no code change | fall back to a Go origin (Paseo's route) |

**Lead's input to the decision (not a decision):** build D9 first (it contains the keepalive and reconnect behaviour
every option needs); (D) on a small VPS or Cloud Run is the only option that can be run today and needs no protocol work
beyond the single-port mode; (C) is the natural target if the beta access and pricing work out, because it keeps the
protocol and the Cloudflare account; (B) is the most work for the least benefit now that (C)/(D) keep gRPC.

**Trust model (Human: OK as proposed, 2026-10-13; independent of the hosting option).**

- **TLS.** Hosted = CA-verified; self-hosted = self-signed + fingerprint pin (ADR-0011). Clients accept both: an
  **empty fingerprint means "verify chain and hostname against the system roots"** (`offer.go:60-63` already documents
  "empty when the relay uses a public CA"), a non-empty one pins exactly that certificate. Today `relay.Dial` refuses
  an empty fingerprint (`internal/relay/client/client.go:37-40`) and the Rust client requires 32 bytes
  (`desktop/daemon-client/src/relay/client.rs:139-146`); both change. `smind relay connect` first tries normal
  verification (CA mode, empty fingerprint) and only otherwise offers a pin, with an explicit confirmation showing the
  fingerprint or `--fingerprint <hex>` — **replacing silent trust-on-first-use** (`bridge.FetchFingerprint`). The mobile
  client does not pin at all today (it parses `fp` but never uses it, `mobile/src/relay/pairing.ts:12,81`; a self-signed
  relay probably does not work on a real phone — unverified), so a CA-verified hosted relay is also what makes mobile
  usable.
- **E2EE unchanged** (ADR-0007/0011): the relay sees only ciphertext envelopes.
- **What the operator can see** (smind as operator; Cloudflare as a TLS-terminating processor under (D)/(C)): client IP
  addresses and connection times/durations, relay host ids, the daemon's public key id (sent in admission), device ids and
  counts per host, message sizes and counts, admission outcomes. It **cannot** see messages, tasks, prompts, file paths or
  code, and cannot forge admission (SCRAM stores only `StoredKey`; ADR-0011 residual: an operator who also records a full
  admission exchange can recover the secret). Logs keep no payload and no IP beyond decision 32's retention; a privacy note
  on spacingmind.com is a landing-repo follow-up.

**Self-service enrolment (hosting-agnostic design, PR blocked until the hosting decision).** Enrolment registers **this
daemon once on the relay** (a relay host): a new unary RPC `Enrol` (additive in `relay.proto`) returns a server-generated
host id (random, e.g. `h_` + 26 base32 chars) and a 256-bit secret shown once; `smind relay connect` with no address
enrols at `relay.spacingmind.com`, stores them in `$SMIND_HOME`, and the daemon dials in on every start. **Nothing is
created in smind's data model**; every paired client sees all of the daemon's workspaces, spaces and tasks. The relay
stores only `StoredKey`.
- *No accounts in v1* (the secret is the identity; `smind relay host rotate` replaces it); GitHub login is a possible later
  layer.
- *Abuse limits (defaults, tunable, Human to confirm — decisions 27/28):* ≤ 5 enrolments per IP per hour and ≤ 20 per day;
  ≤ 50,000 hosts globally; a host never admitted within 24 h is deleted, one with no admission for 90 days is deleted; ≤ 16
  devices per host (D9); ≤ 1 MiB per message; sustained ≤ 1 MiB/s and a soft ≈ 20 GB/month per host; ≤ 8 concurrent
  connection attempts per IP; an operator ban list keyed by host id. The client IP is taken from the platform's trusted
  header only (`CF-Connecting-IP` behind Cloudflare). Content is opaque, so abuse handling is by volume and metadata.
- *The offer is still the secret.* With D9's pairing window a leaked offer stops working for **new devices** after the
  window, and hosted hosts **require a daemon password** (decision 14, P4's `smind set-password`) before `relay connect`
  completes; `smind relay host rotate` revokes everything.
- *Default address.* `smind relay connect` with no arguments uses the hosted relay after a one-time consent line ("traffic
  is end-to-end encrypted; the relay sees IP addresses, timing and sizes"); a self-hosted relay always needs an explicit
  address (decision 30).

**Ops (generalised; applies to whichever option is chosen).**

- *Monorepo, not a separate repository (Human, 2026-10-13).* The code stays in the smind repository: the Go relay in
  `internal/relay/`, deployment definition in a self-contained directory (`relay-worker/` if option B is ever chosen,
  otherwise `deploy/relay/`), and any future shared protocol definition and conformance scenarios in `relay/protocol/` and
  `relay/conformance/`. Split the deploy directory into its own repository later if it ever needs separate ops or licensing
  (AGPL-3.0 like the rest, ADR-0003; ADR-0007 (h)). Paseo precedent: `refs/paseo/packages/relay` plus
  `.github/workflows/deploy-relay.yml` in its monorepo.
- *Pipeline:* a **separate `deploy-relay.yml`, not tied to release-please**, triggered only by changes under the relay
  deployment paths (`internal/relay/**`, `deploy/relay/**`, or `relay-worker/**`) on `develop` and by manual dispatch;
  tests (and, for B, the conformance suite) run first. The relay therefore ships independently of smind releases, which
  makes "accept the current and previous relay protocol revision" the compatibility rule.
- *Secrets and approval:* the credentials live in a GitHub Environment **`relay-production` with required reviewers**
  (Human approves each production deploy), not in repository-wide secrets. What goes in it depends on the option:
  Cloudflare token (DNS record, zone settings for the gRPC toggle, Workers/Containers for (C)/(B)), and a GCP service
  account (Cloud Run) or SSH/deploy key (VPS). **BLOCKED on the hosting decision.** Required reviewers need a public
  repository or a plan that supports them — Human to confirm.
- *DNS and TLS:* a proxied `relay` record on the `spacingmind.com` zone for (D); a Workers custom domain for (C)/(B);
  Cloudflare issues the edge certificate.
- *Monitoring:* a health endpoint on the relay, an external synthetic check, Cloudflare/cloud billing notifications
  (Human-owned accounts), an error-rate alert, a status page later; no SLA.
- *Privacy:* a page on spacingmind.com stating what the relay sees, the retention, and that there are no accounts — a
  landing-repo follow-up, **required before the hosted default is switched on**.

**Paseo reference — ported (the hosted-relay concept, `custom_domain` route, observability) and flagged (the Fly
cutover).** `refs/paseo/packages/relay/wrangler.toml`, `src/cloudflare-adapter.ts`, `src/cutover-proxy.ts`,
`.github/workflows/deploy-relay.yml`; the open (secret-less) relay model is why Paseo needs no enrolment, `SECURITY.md`
"Relay threat model".

### D12. Terminology and CLI: relay host, not workspace

See "Terminology" at the top. `smind relay workspace new|ls` becomes **`smind relay host new|ls|rm|rotate`**;
`workspace` stays as a deprecated alias printing a one-line note to stderr for two releases.
`smind relay connect <address> <host-id> <secret>` keeps its positional shape; `smind relay connect` with no
address enrols on the hosted relay (D11). Help text, README, `docs/homelab.md`, the desktop "Add via relay link"
preview (it shows "Relay host", not "workspace") and error strings all use "relay host". **Wire
compatibility:** the offer's JSON key `ws` and the v2 gRPC field `workspace_id` are untouched (documented as the
host id); the Go/Rust/TS identifiers (`bridge.Config.WorkspaceID`, `admission.Workspace`, `relay_workspace_id`)
are renamed opportunistically, never by a wire-breaking change; the new messages D9 adds speak of `host` and
`device` in their comments.

## Alternatives considered

- **`smind update` via a package manager / separate updater.** More moving parts per OS;
  the daemon already has download, checksum, atomic rename. Not chosen.
- **Windows Service (via `x/sys/windows/svc`).** Runs as LocalSystem or a service account,
  so `SMIND_HOME`, agent logins, Git for Windows and `PATH` would not be the user's; needs
  elevation; the secret ACLs (ADR-0029 D3) and per-user token model assume the user's
  profile; ADR-0030 rejected "a Windows Service or Task Scheduler entry" for the *app* on
  the same costs. Drawback of the Task (starts after login) is stated. Decision 2.
- **System-level systemd unit by default.** Needs root, a dedicated user without the
  operator's agent logins. User unit + linger covers boot with the right identity.
  Decision 3.
- **Paseo's model — no service installer, run `daemon run` under your own
  systemd/Docker.** Fewer commands to maintain, and Paseo gets away with it. Rejected as the
  *only* option because Human asked for first-class boot persistence and per-OS correctness
  (PATH capture, linger, launchd `KeepAlive`, Windows restart policy) is exactly what users
  get wrong; but `smind serve` stays a clean foreground entrypoint so people who prefer their
  own unit lose nothing.
- **A background `smind start` with an in-process supervisor (Paseo `daemon start`).** A
  second supervision mechanism next to the OS's. Not chosen (decision 26).
- **Keep loopback-only and push every remote user to the relay or a tunnel (this ADR's first
  draft).** Forces homelab users on a trusted LAN to run a relay server, and cannot serve the
  Docker `-p` case. Replaced by D8 with the strict guards above.
- **Direct connections without a password (bind-only).** Paseo permits this with a warning.
  Not chosen: the daemon's terminal tab is remote code execution.
- **Self-host only, no hosted relay.** Remains the fallback if none of the hosting options in D11 proceeds; the Human
  wants a hosted `relay.spacingmind.com`, so it is not the plan.
- **WebSocket protocol v3 implemented on Workers + Durable Objects (D11 option B).** Was chosen on 2026-10-13 and
  **withdrawn as preferred on 2026-10-14**: it replaces gRPC with a second protocol implemented in Go, Rust,
  TypeScript and a Worker, while options (C)/(D) keep gRPC. Kept as notes in D11.
- **Multi-device by rewriting the transport first.** Rejected: multi-device needs only per-device routes, a control
  announcement and per-device sessions, all expressible on gRPC with the message slots `relay.proto` already declares
  (D9), so it does not wait for the hosting decision.
- **Ephemeral device keys only (Paseo's model) for multi-device.** Cheaper (no stored device secret, no pairing window)
  but cannot support `smind relay devices ls|revoke` beyond "who is connected now" plus `host rotate`; offered as the
  alternative in decision 35.
- **Keep one device per relay host and document it (this ADR's first draft).** Replaced by D9.
- **Building the Docker image from source inside Docker** (Paseo's approach for Node). Slow
  multi-arch builds under QEMU and a binary that differs from the checksummed release.
- **Baking agent CLIs into the image.** Paseo, too, leaves them out. Decision 19.
- **Silent auto-install on first launch everywhere (status quo on macOS, Paseo's behaviour).**
  Removes the choice to run no local daemon or use an existing one. Not chosen (decision 22).
- **Storing `manageLocalDaemon` inside `connections.json`.** Couples a behaviour flag to a
  registry that tolerates and rewrites corrupt files. Separate file.
- **QR-only pairing for desktop.** A desktop app has no scanner worth building; paste is the
  primary path.

## Decisions (accepted 2026-10-14)

Human approved every decision below on 2026-10-14 with the default stated ("Ok làm đi"). **★ = the default changed, or the
decision is new, because of the Paseo study; ✔ = answered earlier by the Human.** Decisions 10, 14 and 24 are accepted but only take effect together with the
still-open decision 9 (direct connections with a password), so what depends on them waits for it (noted in the plan).

1. **CLI signature verification.** Verify a minisign signature of `checksums.txt` in
   `smind update` with the existing desktop-updater public key (ADR-0023; no new secret),
   signed by a new `publish`-job step, as a separate PR after the unsigned core. *Recommended:
   yes.* (Paseo has nothing to compare: npm integrity.) Install scripts stay checksum-only.

2. **Windows: Scheduled Task vs Service.** *Accepted: per-user Scheduled Task at logon.*

3. **Linux/macOS service scope.** *Accepted: user unit / LaunchAgent by default*;
   `--system` opt-in on Linux (runs as the invoking user, never root); no LaunchDaemon in v1.

4. **Update refusal policy.** *Accepted:* refuse while runs are running or a permission is
   pending; `--force` overrides; no `--wait`; stable only; downgrade needs `--force`.

5. **Restart after update.** *Accepted:* restart only a service-managed daemon; for a
   hand-started one print instructions.

6. **Supervisor signal.** *Accepted:* additive `/healthz` field `managedBy` from env
   `SMIND_SUPERVISOR` (Paseo records `desktopManaged` in its lock file instead — adapted).

7. **★ Daemon log file.** *Accepted: yes* — `smind serve` also writes a rotating
   `$SMIND_HOME/daemon.log` (e.g. 10 MiB × 5), `smind service logs` tails it on every OS;
   journald stays available. Replaces the shell-redirect/per-OS log design. Adds a small
   rotation dependency or ~80 lines of code.

8. **★ `smind config get|set|unset`; no launch flags.** *Accepted: yes, no
   `--port/--listen` flags on `serve`* (Paseo removed the equivalent flags).

10. **★ Built-in TLS for direct connections.** *Accepted: not in v1* — document Tailscale,
    SSH, or a TLS reverse proxy (with the Host-rewrite caveat); revisit with `server.tls`.

11. **★ SSH-tunnel connection type in the desktop app (b2).** *Accepted: yes* — the app runs
    the system `ssh -L` (Paseo `remoteSsh`); no daemon exposure and no password.

13. **★ Multi-device relay on gRPC (D9).** *Accepted: yes — start as soon as this ADR is accepted; independent of hosting.*
    Uses the existing protocol (`DeviceAttach`/`DeviceDetach` + additive messages), per-device E2EE sessions in the bridge,
    `smind relay devices ls|approve|revoke|rename`, keepalive for the 125 s proxy timeout, and a ≥ 3-device test (2 desktop +
    1 mobile). Replaces the earlier "v3 WebSocket" plan.

14. **★ Password for relay devices.** *Accepted: yes* — the daemon password (needs decision 9's `smind set-password`) is
    sent inside `device_hello`; required for hosted relay hosts. With D9's pairing window a leaked offer is no longer a
    standing credential, but the password remains the second factor.

15. **Offer reuse/expiry.** *Accepted:* reusable, no expiry, loud "treat like a password"
    wording, `--png` at 0600 (Paseo is the same: no expiry, no per-device credential).

16. **Deep link `smind://pair`.** *Accepted:* paste-only in v1; later with the confirmation
    screen, never auto-add.

17. **`daemon.info` RPC.** *Accepted:* add a small additive WS method `{version, commit}`
    so the notice works for relay connections (Paseo reads version from `server_info`).

18. **QR default.** *Accepted:* print the QR only on a TTY or `--qr`, fall back to level L,
    never fail the command; offer compaction (v2) deferred.

19. **★ Docker image contents.** *Accepted:* one image (Debian-slim, git, ssh client, Node 22,
    `gosu`, `tini`, curl), **root entrypoint that fixes ownership then drops to uid 1000**
    (Paseo), agent CLIs not baked in + child-image example + compose example, listen
    `0.0.0.0` only with a required password (needs decision 9). Alternative: a `-agents` variant.

20. **Image provenance.** *Accepted:* buildx provenance/SBOM on releases; no cosign in v1.

21. **`install.ps1` location.** *Accepted:* `%LOCALAPPDATA%\Programs\smind` + user `PATH`
    (never the app's managed directory); landing redirect as a follow-up in the landing repo.

22. **★ First-run default and silent install.** *Accepted:* explicit first-run screen on fresh
    installs, no silent install; (a) pre-selected when the port is free, (d) when a smind daemon
    already answers. **Differs from Paseo** (silent auto-start + a settings toggle).
    Alternative: Paseo parity — silent start with a dismissible banner.

23. **★ Local daemon on later launches and quit.** *Accepted:* when `manageLocalDaemon` is
    true and `local` is selected, auto-start on every launch; the daemon keeps running after the
    app quits (smind's ADR-0013 choice; Paseo stops it by default) — optionally add a "stop when
    quitting" setting later.

24. **★ Credential entry in "Connect to a daemon".** *Accepted:* an optional **password** field
    shown when the probe says `authRequired` (needs decision 9); no raw-token field.

25. **★ Default connection / host with several connections.** *Accepted:* keep "last selected";
    keep connection ids derived from daemon identity so a later "relay now, direct when
    reachable" (Paseo `preferredConnectionId`, Maestro `relay-then-direct`) needs no migration.

26. **★ Background `smind start` without an OS service.** *Accepted: no* — `smind service` and
    the desktop app cover it; Paseo's `daemon start` + supervisor is a second supervision path.

33. **✔ Rename — DECIDED by Human (2026-10-13): relay "workspace" → "relay host"** (D12); `workspace` stays a deprecated CLI
    alias; wire/proto field names are not changed; smind's own "Workspace" is not renamed.

34. **✔ Relay code layout and deploy gating — DECIDED by Human (2026-10-13):** the relay code stays in the smind monorepo
    (the Cloudflare Worker directory was to be `relay-worker/`; under (C)/(D) the deployment definition lives in a
    self-contained deploy directory — split into its own repository later only if it needs separate ops/licensing);
    `deploy-relay.yml` is separate from release-please and path-filtered; the credentials sit in a GitHub Environment
    `relay-production` with required reviewers. **The hosting option itself is decision 12.**

35. **★ Device identity and pairing window (D9).** *Accepted: yes* — a persistent Ed25519 device key proven inside
    `device_hello`, a daemon-side device registry, an offer-opened pairing window (10 minutes or one new device), and `smind
    relay devices ls|approve|revoke|rename`; amends ADR-0007 (g). Alternative (Paseo's stance): ephemeral device keys, `devices
    ls` shows only connected devices, `revoke` closes the connection, `host rotate` is the hard reset.

## Open decisions for Human

These remain **Open** (Human, 2026-10-14): the hosted relay (12 and everything it gates: 27–32, 36) and direct connections
with a password (9). Each keeps its recommended default or its option list; **every PR they gate is BLOCKED in the plan.**

9. **★ Direct connections with a password (D8).** *Recommended: yes* —
   `server.listen`, `smind set-password`, refuse to start non-loopback without a password,
   `/api/token` loopback-only, expiring session tokens, Host allowlist, login rate limit;
   **replaces "loopback only, no `--bind`"** and the "don't publish 4648" Docker advice.
   Alternative: stay loopback-only and rely on relay/tunnel (cheaper, leaves Docker and
   homelab LAN users on the relay).

12. **⏳ Hosted relay hosting — PENDING (Human, 2026-10-14).** Options in D11: **(D)** Cloudflare's gRPC proxy (Network →
    gRPC; origin on 443 with TLS + HTTP/2/ALPN; proxied record, SSL/TLS Full; 125 s read / 30 s write timeouts, so 30–60 s
    keepalive and client reconnect; Access ignores gRPC) in front of the unchanged Go relay on **Cloud Run** (documented
    bidi gRPC, streams capped at 60 min) or a **small VPS**; **(C)** the unchanged Go relay in **Cloudflare Containers behind a
    Worker via inbound gRPC/TCP** (announced 2026-08-03, private beta, bidi for Containers only; Human has requested access);
    **(B)** WebSocket v3 on Workers + Durable Objects — **no longer preferred**. No recommendation is recorded as a
    decision; the Lead's input is in D11. **Every hosted-relay PR is blocked on this.** Sources:
    [gRPC connections](https://developers.cloudflare.com/network/grpc-connections/),
    [connection limits](https://developers.cloudflare.com/fundamentals/reference/connection-limits/),
    [gRPC for Workers and Containers](https://blog.cloudflare.com/grpc-workers/).

27. **★ Hosting budget** — **blocked on 12.** Depends on the option (VPS or Cloud Run bill; or Workers Paid + Containers
    active CPU for (C)); recommended once chosen: a monthly usage alert and a kill-switch (disable open enrolment) when
    it fires.

28. **★ Enrolment model** — design is hosting-agnostic, **PR blocked on 12.** *Recommended:* anonymous, self-service, no
    accounts in v1, server-generated host ids via an additive `Enrol` RPC, the abuse limits in D11. Alternative: GitHub
    login from day one.

29. **★ Credentials and GitHub Environments** — **blocked on 12 (BLOCKED on Human).** The `relay-production` Environment
    with required reviewers (decided, decision 34) and the secrets that go in it depend on the option: Cloudflare token
    (DNS, zone settings for the gRPC toggle, Workers/Containers) and a GCP service account or deploy key.

30. **★ Default relay address** — **blocked on 12.** *Recommended:* `smind relay connect` with no arguments uses
    `relay.spacingmind.com` after a one-time consent line; a self-hosted relay always needs an explicit address.

31. **★ Relay protocol compatibility.** *Recommended:* the D9 proto changes are additive (no version bump); a relay always
    accepts the current and previous protocol revision, because the hosted relay (whenever it exists) deploys
    independently of smind releases. (Replaces the earlier "gRPC v2 retirement" decision.)

32. **★ Logging and privacy.** *Recommended:* no payload and no content-derived data are ever logged; IP addresses appear
    only in short-lived edge logs (≤ 7 days); a privacy page on spacingmind.com **before** the hosted default is switched
    on (landing-repo follow-up).

36. **★ Single-port gRPC + grpc-web mode** — part of the hosted PRs (**blocked on 12**), listed so it is not missed: Cloudflare
    proxies gRPC on 443 only, while the relay serves gRPC and grpc-web on two ports today. *Recommended:* a `--listen-mux`
    option serving both on one TLS/h2 listener when option (C) or (D) is chosen.

## Consequences

- Twenty-one shippable PRs (plan); riskiest: `smind update` (replaces an executable), direct connections (opens the
  daemon to the network), multi-device relay (a new device-identity layer across Go, Rust and TypeScript), the public
  hosted relay (abuse, cost, privacy — blocked on the hosting decision), the Docker release job (publishes to a
  registry), the first-run flow (changes first launch).
- Public API / security changes proposed, each gated on a decision: `/healthz managedBy` (6),
  `daemon.info` (17), non-loopback listen + login + session tokens + `/api/token` gating (9),
  additive relay messages for multi-device (13, 35) and an open enrolment RPC (28, blocked).
- The macOS silent-install default changes (22); existing users are unaffected by the upgrade rule.
- Two footguns are fixed on the way: macOS Take-over adopting a non-smind owner, and a pairing link
  pasted into the URL form being saved as a bogus direct connection.
- ADR-0013 ("daemon binds loopback") is amended by D8 if accepted; ADR-0007 (b) ("one device") and (g)
  (ephemeral device key) and ADR-0011 (TOFU pinning only, per-device identity deferred) are amended by D9/D11; ADR-0007 (f)
  (gRPC transport) is **kept**; their texts
  get a pointer to this ADR. `docs/ROADMAP.md:98` ("relay.spacingmind.sh") should read `relay.spacingmind.com`.
- The hosted relay makes smind an operator: a privacy page, abuse handling and cost monitoring become
  ongoing work.

## Cross-references

- ADR-0007, ADR-0011 (relay, admission, per-device identity deferred), ADR-0013 (desktop bundled UI,
  remote daemons never updated by the app, loopback posture), ADR-0023 (updater keys), ADR-0029
  (`/api/shutdown`), ADR-0030 (Windows native daemon; rejected a service for the app).
- `docs/plans/completed/desktop-relay-transport.md` (single-device limit),
  `docs/plans/active/desktop-windows-native-daemon.md` and the `feat/windows-bundled-daemon` branch.
- Paseo (`refs/paseo` @ `8d9e7d3a`): see the reference table above and each "Paseo reference" block.
- Plan: [`docs/plans/active/homelab-cli.md`](../plans/active/homelab-cli.md).
