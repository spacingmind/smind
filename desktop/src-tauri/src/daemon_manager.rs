//! ADR-0013 part D2: install/update/restart the local daemon the app
//! manages, on Windows+WSL2 and macOS native (Windows-native is blocked,
//! see the ADR -- `Platform::Unsupported` covers it and every other host).
//! Orchestrates the pure logic in `smind_daemon_client::daemon_manager`
//! with the OS calls it needs (spawning `wsl.exe`, spawning the daemon
//! process, hitting the network) plus Tauri glue (app data dir, event
//! emission, app version). See `docs/plans/active/desktop-managed-daemon.md`.

use std::time::{SystemTime, UNIX_EPOCH};

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager};

use smind_daemon_client::daemon_manager::managed::{self, ManagedRecord, ManagedState};
use std::path::Path;
use smind_daemon_client::daemon_manager::{native, release, version, wsl};

use crate::state::DesktopState;

const PROGRESS_EVENT: &str = "daemon-progress";
const DEFAULT_PORT: u16 = 4648;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Platform {
    Wsl2,
    Macos,
    Unsupported,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DaemonStatus {
    pub platform: Platform,
    pub reachable: bool,
    pub daemon_version: Option<String>,
    pub app_version: String,
    pub comparison: version::Comparison,
    pub managed_state: ManagedState,
    pub pid: Option<u32>,
    pub installed_path: Option<String>,
    pub log_path: Option<String>,
    /// Whether the *managed* binary exists (and, on WSL2, is executable)
    /// on disk right now -- distinct from `managed_state`, since a bare
    /// take-over (no prior Install/Update) can be `Managed` with no
    /// managed binary installed at all. Drives whether the UI's Restart
    /// action is enabled (`managed::assert_restartable`'s precondition).
    pub binary_installed: bool,
}

/// ConnectionVersionInfo is the banner's data source for a connection that
/// **isn't** the local one (AC7: remote/url/relay connections get a plain
/// notice, never an action) -- probed directly (bypassing the loopback
/// proxy, which doesn't forward `/healthz`), the same way `DaemonStatus`
/// probes the local connection.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ConnectionVersionInfo {
    pub reachable: bool,
    pub daemon_version: Option<String>,
    pub app_version: String,
    pub comparison: version::Comparison,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct Progress {
    stage: &'static str,
    message: String,
}

fn emit_progress(app: &AppHandle, stage: &'static str, message: impl Into<String>) {
    let _ = app.emit(PROGRESS_EVENT, Progress { stage, message: message.into() });
}

/// detect_platform is capability-detected, not a bare `target_os` match:
/// macOS is always native-capable, and WSL2 support follows from whether
/// `wsl.exe` is actually reachable (a real Windows host without WSL
/// configured has none either) -- see the plan's Decisions for why this
/// also happens to make the WSL2 path exercisable outside a literal
/// Windows target.
fn detect_platform() -> Platform {
    if cfg!(target_os = "macos") {
        return Platform::Macos;
    }
    let has_wsl = wsl::run(&wsl::list_verbose_argv()).map(|o| o.status.success()).unwrap_or(false);
    if has_wsl {
        Platform::Wsl2
    } else {
        Platform::Unsupported
    }
}

fn app_version_string(app: &AppHandle) -> String {
    app.package_info().version.to_string()
}

fn local_base_url(state: &DesktopState) -> url::Url {
    let raw = state
        .proxy
        .registry
        .lock()
        .unwrap()
        .list()
        .iter()
        .find(|c| c.id == smind_daemon_client::proxy::connections::LOCAL_ID)
        .map(|c| c.base_url.clone())
        .unwrap_or_else(|| smind_daemon_client::config::daemon_url_from("").unwrap().to_string());
    url::Url::parse(&raw).expect("smind desktop: local connection base_url is always a valid URL")
}

async fn probe_healthz(base_url: &url::Url) -> (bool, Option<String>) {
    #[derive(serde::Deserialize)]
    struct Healthz {
        version: Option<String>,
    }
    let mut url = base_url.clone();
    url.set_path("/healthz");
    url.set_query(None);
    let client = reqwest::Client::new();
    match client.get(url).timeout(std::time::Duration::from_secs(2)).send().await {
        Ok(resp) if resp.status().is_success() => {
            let version = resp.json::<Healthz>().await.ok().and_then(|h| h.version);
            (true, version)
        }
        _ => (false, None),
    }
}

fn now_unix_seconds() -> String {
    SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs().to_string()).unwrap_or_else(|_| "0".to_string())
}

// ---------------------------------------------------------------------------
// macOS native
// ---------------------------------------------------------------------------

fn macos_layout(app: &AppHandle) -> Result<native::Layout, String> {
    let base = app.path().app_data_dir().map_err(|e| e.to_string())?.join("managed-daemon");
    Ok(native::Layout::new(&base))
}

// ---------------------------------------------------------------------------
// WSL2
// ---------------------------------------------------------------------------

/// resolve_distro detects the default WSL distro once per app run and
/// caches it -- the default distro is not expected to change while the
/// app is running.
fn resolve_distro(state: &DesktopState) -> Result<String, String> {
    let mut cache = state.daemon_manager_distro.lock().unwrap();
    if let Some(d) = cache.as_ref() {
        return Ok(d.clone());
    }
    let output = wsl::run(&wsl::list_verbose_argv()).map_err(|e| format!("wsl.exe not available: {e}"))?;
    let distro = wsl::parse_default_distro(&output.stdout).ok_or_else(|| "no default WSL distro found".to_string())?;
    *cache = Some(distro.clone());
    Ok(distro)
}

fn wsl_load_managed(distro: &str) -> Option<ManagedRecord> {
    let out = wsl::run(&wsl::cat_argv(distro, wsl::STATE_SUBPATH)).ok()?;
    if !out.status.success() {
        return None;
    }
    serde_json::from_slice(&out.stdout).ok()
}

fn wsl_save_managed(distro: &str, record: &ManagedRecord) -> Result<(), String> {
    let json = serde_json::to_string(record).map_err(|e| e.to_string())?;
    let b64 = base64::Engine::encode(&base64::engine::general_purpose::STANDARD, json.as_bytes());
    let out = wsl::run(&wsl::write_file_base64_argv(distro, wsl::STATE_SUBPATH, &b64)).map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(format!("failed to write managed state: {}", String::from_utf8_lossy(&out.stderr)));
    }
    Ok(())
}

fn wsl_arch(distro: &str) -> Result<&'static str, String> {
    let out = wsl::run(&wsl::uname_arch_argv(distro)).map_err(|e| e.to_string())?;
    release::linux_target(&String::from_utf8_lossy(&out.stdout))
}

fn wsl_install(distro: &str, urls: &release::ReleaseUrls) -> Result<(), String> {
    let run = |argv: Vec<String>, what: &str| -> Result<std::process::Output, String> {
        let out = wsl::run(&argv).map_err(|e| format!("{what}: {e}"))?;
        if !out.status.success() {
            return Err(format!("{what} failed: {}", String::from_utf8_lossy(&out.stderr)));
        }
        Ok(out)
    };

    run(wsl::mkdir_argv(distro), "mkdir")?;
    run(wsl::curl_download_argv(distro, &urls.checksums, "checksums.txt"), "download checksums.txt")?;
    let checksums_out = run(wsl::cat_argv(distro, "checksums.txt"), "read checksums.txt")?;
    let map = smind_daemon_client::daemon_manager::checksums::parse(&String::from_utf8_lossy(&checksums_out.stdout));
    let expected_hex = map.get(&urls.asset_name).ok_or_else(|| format!("release has no asset {:?} for this platform", urls.asset_name))?;

    run(wsl::curl_download_argv(distro, &urls.tarball, "download.tar.gz"), "download tarball")?;
    let sha_out = run(wsl::sha256sum_argv(distro, "download.tar.gz"), "sha256sum")?;
    let actual_hex = wsl::parse_sha256sum_output(&String::from_utf8_lossy(&sha_out.stdout))
        .ok_or_else(|| "could not parse sha256sum output".to_string())?;
    if !smind_daemon_client::daemon_manager::checksums::hex_eq(&actual_hex, expected_hex) {
        return Err(format!("checksum mismatch for {:?} -- refusing to install a corrupted download", urls.asset_name));
    }

    run(wsl::tar_extract_argv(distro, "download.tar.gz"), "extract")?;
    run(wsl::chmod_x_argv(distro), "chmod")?;
    Ok(())
}

fn wsl_start(distro: &str) -> Result<(), String> {
    let out = wsl::run(&wsl::start_detached_argv(distro)).map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(format!("failed to start daemon: {}", String::from_utf8_lossy(&out.stderr)));
    }
    Ok(())
}

/// wsl_binary_installed answers `restart`'s precondition (and
/// `compute_status`'s `binary_installed` field) for WSL2 -- `test -x`'s
/// exit status alone, no output to parse.
fn wsl_binary_installed(distro: &str) -> bool {
    wsl::run(&wsl::test_executable_argv(distro)).map(|o| o.status.success()).unwrap_or(false)
}

// ---------------------------------------------------------------------------
// Shared: port ownership, status, and the four commands
// ---------------------------------------------------------------------------

fn find_port_owner(platform: Platform, distro: Option<&str>, port: u16) -> Option<u32> {
    match platform {
        Platform::Macos => native::find_port_owner(port).ok().flatten(),
        Platform::Wsl2 => {
            let distro = distro?;
            let out = wsl::run(&wsl::port_owner_argv(distro, port)).ok()?;
            wsl::parse_ss_pid(&String::from_utf8_lossy(&out.stdout))
        }
        Platform::Unsupported => None,
    }
}

/// binary_installed answers "is the managed binary on disk (and, on
/// WSL2, executable) right now" -- `restart`'s precondition
/// (`managed::assert_restartable`) and `compute_status`'s
/// `binary_installed` field share this one check.
fn binary_installed(app: &AppHandle, platform: Platform, distro: Option<&str>) -> bool {
    match platform {
        Platform::Macos => macos_layout(app).ok().map(|l| l.bin_path.exists()).unwrap_or(false),
        Platform::Wsl2 => distro.map(wsl_binary_installed).unwrap_or(false),
        Platform::Unsupported => false,
    }
}

/// wsl_resolve_exe_path resolves the pid's real executable via
/// `/proc/<pid>/exe` (through `wsl.exe`). `None` on any lookup failure --
/// callers must never treat "couldn't confirm" as "confirmed" (Bug 1's
/// exact failure mode).
fn wsl_resolve_exe_path(distro: &str, pid: u32) -> Option<String> {
    let out = wsl::run(&wsl::exe_path_argv(distro, pid)).ok()?;
    wsl::parse_exe_path(&String::from_utf8_lossy(&out.stdout))
}

/// wsl_exe_matches_expected checks `pid`'s real executable against a
/// specific previously-recorded path -- used before signalling a
/// previously-managed (or previously-adopted, via take-over) pid, where
/// `expected` is that record's own `exe_path`, not necessarily the bin
/// this app would install.
fn wsl_exe_matches_expected(distro: &str, pid: u32, expected: &str) -> bool {
    wsl_resolve_exe_path(distro, pid).map(|p| wsl::exe_path_matches(&p, expected)).unwrap_or(false)
}

/// macos_exe_matches resolves `pid`'s real command via `ps` and compares
/// it against `expected` -- either a previously-recorded `exe_path`
/// (pre-kill checks) or the managed `Layout`'s own binary path (post-
/// start checks); macOS always knows the managed path as a concrete
/// literal, so unlike WSL2 this one function serves both purposes.
fn macos_exe_matches(pid: u32, expected: &Path) -> bool {
    native::exe_path_for_pid(pid).ok().flatten().map(|p| native::exe_matches(&p, expected)).unwrap_or(false)
}

fn log_path_display(app: &AppHandle, platform: Platform, distro: Option<&str>) -> Option<String> {
    match platform {
        Platform::Macos => macos_layout(app).ok().map(|l| l.log_path.display().to_string()),
        Platform::Wsl2 => distro.map(|d| format!("~/{} (inside WSL distro {d:?})", wsl::LOG_SUBPATH)),
        Platform::Unsupported => None,
    }
}

async fn compute_status(app: &AppHandle, state: &DesktopState) -> DaemonStatus {
    let platform = detect_platform();
    let app_version = app_version_string(app);
    let base_url = local_base_url(state);
    let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);

    let (reachable, daemon_version) = probe_healthz(&base_url).await;

    let distro = if platform == Platform::Wsl2 { resolve_distro(state).ok() } else { None };

    let record = match platform {
        Platform::Macos => macos_layout(app).ok().and_then(|l| managed::load(&l.state_path)),
        Platform::Wsl2 => distro.as_deref().and_then(wsl_load_managed),
        Platform::Unsupported => None,
    };

    let port_owner = find_port_owner(platform, distro.as_deref(), port);
    let managed_state = managed::classify(record.as_ref(), port_owner);

    let comparison = daemon_version.as_deref().map(|d| version::compare(&app_version, d)).unwrap_or(version::Comparison::Unknown);

    DaemonStatus {
        platform,
        reachable,
        daemon_version,
        app_version,
        comparison,
        managed_state,
        pid: port_owner,
        installed_path: record.as_ref().map(|r| r.exe_path.clone()),
        log_path: log_path_display(app, platform, distro.as_deref()),
        binary_installed: binary_installed(app, platform, distro.as_deref()),
    }
}

async fn install_or_update(app: &AppHandle, state: &DesktopState) -> Result<DaemonStatus, String> {
    let platform = detect_platform();
    let app_version = app_version_string(app);
    let base_url = local_base_url(state);
    let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);

    match platform {
        Platform::Macos => {
            let layout = macos_layout(app)?;
            let existing_record = managed::load(&layout.state_path);

            // Bug 1: refuse outright, before touching the disk or the
            // network, if the port is already owned by a daemon this app
            // doesn't manage (e.g. the user's own `./bin/smind serve`).
            // Only `take_over` (explicit UI confirmation) may adopt it.
            let port_owner_before = native::find_port_owner(port).ok().flatten();
            managed::assert_safe_to_install(existing_record.as_ref(), port_owner_before, port)?;

            emit_progress(app, "downloading", "Downloading the daemon release…");
            let (os, arch) = release::native_target(std::env::consts::OS, std::env::consts::ARCH)?;
            let urls = release::release_urls(&app_version, os, arch);
            let client = reqwest::Client::new();
            native::install_from_release(&client, &urls, &layout).await.map_err(|e| e.to_string())?;

            emit_progress(app, "starting", "Starting the daemon…");
            // Bug 2: only signal the previously-managed pid if it's still
            // the live port owner *and* really our binary -- a bare
            // liveness check (the pid merely responds to signal 0) is not
            // enough, since a pid can be reused by an unrelated process.
            if let Some(record) = &existing_record {
                // Compare against *this record's own* exe_path, not the
                // managed layout path -- a take-over's record.exe_path is
                // the adopted process's real (non-managed) path, and it
                // must still be recognized as "safe to kill" so Update
                // can actually replace it.
                let exe_matches = macos_exe_matches(record.pid, Path::new(&record.exe_path));
                if managed::safe_to_kill(record, port_owner_before, exe_matches) {
                    let _ = native::kill_process(record.pid);
                } else if port_owner_before.is_some() {
                    emit_progress(app, "starting", "The previously-managed process could not be verified; starting alongside it instead of stopping it.");
                }
            }

            let child = native::spawn_detached(&layout).map_err(|e| e.to_string())?;
            drop(child); // detached (its own process group); tracked by pid below, not by this handle.

            // Don't trust "whoever now owns the port" on its own -- verify
            // it's really the binary just installed (and, since /healthz
            // answered, that it reports the version just installed)
            // before saving a ManagedRecord. This is what would have
            // caught Bug 1: if the new process failed to bind because the
            // port was already taken, the port's owner is still the old
            // occupant, whose exe path won't match.
            tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            let (_, daemon_version) = probe_healthz(&base_url).await;
            let port_owner_after = native::find_port_owner(port).ok().flatten();
            let exe_matches_after = port_owner_after.map(|pid| macos_exe_matches(pid, &layout.bin_path)).unwrap_or(false);
            let pid = managed::verify_fresh_start(port_owner_after, exe_matches_after, daemon_version.as_deref(), &app_version)?;

            let record = ManagedRecord { pid, version: app_version.clone(), installed_at: now_unix_seconds(), exe_path: layout.bin_path.display().to_string() };
            managed::save(&layout.state_path, &record).map_err(|e| e.to_string())?;
        }
        Platform::Wsl2 => {
            let distro = resolve_distro(state)?;
            let existing_record = wsl_load_managed(&distro);

            // Bug 1, same guard as the macOS branch above.
            let port_owner_before = find_port_owner(platform, Some(&distro), port);
            managed::assert_safe_to_install(existing_record.as_ref(), port_owner_before, port)?;

            emit_progress(app, "downloading", "Downloading the daemon release…");
            let arch = wsl_arch(&distro)?;
            let urls = release::release_urls(&app_version, "linux", arch);
            wsl_install(&distro, &urls)?;

            emit_progress(app, "starting", "Starting the daemon…");
            // Bug 2, same guard as the macOS branch above.
            if let Some(record) = &existing_record {
                // Same reasoning as the macOS branch: compare against
                // this record's own exe_path (the adopted path, for a
                // take-over), not the managed bin's.
                let exe_matches = wsl_exe_matches_expected(&distro, record.pid, &record.exe_path);
                if managed::safe_to_kill(record, port_owner_before, exe_matches) {
                    let _ = wsl::run(&wsl::kill_argv(&distro, record.pid));
                } else if port_owner_before.is_some() {
                    emit_progress(app, "starting", "The previously-managed process could not be verified; starting alongside it instead of stopping it.");
                }
            }

            wsl_start(&distro)?;

            // Bug 1's post-start half: don't trust "whoever now owns the
            // port" -- verify identity (and reported version) before
            // saving a ManagedRecord, exactly as the macOS branch does.
            tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            let (_, daemon_version) = probe_healthz(&base_url).await;
            let port_owner_after = find_port_owner(platform, Some(&distro), port);
            // Resolve (not just check) the new process's real path, so
            // the saved record's exe_path is a real absolute path future
            // pre-kill checks can compare against -- never the "~/..."
            // literal, which `readlink -f` could never match exactly.
            let resolved_exe_after = port_owner_after.and_then(|pid| wsl_resolve_exe_path(&distro, pid));
            let exe_matches_after = resolved_exe_after.as_deref().map(wsl::exe_path_matches_managed_bin).unwrap_or(false);
            let pid = managed::verify_fresh_start(port_owner_after, exe_matches_after, daemon_version.as_deref(), &app_version)?;

            let record = ManagedRecord {
                pid,
                version: app_version.clone(),
                installed_at: now_unix_seconds(),
                exe_path: resolved_exe_after.expect("smind desktop: verify_fresh_start only succeeds when exe_matches_after is true, which requires Some"),
            };
            wsl_save_managed(&distro, &record)?;
        }
        Platform::Unsupported => {
            return Err("this platform isn't supported for app-managed daemon install (Windows without WSL2, or an unsupported host)".to_string());
        }
    }

    emit_progress(app, "installing", "Done.");
    Ok(compute_status(app, state).await)
}

async fn restart(app: &AppHandle, state: &DesktopState) -> Result<DaemonStatus, String> {
    let platform = detect_platform();
    let base_url = local_base_url(state);
    let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);

    let distro = if platform == Platform::Wsl2 { Some(resolve_distro(state)?) } else { None };
    let record = match platform {
        Platform::Macos => macos_layout(app).ok().and_then(|l| managed::load(&l.state_path)),
        Platform::Wsl2 => distro.as_deref().and_then(wsl_load_managed),
        Platform::Unsupported => return Err("this platform isn't supported for app-managed daemon restart".to_string()),
    };
    let Some(record) = record else {
        return Err("no managed daemon to restart -- install it first".to_string());
    };

    // Precondition, checked before touching any process: a bare take-over
    // (no prior Install/Update) records an adopted pid that passes the
    // identity check below, but there is no managed binary on disk for
    // this function's own start step (which only ever execs the managed
    // bin) to start in its place -- refuse rather than kill the adopted
    // daemon and leave nothing running.
    managed::assert_restartable(binary_installed(app, platform, distro.as_deref()))?;

    // Re-check immediately before signalling: refuse unless the recorded
    // pid is still the real port owner (closes a PID-reuse race) *and*
    // really our binary (Bug 2's guard -- a bare port-ownership match
    // isn't enough on its own, since the pid could have been reused by an
    // unrelated process, including the user's own unmanaged daemon).
    let live_owner = find_port_owner(platform, distro.as_deref(), port);
    // Compare against this record's own exe_path -- for a normal managed
    // daemon that's already the managed bin's path, and for one reached
    // via take-over it's the adopted process's real path, so a take-over
    // followed directly by Restart still passes this check correctly.
    let exe_matches = match platform {
        Platform::Macos => macos_exe_matches(record.pid, Path::new(&record.exe_path)),
        Platform::Wsl2 => distro.as_deref().map(|d| wsl_exe_matches_expected(d, record.pid, &record.exe_path)).unwrap_or(false),
        Platform::Unsupported => unreachable!(),
    };
    if !managed::safe_to_kill(&record, live_owner, exe_matches) {
        return Err("the managed daemon's pid no longer matches what's actually running -- refusing to restart (use status to re-check)".to_string());
    }

    emit_progress(app, "stopping", "Stopping the daemon…");
    match platform {
        Platform::Macos => {
            native::kill_process(record.pid).map_err(|e| e.to_string())?;
        }
        Platform::Wsl2 => {
            let distro = distro.as_deref().unwrap();
            wsl::run(&wsl::kill_argv(distro, record.pid)).map_err(|e| e.to_string())?;
        }
        Platform::Unsupported => unreachable!(),
    }

    emit_progress(app, "starting", "Starting the daemon…");
    match platform {
        Platform::Macos => {
            let layout = macos_layout(app)?;
            let child = native::spawn_detached(&layout).map_err(|e| e.to_string())?;
            drop(child);

            tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            let port_owner_after = native::find_port_owner(port).ok().flatten();
            let exe_matches_after = port_owner_after.map(|pid| macos_exe_matches(pid, &layout.bin_path)).unwrap_or(false);
            let pid = managed::verify_started_identity(port_owner_after, exe_matches_after)?;

            // `spawn_detached` always execs `layout.bin_path` -- refresh
            // exe_path to that (never inherit the old record's, which for
            // a take-over-then-restart would still be the adopted path).
            let new_record = ManagedRecord { pid, exe_path: layout.bin_path.display().to_string(), installed_at: now_unix_seconds(), ..record };
            managed::save(&layout.state_path, &new_record).map_err(|e| e.to_string())?;
        }
        Platform::Wsl2 => {
            let distro = distro.as_deref().unwrap();
            wsl_start(distro)?;

            tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            let port_owner_after = find_port_owner(platform, Some(distro), port);
            let resolved_exe_after = port_owner_after.and_then(|pid| wsl_resolve_exe_path(distro, pid));
            let exe_matches_after = resolved_exe_after.as_deref().map(wsl::exe_path_matches_managed_bin).unwrap_or(false);
            let pid = managed::verify_started_identity(port_owner_after, exe_matches_after)?;

            // `start_detached_argv` always execs the managed bin -- refresh
            // exe_path to the freshly-resolved path (never inherit the old
            // record's, which for a take-over-then-restart would still be
            // the adopted path).
            let new_record = ManagedRecord {
                pid,
                exe_path: resolved_exe_after.expect("smind desktop: verify_started_identity only succeeds when exe_matches_after is true, which requires Some"),
                installed_at: now_unix_seconds(),
                ..record
            };
            wsl_save_managed(distro, &new_record)?;
        }
        Platform::Unsupported => unreachable!(),
    }

    Ok(compute_status(app, state).await)
}

async fn take_over(app: &AppHandle, state: &DesktopState) -> Result<DaemonStatus, String> {
    let platform = detect_platform();
    let base_url = local_base_url(state);
    let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);
    let distro = if platform == Platform::Wsl2 { Some(resolve_distro(state)?) } else { None };

    let owner = find_port_owner(platform, distro.as_deref(), port)
        .ok_or_else(|| "nothing is listening on the configured port -- there is no daemon to take over".to_string())?;

    // Resolve the adopted process's real executable path up front: this
    // becomes the record's own identity check from now on
    // (safe_to_kill/verify_started_identity compare against it), so a
    // take-over that can't resolve it must refuse outright rather than
    // store a placeholder that could never pass an identity check again
    // -- which would make Update/Restart permanently refuse afterwards.
    let exe_path = match platform {
        Platform::Macos => native::exe_path_for_pid(owner).ok().flatten(),
        Platform::Wsl2 => wsl_resolve_exe_path(distro.as_deref().unwrap(), owner),
        Platform::Unsupported => None,
    };
    let Some(exe_path) = exe_path else {
        return Err(format!(
            "could not resolve the executable for pid {owner} -- refusing to take over management (it may have already exited, or you may lack permission to inspect it)"
        ));
    };

    let record = managed::take_over(owner, "unknown", &exe_path, &now_unix_seconds());
    match platform {
        Platform::Macos => {
            let layout = macos_layout(app)?;
            managed::save(&layout.state_path, &record).map_err(|e| e.to_string())?;
        }
        Platform::Wsl2 => {
            wsl_save_managed(distro.as_deref().unwrap(), &record)?;
        }
        Platform::Unsupported => return Err("this platform isn't supported for app-managed daemon takeover".to_string()),
    }

    Ok(compute_status(app, state).await)
}

// ---------------------------------------------------------------------------
// Tauri commands
// ---------------------------------------------------------------------------

#[tauri::command]
pub async fn daemon_status(app: AppHandle, state: tauri::State<'_, DesktopState>) -> Result<DaemonStatus, String> {
    Ok(compute_status(&app, &state).await)
}

#[tauri::command]
pub async fn daemon_install(app: AppHandle, state: tauri::State<'_, DesktopState>) -> Result<DaemonStatus, String> {
    install_or_update(&app, &state).await
}

#[tauri::command]
pub async fn daemon_update(app: AppHandle, state: tauri::State<'_, DesktopState>) -> Result<DaemonStatus, String> {
    install_or_update(&app, &state).await
}

#[tauri::command]
pub async fn daemon_restart(app: AppHandle, state: tauri::State<'_, DesktopState>) -> Result<DaemonStatus, String> {
    restart(&app, &state).await
}

#[tauri::command]
pub async fn take_over_daemon(app: AppHandle, state: tauri::State<'_, DesktopState>) -> Result<DaemonStatus, String> {
    take_over(&app, &state).await
}

/// connection_version reports version skew for *any* saved connection by
/// id, not just the local one -- the banner (AC7) needs this for the
/// "remote/url/relay connections just get a notice" case, since the
/// loopback proxy doesn't forward `/healthz` at all.
#[tauri::command]
pub async fn connection_version(app: AppHandle, state: tauri::State<'_, DesktopState>, id: String) -> Result<ConnectionVersionInfo, String> {
    let base_url = {
        let registry = state.proxy.registry.lock().unwrap();
        registry.list().iter().find(|c| c.id == id).map(|c| c.base_url.clone())
    }
    .ok_or_else(|| format!("no connection with id {id:?}"))?;
    let url = url::Url::parse(&base_url).map_err(|e| e.to_string())?;
    let app_version = app_version_string(&app);
    let (reachable, daemon_version) = probe_healthz(&url).await;
    let comparison = daemon_version.as_deref().map(|d| version::compare(&app_version, d)).unwrap_or(version::Comparison::Unknown);
    Ok(ConnectionVersionInfo { reachable, daemon_version, app_version, comparison })
}
