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
    }
}

async fn install_or_update(app: &AppHandle, state: &DesktopState) -> Result<DaemonStatus, String> {
    let platform = detect_platform();
    let app_version = app_version_string(app);

    emit_progress(app, "downloading", "Downloading the daemon release…");

    match platform {
        Platform::Macos => {
            let (os, arch) = release::native_target(std::env::consts::OS, std::env::consts::ARCH)?;
            let urls = release::release_urls(&app_version, os, arch);
            let layout = macos_layout(app)?;

            let client = reqwest::Client::new();
            native::install_from_release(&client, &urls, &layout).await.map_err(|e| e.to_string())?;

            emit_progress(app, "starting", "Starting the daemon…");
            if let Some(record) = managed::load(&layout.state_path) {
                if native::is_pid_alive(record.pid) {
                    let _ = native::kill_process(record.pid);
                }
            }
            let child = native::spawn_detached(&layout).map_err(|e| e.to_string())?;
            let record = ManagedRecord {
                pid: child.id(),
                version: app_version.clone(),
                installed_at: now_unix_seconds(),
                exe_path: layout.bin_path.display().to_string(),
            };
            managed::save(&layout.state_path, &record).map_err(|e| e.to_string())?;
        }
        Platform::Wsl2 => {
            let distro = resolve_distro(state)?;
            let arch = wsl_arch(&distro)?;
            let urls = release::release_urls(&app_version, "linux", arch);

            wsl_install(&distro, &urls)?;

            emit_progress(app, "starting", "Starting the daemon…");
            if let Some(record) = wsl_load_managed(&distro) {
                let _ = wsl::run(&wsl::kill_argv(&distro, record.pid));
            }
            wsl_start(&distro)?;

            // The pid isn't reliably recoverable from the detach script
            // itself (see AC4's design note); resolve it the same way
            // "managed vs unmanaged" does, by asking who now owns the
            // configured port, after giving the daemon a moment to bind.
            tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            let base_url = local_base_url(state);
            let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);
            let pid = find_port_owner(platform, Some(&distro), port)
                .ok_or_else(|| "daemon did not start (nothing is listening on the configured port)".to_string())?;
            let record = ManagedRecord {
                pid,
                version: app_version.clone(),
                installed_at: now_unix_seconds(),
                exe_path: format!("~/{}", wsl::BIN_SUBPATH),
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

    // Re-check immediately before signalling: refuse if the recorded pid
    // is no longer the real port owner (AC3's PID-reuse guard).
    let live_owner = find_port_owner(platform, distro.as_deref(), port);
    if live_owner != Some(record.pid) {
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
            let new_record = ManagedRecord { pid: child.id(), installed_at: now_unix_seconds(), ..record };
            managed::save(&layout.state_path, &new_record).map_err(|e| e.to_string())?;
        }
        Platform::Wsl2 => {
            let distro = distro.as_deref().unwrap();
            wsl_start(distro)?;
            tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            let pid = find_port_owner(platform, Some(distro), port)
                .ok_or_else(|| "daemon did not restart (nothing is listening on the configured port)".to_string())?;
            let new_record = ManagedRecord { pid, installed_at: now_unix_seconds(), ..record };
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

    let record = managed::take_over(owner, "unknown", "unknown", &now_unix_seconds());
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
