# Desktop-managed daemon (ADR-0013 part D2)

## Context

ADR-0013's version-skew sub-decision (`docs/decisions/0013-desktop-bundled-ui.md`,
user-approved 2026-09-25) lets the desktop app **install, update and restart
the local daemon it manages**, and offer "update daemon and restart" when the
app is newer than the daemon. Three prerequisite pieces are already on
`develop`:

- the loopback proxy + connection list (`desktop-bundled-ui.md`, PR #196) —
  `smind_daemon_client::proxy::{Registry, Connection, ConnectionKind}`,
  `desktop/src-tauri/src/{commands.rs,state.rs}`, `capabilities/proxy.json`;
- the version signal (`daemon-version-signal.md`, PR #195) — `GET /healthz`
  returns `{"status","service","version"}`, `smind --version`;
- release binaries (`release-binaries.md`, PR #198) — every GitHub Release
  gets `smind_<version>_<os>_<arch>.tar.gz` (`os` = `linux`/`darwin`, `arch` =
  `amd64`/`arm64`) plus `LICENSE` inside, and one `checksums.txt` (SHA-256)
  over every asset including the Windows installers. Desktop version fields
  are kept in sync with the release manifest by `scripts/check-version-sync.sh`.

**User approval, 2026-09-25:** Windows+WSL2 and macOS native, both in scope;
Windows-native is blocked (`internal/terminal`'s `syscall.Kill`/no ConPTY —
see ADR-0013) and out of scope here — show a clear "not supported on native
Windows" message instead.

**Hard constraints (task, ADR-0013):**
- No Go daemon changes. No new endpoint, no CLI subcommand, no daemon-side
  "who am I" identity beyond the existing `/healthz` `version` field.
- The app only touches a daemon it manages. It tracks *its own* managed
  instance via a pid/state file (app data dir on macOS, inside WSL for the
  WSL2 case) — it never infers "mine" from the daemon's own state, because
  there is none to ask (no lock file, no stop endpoint).
- Downloads only from the fixed `github.com/spacingmind/smind` releases,
  over https, checksum-verified. No user-supplied URLs.
- WSL2 execution is argv-only for anything that touches `wsl.exe` — no shell
  string interpolation of *untrusted* values (fixed, app-computed paths are
  fine inside a `sh -c` wrapper where the shell itself is unavoidable, e.g.
  detached-start plumbing; user input like a connection URL/label never
  reaches a shell string).

**Paseo reference (behavior ported, not code):**
`refs/paseo/packages/desktop/src/daemon/daemon-manager.ts` — `ownedByDesktop`
(compare a captured `{pid, startedAt}` against the live process, not trust
the daemon), `shouldRestartForVersion` (only restart if owned, and versions
differ after stripping a `v` prefix), explicit-confirmation stop
(`confirmedInstance` must match `{pid, startedAt}` exactly).
`refs/paseo/packages/app/src/desktop/updates/desktop-updates.ts` —
`isVersionMismatch`/`normalizeVersionForComparison` shape (this plan goes
further: an actual older/same/newer ordering, since the banner text says
"vX is older than this app").

Paseo's daemon writes its own `paseo.pid` lock file with `{pid, startedAt}`,
so "owned" is a strict identity match. smind's daemon writes nothing — the
app-side pid/state file is the *only* source of truth for "the app started
this," and "is it still alive" is a liveness check the app does itself
(finding the process actually bound to the configured port), not a trust
relationship with the daemon.

## Acceptance Criteria

- **AC1: version comparison.** `smind_daemon_client::daemon_manager::version`
  classifies a version string as `Release(major, minor, patch)`, `Dev`, or
  `Unknown`, and compares two versions to `Older | Same | Newer | Unknown`.
  - `"dev"` (bare) and anything containing `-dev` (matches the Taskfile's
    `<manifest>-dev+<shortsha>[.dirty]` shape) classify as `Dev`.
  - Comparison is `Unknown` unless **both** sides are `Release`. This is
    the "never nag in a loop, unknown is not older" rule: a local `go build`
    daemon (`dev`) next to a packaged app build never shows a stale banner,
    and vice versa.
  - A leading `v` is stripped before classifying (daemon tags are `v0.7.0`;
    `/healthz` and `tauri.conf.json` both already report bare `0.7.0`, but
    comparison must tolerate either).
- **AC2: release asset resolution.**
  `smind_daemon_client::daemon_manager::release`:
  - `asset_name(version, os, arch) -> String` builds
    `smind_<version>_<os>_<arch>.tar.gz` exactly as `release-please.yml`
    produces it; `os`/`arch` are the release's own vocabulary
    (`linux`/`darwin`, `amd64`/`arm64`), not Rust's `std::env::consts`
    spelling — a separate `native_target() -> (os, arch)` maps
    `env::consts::{OS,ARCH}` (`"macos"/"x86_64"` etc.) to it for the macOS
    path, and the WSL2 path gets its target by asking the distro itself
    (AC4).
  - `checksums::parse(text) -> HashMap<filename, hex>` parses a
    `sha256sum`-style `checksums.txt`; `checksums::verify(bytes, expected_hex)`
    hashes with `sha2` and compares.
  - `release_urls(version) -> {tarball, checksums}` builds the two fixed
    `https://github.com/spacingmind/smind/releases/download/v<version>/...`
    URLs — the version is always **the app's own version** (never "latest";
    "only the app's own release" per the task, and it sidesteps needing a
    GitHub API call to resolve "latest").
  - A 404 on either URL (dev build with no release, or a real version with
    no asset for this os/arch) surfaces as a typed `ReleaseError` the UI
    renders as a clear message — never a silent no-op.
- **AC3: managed vs. unmanaged.**
  `smind_daemon_client::daemon_manager::managed`:
  - `ManagedRecord { pid, version, installed_at, exe_path }`, persisted as
    JSON at `<managed-state-dir>/managed.json` (macOS: app data dir; WSL2:
    inside the distro, alongside the installed binary — see AC4/AC5).
  - `find_port_owner` is a platform primitive returning `Option<u32>` (the
    pid bound to a TCP port) — `ss` inside WSL2 (AC4), `lsof` on macOS
    (AC5). It is the single mechanism for both "is my recorded pid still
    the one actually serving" and "whose daemon is this" (unmanaged case),
    so both features get the exact same liveness answer.
  - Decision table (`managed::classify`, pure — takes the record, the
    live port-owner pid, and reachability, all pre-resolved by the caller,
    so it's unit-testable with no OS calls):
    | daemon on port? | recorded pid == port owner? | → |
    |---|---|---|
    | no | – | **NotRunning** (offer Install) |
    | yes | yes | **Managed** (offer Update/Restart) |
    | yes | no (or no record) | **Unmanaged** (offer "take over", never kill) |
  - **Take over** (`managed::take_over`): given the live port-owner pid,
    write a fresh `ManagedRecord` for it. It never sends a signal, starts,
    or stops anything — it only starts trusting an already-running process.
    Requires explicit confirmation in the UI (AC7); the IPC command itself
    has no separate "confirm" flag because invoking it *is* the confirmed
    action — the confirmation dialog is what gates the invoke.
  - Restart/update re-check `find_port_owner` immediately before signalling,
    and refuse (typed error, no signal sent) if it no longer matches the
    recorded pid — closes the PID-reuse race Paseo's `explicit` check also
    guards against.
- **AC4: Windows + WSL2.**
  `smind_daemon_client::daemon_manager::wsl` — pure argv builders, unit
  tested against captured sample output, plus a thin runner
  (`Command::new("wsl.exe")`) that's exercised live, not unit tested:
  - `parse_default_distro(raw: &[u8]) -> Option<String>`: `wsl.exe -l -v`
    prints UTF-16LE (with stray NUL bytes when not attached to a real
    console — confirmed empirically), marks the default with a leading
    `*`. Decode UTF-16LE, find the `*`-prefixed row, return the name
    column.
  - `detect_target(distro) -> argv` = `["wsl.exe", "-d", distro, "--",
    "uname", "-m"]`; map `x86_64`→`amd64`, `aarch64`/`arm64`→`arm64` (unknown
    → error, never silently default to amd64).
  - `download_argv`/`verify_argv`/`install_argv`: run entirely with
    coreutils already present in a stock Ubuntu WSL distro (`curl -fsSL -o`,
    `sha256sum -c`, `install -Dm755` or `mkdir -p && cp`) — each a fixed
    argv list, `-d distro` selecting the target, URLs passed as their own
    argv element (never concatenated into a shell string).
  - `start_argv(distro, bin_path, log_path)` is the one place a shell is
    used, because detach+redirect needs it: `["wsl.exe", "-d", distro, "--",
    "sh", "-c", "setsid nohup '<bin_path>' serve >>'<log_path>' 2>&1 </dev/null &"]`
    — `bin_path`/`log_path` are app-computed fixed paths under
    `~/.local/share/smind/`, never user input, so this isn't the
    "untrusted value in a shell string" the constraint rules out.
  - `port_owner_argv(distro, port)` = `["wsl.exe", "-d", distro, "--", "ss",
    "-H", "-tlnp", "sport", "=", ":<port>"]` (argv-only — `ss`'s filter
    grammar accepts `sport`/`=`/`:<port>` as separate tokens, no shell
    needed); `parse_ss_pid(stdout) -> Option<u32>` extracts `pid=NNNN`.
  - `kill_argv(distro, pid)` = `["wsl.exe", "-d", distro, "--", "kill",
    "<pid>"]`.
  - Install path: `~/.local/share/smind/bin/smind` inside WSL; log file
    `~/.local/share/smind/smind.log`; state file
    `~/.local/share/smind/managed.json`. `~` is expanded by the WSL-side
    shell/coreutils, never by Rust.
- **AC5: macOS native.**
  `smind_daemon_client::daemon_manager::native` — generic local-process
  install/start/stop, parameterized on a base directory (so it's
  unit/integration-testable with a temp dir standing in for the real path):
  - install: download (reqwest, reusing the crate's existing client),
    verify (AC2), extract (`tar -xzf <tarball> -C <dir> smind` via argv, no
    shell — `tar` ships on macOS and every Linux distro including WSL2's
    Ubuntu, so this same helper is reused by AC4's install step rather than
    duplicated).
  - **Decision: child process, not a LaunchAgent.** A launchd `.plist` +
    `launchctl bootstrap` survives the app quitting, which fights the
    app-managed lifecycle (`connections_select`-style "the app owns
    this"), needs its own uninstall path, and adds a second persistence
    format (plist) for zero benefit here — the app itself is what's
    running when the user cares about the daemon. `Command::new(bin)
    .arg("serve").process_group(0)` (stable `std::os::unix::process`
    API) detaches into its own process group so hiding/closing the main
    window doesn't SIGHUP it, mirroring WSL2's `setsid`. Stdout/stderr
    redirect to `smind.log` in the same install dir.
  - Real path: `~/Library/Application Support/smind/` (`bin/smind`,
    `smind.log`, `managed.json`), wired only under `cfg(target_os =
    "macos")` in `src-tauri`; the generic `native` module itself has no
    `cfg` and is exercised on Linux in this session with a temp dir.
  - `find_port_owner` on macOS: `lsof -tiTCP:<port> -sTCP:LISTEN` (argv,
    first line parsed as the pid; empty output = nothing listening).
- **AC6: only the app's own release, everywhere.**
  `release_urls` (AC2) is the only place a download URL is constructed;
  there is no code path that accepts a URL from settings, an env var, or
  IPC input. A version with no release yet, or a release with no asset for
  this os/arch, is a typed error surfaced verbatim in the UI (AC7) — never
  swallowed.
- **AC7: IPC + UI.**
  - New commands, all under `desktop/src-tauri/src/commands.rs`, added to
    `capabilities/proxy.json` only (`default.json` untouched):
    - `daemon_status() -> DaemonStatus` — `{platform: "wsl2"|"macos"|
      "unsupported", reachable, daemonVersion, appVersion, comparison:
      "older"|"same"|"newer"|"unknown", managed, pid, installedPath,
      logPath}`. Reachability/version come from the existing daemon-client
      `healthz`/token machinery against the **local** connection's URL,
      independent of which connection is currently selected in the picker.
    - `daemon_install()`, `daemon_update()`, `daemon_restart()`,
      `take_over()` — all `Result<DaemonStatus, String>`, all validate
      platform support first (`"unsupported"` → `Err` with a clear
      message, no-op). Long-running ones (`install`/`update`) emit a
      `daemon://progress` event (`{stage: "downloading"|"verifying"|
      "installing"|"starting"|"stopping", message}`) as they go, and
      resolve with the final status.
    - `capabilities/proxy.json` also gains `"core:event:allow-listen"` (and
      `-unlisten`) — the six existing commands never needed the webview to
      *listen* for anything Rust-initiated; progress events do.
  - `web/packages/ui/src/lib/platform.ts`: extends `DesktopApi` with
    `daemonStatus`, `daemonInstall`, `daemonUpdate`, `daemonRestart`,
    `takeOver`, `onDaemonProgress(cb) -> unsubscribe` (wraps
    `@tauri-apps/api/event`'s `listen`, dynamically imported next to
    `core`, same pattern as `loadInvoke`).
  - **Banner** (App.tsx, desktop builds only, near the existing connection
    status text): shown when `comparison === "older"` **and** the current
    connection is `kind === "local"` **and** `managed === true` — "daemon
    v`<daemonVersion>` is older than this app (v`<appVersion>`) — Update &
    restart" with a button calling `daemonUpdate()` and showing progress.
    Local-but-unmanaged and remote/url/relay connections that are `older`
    get a plain, non-actionable notice instead (same text, no button) —
    the same "never touch a daemon we don't manage" rule from AC3 applies
    to the UI, not just the Rust side.
  - **Settings → Daemon** (new `components/settings/daemon-section.tsx`,
    registered only `if (isDesktop)`): status, version, managed/unmanaged,
    contextual action button (Install when not running, Update when
    managed+older, Restart when managed, "Take over management" +
    confirmation when unmanaged-but-running), log file path (text, not a
    link — no "open in Finder/Explorer" command exists or is needed),
    platform-unsupported message on Windows-native.
- **AC8: no regressions.** No Go changes (`git diff --stat -- internal/
  cmd/` empty). Existing `cargo test`/web tests/`task test`/`task lint`
  stay green. No change to the proxy, connection list, or existing IPC
  commands beyond the additions above.

## Test Scenarios

- **Rust unit (`daemon_manager::version`):** `"0.7.0"` vs `"0.6.0"` →
  `Newer`/`Older`; equal → `Same`; `"dev"` vs anything → `Unknown`;
  `"0.7.0-dev+abc123"` vs `"0.7.0"` → `Unknown`; `"0.7.0-dev+abc.dirty"` →
  still `Dev`; leading `v` stripped both sides; a garbage string (`"foo"`)
  → `Unknown`, not a panic.
- **Rust unit (`checksums`):** parses a multi-line `sha256sum` file
  (including one with a `*binary` prefix, since some tools emit that);
  `verify` true/false; a malformed line is skipped, not a parse error for
  the whole file.
- **Rust unit (`release`):** `asset_name` for all four `(os, arch)`
  combinations matches the exact string `release-please.yml` produces
  (checked against a literal from that workflow); `native_target()` maps
  `("macos","x86_64")→("darwin","amd64")`,
  `("macos","aarch64")→("darwin","arm64")`; an unmapped arch is an error.
- **Rust unit (`managed::classify`):** all three rows of the decision
  table above, plus: no record + no port owner → `NotRunning`; record
  present but port owner is `None` (nothing listening even though we
  think we installed it) → `NotRunning`, not `Managed` (a crashed managed
  daemon must offer "Install"/"Restart", not silently claim to be
  running); `take_over` produces a record whose pid matches the given
  port owner exactly.
- **Rust unit (`wsl`):** `parse_default_distro` against a captured
  UTF-16LE byte sample (this session's own `wsl.exe -l -v` output,
  transcribed into the test as a byte literal) returns `"Ubuntu"`;
  `detect_target`'s arch mapping (`x86_64`→`amd64`, `aarch64`→`arm64`,
  unknown → error); every `*_argv` builder returns the exact expected
  `Vec<String>` for a set of sample inputs, with **no** element containing
  a shell metacharacter combination that would matter if it were
  accidentally run through a shell (defense in depth, since the argv
  builders are the security boundary here); `parse_ss_pid` against a
  captured `ss -H -tlnp` sample line.
- **Rust unit (`native`):** the macOS `find_port_owner` output parser
  against a captured `lsof -tiTCP -sTCP:LISTEN` sample; install/start
  round-trip against a temp base dir using a **locally-built** fake
  release (see Live below) rather than a real download.
- **Rust integration/live, WSL2 (this sandbox genuinely is WSL2 —
  `wsl.exe` is reachable via interop, confirmed):**
  - a temp `HOME`-equivalent inside the same Ubuntu distro (a throwaway
    subdirectory, never `~/.local/share/smind`) and a spare port;
  - serve a **locally-built** `smind` tarball + `checksums.txt` from a
    `python3 -m http.server` bound to `127.0.0.1` on a throwaway port,
    with production `release_urls` bypassed via a test-only base-URL seam
    (an `Option<&str>` override on the install function, `None` in
    production, `Some(&test_server_url)` only from the test) — production
    code path itself still only ever builds the fixed GitHub URL;
  - install → verify checksum → start detached → `find_port_owner`
    resolves the real pid → `/healthz` reachable → restart (kill by
    resolved pid, confirm the port is free, start again, confirm a new
    pid) → stop for cleanup;
  - **never** touches `127.0.0.1:4648` or anything under
    `~/.local/share/smind` — confirmed by checking the real daemon (if
    any) is unaffected before/after.
- **macOS: not live-testable in this sandbox (Linux only).** The
  `native` module's generic logic (install/start/restart against a temp
  base dir standing in for `~/Library/Application Support/smind`) is
  exercised live on Linux instead, per the `native` unit/integration
  tests above; the macOS-specific wiring (`cfg(target_os = "macos")` in
  `src-tauri`, the real path, `lsof`) is unit-tested for its pure parts
  only and otherwise relies on `cargo build` type-checking cleanly for
  that target (cross-compile check, not a run).
- **Web tests (mocked desktop API, same pattern as
  `connections-section.test.tsx`):**
  - banner: renders with "Update & restart" only for local+managed+older;
    plain notice (no button) for local+unmanaged+older and for
    url/relay+older; nothing when `same`/`newer`/`unknown`;
  - `daemon-section.test.tsx`: shows status/version/managed badge; button
    set changes with status (`NotRunning`→Install,
    `Managed`+older→Update, `Managed`→Restart, `Unmanaged`→"Take over" +
    confirm step); a progress event updates the shown stage text; an
    install/update error surfaces verbatim; Windows-native shows the
    unsupported message and no action buttons.
- **Regression:** full existing web suite + `cargo test` (both crates)
  count unchanged plus the new tests above; `task test`/`task lint` green.

## Decisions

(filled in as work proceeds)

## Progress

- [ ] AC1 version comparison
- [ ] AC2 release asset resolution
- [ ] AC3 managed vs unmanaged
- [ ] AC4 WSL2
- [ ] AC5 macOS native
- [ ] AC6 fixed-release-only (folded into AC2, checked off with it)
- [ ] AC7 IPC + UI
- [ ] AC8 regressions

## Validation

(filled in as work proceeds)
