# 0031: Homelab CLI distribution — `smind update`, `smind service`, install scripts, Docker, relay pairing and first-run for desktop clients

## Status

**Proposed (2026-10-13).** Nothing here is decided until Human answers the
"Open decisions for Human" list below (AGENTS.md rule d). Each open decision
carries a recommended default; the plan
([`docs/plans/active/homelab-cli.md`](../plans/active/homelab-cli.md)) is
written against those defaults and flags which PRs are blocked on which
answer. Builds on ADR-0007/0011 (relay, admission), ADR-0013/0023/0030
(desktop app, updater, native Windows daemon) and ADR-0029 (`POST
/api/shutdown`). Does not supersede any of them.

## Context

Human wants to run smind on a homelab/server through the CLI and reach it from
desktop clients (and mobile) over the relay, with the same one-link/QR
pairing the mobile app uses; plus a polished first launch for the desktop app.
Today:

**CLI and install**

- `smind` has no `update` and no `service` command. The dispatcher knows
  `serve stop relay workspace account space task profile usage mcp skill
  version` (`cmd/smind/main.go:40-66`; usage text `:75-132`). `smind serve`
  takes no arguments at all (`cmd/smind/serve.go:34-37`): the port comes
  from `~/.smind/config.yaml` `server.port` (`internal/config/config.go:90-93`)
  and is read the same way by every CLI client (`cmd/smind/client.go:41-49`,
  `cmd/smind/stop.go:52`), so daemon and CLI must see the same config file.
- `scripts/install.sh` installs `smind_<v>_<os>_<arch>.tar.gz`, verifying only
  the SHA-256 in the `checksums.txt` fetched from the same release
  (`scripts/install.sh:69-82`). Version pinning is the `SMIND_VERSION` env
  only (`:7`, `:56`); there is no `--version`/`--service`. It still says
  "no native Windows build yet" (`:13-14`, `:30-32`) although v0.11 ships
  `smind_<v>_windows_<arch>.zip` (`.github/workflows/release-please.yml:139-154`).
  README `:123-126` is stale the same way (and still says the app manages the
  daemon "via WSL2", which ADR-0030 removed).
- `https://spacingmind.com/install.sh` is a **307 redirect** to
  `raw.githubusercontent.com/spacingmind/smind/master/scripts/install.sh`
  (checked with `curl -I`, 2026-10-13). The redirect lives in the landing repo
  (`spacingmind/spacing-mind-landing-page`, `apps/web/next.config.ts`
  `redirects()`, destination `siteConfig.links.installScript`, comment:
  "served from smind's master branch, which only moves on release").
  `/install.ps1` returns 404 today. So a new script is live at spacingmind.com
  only after (1) a release carrying it moves `master` and (2) a landing PR
  adds the redirect.
- Release assets: per-OS archives plus one `checksums.txt` built in the same
  `publish` job that uploads them (`release-please.yml:307-317`, `:332-342`).
  `.sig` files exist only for the desktop updater bundles; the minisign
  public key is in `desktop/src-tauri/tauri.conf.json:25` and the private key
  is the `TAURI_SIGNING_PRIVATE_KEY` secret (ADR-0023). A compromised release
  could swap an archive *and* `checksums.txt`; the checksum only detects
  corruption. No Docker image is built anywhere (no `docker`/`ghcr` in
  `.github/workflows/*`).
- Version signal exists: `/healthz` returns `{status, service, version}`
  (`internal/server/server.go:148-153`), `smind --version`
  (`cmd/smind/main.go:63`).
- The daemon binds `127.0.0.1` only (`Server.Addr`, `internal/server/server.go:139-146`)
  and serves `GET /api/token` **unauthenticated** (`:170-174`, whose own doc
  comment says it is "scoped to smind's current single-user-localhost posture;
  revisit if remote access is ever added"). Consequences for a homelab:
  `-p 4648:4648` on a container cannot work (the in-container daemon listens
  on the container's loopback), and putting a reverse proxy on the same host
  in front of it would publish the token. Remote access today is the relay,
  or an SSH/Tailscale tunnel to loopback.
- Graceful stop is SIGINT/SIGTERM *or* `POST /api/shutdown`; both run one
  shutdown path (`cmd/smind/serve.go:118`, `:130-147`; ADR-0029 D2;
  `internal/server/server.go:183-186`); `smind stop` waits up to 30 s
  (`cmd/smind/stop.go:17-22`). A clean shutdown exits 0 (`serve.go:147`), a
  fact the launchd `KeepAlive` design below relies on.
- Agent providers are external processes: GLM runs `npx -y glm-acp-agent@1.14.0`
  (`internal/acp/glm.go:32`) and the Claude runner finds `claude` via
  `exec.LookPath` (`internal/claudecli/resolve.go:45`). Any service or
  container environment must therefore provide `PATH` with `git`, `node`/`npx`
  and the agent CLIs.

**Relay pairing (re-verified)**

- The relay server is `smind relay` (`cmd/smind/relay.go:34-74`; defaults
  `:7400` native gRPC and grpc-web `:7401`,
  `internal/relay/server/run.go:51-54`). `smind relay workspace new <id>`
  prints the workspace secret once (`relay.go:83-93`). `smind relay connect
  <addr> <workspace-id> <secret>` stores the config and pins the relay's TLS
  fingerprint **trust-on-first-use** (`relay.go:130-155`); `smind serve` then
  dials the relay on every start (`serve.go:128`, `:150-176`).
- `smind relay offer` prints **only a URL** (`relay.go:213`), takes no flags
  (`:176-178`) and its text says "for a mobile device" (`:153`, `:252`).
  `Offer.QRText`/`QRPNG` exist (`internal/relay/pairing/qr.go:218-275`) but
  **nothing calls them** outside the package (grep over `cmd/` and `internal/`).
- **A realistic offer does not fit the QR encoder.** The in-tree encoder
  stops at version 15; at level M that is 412 bytes (`qr.go:32`, `:86`). An
  offer for `https://homelab-relay.example.com:7401`, workspace `homelab`,
  a pinned fingerprint and a 32-byte secret is a **432-byte URL** (measured
  with a throw-away test, 2026-10-13) and returns `ErrContentTooLong` at level
  M. Level L is supported (`qr.go:72`) but is unused by `QRText`/`QRPNG`.
- The offer is the **workspace master secret** plus the daemon public key and
  the relay's grpc-web address (`internal/relay/pairing/offer.go:64-82`; the
  address is derived as native port + 1, `relay.go:163-174`,
  `desktop/daemon-client/src/relay/client.rs:45-68`). It has **no expiry, is
  not single-use, and is identical every time it is printed**; there is no
  per-device credential (`offer.go:71-81` calls this "a known milestone-1
  simplification"; ADR-0011 defers per-device identity). The payload lives in
  the URL fragment (`offer.go:173-192`, `:194-211`).
- `DefaultPairURL` is `https://spacingmind.sh/pair` (`offer.go:28-31`).
  `spacingmind.sh` does not resolve (checked 2026-10-13), so it can never act
  as a universal link; it is harmless only because desktop and mobile parse
  the fragment and ignore the host.
- The mobile app is **paste-only**: its pairing screen is a text field
  (`mobile/src/screens/PairingScreen.tsx:3`, `:50`) and no camera/barcode
  dependency exists under `mobile/`. A QR code is therefore not scannable by
  any smind client today; it is useful once mobile gains a scanner.
- **One attached device per workspace.** The daemon bridge hardcodes a single
  `(session, device)` pair (`internal/relay/bridge/bridge.go:12-35`) and the
  relay's `attachRoute` cancels the previous stream for the same route key
  and replaces it (`internal/relay/server/server.go:576-604`).
  `docs/plans/completed/desktop-relay-transport.md` records the consequence:
  "a mobile device and a desktop relay connection to the same workspace cannot
  both be attached at once — connecting one will disconnect the other".
  The desktop client has an automatic reconnect loop with backoff capped at
  30 s (`desktop/daemon-client/src/relay/client.rs:39`, `:485-491`), so two
  clients auto-reconnecting against one workspace will likely evict each other
  repeatedly (unverified; the plan has a test scenario).
- Desktop side: Rust supports relay connections and exposes
  `connections_add_relay(label, pairing_url)` (`desktop/src-tauri/src/commands.rs:76-120`,
  registered `lib.rs:105`, `build.rs:34`, already allowed by the capability
  `capabilities/proxy.json:13`). The bundled UI never calls it:
  `DesktopApi` has only `addConnection(label, url)`
  (`web/packages/ui/src/lib/platform.ts:98-102`, stub `:196-202`, impl
  `:257-263`) and Settings → Connections has one label + URL form
  (`web/packages/ui/src/components/settings/connections-section.tsx:53`,
  `:128-147`). **Footgun:** pasting a pairing link there goes to
  `connections_add`, whose `validate_base_url` accepts any http(s) URL and
  strips the path and fragment (`desktop/daemon-client/src/proxy/connections.rs:41-62`),
  so it silently saves a bogus direct connection to `https://spacingmind.sh`.
- A relay connection's `base_url` is `relay://<workspace>@<native-addr>`
  (`connections.rs:157-173`). `connection_version` probes
  `GET <base_url>/healthz` with `reqwest` (`desktop/src-tauri/src/daemon_manager.rs:149-165`,
  `:1008-1024`), which cannot work for `relay://`; so the existing "daemon is
  older than this app" banner (`web/packages/ui/src/components/desktop-daemon-banner.tsx`)
  **can never show for a relay connection**. No WS RPC returns the daemon
  version either (grep of `internal/wsapi`). The banner today is text only for
  non-managed connections (it renders the version sentence; its buttons are
  gated on `actionable`).
- `smind://` deep links exist in the app but only navigate to tasks
  (`desktop/src-tauri/src/deeplink.rs:1-5`; scheme registered at
  `tauri.conf.json:30-35`).

**Desktop first launch (audit, 2026-10-13)**

- The connection registry always contains a built-in, non-removable `local`
  entry at index 0 and a fresh registry selects it
  (`desktop/daemon-client/src/proxy/connections.rs:1-3`, `:100-107`, `:216-248`).
  There is no "first run completed" flag, no onboarding screen, and no
  setting to stop the app managing a local daemon.
- **macOS:** `auto_start` runs on every launch (`lib.rs:303-307`,
  `daemon_manager.rs:313-345`): if the `local` connection is selected and
  nothing answers, it **silently installs the bundled daemon and starts it**;
  an older managed daemon is offered an update; an unmanaged owner of the port
  is never touched automatically (`launch_decision`, `:295-310`).
- **Windows:** no auto-start (`#[cfg(target_os = "macos")]`, `lib.rs:303`). A
  fresh launch shows "Can't reach Local … Start the daemon (`smind serve`),
  or switch to a different connection" (`web/packages/ui/src/components/desktop-unreachable.tsx`);
  the only route to a daemon is Settings → Daemon → Install
  (`settings/daemon-section.tsx`). A parallel branch
  (`feat/windows-bundled-daemon`) is changing Install/Update to copy a bundled
  `smind.exe`.
- **Linux (deb):** `Platform::Unsupported` (`daemon_manager.rs:94-108`): no
  daemon management at all.
- **Unmanaged port owner:** `classify` = Managed only if the recorded pid owns
  the port, else Unmanaged (`desktop/daemon-client/src/daemon_manager/managed.rs:43-51`);
  install is refused (`:64-70`); *Take over management* is offered with a
  confirm. On Windows take-over is refused unless the owner image is
  `smind.exe` (`daemon_manager.rs:389-397`); on **macOS it is never refused**
  (`:389` doc, test at `:1215`), so Take over can adopt *any* port owner (for
  example the `ssh` process of an `ssh -L 4648:…` tunnel) and a later
  Restart/Update then treats that executable as "ours".
- URL connections need no token entry: the proxy forwards the remote's
  unauthenticated `GET /api/token` (ADR-0013 decision 1). Given the loopback
  bind above, a "LAN URL" connection works only through a tunnel/port-forward
  to the daemon's loopback.
- `desktop/ui/offline.html` is the Tauri `frontendDist` (`tauri.conf.json:7`)
  fallback page; the bundled UI's own `desktop-unreachable.tsx` took over its
  job (ADR-0013 Consequences).
- Reference apps: Paseo's `WelcomeScreen` offers scan-QR / direct connection /
  paste pairing link / remote SSH, redirects to the app as soon as any host is
  online, shows a **host confirmation** (key fingerprint + relay endpoint)
  before saving a host from a pairing link, and has a persisted
  `manageBuiltInDaemon` setting (default true) toggled with a confirm
  (`refs/paseo/packages/app/src/components/welcome-screen.tsx`,
  `runtime/host-confirmation.ts`, `desktop/settings/desktop-settings.ts`,
  `desktop/daemon/daemon-management-toggle.ts`). ZCode's first-run screen is an
  account login with a "skip" escape (`refs/zcode/packages/ui/src/WelcomeScreen.tsx`);
  it has no connection onboarding, so it contributes only visual style.

## Decision (proposed)

### D1. `smind update`

`smind update [--check] [--version <v>] [--force] [--dry-run] [--json]`.

- Source: GitHub Releases of `spacingmind/smind`; "latest" is resolved the same
  way as `install.sh` (the `releases/latest` redirect, not the rate-limited
  API; `install.sh:58-62`). Stable channel only — `release-please-config.json`
  sets `prerelease: false` and the desktop updater already relies on this
  (ADR-0023 §3).
- Verification: download `smind_<v>_<os>_<arch>.tar.gz|zip` and
  `checksums.txt`, verify the SHA-256 exactly like `install.sh`
  (`:79-82`) and ADR-0030's Windows install; extract **only** the `smind` /
  `smind.exe` member (reject path traversal and oversize members); run the new
  binary's `version` to confirm it matches the target before swapping.
  Signature verification is open decision 1.
- Refusals (each exits non-zero with a one-line reason): running agent work
  exists (open decision 4); the current binary is a `dev` build; the running
  binary lives inside the desktop app's bundle or managed-daemon directory
  (ADR-0013/0030: that daemon belongs to the app); the target is older than
  current (downgrade; the store may have migrated forward) unless `--force`;
  the binary's directory is not writable (message tells the user to re-run
  with the privileges they installed with or use `install.sh`; never
  self-elevates).
- Active-run detection: dial the local daemon (`dialDaemon`,
  `cmd/smind/client.go:25`) and call `run.list` (`internal/wsapi/handlers.go:67`);
  a daemon that is not running means "nothing to protect".
- Atomic replace: write `<dir>/.smind.new.<pid>` next to the target,
  `chmod 755`, keep the old binary as `smind.old`, `rename` over the target
  (same pattern as `install.sh:97-100`). **Windows** cannot overwrite a running
  `smind.exe` (and the daemon re-executes `smind.exe` as the Claude launcher,
  `cmd/smind/main.go:24`), but it can rename it: rename `smind.exe` →
  `smind.exe.old`, move the new file in, and delete `.old` on the next
  `smind` start or update. If the post-swap check fails, roll back from `.old`.
- Restart: only a **service-managed** daemon is restarted (via D2's
  `service restart`, then poll `/healthz` until `version == target`, rolling
  back on timeout). A manually started daemon is never respawned silently; the
  command prints "restart it with `smind stop && smind serve`" (open
  decision 5).
- `--check` prints current/latest and exits 0 either way (`--json` for
  scripts); `--dry-run` does everything except the swap.
- Test seam: `SMIND_UPDATE_BASE_URL` redirects downloads to an `httptest`
  server; unset in production.

### D2. `smind service install|uninstall|status|start|stop|restart|logs`

One `internal/service` interface with three backends; the CLI is identical on
every OS.

| | Linux | macOS | Windows |
|---|---|---|---|
| unit | systemd **user** unit `~/.config/systemd/user/smind.service` (default) or `--system` unit `/etc/systemd/system/smind.service` | LaunchAgent `~/Library/LaunchAgents/com.spacingmind.smind.plist` | per-user **Scheduled Task** `smind` at logon |
| start/stop | `systemctl [--user]` | `launchctl bootstrap/bootout/kickstart gui/<uid>` | `schtasks /Run`, `smind stop`, `schtasks /End` fallback |
| restart policy | `Restart=on-failure`, `RestartSec=5`, `TimeoutStopSec=30` | `KeepAlive={SuccessfulExit:false}` | task restart-on-failure (3× / 1 min) |
| logs | journald (`journalctl [--user] -u smind`) | `~/Library/Logs/smind/smind.log` | `%LOCALAPPDATA%\smind\smind.log` |

Rules common to all backends:

- `ExecStart` is the absolute, symlink-resolved path of the running `smind`
  followed by `serve`; install warns if that path looks temporary.
- **Environment.** Service managers start with a minimal `PATH`, so the unit
  captures the installing shell's `PATH` (override `--path`) so `git`, `node`,
  `npx` and agent CLIs resolve. `SMIND_HOME` is written only if it is set
  (override `--home`). There is **no `--port`**: the port lives in
  `config.yaml` because CLI and daemon must agree (open decision 7).
- A graceful stop means exit 0: systemd `KillSignal=SIGTERM` reaches
  `serve.go:118`; launchd `SuccessfulExit:false` therefore does *not* resurrect
  a daemon the user stopped with `smind stop`; a crash still restarts.
- No sandboxing directives (`ProtectHome`, etc.): agents must write to the
  user's repositories.
- `install` is idempotent (rewrites if the rendered unit differs), enables the
  unit and starts it unless `--no-start`. If something already answers
  `/healthz` on the configured port that this service did not start, install
  enables the unit but refuses to start it and says why; it never kills a
  foreign daemon.
- `uninstall` stops and removes the unit; it never deletes `SMIND_HOME`.
- `status` reports installed/enabled/active, pid, port, `/healthz` version vs
  the binary's version, and the managing backend; exit codes follow
  `systemctl` (0 running, 3 not running, 4 not installed).
- Log files (macOS/Windows) are rotated by renaming to `.1` when over 10 MiB at
  install/start/restart.
- Linux `--user` note: the unit only runs at boot if lingering is enabled;
  `install` checks `loginctl show-user` and prints `sudo loginctl enable-linger
  $USER` when it is off. `--system` writes `User=<invoking user>` (from
  `SUDO_USER`), never root.
- Windows: the task registers via XML (`schtasks /Create /XML`) so restart
  settings can be expressed; the action hides the console and appends to
  `smind.log` (for example `conhost.exe --headless cmd /c "smind.exe serve >>
  log 2>&1"`). Whether registering a logon task needs no elevation, and
  whether the headless wrapper behaves on Windows 10/11, are **to be proven on
  a real machine** in the plan's spike (AC verification), not assumed. A task
  runs only after the user logs in; a headless Windows box needs auto-logon —
  stated in the docs, not solved here.

**Coexistence with the desktop app.** The app already refuses to install over
an unmanaged owner (`managed.rs:64-70`) and starts nothing automatically when
the port is held (`launch_decision`), so it will not fight a service. What
must change: (1) a service-managed daemon must be recognised as *service-managed*,
not just "unmanaged", so the UI hides **Take over** and says "managed by
systemd/launchd/Task Scheduler — stop the service to hand it to the app"
(taking over a supervised daemon would make the supervisor restart it
immediately); the signal is an additive `/healthz` field `managedBy` set from
an env var the unit defines (`SMIND_SUPERVISOR=systemd|launchd|task`),
the same kind of additive change as the `version` field in ADR-0013
(open decision 6); (2) macOS Take over must be refused unless the port owner
is a `smind` image, closing the `ssh -L` footgun found above (a bug fix, no
decision needed); (3) the app's "Run smind on this computer" is disabled with an
explanation while a service owns the port, and `smind service install`
refuses to start while the app's managed daemon owns it.

Windows backend choice (Scheduled Task vs real service) is open decision 2;
the rationale for the Task default is below.

### D3. Install scripts

- `scripts/install.sh`: add `--version <v>` (alongside `SMIND_VERSION`),
  `--service` (runs `smind service install` after installing; `--system`
  passes through) and `--help`; document `curl -fsSL … | sh -s -- --service`.
  For `MINGW*|MSYS*|CYGWIN*` print the PowerShell one-liner instead of the
  stale message (`:30-32`); fix the header comment (`:11-14`) and README
  `:123-126`. Checksum stays mandatory; no behaviour change for existing
  callers.
- New `scripts/install.ps1` for `irm https://spacingmind.com/install.ps1 | iex`:
  OS architecture (not process architecture, as in ADR-0030) → asset name,
  TLS 1.2 enforced for Windows PowerShell 5.1, `Get-FileHash` SHA-256 against
  `checksums.txt`, extract only `smind.exe` from the zip (zip-slip safe),
  install to `%LOCALAPPDATA%\Programs\smind\`, add that directory to the user
  `PATH`. **It must not install into `%LOCALAPPDATA%\smind\bin\`**: that is
  the desktop app's managed-daemon directory (ADR-0030) and the app would
  overwrite or fight over it. Warn if Git for Windows is missing
  (ADR-0029 D1). Parameters arrive via env (`SMIND_VERSION`,
  `SMIND_INSTALL_DIR`) because `irm | iex` cannot take switches; `-Service` is
  available when the script is invoked as a script block
  (`& ([scriptblock]::Create((irm …))) -Service`).
- Serving: `install.ps1` needs a landing redirect identical to `install.sh`
  (a follow-up PR in `spacingmind/spacing-mind-landing-page`, **not part of
  this repo's PRs**), and goes live only after a release moves `master`.
  Until then the documented URL is the raw GitHub one.

### D4. "Daemon is older than this app" notice with a copyable command

Extend the existing banner for non-managed connections (remote/url/relay and
service-managed local) with the exact command in a copy-to-clipboard code
block. Which command depends on the remote daemon version: a daemon older than
the first release that contains `smind update` does not have the command, so
the banner shows the installer one-liner for the daemon's OS family when
known/guessable (both `curl … | sh` and `irm … | iex`, since the app cannot
know the remote OS) and `smind update` once the daemon reports a version that
has it; a Docker daemon is told `docker pull`. **For relay connections the
version must come from a new additive WS RPC `daemon.info`** (open decision
14), called by the UI over its normal `/ws`, because `/healthz` is unreachable
through `relay://`. A relay daemon too old to answer `daemon.info` shows "version
unknown" with the installer commands, never a false "up to date".

### D5. Docker image

`ghcr.io/spacingmind/smind`, `linux/amd64` + `linux/arm64`, built by a new job
in `release-please.yml` that runs after `publish` and **reuses the release's
own linux binaries** (the Dockerfile `COPY`s the per-arch binary from the
build artifacts via `TARGETARCH`; no Go build inside Docker, no QEMU
compilation, byte-identical to the checksummed release). Tags: `<version>`,
`<major>.<minor>`, `latest` — stable releases only; on `workflow_dispatch`
dry runs the image is built but not pushed.

- Base `debian:bookworm-slim` (glibc, so agent CLIs and Node run). Contents:
  `smind`, `git`, `openssh-client`, `ca-certificates`, `curl` (for the
  healthcheck), Node 22 LTS (GLM's `npx`), `tini` as PID 1 (zombie reaping for
  agent child processes).
- Non-root user `smind` (uid/gid 10001), `HOME=/home/smind`, `SMIND_HOME=/data`,
  `VOLUME /data`; repositories are expected under a second mount
  (`/workspaces`); stored paths are container paths. Run with `--user
  "$(id -u):$(id -g)"` for bind mounts.
- `HEALTHCHECK curl -fsS http://127.0.0.1:4648/healthz` (works because it runs
  in the container's own network namespace).
- **Honest limits, stated in the docs:**
  1. Agent CLIs (`claude`, `codex`) are **not** baked in: third-party
     binaries with their own licences, versions and credentials. The docs give
     a `FROM ghcr.io/spacingmind/smind:<v>` + `npm i -g …` example (open
     decision 8). GLM works out of the box except it downloads its agent with
     `npx` on first use (needs network).
  2. The daemon listens on the container's loopback, so **do not publish
     4648**. Reach it with the relay (`docker exec … smind relay connect …`;
     the config persists in `/data`, the container dials out and needs no
     inbound port), or `--network host` on Linux, or `docker exec` for the CLI.
  3. Browser OAuth (`smind account login`) wants a localhost callback and is
     awkward in a container; use `smind account add` (credential on stdin) or a
     mounted agent-CLI credential directory. To be verified in the Docker PR;
     the docs will say exactly what was tested.
  4. `smind update` inside a container is refused (container detection); update
     by pulling a new image tag.

### D6. Relay pairing for desktop clients

- `smind relay offer` prints the URL (stdout, unchanged for scripts) and, with
  `--qr` (default on when stdout is a TTY), a terminal QR; `--png <file>`
  writes a PNG with mode 0600. QR encoding falls back from level M to level L
  when M does not fit, and if the offer still does not fit it prints the URL
  and a one-line "too long for a QR code" note instead of failing the
  command. Wording becomes "for a mobile device or the desktop app". The QR is
  convenience for a future mobile scanner; **the paste-able URL is what works
  today for both clients.**
- Offer semantics (open decision 11): reusable, non-expiring, equivalent every
  time, because it *is* the workspace secret and the relay has no per-device
  credential; the CLI says so ("treat this like a password; to revoke, enrol a
  new workspace"). Expiry or one-use offers would be cosmetic without
  relay-side per-device admission and are deferred to that ADR.
- Desktop UI: Settings → Connections gains **Add via relay link**: paste →
  Rust parses (new `connections_preview_relay` returns only non-secret
  fields: relay host:port, workspace id, a grouped daemon-key fingerprint,
  whether the relay is fingerprint-pinned) → the user confirms those values
  (Paseo's host-confirmation pattern) → `connections_add_relay`. The pasted
  string is held in component state only until submit and never logged. The
  existing URL form rejects a string that contains `#offer=` and points to the
  new action (fixing the footgun above). E2EE and fingerprint pinning are
  unchanged (ADR-0007/0011); no key material enters the webview beyond what
  the user pasted.
- Deep link `smind://pair#offer=…`: not in v1 (open decision 13); when added it
  must open the same confirmation screen and never add a connection from a
  click alone, because a web page can trigger `smind://` and an attacker-chosen
  relay would otherwise become a trusted daemon.
- Multi-device (open decision 12): the quickstart and the UI warn that one
  device per workspace can be attached at a time. Lifting that is a separate
  ADR + plan (daemon `DeviceAttach`, relay fan-out, Rust and TS clients).
- Homelab quickstart `docs/homelab.md` (relay on a VPS or on the homelab with
  `7400/tcp` and `7401/tcp` open — the desktop derives the native port as
  grpc-web − 1, `client.rs:45-68`, so the two must be adjacent; compare the
  fingerprint printed by `smind relay` with the one `relay connect` pins,
  because pinning is trust-on-first-use).

### D7. Desktop first-run and connection onboarding (macOS + Windows)

**Settings model.** A small Rust-owned `app-settings.json` next to
`connections.json` (not inside the registry, so the registry format and its
non-removable `local` entry stay as they are):
`{ onboardingCompleted: bool, manageLocalDaemon: bool }`.
`manageLocalDaemon = false` is the **"no local daemon" mode**: the app never
installs, starts, updates or takes over a daemon, `auto_start` is a no-op and
the Settings → Daemon actions that mutate are disabled with an explanation. The
`local` entry stays in the list as a plain "This computer (127.0.0.1:4648)"
connection that works if anything answers there (that is also how (d) below
connects to a CLI/service daemon without managing it).

**Upgrade rule.** Existing installs never see first-run: when
`app-settings.json` is absent but `connections.json` or the managed-daemon
record exists, the app writes `{onboardingCompleted: true, manageLocalDaemon:
true}` (preserving today's macOS auto-start) and shows nothing.

**First-run screen.** Shown by the bundled UI before the workspace UI when
`onboardingCompleted == false`, full-window, theme-aware, with a "Skip for now"
that leaves first-run pending (the normal unreachable state appears, with a
"Finish setup" entry in Settings; nothing is installed or started while it is
pending; open decision 16). Four choices:

- **(a) Run smind on this computer** — calls the existing install path
  (`desktop.daemonInstall()`, `daemon_install`; whatever the Windows
  bundled-daemon branch makes of it), sets `manageLocalDaemon = true`, selects
  `local`, waits for `/healthz`. macOS/Windows only; on Linux (deb,
  `Platform::Unsupported`) the choice is replaced by guidance to run
  `smind service install`.
- **(b) Connect to a daemon** — URL form (validated, then probed through a new
  `connections_probe(url)` that returns `{reachable, version, service}` so the
  screen can show "reachable, v0.11.1" before saving). **There is no token
  field**: the proxy reads the token from the remote's `/api/token`; because
  daemons bind loopback only, the screen says so and shows the two working
  ways to reach one (SSH `-L`, Tailscale serve) plus (c) (open decision 18).
- **(c) Pair via relay link** — the same paste → confirm → add component as
  item D6 (shared component, one implementation).
- **(d) Use the daemon already running here** — shown first and pre-selected
  when `daemon_status` finds something answering on the local port that the app
  does not manage. The screen names what it can ("smind v0.11.1, managed by
  systemd" when `managedBy` is present, "started outside the app" otherwise,
  "something that is not smind" when `/healthz` does not identify smind) and
  offers **Connect** only: `manageLocalDaemon = false`, never install, never
  take over. If the port is held by a non-smind or non-HTTP process, (a) is
  disabled with the port-conflict explanation (the app does not choose the
  port; ADR-0030).

Pre-selection: nothing on the port → (a); a smind daemon answering → (d);
otherwise none. macOS **no longer installs silently on first launch**: that was
the old default, now replaced by one explicit click (open decision 16).

**After first-run (Settings → Connections / Daemon).** One "Add connection"
menu offers (b)/(c) (same components as first-run); the list keeps
add/switch/remove with multiple connections; a **Local daemon** row has a
"Manage this computer's daemon" toggle (`manageLocalDaemon`, with a confirm
before turning it off while the app owns a running daemon — Paseo's pattern;
turning it off never stops a daemon without asking). Starting up: when
`manageLocalDaemon` is true and `local` is the selected connection,
`auto_start` runs exactly as today (macOS; Windows when the bundled-daemon
change enables it) on every launch (open decision 17); in every other case it
does nothing. The "default connection" is the last selected one, as today
(open decision 19). A "Run setup again" command in the Help/app menu reopens
first-run without touching saved connections.

**Compatibility with the Windows bundled-daemon work.** This ADR only gates
*whether* the app installs/starts a daemon; the *how* stays in
`daemon_manager.rs`'s install/update path, called by (a) through the existing
`daemon_install` command. No new install code is added here.

## Alternatives considered

- **`smind update` via the package manager / a separate `smind-updater`.** More
  moving parts per OS; the daemon already has everything needed (download,
  checksum, atomic rename). Not chosen.
- **Replacing the binary while it runs and `exec`-ing into it.** Not portable
  to Windows and harder to roll back. Not chosen.
- **A Windows Service (via `x/sys/windows/svc`).** Runs as LocalSystem or a
  service account, so `SMIND_HOME`, agent logins, Git for Windows and `PATH`
  would not be the user's; needs elevation to install; the secret ACLs
  (ADR-0029 D3: current user + SYSTEM) and the per-user token model assume the
  user's profile; ADR-0030 already rejected "a Windows Service or Task
  Scheduler entry" for the *app* on exactly these costs. A per-user logon task
  keeps one security model. Its drawback (starts only after login) is stated.
  Open decision 2.
- **System-level systemd unit by default.** Starts without login but needs
  root, a dedicated user without the operator's agent logins, and `HOME`
  mapping. The user unit + linger covers the same boot case with the right
  identity. Open decision 3.
- **Building the Docker image from source inside Docker.** Slow multi-arch
  builds under QEMU and a binary that differs from the checksummed release.
  Not chosen.
- **Baking agent CLIs into the image.** Version churn and redistribution
  questions outweigh the convenience; a documented `FROM` extension is cheaper
  to keep honest. Open decision 8.
- **Adding `server.bind`/non-loopback listening to make Docker `-p` work.**
  Would expose the unauthenticated `/api/token`; needs its own ADR (token
  gating). Not in this plan. Open decision 7.
- **Silently auto-installing on first launch everywhere (status quo on
  macOS).** Removes the user's choice to run no local daemon or use an existing
  one. Not chosen (open decision 16).
- **Storing `manageLocalDaemon` inside `connections.json`.** Couples a
  behaviour flag to the connection registry whose load path deliberately
  tolerates and rewrites corrupt files (`connections.rs:216-248`). Separate
  file chosen.
- **QR-only pairing for desktop.** A desktop app has no scanner worth building;
  paste is the primary path.

## Open decisions for Human

Each has a recommended default; the plan assumes the recommendation.

1. **CLI signature verification.** Verify a minisign signature of
   `checksums.txt` in `smind update` using the existing desktop-updater public
   key (ADR-0023; no new secret; one rotation story), signed by a new
   `publish`-job step, as a **separate PR after** the unsigned core lands.
   *Recommended: yes.* Install scripts stay checksum-only (optionally verifying
   when `minisign` is on `PATH`). Alternatives: a dedicated release key; no
   signature (checksum parity with `install.sh`, which only detects
   corruption).
2. **Windows: Scheduled Task vs Windows Service.** *Recommended: per-user
   Scheduled Task at logon* (rationale above); a real service stays a later
   option.
3. **Linux/macOS service scope default.** *Recommended: user unit / LaunchAgent
   by default*, `--system` opt-in on Linux (runs as the invoking user, never
   root), no LaunchDaemon in v1.
4. **Update refusal policy.** *Recommended:* refuse while any run is running or
   a permission request is pending; `--force` overrides; no `--wait` in v1;
   stable channel only; downgrade needs `--force`.
5. **Restart after update.** *Recommended:* restart only a service-managed
   daemon; for a manually started daemon print instructions, never respawn it.
6. **Supervisor signal.** *Recommended:* additive `/healthz` field `managedBy`
   from env `SMIND_SUPERVISOR`, so the app can recognise service-managed
   daemons and hide Take over.
7. **Port and bind.** *Recommended:* no `--port`/`--bind` flags (port stays in
   `config.yaml`, daemon stays loopback-only); remote access is relay or
   tunnel; a non-loopback bind needs its own ADR that gates `/api/token`.
8. **Docker image contents.** *Recommended:* one image (`debian:bookworm-slim`,
   git, ssh client, Node 22, tini, curl), no agent CLIs baked, documented
   extension; a `-agents` variant only if Human wants one later.
9. **Image provenance.** *Recommended:* buildx provenance/SBOM attestations on;
   no cosign in v1.
10. **`install.ps1` location.** *Recommended:* `%LOCALAPPDATA%\Programs\smind`
    + user `PATH` (never the app's managed directory); landing redirect as a
    follow-up PR in the landing repo.
11. **Offer reuse/expiry.** *Recommended:* reusable, no expiry, loud
    "treat like a password" wording, `--png` at 0600; per-device admission is a
    future ADR.
12. **Multi-device attach.** *Recommended:* ship pairing now with a documented
    and UI-visible "one device at a time" limit; schedule multi-device attach
    as its own ADR + L/XL plan. Alternative: make it a prerequisite (delays
    everything in D6 by a full cross-language project).
13. **Deep link `smind://pair`.** *Recommended:* paste-only in v1; deep link
    later with the confirmation screen, never auto-add.
14. **`daemon.info` RPC.** *Recommended:* add a small additive WS method
    returning `{version, commit}` so the notice works for relay connections.
15. **QR default.** *Recommended:* print the QR only when stdout is a TTY (or
    `--qr`), fall back to level L, never fail the command on an oversize offer.
16. **First-run default and silent install.** *Recommended:* first-run screen
    on fresh installs, no silent install (macOS changes from silent to one
    click); (a) pre-selected when the port is free, (d) when a smind daemon is
    already answering; "Skip for now" leaves first-run pending.
17. **Local daemon on later launches.** *Recommended:* when `manageLocalDaemon`
    is true and `local` is selected, auto-start on every launch (today's macOS
    rule); otherwise never.
18. **Token entry UX.** *Recommended:* no token field in v1 (the daemon serves
    its token on loopback and URL connections already fetch it); add an
    "Advanced: token" field only when a future ADR gates `/api/token`.
19. **Default connection.** *Recommended:* keep "last selected" (today's
    `selectedId`); a pinned default is a later nicety.

## Consequences

- Seven shippable PRs (plan), most independent; the riskiest are `smind
  update` (replaces an executable), the Docker release job (publishes to a
  registry) and the first-run flow (changes what happens on first launch).
- Two small additive daemon API changes are proposed (`/healthz managedBy`,
  `daemon.info`); both are public API shape and wait on decisions 6 and 14.
- The macOS silent-install default changes (decision 16); existing users are
  unaffected by the upgrade rule.
- Relay pairing for desktop is honest about its limits instead of hiding them:
  one device at a time, offers are passwords, QR is not scannable yet.
- A macOS Take-over footgun and the "paste pairing link into the URL form"
  footgun are fixed as part of the work.

## Cross-references

- ADR-0007, ADR-0011 (relay, admission, per-device identity deferred),
  ADR-0013 (desktop bundled UI, remote daemons never updated by the app),
  ADR-0023 (updater keys), ADR-0029 (`/api/shutdown`), ADR-0030 (Windows native
  daemon; rejected a service for the app).
- `docs/plans/completed/desktop-relay-transport.md` (single-device limit),
  `docs/plans/active/desktop-windows-native-daemon.md` and the
  `feat/windows-bundled-daemon` branch (install path (a) calls).
- Plan: [`docs/plans/active/homelab-cli.md`](../plans/active/homelab-cli.md).
