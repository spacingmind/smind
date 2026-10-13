# Homelab CLI: update, service, install scripts, Docker, relay pairing and desktop first-run

Implements [ADR-0031](../../decisions/0031-homelab-cli-distribution.md)
(**Proposed**). Every PR below that depends on an open decision says so; the
plan is written against the ADR's recommended defaults, and a PR must not start
before its blocking decisions are answered (AGENTS.md rule d).

Size (whole plan): **XL (10)** — scope 2, PRs 2, risk 2, decisions 2,
verification 2 → split into ten PRs, each sized below (WORKSPACE_PROTOCOL.md).
All PRs branch from `origin/develop`, none are stacked; where two PRs share a
file they are sequenced, never parallel.

## PR split, write scopes, order

| PR | Item | Write scope | Size | Blocking decisions | Depends on |
|---|---|---|---|---|---|
| **P1** install scripts | 3 | `scripts/install.sh`, `scripts/install.ps1`, `scripts/install_test.sh` (new), `README.md` install/update sections | **M (4)** — scope 1, PRs 0, risk 1, decisions 0, verify 2 (real Windows + Linux) | 10 (path) | — |
| **P2** relay offer QR + quickstart | 6 | `cmd/smind/relay.go` (+ test), `internal/relay/pairing/qr.go` (L fallback, + test), `docs/homelab.md` (new) | **S (2)** — scope 0, PRs 0, risk 1, decisions 1, verify 0 | 11, 15 | — |
| **P3** desktop "Add via relay link" | 6 | `web/packages/ui/src/lib/platform.ts`, `…/settings/connections-section.tsx` (+ new `add-relay-link.tsx`), `desktop/src-tauri/src/commands.rs` (+ `lib.rs`, `build.rs`, `capabilities/proxy.json`, capability test) for `connections_preview_relay` | **M (4)** — scope 2, PRs 0, risk 1, decisions 0, verify 1 | 11, 12, 13 | — |
| **P4** first-run + connection onboarding | 7 | `desktop/src-tauri/src/{app_settings.rs (new),daemon_manager.rs (gating + take-over fix),commands.rs,lib.rs,build.rs}`, `desktop/daemon-client/src/proxy/*` (probe), `desktop/src-tauri/capabilities/proxy.json`, `web/packages/ui/src/{lib/platform.ts,components/first-run/*,settings/{connections,daemon}-section.tsx,App.tsx}` | **L (7)** — scope 2, PRs 1, risk 1, decisions 1, verify 2 (real macOS + Windows app, light+dark screenshots) | 16, 17, 18, 19 | **P3** (shared relay-link component, same files), and the Windows bundled-daemon PR (`feat/windows-bundled-daemon`, touches `daemon_manager.rs` install path) merged first |
| **P5** `smind update` (checksum) | 1 | `cmd/smind/update.go` (+ test), `internal/selfupdate/*` (new), `cmd/smind/main.go` (dispatch + usage) | **L (7)** — scope 1, PRs 1, risk 2, decisions 1, verify 2 (real Windows rename-aside, real release) | 4, 5 | — |
| **P6** release signing + CLI verification | 1 | `.github/workflows/release-please.yml` (`publish` job), `internal/selfupdate/verify.go` (+ test), embedded pubkey | **L (6)** — scope 1, PRs 0, risk 2, decisions 1, verify 2 (needs a real release / signed dry run) | 1 | **P5** |
| **P7** `smind service` (systemd, launchd) + `managedBy` | 2 | `cmd/smind/service.go` (+ test), `internal/service/*` (new, linux+darwin backends + golden units), `internal/server/server.go` (`/healthz managedBy`), `cmd/smind/serve.go` (read `SMIND_SUPERVISOR`), `scripts/install.sh` (`--service`), `docs/homelab.md` section | **L (6)** — scope 1, PRs 1, risk 1, decisions 1, verify 2 (real systemd + launchd) | 3, 6, 7 | **P1** (owns `install.sh` before this), P2 (docs file) |
| **P8** `smind service` Windows + `install.ps1 -Service` | 2 | `internal/service/windows*.go`, `scripts/install.ps1` (`-Service`), `docs/homelab.md` section | **M (5)** — scope 1, PRs 0, risk 1, decisions 1, verify 2 (real Windows 11) | 2 | **P7** (interface), P1 |
| **P9** "older than app" notice + service recognition | 4 | `web/packages/ui/src/components/desktop-daemon-banner.tsx`, `…/settings/daemon-section.tsx`, `web/packages/ui/src/lib/platform.ts`, `desktop/src-tauri/src/daemon_manager.rs` (unmanaged kind), `internal/wsapi/handlers.go` (`daemon.info`) | **L (6)** — scope 2, PRs 0, risk 1, decisions 1, verify 2 (screenshots + real relay daemon) | 6, 14 | **P4** (same web/desktop files), **P5** (command exists), **P7** (`managedBy`) |
| **P10** Docker image | 5 | `Dockerfile`, `.dockerignore`, `.github/workflows/release-please.yml` (new `docker` job), `docs/homelab.md` section | **L (6)** — scope 1, PRs 0, risk 2, decisions 1, verify 2 (real `docker buildx`, multi-arch, run + healthcheck) | 7, 8, 9 | **P6** (both edit `release-please.yml`; one owner at a time) |

**Suggested order**

1. *Wave 1 (parallel, disjoint scopes):* **P1**, **P2**, **P3** — the quick wins
   (installers, `relay offer` QR, "Add via relay link").
2. *Wave 2 (parallel):* **P4** (after P3 and the Windows bundled-daemon PR) and
   **P5**.
3. *Wave 3:* **P6**, **P7** (parallel; disjoint except neither touches the other's files).
4. *Wave 4:* **P8**, **P9** (P9 after P4, P5, P7).
5. *Wave 5:* **P10**.

Not in this plan (needs its own ADR + plan, decision 12): multi-device relay
attach. Not in this plan (landing repo, listed so it is not forgotten):
`/install.ps1` redirect in `spacingmind/spacing-mind-landing-page`
`apps/web/next.config.ts` `redirects()`, added after P1 is in a release.

## Acceptance Criteria

### P1 — install scripts

1. `install.sh --version 0.11.1` installs that release; `SMIND_VERSION` still
   works; if both are given the flag wins; `--help` prints usage.
2. On `MINGW*|MSYS*|CYGWIN*` the script exits non-zero with the PowerShell
   one-liner, not "no native Windows build yet"; the header comment and
   `README.md` no longer claim there is no native Windows daemon or that the
   app uses WSL2.
3. Checksum verification is unchanged and still mandatory (a tampered archive
   exits non-zero and installs nothing).
4. `install.ps1` resolves the OS architecture (amd64/arm64, including an x64
   PowerShell on an ARM64 OS → arm64), downloads
   `smind_<v>_windows_<arch>.zip` + `checksums.txt`, verifies SHA-256, extracts
   only `smind.exe`, installs to `%LOCALAPPDATA%\Programs\smind\`, adds it to
   the user `PATH` once (idempotent), and never writes under
   `%LOCALAPPDATA%\smind\`.
5. `install.ps1` honours `SMIND_VERSION` / `SMIND_INSTALL_DIR`, enforces TLS
   1.2 on Windows PowerShell 5.1, warns (does not fail) when Git for Windows is
   missing, and leaves a previous install untouched on any failure.
6. README documents both one-liners and states that `install.ps1` is served from
   spacingmind.com only after the landing redirect exists (raw GitHub URL
   until then).

### P2 — `smind relay offer` + quickstart

1. `smind relay offer` prints the pairing URL on stdout exactly as today (a
   script capturing stdout still gets only the URL); with a TTY on stdout or
   `--qr` it also prints a terminal QR (to stderr, so `$(smind relay offer)`
   stays clean).
2. `--png <file>` writes a PNG QR, file mode `0600`, refuses to overwrite an
   existing file without `--force`.
3. A 432-byte offer (the realistic example in ADR-0031) renders: QR encoding
   tries level M and falls back to level L; an offer that fits neither prints
   the URL plus "too long for a QR code" and exits 0.
4. Help/usage text and `relay connect`'s closing message say "for a mobile
   device or the desktop app" and "treat this URL like a password: anyone with
   it can reach this daemon through the relay; to revoke, enrol a new workspace
   and run `smind relay connect` again".
5. `docs/homelab.md` quickstart (relay on a VPS, relay on the homelab,
   enrol → connect → offer → pair desktop → pair mobile) names the required
   open ports (`7400/tcp` native + `7401/tcp` grpc-web, adjacent), the
   fingerprint check (printed by `smind relay`, pinned by `relay connect` on
   first use), the one-device-at-a-time limit, the SSH-tunnel alternative, and
   that smind listens on loopback only.

### P3 — desktop "Add via relay link"

1. Settings → Connections has **Add via relay link** (desktop build only):
   paste → preview → confirm → connection appears in the list and can be
   selected; the pasted string is cleared on success, on failure and on
   unmount, never logged, and the input does not autocomplete.
2. `connections_preview_relay(pairing_url)` returns only non-secret fields
   (relay host:port, workspace id, grouped daemon-key fingerprint, `pinned:
   bool`); no field contains the admission secret or key bytes. It is added to
   `build.rs`, `capabilities/proxy.json` and the capability allowlist test.
3. The confirmation screen shows those values and requires an explicit
   **Add**; Cancel adds nothing and saves nothing.
4. The existing "Add a connection" URL form rejects any input containing
   `#offer=` (or whose fragment parses as an offer) with a message pointing to
   "Add via relay link", and never saves a connection for it.
5. A malformed link, a version-mismatched offer and an offer with no
   fingerprint each show the Rust error text inline (`role="alert"`) and add
   nothing.
6. Light and dark screenshots of the section, the preview and an error state
   were looked at on the real desktop app (feedback_ui_clean_screenshot_verify).

### P4 — first-run and connection onboarding (macOS + Windows)

1. **Settings file.** `app-settings.json` (`onboardingCompleted`,
   `manageLocalDaemon`) is read/written by two new commands
   (`app_settings_get`, `app_settings_set`), both on the capability allowlist;
   a corrupt file falls back to "first run" without crashing the app.
2. **Upgrade rule.** Absent settings + existing `connections.json` or managed
   daemon record → settings are written as `{true, true}` and no first-run
   appears; macOS auto-start behaves exactly as v0.11.1.
3. **No silent install.** On a fresh install (no settings, no connections, no
   managed record), nothing is installed or started until the user makes a
   choice; `auto_start` is a no-op while `onboardingCompleted == false`.
4. **Screen.** The first-run screen shows (a) *Run smind on this computer*, (b)
   *Connect to a daemon*, (c) *Pair via relay link*, and (d) *Use the daemon
   already running here* when one is detected; "Skip for now" leaves first-run
   pending and installs nothing. On Linux (deb) (a) is replaced by guidance to
   run `smind service install`.
5. **Pre-selection.** Port free → (a) selected; a smind daemon answering and
   not managed by the app → (d) selected and (a) disabled with the reason;
   anything else answering or the port occupied → no pre-selection, (a)
   disabled with a port-conflict message.
6. **(a)** calls the same `daemon_install` path as Settings → Daemon → Install
   (no new install code), then sets `manageLocalDaemon=true`, selects `local`,
   and enters the app only after `/healthz` answers; an install error stays on
   the screen with Retry and the other choices still available.
7. **(b)** validates the URL, probes it through `connections_probe(url)`
   (reachable? version? `service == "smind"`?) and shows the result before
   saving; there is no token field; the screen states that smind daemons listen
   on loopback only and shows the SSH `-L` / Tailscale / relay alternatives;
   adding a non-smind endpoint is refused with an explanation.
8. **(c)** uses P3's component unchanged (no second implementation).
9. **(d)** connects to the local daemon with `manageLocalDaemon=false`; the app
   never installs, starts, updates or takes over afterwards; Settings → Daemon
   shows read-only status and an "Enable app-managed daemon" action.
10. **No local daemon mode.** With `manageLocalDaemon=false`, `auto_start`
    never runs, the Daemon section's mutating actions are disabled with an
    explanation, and switching to `local` never installs anything; the app is
    fully usable with only remote/relay connections.
11. **Settings → Connections** keeps add/switch/remove with multiple
    connections, gains a single **Add connection** menu (URL / relay link, same
    components as first-run) and a **Manage this computer's daemon** toggle
    with a confirmation before turning it off while the app owns a running
    daemon (turning it off does not stop the daemon unless the user also
    chooses to). **Run setup again** is reachable from the Help/app menu and
    changes no saved connection.
12. **Take-over safeguard (bug fix).** On macOS, Take over management is refused
    unless the port owner's image is a `smind` executable (parity with
    Windows); an `ssh` tunnel owner is never adopted.
13. **Later launches.** `manageLocalDaemon=true` and `local` selected →
    `auto_start` runs as today on every launch; otherwise nothing starts.
14. Light and dark screenshots of the first-run screen (each pre-selection
    state) and of Settings → Connections were looked at on the real macOS app
    and on Windows 11; Windows verified with whatever install path the bundled
    daemon branch has merged by then.

### P5 — `smind update`

1. `smind update --check` prints current and latest and exits 0 whether or not
   an update exists; `--json` emits `{current, latest, updateAvailable}`.
2. `smind update` downloads `smind_<v>_<os>_<arch>.tar.gz|zip` and
   `checksums.txt`, verifies SHA-256, extracts only the `smind`/`smind.exe`
   member, runs the new binary's `version` and compares it to the target before
   swapping; any failure leaves the installed binary byte-identical.
3. Refuses (non-zero, one-line reason, nothing changed): runs are running or a
   permission is pending (override `--force`); current version is `dev`; the
   running binary is inside the desktop app bundle / managed-daemon directory;
   target is older than current without `--force`; the target directory is not
   writable (message names the cause; no self-elevation); running inside a
   container.
4. Active runs are detected via the local daemon's `run.list`; a daemon that
   is not running is treated as "nothing to protect".
5. Unix swap is atomic (`rename` over the target, `smind.old` kept until the
   post-swap check passes); Windows renames the running `smind.exe` aside to
   `smind.exe.old`, moves the new file in, and removes `.old` on the next
   `smind` start or update; a failed post-swap check rolls back.
6. When a service-managed daemon exists, the update restarts it through
   `smind service restart` and succeeds only after `/healthz` reports the new
   version within 30 s, otherwise rolls back and restarts the old binary;
   a manually started daemon is never respawned (instructions printed).
   (Service restart is exercised through an injected restarter until P7 lands.)
7. `--version <v>` installs that release (downgrade needs `--force`);
   `--dry-run` performs everything except the swap; `SMIND_UPDATE_BASE_URL`
   (test seam) redirects downloads and is documented as test-only.
8. Latest-version resolution uses the `releases/latest` redirect, not the
   GitHub API, and honours `HTTPS_PROXY`.

### P6 — release signing + CLI verification

1. The `publish` job signs `checksums.txt` with the existing minisign key
   (`TAURI_SIGNING_PRIVATE_KEY`) and uploads `checksums.txt.minisig`; on a real
   release a missing key fails the job (same rule as ADR-0023 §4), on a dry run
   it is skipped with a warning.
2. `smind update` downloads the `.minisig` and verifies it against the
   embedded desktop-updater public key before trusting `checksums.txt`; a bad
   or missing signature aborts with nothing changed; there is no flag to skip
   it.
3. The embedded key equals `plugins.updater.pubkey` in `tauri.conf.json`
   (a test fails if they diverge).
4. Releases published before P6 are not updatable *to* by a P6 binary
   (documented): the verifier reports "release has no signature" rather than
   falling back to checksum-only.

### P7 — `smind service` (systemd, launchd) and `managedBy`

1. `smind service install|uninstall|status|start|stop|restart|logs` exist
   with the flags in ADR-0031 D2 (`--system`, `--home`, `--path`, `--no-start`,
   `-f/-n` for logs); there is no `--port`.
2. Linux: user unit by default; rendered unit contains `ExecStart=<abs>
   serve`, `Restart=on-failure`, `RestartSec=5`, `TimeoutStopSec=30`, the
   captured `PATH`, `SMIND_HOME` only when set, `SMIND_SUPERVISOR=systemd`, no
   sandboxing directives; `--system` writes `User=<SUDO_USER>` and refuses to
   run as plain root without `SUDO_USER`; install prints the linger command when
   lingering is off.
3. macOS: LaunchAgent plist has `RunAtLoad`, `KeepAlive={SuccessfulExit:false}`,
   `EnvironmentVariables` with the captured `PATH` and `SMIND_SUPERVISOR=launchd`,
   stdout/stderr to `~/Library/Logs/smind/smind.log`; `smind stop` leaves the
   daemon stopped, `kill -9` restarts it.
4. `install` is idempotent; refuses to *start* (but still enables) when a
   foreign daemon answers on the port; never kills a process; `uninstall` never
   touches `SMIND_HOME`.
5. `status` output and exit codes (0 running / 3 stopped / 4 not installed)
   match the ADR; it reports daemon version vs binary version.
6. `/healthz` gains `managedBy` (absent when `SMIND_SUPERVISOR` is unset);
   existing fields are unchanged.
7. `install.sh --service` installs then runs `smind service install`
   (`--system` passes through); it fails clearly on an OS without a backend.
8. Log rotation at install/start/restart for the macOS file backend (> 10 MiB
   → `.1`).
9. `docs/homelab.md` documents systemd user vs system, linger, macOS
   LaunchAgent limits (needs a logged-in GUI session), where logs live, and how
   the desktop app treats a service-managed daemon.

### P8 — `smind service` Windows + `install.ps1 -Service`

1. A per-user Scheduled Task `smind` runs `smind.exe serve` at logon, hidden
   (no console window), restarts on failure (3× at 1-minute intervals), output
   appended to `%LOCALAPPDATA%\smind\smind.log` with the same rotation rule.
2. `start`/`stop`/`restart`/`status`/`logs`/`uninstall` behave as in P7; `stop`
   uses `POST /api/shutdown` (ADR-0029) first and `schtasks /End` only if the
   daemon does not exit within the stop timeout.
3. Registration does not require elevation (verified on Windows 11); if it
   does on some setups the error says exactly which step needs it.
4. `SMIND_SUPERVISOR=task` is set for the task; the headless wrapper works on
   Windows 10 and 11 (spike result recorded in Validation, with the fallback
   used if `conhost --headless` is unavailable).
5. `install.ps1 -Service` installs then registers the task.
6. Docs state plainly that the task starts at logon, not at boot.

### P9 — "older than app" notice, service recognition, `daemon.info`

1. For a non-managed connection (url, relay, or service-managed local) whose
   daemon is older than the app, the banner shows the update command in a code
   block with a **Copy** button: `smind update` when the daemon's version is
   at least the first release that has the command, otherwise the installer
   one-liners (`curl -fsSL https://spacingmind.com/install.sh | sh` and
   `irm https://spacingmind.com/install.ps1 | iex`).
2. Copy puts exactly the command on the clipboard and the button confirms
   ("Copied") without moving focus.
3. `daemon.info` (WS) returns `{version, commit}`; the UI reads it over `/ws`
   for relay connections, so the notice shows for relay daemons; a relay daemon
   that does not know the method shows "version unknown" plus the installer
   commands and never claims "up to date".
4. A managed local daemon keeps its **Update & restart** button and does not
   show the copy block (no regression to the existing banner tests).
5. A service-managed local daemon (`/healthz managedBy` present) shows "managed
   by <systemd|launchd|Task Scheduler>", hides **Take over management**, and
   (with P4's setting) never offers (a) while the service owns the port.
6. Light/dark screenshots of the banner in all four states were looked at.

### P10 — Docker image

1. A `docker` job in `release-please.yml` runs after `publish` on real
   releases, builds `linux/amd64` + `linux/arm64` from the release's own linux
   binaries (no Go build in Docker), pushes `ghcr.io/spacingmind/smind:<version>`,
   `:<major>.<minor>`, `:latest`, with provenance/SBOM attestations; on
   `workflow_dispatch` it builds without pushing.
2. The image runs as uid 10001, `HOME=/home/smind`, `SMIND_HOME=/data` with
   `VOLUME /data`, `tini` as entrypoint, `CMD ["smind","serve"]`, and contains
   `git`, `openssh-client`, `ca-certificates`, `curl`, Node 22.
3. `HEALTHCHECK` on `http://127.0.0.1:4648/healthz` turns `healthy` within
   30 s of start.
4. State persists: stop/remove/recreate the container with the same `/data`
   volume and workspaces, tokens, accounts and the relay config are still there.
5. `docker exec <c> smind relay connect …` persists the relay config in `/data`
   and a restarted container dials the relay again (no inbound port published).
6. `smind update` inside the image refuses with "update by pulling a new image".
7. `docs/homelab.md` Docker section states the limits from ADR-0031 D5
   (loopback bind → don't publish 4648; agent CLIs not included + the `FROM`
   extension example that was actually built; browser OAuth caveat as tested;
   bind-mount `--user`).

## Test Scenarios

Names are the exact test/case names the implementer must create (Go `TestX`,
shell `test_x`, TS `it("…")`, Rust `fn x`). A scenario marked **(real)** needs a
real platform and is recorded in Validation, not in CI.

### P1

- `test_version_flag_wins_over_env` — `SMIND_VERSION=0.1.0 install.sh --version 0.11.1` fetches 0.11.1 (local HTTP fixture via a curl shim).
- `test_help_prints_usage`
- `test_msys_prints_powershell_oneliner` — fake `uname -s` = `MINGW64_NT`.
- `test_checksum_mismatch_installs_nothing`
- `test_unknown_flag_exits_2`
- Pester (or PowerShell script) `install.ps1`:
  `Select-Arch_x64PowerShellOnArm64Os_ReturnsArm64`, `Install_ChecksumMismatch_LeavesExistingExe`,
  `Install_ZipWithPathTraversal_Rejected`, `Install_AddsUserPathOnce`,
  `Install_NeverTouchesAppManagedDir`, `Install_WarnsWhenGitMissing`.
- **(real)** run `install.sh` on Linux + macOS and `install.ps1` on Windows 11 against a real release; `smind --version` prints the version.

### P2

- `TestRelayOfferURLOnlyOnStdout` — stdout is exactly the URL + newline (golden), QR goes to stderr.
- `TestRelayOfferQRFallsBackToLevelL` — the 432-byte offer encodes (level L); asserted by decoding with the existing `qr_decode_test.go` helper.
- `TestRelayOfferTooLongPrintsNoteAndExitsZero`
- `TestRelayOfferPNGMode0600AndNoOverwrite`
- `TestRelayOfferWording` — usage/help contain "desktop app" and "treat this URL like a password".
- `TestQRTextLevelFallbackBoundary` in `internal/relay/pairing`: 412 and 413-byte inputs.

### P3

- UI (`connections-section.test.tsx`, `add-relay-link.test.tsx`):
  `it("previews a pasted relay link and shows host, workspace and fingerprint without the secret")`,
  `it("adds the connection only after Add is pressed")`,
  `it("Cancel adds nothing")`,
  `it("clears the pasted link after success, failure and unmount")`,
  `it("rejects a pairing link pasted into the URL form and points to Add via relay link")`,
  `it("shows the Rust error inline for a malformed link")`,
  `it("is not registered in a non-desktop build")`.
- Rust: `connections_preview_relay_returns_no_secret_fields`, `connections_preview_relay_rejects_wrong_version`, capability test `proxy_capability_lists_preview_relay`.
- **(real)** run a local `smind relay` + `smind serve` + `smind relay connect/offer`, paste into the real desktop app, add, select, see the workspace list; light+dark screenshots.

### P4

- Rust: `app_settings_missing_file_means_first_run`, `app_settings_corrupt_file_falls_back_to_first_run`, `upgrade_rule_existing_connections_marks_onboarded`, `upgrade_rule_existing_managed_record_marks_onboarded`, `auto_start_noop_until_onboarded`, `auto_start_noop_when_manage_local_daemon_false`, `auto_start_runs_when_managed_and_local_selected` (extends the existing `launch_decision` table), `take_over_refused_for_non_smind_owner_on_macos`, `take_over_allowed_for_smind_owner_on_macos`, `connections_probe_reports_reachable_version_and_service`, `connections_probe_rejects_non_smind_healthz`, capability test for the new commands.
- UI (`first-run.test.tsx`):
  `it("preselects Run here when the port is free")`,
  `it("preselects Use the running daemon and disables Run here when a smind daemon answers")`,
  `it("disables Run here with a port-conflict message when something else owns the port")`,
  `it("Skip for now installs nothing and leaves first-run pending")`,
  `it("Run here calls daemonInstall once, waits for healthz, then enters the app")`,
  `it("an install failure keeps the screen and the other choices usable")`,
  `it("Connect by URL probes before saving and shows the version")`,
  `it("refuses a URL that is not a smind daemon")`,
  `it("has no token field and explains the loopback-only constraint")`,
  `it("Pair via relay link uses the shared AddRelayLink component")`,
  `it("Use running daemon sets manageLocalDaemon=false and never calls daemonInstall")`,
  `it("shows guidance instead of Run here on Linux")`.
- UI settings: `it("Manage this computer's daemon toggle confirms before disabling while the app owns a running daemon")`, `it("Run setup again leaves saved connections untouched")`, `it("with manageLocalDaemon=false the daemon section disables install/update/restart/take-over")`.
- **(real, macOS + Windows 11)**: (1) wipe app data → first-run appears, nothing running; (2) choose (a) → daemon up, app usable; (3) quit/relaunch → auto-start per setting; (4) start `smind serve` by hand first → (d) pre-selected, Connect works, Settings shows "not managed", no take-over side effects; (5) `ssh -L 4648:…` tunnel → (d)/refusal path, Take over refused on macOS; (6) manage=false → relaunch, nothing starts; (7) relay link via (c); light+dark screenshots of every state (and every transition frame for the screen's entry/exit, per feedback_ui_transition_frames).

### P5

- `TestUpdateCheckReportsLatestAndExitsZero`
- `TestUpdateVerifiesChecksumAndInstalls` (httptest release fixture via `SMIND_UPDATE_BASE_URL`)
- `TestUpdateChecksumMismatchLeavesBinaryIdentical`
- `TestUpdateRejectsArchiveWithPathTraversal`, `TestUpdateRejectsMissingBinaryMember`, `TestUpdateRejectsOversizeMember`
- `TestUpdateRefusesWhileRunsActive`, `TestUpdateRefusesWhilePermissionPending`, `TestUpdateForceOverridesActiveRuns`
- `TestUpdateNoDaemonRunningIsFine`
- `TestUpdateRefusesDevBuild`, `TestUpdateRefusesDesktopManagedPath`, `TestUpdateRefusesInsideContainer`, `TestUpdateRefusesDowngradeWithoutForce`, `TestUpdateRefusesUnwritableDir`
- `TestUpdateAtomicSwapKeepsOldUntilVerified`, `TestUpdateRollsBackWhenNewBinaryVersionMismatches`
- `TestUpdateWindowsRenameAsideAndCleanup` (build-tagged `windows`, runs in the existing windows CI job)
- `TestUpdateRestartsServiceAndWaitsForHealthzVersion` (fake restarter), `TestUpdateRollsBackWhenHealthzNeverReportsNewVersion`
- `TestUpdateDoesNotRespawnManualDaemon` (prints instructions)
- `TestUpdateLatestUsesRedirectNotAPI`, `TestUpdateHonoursHTTPSProxy`
- `TestUpdateDryRunChangesNothing`
- **(real)** on Linux, macOS, Windows 11: `smind update --version <older>` then `smind update` against a real published release; Windows with a running `smind.exe` daemon.

### P6

- `TestVerifyChecksumsSignatureAccepts`, `TestVerifyRejectsTamperedChecksums`, `TestVerifyRejectsMissingMinisig`, `TestVerifyRejectsWrongKey`
- `TestEmbeddedPubkeyMatchesTauriConf` (parses `tauri.conf.json`)
- CI: a `workflow_dispatch` dry run on the PR branch produces `checksums.txt.minisig` that `minisign -V` accepts with the committed public key (recorded in Validation).
- `TestUpdateRefusesReleaseWithoutSignature`

### P7

- `TestSystemdUserUnitGolden`, `TestSystemdSystemUnitGoldenUsesSudoUser`, `TestSystemdSystemRefusesPlainRoot`
- `TestSystemdUnitOmitsSMINDHOMEWhenUnset`, `TestSystemdUnitCapturesPATH`
- `TestLaunchdPlistGolden` (KeepAlive `SuccessfulExit:false`, env, log paths)
- `TestServiceInstallIdempotent`, `TestServiceInstallRefusesToStartWhenForeignDaemonAnswers`, `TestServiceUninstallKeepsSMINDHOME`
- `TestServiceStatusExitCodes`
- `TestLingerHintWhenDisabled`
- `TestLogRotationOver10MiB`
- `TestHealthzIncludesManagedByWhenEnvSet`, `TestHealthzOmitsManagedByWhenUnset`
- `test_install_sh_service_flag_runs_service_install` (shim `smind`)
- **(real, Linux with systemd and a macOS box)**: install → reboot/logout cycle (linger) → daemon is up; `smind stop` stays stopped (launchd); `kill -9` restarts; `smind update` restarts via the service; `journalctl --user -u smind` and the macOS log show output; the desktop app (macOS) shows the daemon as service-managed with no Take over.

### P8

- `TestWindowsTaskXMLGolden` (logon trigger, restart-on-failure, hidden, action with log redirect)
- `TestWindowsTaskStopUsesShutdownEndpointFirst`, `TestWindowsTaskStopFallsBackToEnd`
- `TestWindowsServiceStatusExitCodes`, `TestWindowsLogRotation`
- `Install_Service_RegistersTask` (Pester)
- **(real, Windows 11, a spike first)**: register without elevation; no console window appears at logon; kill the process → restarts; `smind service stop` is graceful; log file grows; Windows 10 result recorded or marked unverified.

### P9

- UI (`desktop-daemon-banner.test.tsx`):
  `it("shows smind update with a Copy button for a url connection whose daemon has the command")`,
  `it("shows the installer one-liners for a daemon older than the update command")`,
  `it("copies exactly the displayed command to the clipboard")`,
  `it("shows the notice for a relay connection using daemon.info")`,
  `it("shows version unknown plus installer commands when a relay daemon lacks daemon.info")`,
  `it("keeps Update & restart for the managed local daemon and shows no copy block")`,
  `it("shows managed-by-service and hides Take over for a service-managed local daemon")`.
- Go: `TestDaemonInfoReturnsVersionAndCommit`, `TestDaemonInfoIsReadableOverRelayChannel`.
- Rust: `classify_unmanaged_service_when_healthz_managed_by_present`.
- **(real)** pair a desktop app to an older (v0.11.1) relay-connected daemon and to a newer-than-daemon fake; screenshots light/dark.

### P10

- CI (`docker` job on a PR via `workflow_dispatch`): `build amd64+arm64 succeeds`, `container becomes healthy within 30s`, `smind --version equals the release version`, `runs as uid 10001`.
- `test_image_state_survives_recreate.sh` (volume persistence), `test_image_relay_config_persists.sh` (against a throw-away local relay), `test_image_update_refused.sh`.
- **(real)** pull the published `:<version>` on an amd64 and an arm64 host; run the docs' quickstart verbatim; verify the `FROM` extension example builds and `claude --version` runs inside.

## Decisions

- Architecture and the 19 open decisions live in
  [ADR-0031](../../decisions/0031-homelab-cli-distribution.md); this plan uses
  the recommended defaults. Any answer that differs reopens only the PRs listed
  in the table's "Blocking decisions" column.
- PRs are sequenced, not stacked (WORKSPACE_PROTOCOL.md); shared files:
  `scripts/install.sh` (P1 → P7), `scripts/install.ps1` (P1 → P8),
  `docs/homelab.md` (P2 → P7 → P8 → P10, section-owned),
  `release-please.yml` (P6 → P10), `platform.ts` / `connections-section.tsx` /
  `daemon_manager.rs` / banner (P3 → P4 → P9).
- P4 must not edit the install/update path the Windows bundled-daemon branch
  is changing; it only gates it and calls the existing `daemon_install`.
- Adding `connections_preview_relay`, `connections_probe`, `app_settings_*`
  extends the IPC allowlist; each is minimal and returns no key material
  (ADR-0013 decision 2, ADR-0023 §5).

## Progress

- 2026-10-13: ADR-0031 and this plan written (docs only). Nothing implemented.
  Waiting on Human for the open decisions; wave 1 (P1, P2, P3) is unblocked by
  decisions 10, 11, 12, 13, 15 only.

## Validation

To be filled per PR as work lands: for each Acceptance Criterion, the test name
or the real-platform check that confirmed it, the exact command run, and what
was **not** verified (for example Windows 10, an arm64 Docker host, a real
signed release). Items marked **(real)** above are not satisfied by CI alone.
