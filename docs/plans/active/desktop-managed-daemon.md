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

- **AC1 (version compare):** `Comparison`/`VersionKind` live in
  `smind_daemon_client::daemon_manager::version`, both `Serialize` (used
  directly as the wire DTO field, no separate TS-facing enum needed).
  `Unknown` unless *both* sides are `Release` -- confirmed this is what
  "never nag in a loop" needs: any dev build on either side short-circuits
  to `Unknown` before any ordering happens.
- **AC2 (release resolution):** `asset_name`/`native_target`/`linux_target`/
  `release_urls` all live in `release.rs`; `release_urls` is the *only*
  function that formats a download URL, and it always takes the app's own
  version, never "latest" -- there is no code path in this plan that calls
  the GitHub API at all.
- **AC3 (managed/unmanaged):** the decision table takes an already-
  resolved `port_owner: Option<u32>` rather than probing anything itself,
  so `managed::classify` is pure and platform-agnostic; `find_port_owner`
  (the actual probe) is implemented once per platform (`native::` for
  macOS via `lsof`, inline in `src-tauri`'s `daemon_manager.rs` for WSL2
  via `ss`, since that one needs `wsl.exe` plumbing rather than a plain
  local syscall). `take_over` never signals anything -- it only starts
  trusting a pid the caller already resolved, so the UI's confirmation
  step is the only gate that matters.
- **AC4 (WSL2):**
  - **Platform detection is capability-based, not `cfg(target_os =
    "windows")`.** `detect_platform()` treats "is `wsl.exe` reachable and
    does `-l -v` succeed" as the WSL2 signal. A real Windows host without
    WSL2 has no `wsl.exe`-reachable distro either, so this is equivalent
    in practice, and it's also what let the WSL2 argv plumbing be
    exercised at all from this (Linux) dev sandbox -- see Validation for
    what that looked like and where it stopped.
  - **Pid recovery after start doesn't trust `$!`.** `setsid`/`nohup`'s
    own forking behavior makes `$!` unreliable (see Validation), so
    `install_or_update`/`restart` resolve the real pid the same way the
    managed/unmanaged decision does: ask `ss` who is bound to the
    configured port, after a short delay for the daemon to come up. This
    reuses one primitive for two purposes instead of inventing a second,
    less-reliable way to learn a pid.
  - **`managed.json` is written via base64, not literal JSON in a shell
    string.** The content is app-generated (pid/version/timestamp), never
    user input, but base64 (an alphabet with zero shell metacharacters)
    means the one shell invocation this needs can't have its shape
    changed by the data, matching the same discipline as
    `start_detached_argv`.
  - **Reachability doesn't need `wsl.exe` at all.** WSL2's own localhost
    port forwarding means a daemon bound to `127.0.0.1:<port>` inside the
    distro is directly reachable from the Windows host at the same
    address -- `probe_healthz` just does a normal `reqwest` call against
    the local connection's URL, regardless of platform.
- **AC5 (macOS):** child process (`process_group(0)`), not a launchd
  LaunchAgent -- see the doc comment on `daemon_manager::native` for the
  reasoning (a LaunchAgent survives the app quitting and needs its own
  plist/`launchctl` lifecycle for no benefit here). Base dir is `app.path()
  .app_data_dir()/managed-daemon` (Tauri's own per-app data dir), not a
  literal hardcoded `~/Library/Application Support/smind` -- functionally
  the same location class the task's example points at, and consistent
  with how AC4's connection list already stores `connections.json`.
- **AC6/AC7 (IPC + UI):**
  - **`daemon_status` always asks about the *local* connection**,
    independent of whichever connection is currently selected in the
    picker -- install/update/restart/take-over only ever make sense for
    the local daemon, so this keeps that command's contract simple.
  - **A separate `connection_version` command for the banner's non-local
    case.** The loopback proxy only forwards `/api/*` and `/ws`
    (`proxy::server`'s router), not `/healthz` -- so a remote/url/relay
    connection's version can't be read through the same path the bundled
    UI uses for everything else. `connection_version(id)` probes that
    connection's real base URL directly (bypassing the proxy, exactly
    like `probe_healthz` already does for local), given an id from the
    already-saved connection list -- never a raw user-supplied URL.
  - **`install_or_update` backs both `daemon_install` and `daemon_update`**
    (same operation: ensure the installed binary matches the app's own
    version, then start it) -- the IPC surface keeps them as two named
    commands per the task/plan spec, since "install" and "update" are
    different user intents even though the implementation converges.
  - Progress events use a plain `daemon-progress` event name (not
    `daemon://progress` as sketched in the AC7 draft) -- simpler, and nothing
    else in this codebase uses a URL-shaped event name.
  - `capabilities/proxy.json` gained `core:event:allow-listen`/
    `-unlisten` alongside the six new `allow-*` command permissions --
    the pre-existing six commands never needed the webview to *listen*
    for anything Rust-initiated, so this capability had never needed
    Tauri's core event permissions before.
  - **`build.rs`'s command list must be kept in sync by hand.**
    Discovered while wiring this up: `tauri_build::try_build`'s ACL
    generation only knows about the commands explicitly listed in
    `AppManifest::new().commands(&[...])` in `build.rs` -- adding a
    `#[tauri::command]` function and registering it in
    `generate_handler!` is not enough by itself; forgetting the `build.rs`
    entry fails the build with "Permission allow-X not found" rather than
    silently missing the command.
- **Post-review safety fix:** "who owns the port now" was being trusted
  as "who we just started" with no independent check -- correct only
  when nothing else could already be on the port, which is exactly the
  case (an unmanaged daemon already running) AC3 exists to handle safely.
  Fixed by never treating port ownership alone as proof of identity:
  every place that's about to signal a pid or persist it as managed now
  also requires an exe-identity match (`ps -o args=` on macOS, `readlink
  -f /proc/<pid>/exe` on WSL2) against the binary this app actually
  manages. See Validation for the full breakdown.

## Progress

- [x] AC1 version comparison
- [x] AC2 release asset resolution
- [x] AC3 managed vs unmanaged
- [x] AC4 WSL2
- [x] AC5 macOS native
- [x] AC6 fixed-release-only (folded into AC2)
- [x] AC7 IPC + UI
- [x] AC8 regressions

## Validation

- **Rust unit tests** (`cargo test` in `desktop/daemon-client`): 116
  passed (up from the pre-existing 77 baseline), covering `version`,
  `checksums`, `release`, `managed`, `wsl` (argv builders + UTF-16LE
  distro-list parsing against a byte sample captured live in this
  sandbox, `sha256sum`/`ss` output parsing), and `native` (argv shape,
  `lsof`/`kill -0` output parsing, and two live integration-style tests:
  one binds a real `TcpListener` and confirms `find_port_owner` correctly
  names this test process as the owner; another builds a real fake
  binary + tarball with `tar` and round-trips install/start/find/kill
  against it). Plus 3 tests against a real in-process `axum` server for
  `download_and_verify`/`install_from_release` (good tarball, bad
  checksum, no asset for this platform). The pre-existing proxy
  integration test (1) and doc-tests (0) are unaffected.
- **`desktop/src-tauri`**: `cargo build` and `cargo build --release`
  both clean, no warnings, including the real `webkit2gtk`/`tao`/
  `tray-icon` dependency chain. `cargo test` is 0/0 by design, matching
  this crate's existing precedent (all pure logic lives in
  `daemon-client`; `daemon_manager.rs` here is glue over already-tested
  primitives plus Tauri-specific plumbing like `AppHandle`/event
  emission that can't be unit-tested without a webview).
- **Full pipeline dry run**: `bun run tauri build --no-bundle` (from
  `desktop/`) ran the entire pipeline -- `beforeBuildCommand`
  (`bun install && bun run --filter @smind/ui build:desktop`) then the
  full release `cargo build` -- and produced a working release binary.
  The desktop bundle's `event-*.js` chunk (from `onDaemonProgress`'s
  dynamic `@tauri-apps/api/event` import) is present and separately
  code-split, confirmed absent from the daemon-embedded (`bun run build`)
  bundle -- same pattern the AC6 desktop-platform-layer plan established
  for `@tauri-apps/api/core`.
- **Web tests**: `bun run --filter '@smind/ui' test` -- 1263/1263 green
  across 103 files (up from the 1079/97 baseline recorded in
  `desktop-bundled-ui.md`, reflecting both this plan's additions and
  other work landed on `develop` since), including:
  - `lib/platform.test.ts`: every new `DesktopApi` method rejects in a
    non-desktop build and calls the right `invoke` command (with the
    right args) in a desktop build; `onDaemonProgress` subscribes via a
    dynamically-imported `listen`, forwards a fired event's payload to
    the caller's callback, and unsubscribing calls the returned
    `unlisten`.
  - `components/desktop-daemon-banner.test.tsx`: renders nothing when
    not desktop or when up to date; shows the actionable "Update &
    restart" banner only for the local+managed+older case; shows the
    same text with no button for local-but-unmanaged and for a url
    connection, both older; the button calls `daemonUpdate`.
  - `components/settings/daemon-section.test.tsx`: does not register in
    a non-desktop build; shows status/version/managed state/log path;
    the action button set changes correctly across `notRunning`
    (Install), `managed`+older (Update & restart, plus Restart),
    `managed`+same (Restart only), and `unmanaged` (Take over ->
    confirm/cancel, confirm calls `takeOverDaemon`, cancel never does);
    a fired progress event updates the shown stage text while an action
    is busy; an install error surfaces verbatim; the `unsupported`
    platform shows the not-supported message and no action buttons.
  - Full existing suite re-run alongside these confirms no regression.
- **Typecheck**: `bun run --filter '@smind/ui' typecheck` clean.
- **`task test`**: `go test ./...` all green except
  `internal/taskrunner`'s `TestRunner_RunPrompt_ClaudeNative_ToolCallEvents`,
  which failed once on the full-suite run and passed 3/3 on an isolated
  rerun immediately after -- a pre-existing flake unrelated to this
  change (`git diff --stat -- internal/ cmd/` is empty; no Go file is
  touched by this plan at all). `bun run --filter '@smind/ui' test` is
  the 1263/1263 above. **`task lint`** (`go vet ./...` + `gofmt -l`) is
  silent.
- **Live, this sandbox (a genuine WSL2 Ubuntu distro, confirmed via
  `wsl.exe --version`):**
  - `wsl.exe -l -v` is reachable via interop from inside the distro
    itself and returns real output; its raw bytes (UTF-16LE, no BOM) were
    captured and became `wsl::parse_default_distro`'s unit test fixture.
  - **Attempted, and stopped, a full install/start/restart live run
    against a throwaway `HOME`.** `env HOME=<tmp> wsl.exe -d Ubuntu --
    ...` and even `sh -c 'export HOME=<tmp>; ...'` **did not** redirect
    `$HOME` for the invoked command -- `wsl.exe`, even when re-entered
    from inside the same distro it targets, resets `HOME` (and `pwd`
    inherited the *caller's* cwd, itself a strong sign this sandbox's
    nested `wsl.exe` interop does not behave like genuine per-invocation
    Windows+WSL2 session semantics). A `mkdir -p "$HOME/.local/share/
    smind/bin"` run before this was discovered created a stray *empty*
    directory under the real `~/.local/share/smind` (cleaned up
    immediately; nothing was ever written inside it, and the real daemon
    on `127.0.0.1:4648` answered `{"service":"smind","status":"ok"}`
    identically before and after, confirmed both times). Given this
    isolation gap, continuing to drive the full flow here risked writing
    into the real `~/.local/share/smind` or colliding with the real
    daemon's port -- exactly what the task requires never happens -- so
    the live attempt stopped there rather than pushing further.
  - **What this means for AC4's confidence:** the argv builders, output
    parsers, and the managed/unmanaged decision table are unit-tested
    (including against real captured output). The `wsl::run`/orchestration
    layer that actually drives `wsl.exe` end-to-end (download -> verify
    -> install -> start -> find-pid -> restart) is *not* exercised live
    in this session, and the app itself was not run against the WSL2
    path for the same reason -- this is short of the task's "live WSLg
    run where feasible" bar, and is called out explicitly rather than
    implied by a passing test suite.
  - **macOS: not feasible at all in this sandbox (Linux only).** The
    platform-agnostic parts of the same logic (`native::` install/start/
    find/kill/download) were instead exercised live on Linux with a temp
    dir standing in for the real macOS path (see the Rust unit test list
    above) -- the macOS-specific wiring in `daemon_manager.rs`
    (`cfg(target_os = "macos")`'s real path) is unverified beyond
    `cargo build` type-checking correctly for the logic it shares with
    the tested `native` module.
- **Windows CI**: pushed to `feat/desktop-managed-daemon`.
  - Run [36103682288](https://github.com/spacingmind/smind/actions/runs/36103682288)
    **failed** at "Build installers": `smind-daemon-client` doesn't
    compile for the `windows` target because `daemon_manager::native`
    unconditionally imported `std::os::unix::process::CommandExt` and
    called `.process_group(0)` -- both unix-only. This module has no
    target_os cfg of its own (by design, per AC5's decision -- it's
    exercised live on Linux with a temp dir standing in for the real
    macOS path), so it still has to *compile* cleanly cross-platform
    even though `spawn_detached`'s real callers are never reached on
    Windows (`detect_platform()` only returns `Platform::Macos` when
    `cfg!(target_os = "macos")`). This one call was the sole gap --
    `install_binary`'s own unix-only bit (`PermissionsExt`/`set_mode`)
    was already correctly `#[cfg(unix)]`-gated from the start.
  - Fixed by gating both the `use std::os::unix::process::CommandExt`
    import and the `cmd.process_group(0)` call behind `#[cfg(unix)]`,
    with no other change to `spawn_detached`'s behavior on unix (macOS
    still gets the real detach-into-its-own-process-group call; Windows
    just skips a call it would never reach at runtime anyway).
  - Re-scanned the whole `desktop/daemon-client`/`desktop/src-tauri`
    tree for other unix-only surface (`std::os::unix::*`, `libc::`,
    `nix::`, `PermissionsExt`, `MetadataExt`, `OsStrExt`, `FileExt`,
    `process_group`): the only other hits are three `PermissionsExt`
    uses inside `#[cfg(test)] mod tests` (the install/download
    integration tests build a real unix-executable fixture with
    `chmod`-equivalent bits). Confirmed these don't affect the Windows
    build: `desktop-windows.yml`'s only Rust step is `npx tauri build`
    (a release build), which never compiles `#[cfg(test)]` code, and no
    workflow in this repo runs `cargo test` on a Windows runner
    (`ci.yml` is `ubuntu-latest` only). Left them as-is rather than
    gating code that's never actually built there.
  - Re-run [36113216793](https://github.com/spacingmind/smind/actions/runs/36113216793)
    **succeeded** in 9m47s: `Build installers` (`npx tauri build`) built
    the release binary and both installers, `Stage installers` and the
    artifact upload both completed. `desktop-daemon-client`'s own test
    suite (116 tests) was re-run locally after pulling this fix and is
    still green.
- **Post-review safety fix (2a147ac):** a review caught a blocking bug in
  `install_or_update` matching the user's actual setup exactly -- an
  unmanaged `./bin/smind serve` already on `:4648`. Clicking Install/
  Update started a new process on the already-occupied port (which fails
  to bind), `find_port_owner` then returned the *unmanaged* pid, and that
  got saved as the managed record -- the next Restart/Update would have
  killed the user's own daemon. The macOS branch had the same shape of
  bug (kill-before-restart only checked "is the recorded pid merely
  alive", not "is it still the real port owner and really our binary").
  Fixed with three pure decisions in `managed.rs` (tested without any
  real process tree):
  - `assert_safe_to_install(record, port_owner, port)` refuses Install/
    Update outright -- before touching disk or network -- when
    `classify()` says the port is `Unmanaged`. Only `take_over` (already
    gated behind explicit UI confirmation in `daemon-section.tsx`'s
    confirm/cancel step -- re-verified this session) may adopt an
    unmanaged daemon.
  - `safe_to_kill(record, port_owner, exe_matches)` requires *both* the
    live port owner to equal the recorded pid *and* an independent
    exe-identity check, before either branch signals a previously-
    managed pid.
  - `verify_started_identity`/`verify_fresh_start(port_owner, exe_matches,
    reported_version, expected_version)` refuse to save a fresh
    `ManagedRecord` unless the just-started process's identity checks out
    (and, for install/update specifically, `/healthz` reports the exact
    version just installed) -- this is what would have caught the
    original bug directly: if the new process fails to bind, the port's
    owner is still the old occupant, whose exe path doesn't match.
  - The identity check itself: `native::exe_path_for_pid` (`ps -o args=`,
    first token) on macOS, `wsl::exe_path_argv`/`parse_exe_path`
    (`readlink -f /proc/<pid>/exe`) on WSL2, both wired into `restart`'s
    pre-kill guard too (previously port-ownership only).
  - New tests: `daemon-client` grew from 116 to 127 (11 new: 6 in
    `managed` for the three pure decisions directly -- including the
    exact three scenarios asked for: unmanaged owner + install refuses,
    a stale record pid that no longer matches the owner blocks the kill,
    and a foreign exe on the port after a start attempt is rejected
    rather than saved; 3 in `native` for `exe_path_for_pid`/`exe_matches`
    including a live test against a real spawned `/bin/sleep`; 2 in `wsl`
    for the argv/parser/suffix-match). All 127 pass; `cargo build`/
    `cargo build --release` for `src-tauri` clean, no warnings;
    `task lint` and the full web suite (1263/1263, unaffected -- no web
    files changed by this fix) both green; `go test ./...` green with no
    Go files touched.
  - **Windows CI re-run**: [36115170662](https://github.com/spacingmind/smind/actions/runs/36115170662)
    -- green in 3m59s (rust-cache hit, since only `desktop/` Rust sources
    changed, no new dependency).
- **Follow-up fix (12b10db): take-over was a dead end.** The previous
  fix's exe-identity check compared every pid against the *managed*
  layout path, but `take_over` saved `exe_path: "unknown"` for the
  adopted daemon -- so the identity check could never pass for it, and
  every Restart/Update after a Take-over refused forever ("safe", but
  pointless for the one real scenario Take-over exists for: the user's
  own `./bin/smind serve`).
  - `take_over` now resolves the owner pid's real executable up front
    (same primitives as everywhere else: `native::exe_path_for_pid` /
    `wsl::exe_path_argv`+`parse_exe_path`) and refuses the take-over
    outright -- a typed error, no record saved -- if it can't resolve
    one, rather than storing a placeholder.
  - The identity check is now parameterized by *which* path a call site
    expects: pre-kill guards (`install_or_update`, `restart`) compare
    against the record's own `exe_path` (the adopted path right after a
    take-over, or the managed path for a normal managed daemon --
    `wsl::exe_path_matches`, exact string match, since it's always a
    previously-resolved absolute path now); the post-start check after a
    fresh install/update/restart compares against the *managed* binary
    specifically (`wsl::exe_path_matches_managed_bin`, suffix match,
    since `$HOME` is never known to Rust on the WSL2 path -- macOS always
    knows the literal managed path, so one function serves both cases
    there).
  - Fixed a related staleness bug found while doing this: `restart`'s
    saved record inherited the old `exe_path` unchanged via `..record`,
    which would keep an adopted path around forever even after the
    managed binary took over the port on a successful restart. Both
    platforms now refresh `exe_path` to whatever was just verified.
  - **New tests** (`daemon-client` 127 -> 131): `managed.rs` gained the
    three scenarios asked for directly -- take-over stores the resolved
    path, not a placeholder; a take-over record passes `safe_to_kill`
    against its own adopted path (this is what makes Restart-after-
    take-over work); and the update-after-take-over sequence (kill the
    adopted pid via the same guard, then a freshly-verified managed pid
    gets a record with the managed path, distinct from the adopted one).
    `wsl.rs` gained `exe_path_matches`'s own test. All 131 pass; `cargo
    build`/`--release` clean for `src-tauri` (one `dead_code` warning
    from an unused convenience wrapper, removed); `task lint` and the
    full web suite (1263/1263, unaffected) both green; no Go files
    touched.
  - **Residual gap noted, not fixed (out of scope for this ask):**
    `restart()` always starts the *managed* binary
    (`start_detached_argv`/`spawn_detached` never target the adopted
    path), regardless of the record's own `exe_path`. If a user does
    Take-over and then clicks Restart *without ever having clicked
    Install/Update first*, the pre-kill guard now correctly kills the
    adopted process (as designed), but there is no managed binary on
    disk yet for the restart to start -- leaving nothing running. This
    is a availability/UX gap, not a wrong-process-killed safety bug (the
    process identity checks are exactly what's intended), and Update's
    own flow (install, *then* kill-and-start) does not hit it. Flagging
    for a future decision on whether to gate Restart or nudge the UI
    toward Update-first for a purely-adopted, never-installed record.
  - **Windows CI re-run**: pushed; see below.
- **Not done / explicitly deferred:**
  - A full live WSL2 end-to-end run (see above) -- stopped for safety
    once the sandbox's `wsl.exe` isolation gap surfaced.
  - Any live verification on macOS or native Windows (no such host in
    this sandbox).
  - Opening a PR (out of scope per the task's own instructions).
