# 0031: Homelab distribution — `smind update`, `smind service`, install scripts, Docker, direct and relay connections, desktop first-run

## Status

**Proposed (2026-10-13, revised after a study of Paseo the same day).**
Nothing here is decided until Human answers the "Open decisions for Human"
list (AGENTS.md rule d). Each open decision carries a recommended default;
decisions whose default changed *because of* the Paseo study are marked **★**.
The plan ([`docs/plans/active/homelab-cli.md`](../plans/active/homelab-cli.md))
is written against those defaults. Builds on ADR-0007/0011 (relay, admission),
ADR-0013/0023/0030 (desktop app, updater, native Windows daemon) and ADR-0029
(`POST /api/shutdown`). Several decisions here **amend** earlier ones (loopback
only, one relay device per workspace); each says so and, once accepted, the
amended ADR gets a one-line pointer here.

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
  `smind relay workspace new <id>` prints the workspace secret once
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

## Decision (proposed)

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

**Coexistence with the desktop app.** The app already refuses to install over an
unmanaged owner (`managed.rs:64-70`) and starts nothing when the port is held
(`launch_decision`), so it will not fight a service. What must change:
(1) a service-managed daemon must be recognised as *service-managed*, so the UI
hides **Take over** and says "managed by systemd/launchd/Task Scheduler — stop
the service to hand it to the app" (taking over a supervised daemon would make the
supervisor restart it); the signal is an additive `/healthz` field `managedBy`
set from an env var the unit defines (`SMIND_SUPERVISOR=systemd|launchd|task`)
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

- `smind relay offer` prints the URL (stdout, unchanged for scripts) and, with a TTY
  on stdout or `--qr`, a terminal QR on stderr; `--png <file>` writes a PNG at mode
  0600. QR encoding tries level M, falls back to level L when M does not fit, and
  if the offer still does not fit prints the URL plus a one-line "too long for a QR
  code" note instead of failing. Wording becomes "for a mobile device or the desktop
  app". The paste-able URL is what works today for both clients; the QR is
  convenience for a future mobile scanner.
- Offer semantics (open decision 15): reusable, non-expiring, equivalent every time,
  because it *is* the workspace secret and the relay has no per-device credential;
  the CLI says so ("treat this URL like a password; to revoke, enrol a new
  workspace and `relay connect` again"). Optionally a **daemon password** (decision
  14) becomes the second factor so a leaked offer alone is not enough.
- Desktop UI: Settings → Connections gains **Add via relay link**: paste → Rust
  parses (new `connections_preview_relay` returns only non-secret fields: relay
  host:port, workspace id, grouped daemon-key fingerprint, whether the relay is
  fingerprint-pinned) → the user confirms those values → `connections_add_relay`.
  The pasted string is held in component state only until submit and never logged.
  The existing URL form rejects any input containing `#offer=` and points to the new
  action (fixing the footgun). E2EE and fingerprint pinning are unchanged
  (ADR-0007/0011).
- Deep link `smind://pair#offer=…`: not in v1 (decision 16); when added it opens the
  same confirmation screen and never adds a connection from a click alone, because a
  web page can trigger `smind://`.
- Homelab quickstart `docs/homelab.md` documents **both** paths: direct connection
  (D8) for LAN/VPN, and the relay (self-hosted on a VPS or on the homelab with
  `7400/tcp` + `7401/tcp` open — the desktop derives the native port as grpc-web − 1,
  `client.rs:45-68`, so they must be adjacent; compare the fingerprint printed by
  `smind relay` with the one `relay connect` pins, because pinning is
  trust-on-first-use).

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
  workspace admission secret (ADR-0011), so our offer has to carry that secret
  — which is why it is a "password-like" link and why it does not fit a QR.
  Shrinking it (drop the duplicate `id`, base64url the fingerprint) is possible but
  needs a v2 offer decoded by Go, Rust and TS; deferred, level-L fallback instead.
  Paseo also offers a compact `relay://host:port/<serverId>?key=…[&password=…]` URI
  (`daemon-endpoints.ts`) for typing/pasting; we do not need a second format yet.
- *Different, hosting:* Paseo operates `relay.paseo.sh`; smind has none (decision 12).
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

### D9. Relay multi-device attach (amends ADR-0007 (b) / ADR-0011 "one device")

**Proposal.** Lift the one-device-per-workspace limit **in this ADR's scope** (plan PRs
P12/P13), using the model Paseo already runs in production. This supersedes my earlier
position that it be an indefinitely deferred follow-up.

Paseo's v2 protocol (`cloudflare-adapter.ts:119-127,340-395`, `relay-transport.ts:49-55`):
one relay session per daemon; the daemon holds **one control socket**; every client
socket gets a `connectionId` **assigned by the relay** (clients do not choose it); the
relay tells the daemon over the control socket `{type:"connected"|"disconnected",
connectionId}` and a `sync` list on (re)connect; the daemon opens **one data socket per
`connectionId`**; the relay forwards between `client:<id>` and `server:<id>` (buffering
frames until the daemon's data socket exists); **each connection runs its own E2EE
handshake** with a fresh ephemeral client key; many sockets may share a connectionId and
the daemon's data socket stays up while any remain (`cloudflare-adapter.test.ts`,
test `clt_multi`).

smind already has the pieces:

| Paseo | smind today | Change |
|---|---|---|
| control socket | `OpenControl` stream (`relay.proto:63`, `server.go:388-395`: only `Ping` handled) | relay → daemon notifications `DeviceConnected{device_id}`, `DeviceDisconnected{device_id}`, `Sync{device_ids}` (reusing the declared `DeviceAttach`/`DeviceDetach` slots, `relay.proto:121-148`) |
| `connectionId` | route key `(workspace, session, device)` (`server.go:576-604`) | relay assigns `device_id` per admitted device stream; `attachRoute` stops replacing other devices' routes (it keeps replacing *the same* device on reconnect) |
| per-connection data socket | `OpenData` stream per route | daemon opens one `OpenData` per announced device |
| per-connection E2EE | one hardcoded `(m1-default-session, m1-default-device)` bridge (`bridge.go:12-35`) | bridge spawns a handler + E2EE session per `device_id`; fresh ephemeral device key each, daemon key unchanged |
| buffering until daemon attaches | per-side bounded buffers (ADR-0007 (a)) | unchanged |

Clients: the Rust desktop client and the mobile TS client stop hardcoding the fixed ids
and take the relay-assigned device id. Compatibility: relay admission protocol version is
bumped; older clients (single hardcoded id) get a clear "update required" rejection, as
the ADR-0011 amendment did for admission v2 (no dual accept).

**Paseo reference — ported (protocol shape), adapted (admission, ids).** Ported:
control socket + per-connection data stream + relay-assigned ids + per-connection E2EE +
sync-on-reconnect. Adapted: our relay keeps **workspace admission** (ADR-0011) in front,
so device ids are scoped to an admitted workspace; the transport is gRPC streams (native
for desktop, grpc-web for mobile) rather than WebSockets (ADR-0007 (f)); the relay is a Go
process, not a Durable Object. Different on purpose: Paseo's relay needs no enrolment
because it is open; ours stays closed (decision 12).

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
- **Hosted relay like `relay.paseo.sh`.** Requires open enrolment (abuse, cost) or removing the
  workspace secret, i.e. amending ADR-0011, plus infrastructure and on-call. Decision 12.
- **Keep one device per relay workspace and document it (this ADR's first draft).** Contradicted
  by Paseo's production design, which our proto already anticipates. Replaced by D9.
- **Building the Docker image from source inside Docker** (Paseo's approach for Node). Slow
  multi-arch builds under QEMU and a binary that differs from the checksummed release.
- **Baking agent CLIs into the image.** Paseo, too, leaves them out. Decision 19.
- **Silent auto-install on first launch everywhere (status quo on macOS, Paseo's behaviour).**
  Removes the choice to run no local daemon or use an existing one. Not chosen (decision 22).
- **Storing `manageLocalDaemon` inside `connections.json`.** Couples a behaviour flag to a
  registry that tolerates and rewrites corrupt files. Separate file.
- **QR-only pairing for desktop.** A desktop app has no scanner worth building; paste is the
  primary path.

## Open decisions for Human

Each has a recommended default; the plan assumes the recommendation. **★ = the default
changed, or the decision is new, because of the Paseo study.**

1. **CLI signature verification.** Verify a minisign signature of `checksums.txt` in
   `smind update` with the existing desktop-updater public key (ADR-0023; no new secret),
   signed by a new `publish`-job step, as a separate PR after the unsigned core. *Recommended:
   yes.* (Paseo has nothing to compare: npm integrity.) Install scripts stay checksum-only.
2. **Windows: Scheduled Task vs Service.** *Recommended: per-user Scheduled Task at logon.*
3. **Linux/macOS service scope.** *Recommended: user unit / LaunchAgent by default*;
   `--system` opt-in on Linux (runs as the invoking user, never root); no LaunchDaemon in v1.
4. **Update refusal policy.** *Recommended:* refuse while runs are running or a permission is
   pending; `--force` overrides; no `--wait`; stable only; downgrade needs `--force`.
5. **Restart after update.** *Recommended:* restart only a service-managed daemon; for a
   hand-started one print instructions.
6. **Supervisor signal.** *Recommended:* additive `/healthz` field `managedBy` from env
   `SMIND_SUPERVISOR` (Paseo records `desktopManaged` in its lock file instead — adapted).
7. **★ Daemon log file.** *Recommended: yes* — `smind serve` also writes a rotating
   `$SMIND_HOME/daemon.log` (e.g. 10 MiB × 5), `smind service logs` tails it on every OS;
   journald stays available. Replaces the shell-redirect/per-OS log design. Adds a small
   rotation dependency or ~80 lines of code.
8. **★ `smind config get|set|unset`; no launch flags.** *Recommended: yes, no
   `--port/--listen` flags on `serve`* (Paseo removed the equivalent flags).
9. **★ Direct connections with a password (D8).** *Recommended: yes* —
   `server.listen`, `smind set-password`, refuse to start non-loopback without a password,
   `/api/token` loopback-only, expiring session tokens, Host allowlist, login rate limit;
   **replaces "loopback only, no `--bind`"** and the "don't publish 4648" Docker advice.
   Alternative: stay loopback-only and rely on relay/tunnel (cheaper, leaves Docker and
   homelab LAN users on the relay).
10. **★ Built-in TLS for direct connections.** *Recommended: not in v1* — document Tailscale,
    SSH, or a TLS reverse proxy (with the Host-rewrite caveat); revisit with `server.tls`.
11. **★ SSH-tunnel connection type in the desktop app (b2).** *Recommended: yes* — the app runs
    the system `ssh -L` (Paseo `remoteSsh`); no daemon exposure and no password.
12. **★ Hosted relay vs self-host.** *Recommended: self-host only for now.* Paseo hosts
    `relay.paseo.sh`; a smind-hosted relay needs open enrolment or an ADR-0011 change plus ops.
    Revisit after multi-device ships.
13. **★ Relay multi-device (D9).** *Recommended: in scope — PRs P12/P13, scheduled right after
    direct connections* (replaces "accept one device at a time, separate ADR"). Alternative:
    keep the one-device limit and document it.
14. **★ Password for relay devices.** *Recommended: yes* — when a daemon password is set, relay
    devices must present it inside the E2EE hello (Paseo's pattern; Paseo is lenient for old
    clients, we enforce since smind has no legacy builds to protect). Makes a leaked reusable
    offer insufficient alone.
15. **Offer reuse/expiry.** *Recommended:* reusable, no expiry, loud "treat like a password"
    wording, `--png` at 0600 (Paseo is the same: no expiry, no per-device credential).
16. **Deep link `smind://pair`.** *Recommended:* paste-only in v1; later with the confirmation
    screen, never auto-add.
17. **`daemon.info` RPC.** *Recommended:* add a small additive WS method `{version, commit}`
    so the notice works for relay connections (Paseo reads version from `server_info`).
18. **QR default.** *Recommended:* print the QR only on a TTY or `--qr`, fall back to level L,
    never fail the command; offer compaction (v2) deferred.
19. **★ Docker image contents.** *Recommended:* one image (Debian-slim, git, ssh client, Node 22,
    `gosu`, `tini`, curl), **root entrypoint that fixes ownership then drops to uid 1000**
    (Paseo), agent CLIs not baked in + child-image example + compose example, listen
    `0.0.0.0` only with a required password (needs decision 9). Alternative: a `-agents` variant.
20. **Image provenance.** *Recommended:* buildx provenance/SBOM on releases; no cosign in v1.
21. **`install.ps1` location.** *Recommended:* `%LOCALAPPDATA%\Programs\smind` + user `PATH`
    (never the app's managed directory); landing redirect as a follow-up in the landing repo.
22. **★ First-run default and silent install.** *Recommended:* explicit first-run screen on fresh
    installs, no silent install; (a) pre-selected when the port is free, (d) when a smind daemon
    already answers. **Differs from Paseo** (silent auto-start + a settings toggle).
    Alternative: Paseo parity — silent start with a dismissible banner.
23. **★ Local daemon on later launches and quit.** *Recommended:* when `manageLocalDaemon` is
    true and `local` is selected, auto-start on every launch; the daemon keeps running after the
    app quits (smind's ADR-0013 choice; Paseo stops it by default) — optionally add a "stop when
    quitting" setting later.
24. **★ Credential entry in "Connect to a daemon".** *Recommended:* an optional **password** field
    shown when the probe says `authRequired` (needs decision 9); no raw-token field.
25. **★ Default connection / host with several connections.** *Recommended:* keep "last selected";
    keep connection ids derived from daemon identity so a later "relay now, direct when
    reachable" (Paseo `preferredConnectionId`, Maestro `relay-then-direct`) needs no migration.
26. **★ Background `smind start` without an OS service.** *Recommended: no* — `smind service` and
    the desktop app cover it; Paseo's `daemon start` + supervisor is a second supervision path.

## Consequences

- Thirteen shippable PRs (plan); riskiest: `smind update` (replaces an executable), direct
  connections (opens the daemon to the network), relay multi-device (cross-language protocol
  change), the Docker release job (publishes to a registry), the first-run flow (changes first
  launch).
- Public API / security changes proposed, each gated on a decision: `/healthz managedBy` (6),
  `daemon.info` (17), non-loopback listen + login + session tokens + `/api/token` gating (9),
  relay protocol bump for multi-device (13).
- The macOS silent-install default changes (22); existing users are unaffected by the upgrade rule.
- Two footguns are fixed on the way: macOS Take-over adopting a non-smind owner, and a pairing link
  pasted into the URL form being saved as a bogus direct connection.
- ADR-0013 ("daemon binds loopback") and ADR-0007 (b)/ADR-0011 ("one device") are amended by D8/D9
  if accepted; their texts get a pointer to this ADR.

## Cross-references

- ADR-0007, ADR-0011 (relay, admission, per-device identity deferred), ADR-0013 (desktop bundled UI,
  remote daemons never updated by the app, loopback posture), ADR-0023 (updater keys), ADR-0029
  (`/api/shutdown`), ADR-0030 (Windows native daemon; rejected a service for the app).
- `docs/plans/completed/desktop-relay-transport.md` (single-device limit),
  `docs/plans/active/desktop-windows-native-daemon.md` and the `feat/windows-bundled-daemon` branch.
- Paseo (`refs/paseo` @ `8d9e7d3a`): see the reference table above and each "Paseo reference" block.
- Plan: [`docs/plans/active/homelab-cli.md`](../plans/active/homelab-cli.md).
