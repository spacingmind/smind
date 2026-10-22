//! ADR-0013 part D2 / ADR-0030: install/update/restart the local daemon
//! the app manages, natively on Windows and macOS. Orchestrates the pure
//! logic in `smind_daemon_client::daemon_manager` with the OS calls it
//! needs (spawning the daemon process, hitting the network) plus Tauri
//! glue (app data dir, event emission, app version). See
//! `docs/plans/completed/desktop-managed-daemon.md`.

use std::time::{SystemTime, UNIX_EPOCH};

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager};

use smind_daemon_client::daemon_manager::managed::{self, ManagedRecord, ManagedState};
use std::path::{Path, PathBuf};
use smind_daemon_client::daemon_manager::{native, version};
#[cfg(target_os = "windows")]
use smind_daemon_client::daemon_manager::windows_native;

use crate::app_settings::AppSettings;
use crate::state::DesktopState;

const PROGRESS_EVENT: &str = "daemon-progress";
const DEFAULT_PORT: u16 = 4648;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
pub enum Platform {
    #[serde(rename = "macos")]
    Macos,
    #[serde(rename = "windows")]
    Windows,
    #[serde(rename = "unsupported")]
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
    /// `/healthz` `managedBy` (ADR-0031 P8): the OS service manager
    /// ("systemd", "launchd", "taskscheduler") that supervises the daemon, when one
    /// does. A service-managed daemon is never taken over or killed by the
    /// app -- the service would just restart it.
    pub managed_by: Option<String>,
    pub pid: Option<u32>,
    pub installed_path: Option<String>,
    pub log_path: Option<String>,
    /// Whether the *managed* binary exists on disk right now --
    /// distinct from `managed_state`, since a bare take-over (no prior
    /// Install/Update) can be `Managed` with no managed binary installed
    /// at all. Drives whether the UI's Restart action is enabled
    /// (`managed::assert_restartable`'s precondition).
    pub binary_installed: bool,
    /// Windows only (AC6): something the app did not start is answering
    /// on the port and its owner is not an identifiable `smind.exe` --
    /// typically the WSL relay forwarding a daemon inside a distro. The
    /// UI explains what to do and hides take-over.
    pub wsl_daemon_suspected: bool,
    /// Windows only (AC8): whether Git for Windows was found (`null` on
    /// other platforms). Install/start work without it; this only
    /// surfaces the prerequisite.
    pub git_found: Option<bool>,
    /// Set when a prerequisite is missing (AC8), e.g. Git for Windows.
    pub prerequisite_message: Option<String>,
    /// The `service` field of the local port's `/healthz` answer
    /// (`"smind"` for a daemon); `None` when nothing answers or the answer
    /// does not say. Lets first-run name "something that is not smind".
    pub local_service: Option<String>,
    /// Something owns the local port but `/healthz` did not answer: a
    /// non-smind or non-HTTP process. First-run disables "Run here".
    pub port_occupied: bool,
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
    /// `/healthz` `managedBy`; `None` when absent or not reachable.
    pub managed_by: Option<String>,
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

/// detect_platform is a plain `target_os` mapping (ADR-0030): macOS and
/// Windows are native hosts, everything else is Unsupported. Nothing is
/// probed at runtime, so there is no per-call process spawn left to
/// cache.
pub(crate) fn detect_platform() -> Platform {
    platform_for(std::env::consts::OS)
}

/// platform_for is the pure decision: macOS and Windows are native hosts,
/// every other OS -- Linux, WSLg included -- is Unsupported: its daemon
/// runs natively and there is no Windows host to translate paths for or
/// spawn from.
fn platform_for(target_os: &str) -> Platform {
    match target_os {
        "macos" => Platform::Macos,
        "windows" => Platform::Windows,
        _ => Platform::Unsupported,
    }
}

/// UnmanagedKind classifies who owns the daemon port when it isn't the
/// process this app recorded (AC6): only a process whose image file is the
/// platform's smind binary (`smind.exe` on Windows, `smind` on macOS) is
/// take-over eligible. Anything else -- the WSL relay that forwards a WSL
/// daemon's port, an `ssh -L` tunnel, an unresolvable owner (gone or access
/// denied), an unrelated app -- is not, and is never signalled or adopted.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UnmanagedKind {
    /// The port owner's image file name is the platform's smind binary.
    NativeSmind,
    /// Not the smind binary (or not resolvable at all).
    ForeignOrWsl,
    /// `/healthz` reports `managedBy`: an OS service manager (systemd,
    /// launchd, Task Scheduler) owns the daemon. Not take-over eligible
    /// whatever the port owner looks like, because killing it only makes
    /// the service restart it.
    Service,
}

fn classify_unmanaged(platform: Platform, owner_image: Option<&str>, managed_by: Option<&str>) -> UnmanagedKind {
    if managed_by.is_some_and(|m| !m.is_empty()) {
        return UnmanagedKind::Service;
    }
    let is_smind = owner_image
        .map(|img| {
            let name = img.rsplit(['\\', '/']).next().unwrap_or(img);
            match platform {
                Platform::Windows => name.eq_ignore_ascii_case("smind.exe"),
                _ => name == "smind",
            }
        })
        .unwrap_or(false);
    if is_smind { UnmanagedKind::NativeSmind } else { UnmanagedKind::ForeignOrWsl }
}

/// managed_by_label is the display name of a `/healthz` `managedBy` value.
fn managed_by_label(managed_by: &str) -> &str {
    match managed_by {
        // `taskscheduler` is what the Windows scheduled task sets (ADR-0031 P9).
        "taskscheduler" | "task" => "Task Scheduler",
        "docker" => "Docker",
        other => other,
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

/// Health is what `/healthz` answered: `reachable` is any 2xx; the other
/// fields are whatever the JSON body named (all `None` for a non-JSON answer).
#[derive(Debug, Clone, Default, PartialEq, Eq)]
struct Health {
    reachable: bool,
    version: Option<String>,
    service: Option<String>,
    managed_by: Option<String>,
}

async fn probe_health(base_url: &url::Url) -> Health {
    #[derive(serde::Deserialize)]
    struct Healthz {
        version: Option<String>,
        service: Option<String>,
        #[serde(rename = "managedBy")]
        managed_by: Option<String>,
    }
    let mut url = base_url.clone();
    url.set_path("/healthz");
    url.set_query(None);
    let client = reqwest::Client::new();
    match client.get(url).timeout(std::time::Duration::from_secs(2)).send().await {
        Ok(resp) if resp.status().is_success() => {
            let body = smind_daemon_client::proxy::bounded::read_json::<Healthz>(resp, smind_daemon_client::proxy::bounded::MAX_BODY_BYTES).await;
            Health {
                reachable: true,
                version: body.as_ref().and_then(|h| h.version.clone()),
                service: body.as_ref().and_then(|h| h.service.clone()),
                managed_by: body.and_then(|h| h.managed_by).filter(|m| !m.is_empty()),
            }
        }
        _ => Health::default(),
    }
}

/// poll_until_ready calls `probe` up to `attempts` times, `interval` apart
/// (no wait before the first), and returns the first `Some`.
async fn poll_until_ready<T, F, Fut>(mut probe: F, attempts: u32, interval: std::time::Duration) -> Option<T>
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = Option<T>>,
{
    for attempt in 0..attempts {
        if attempt > 0 {
            tokio::time::sleep(interval).await;
        }
        if let Some(v) = probe().await {
            return Some(v);
        }
    }
    None
}

/// wait_for_healthz waits (bounded, ~10s) for a freshly spawned daemon to
/// answer `/healthz` and returns the version it reports. A fixed short
/// sleep isn't enough: a first start creates the database and can take
/// well over half a second, and verifying too early would record nothing
/// and leave a perfectly good daemon looking unmanaged.
async fn wait_for_healthz(base_url: &url::Url) -> Option<String> {
    poll_until_ready(
        || async { Some(probe_health(base_url).await).filter(|p| p.reachable).map(|p| p.version) },
        40,
        std::time::Duration::from_millis(250),
    )
    .await
    .flatten()
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

/// `native::spawn_detached` may wait on the login-shell PATH resolution
/// (bounded; normally already prewarmed at app start), so keep that off the
/// async worker threads.
async fn spawn_native_daemon(layout: &native::Layout) -> Result<std::process::Child, String> {
    let layout = layout.clone();
    tauri::async_runtime::spawn_blocking(move || native::spawn_detached(&layout))
        .await
        .map_err(|e| e.to_string())?
        .map_err(|e| e.to_string())
}

/// Where install/update gets the daemon binary from. macOS takes it from
/// inside the app bundle (desktop-macos-app M2: the release the app itself
/// shipped with, so app and daemon versions always match); Windows does the
/// same with the sidecar `smind.exe` the installer places next to the app
/// (ADR-0030 amendment). Only an unsupported host has no source.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum InstallSource {
    Bundled,
    Release,
}

fn install_source(platform: Platform) -> InstallSource {
    match platform {
        Platform::Macos | Platform::Windows => InstallSource::Bundled,
        Platform::Unsupported => InstallSource::Release,
    }
}

/// bundled_daemon_path is where Tauri's `externalBin` puts the sidecar
/// (`tauri.{macos,windows}.conf.json`): next to the app's own executable,
/// i.e. `smind.app/Contents/MacOS/smind` or `<install dir>\smind.exe`.
fn bundled_daemon_path(exe_dir: &Path) -> PathBuf {
    exe_dir.join(if cfg!(windows) { "smind.exe" } else { "smind" })
}

/// stage_task_hint names the task that stages a daemon into the app for
/// the host this build targets, for the "empty placeholder" error.
fn stage_task_hint() -> &'static str {
    if cfg!(windows) {
        "task desktop:win:daemon"
    } else {
        "task desktop:mac"
    }
}

fn bundled_daemon_path_for_app() -> Result<PathBuf, String> {
    let exe = std::env::current_exe().map_err(|e| format!("could not locate the app executable: {e}"))?;
    let exe_dir = exe.parent().ok_or_else(|| "the app executable has no parent directory".to_string())?;
    Ok(bundled_daemon_path(exe_dir))
}

/// install_bundled_binary copies the app's bundled daemon into the managed
/// layout and makes it executable. It *copies* rather than running the
/// binary in place: an unsigned app opened from Downloads is
/// App-Translocated to a random read-only path, which would break the
/// exe-path match checks (and vanish when the app moves). The copy is
/// staged next to its destination and renamed into place, so replacing the
/// binary of a still-running daemon (an update) never rewrites the file it
/// is executing from. A missing or empty source -- the latter is the
/// placeholder `build.rs` writes for dev builds -- is a clear error, never
/// a download fallback. `bin_path` is the layout's destination
/// (`bin/smind` or `bin\smind.exe`); the staging file is that name plus
/// `.new` (`smind.exe.new`), so it can never be mistaken for the binary.
fn install_bundled_binary(bundled: &Path, bin_path: &Path) -> Result<(), String> {
    let meta = std::fs::metadata(bundled)
        .map_err(|e| format!("the bundled daemon is missing from the app ({}: {e}) -- reinstall smind", bundled.display()))?;
    if !meta.is_file() || meta.len() == 0 {
        return Err(format!(
            "the bundled daemon at {} is empty -- this build has no daemon staged (build the app with `{}`)",
            bundled.display(),
            stage_task_hint()
        ));
    }
    let bin_dir = bin_path.parent().expect("smind desktop: bin_path always has a parent");
    std::fs::create_dir_all(bin_dir).map_err(|e| format!("create {}: {e}", bin_dir.display()))?;

    let mut staging_name = bin_path.file_name().expect("smind desktop: bin_path always has a file name").to_os_string();
    staging_name.push(".new");
    let staging = bin_dir.join(staging_name);
    let _ = std::fs::remove_file(&staging);
    std::fs::copy(bundled, &staging).map_err(|e| format!("copy the bundled daemon to {}: {e}", staging.display()))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&staging, std::fs::Permissions::from_mode(0o755)).map_err(|e| format!("chmod {}: {e}", staging.display()))?;
    }
    // A downloaded (quarantined) app hands its quarantine flag to the copy;
    // best-effort clear so Gatekeeper never judges the spawned daemon.
    #[cfg(target_os = "macos")]
    let _ = std::process::Command::new("xattr").args(["-d", "com.apple.quarantine"]).arg(&staging).output();
    // Windows refuses to rename over a running exe, and Defender or the
    // search indexer can briefly hold a freshly written one: retry a few
    // times, then keep the old binary and say why instead of leaving a bare
    // OS error.
    let mut attempt = 0;
    loop {
        match std::fs::rename(&staging, bin_path) {
            Ok(()) => return Ok(()),
            Err(_) if cfg!(windows) && attempt < RENAME_RETRIES => {
                attempt += 1;
                std::thread::sleep(RENAME_RETRY_DELAY);
            }
            Err(e) => {
                let _ = std::fs::remove_file(&staging);
                let hint = if cfg!(windows) { " (is the daemon still running? stop it first)" } else { "" };
                return Err(format!("install the daemon at {}{hint}: {e}", bin_path.display()));
            }
        }
    }
}

const RENAME_RETRIES: u32 = 5;
const RENAME_RETRY_DELAY: std::time::Duration = std::time::Duration::from_millis(200);

// The startup decision (and its pure helpers, tested on every platform) is only
// used by the macOS-only `auto_start`.

/// LaunchAction is what the app does about the local daemon at startup
/// (desktop-macos-app M2.3/M2.4).
#[cfg(any(target_os = "macos", test))]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum LaunchAction {
    /// Nothing is serving: install the bundled daemon and start it.
    InstallAndStart,
    /// A daemon *we* manage is older than the app: don't restart it here --
    /// the UI owns the update decision, because it knows whether agent runs
    /// are in flight (desktop-macos-app M5).
    OfferUpdate,
    /// Leave everything as it is.
    NoOp,
    /// Something we don't manage holds the port: never touched
    /// automatically -- the existing banner offers take-over from
    /// `managed_state`, nothing else to do here.
    UnmanagedBanner,
}

/// launch_decision is the pure startup policy, over the same three facts
/// `DaemonStatus` already carries. The one rule above all: an unmanaged
/// daemon (the user's own `smind serve`) is never killed automatically.
#[cfg(any(target_os = "macos", test))]
fn launch_decision(reachable: bool, managed_state: ManagedState, comparison: version::Comparison) -> LaunchAction {
    match (reachable, managed_state) {
        (_, ManagedState::Unmanaged) => LaunchAction::UnmanagedBanner,
        (false, ManagedState::NotRunning) => LaunchAction::InstallAndStart,
        // Our own process holds the port but isn't answering (yet): don't
        // pile a second install on top of it.
        (false, ManagedState::Managed) => LaunchAction::NoOp,
        (true, ManagedState::Managed) if comparison == version::Comparison::Older => LaunchAction::OfferUpdate,
        (true, _) => LaunchAction::NoOp,
    }
}

/// startup_decision gates `launch_decision` on the first-run settings
/// (ADR-0031 D7): until onboarding is done, or when the user turned local
/// daemon management off, or when another connection is selected, the app
/// never installs, starts or updates anything.
#[cfg(any(target_os = "macos", test))]
fn startup_decision(
    settings: AppSettings,
    local_selected: bool,
    reachable: bool,
    managed_state: ManagedState,
    comparison: version::Comparison,
) -> LaunchAction {
    if !settings.onboarding_completed || !settings.manage_local_daemon || !local_selected {
        return LaunchAction::NoOp;
    }
    launch_decision(reachable, managed_state, comparison)
}

const MANAGEMENT_OFF: &str = "the app-managed local daemon is turned off in Settings";

/// ensure_managed is the guard on every command that mutates the local
/// daemon (`daemon_install`/`update`/`restart`, `take_over_daemon`).
fn ensure_managed(settings: AppSettings) -> Result<(), String> {
    if settings.manage_local_daemon {
        Ok(())
    } else {
        Err(MANAGEMENT_OFF.to_string())
    }
}

fn guard_management(state: &DesktopState) -> Result<(), String> {
    ensure_managed(*state.app_settings.lock().unwrap())
}

/// auto_start runs once per app launch, after `DesktopState` is managed:
/// installs/starts the bundled daemon when nothing is serving, and
/// updates a managed one that's older than the app, both through the same
/// `install_or_update` (and `daemon-progress` events) the Install/Update
/// buttons use. macOS only, and only while the built-in local connection
/// is the selected one -- someone using a remote daemon didn't ask for a
/// local one, and nothing at all until first-run is complete and local
/// management is on (`startup_decision`). Failures surface as a progress event plus the log; the
/// banner and Settings -> Daemon keep working for a manual retry.
#[cfg(target_os = "macos")]
pub async fn auto_start(app: AppHandle) {
    let state = app.state::<DesktopState>();
    if detect_platform() != Platform::Macos {
        return;
    }
    let settings = *state.app_settings.lock().unwrap();
    if !settings.onboarding_completed || !settings.manage_local_daemon {
        log::info!("daemon auto-start: skipped ({settings:?})");
        return;
    }
    let local_selected = state.proxy.registry.lock().unwrap().current().id == smind_daemon_client::proxy::connections::LOCAL_ID;
    if !local_selected {
        return;
    }
    let status = compute_status(&app, &state).await;
    let action = startup_decision(settings, local_selected, status.reachable, status.managed_state, status.comparison);
    log::info!("daemon auto-start: {action:?} (reachable={}, {:?}, {:?})", status.reachable, status.managed_state, status.comparison);
    if !matches!(action, LaunchAction::InstallAndStart) {
        return;
    }
    match install_or_update(&app, &state).await {
        Ok(_) => {
            // The window usually loaded before the daemon was serving and
            // is sitting on "Can't reach Local" (the UI has no auto-retry),
            // so reload it now -- the same call as View -> Reload.
            if let Some(win) = app.get_webview_window(crate::MAIN_WINDOW) {
                let _ = win.eval("window.location.reload()");
            }
        }
        Err(e) => {
            log::warn!("daemon auto-start failed: {e}");
            emit_progress(&app, "error", format!("Couldn't start the daemon: {e}"));
        }
    }
}

// ---------------------------------------------------------------------------
// Windows native
// ---------------------------------------------------------------------------

/// windows_base_dir resolves the managed layout root's parent (ADR-0030):
/// `%LOCALAPPDATA%`, falling back to `%USERPROFILE%\AppData\Local`. Pure
/// over an injected env getter so the fallback is unit-testable.
#[cfg(target_os = "windows")]
fn windows_base_dir(getenv: impl Fn(&str) -> Option<String>) -> PathBuf {
    if let Some(local) = getenv("LOCALAPPDATA").filter(|s| !s.is_empty()) {
        return PathBuf::from(local);
    }
    let profile = getenv("USERPROFILE").unwrap_or_default();
    Path::new(&profile).join("AppData").join("Local")
}

/// windows_layout is the Windows managed layout (ADR-0030):
/// `%LOCALAPPDATA%\smind` -> `bin/smind.exe`, `smind.log`, `managed.json`.
#[cfg(target_os = "windows")]
fn windows_layout() -> Result<windows_native::Layout, String> {
    Ok(windows_native::Layout::new(&windows_base_dir(|k| std::env::var(k).ok()).join("smind")))
}

/// windows_exe_matches resolves `pid`'s image (IP Helper +
/// `QueryFullProcessImageName`) and compares it against `expected` -- the
/// Windows counterpart of `macos_exe_matches`, serving the same
/// pre-kill/post-start identity checks.
#[cfg(target_os = "windows")]
fn windows_exe_matches(pid: u32, expected: &Path) -> bool {
    windows_native::exe_path_for_pid(pid).ok().flatten().map(|p| windows_native::exe_matches(&p, expected)).unwrap_or(false)
}

/// take_over_refusal is the take-over gate. A service-managed daemon
/// (`/healthz` `managedBy`, ADR-0031 P10) is refused on every platform:
/// stopping or adopting it fights the service manager. Otherwise only a port
/// owner whose image is the platform's smind binary is eligible (`smind.exe`
/// on Windows, AC6; `smind` on macOS, so an `ssh -L` tunnel or any other
/// process squatting the port is never adopted); anything else gets this
/// explanation instead. Unsupported hosts never reach take-over, so they are
/// not refused here.
fn take_over_refusal(platform: Platform, owner_image: Option<&str>, managed_by: Option<&str>, port: u16) -> Option<String> {
    if classify_unmanaged(platform, owner_image, managed_by) == UnmanagedKind::Service {
        if managed_by == Some("docker") {
            return Some(format!(
                "the daemon on port {port} runs in a Docker container (managed by Docker) -- manage it with docker, not from this app"
            ));
        }
        return Some(format!(
            "the daemon on port {port} is managed by {} -- manage it with `smind service` (stop, restart, update), not from this app",
            managed_by_label(managed_by.unwrap_or_default())
        ));
    }
    if platform == Platform::Unsupported || classify_unmanaged(platform, owner_image, managed_by) == UnmanagedKind::NativeSmind {
        return None;
    }
    Some(match platform {
        Platform::Windows => format!("a daemon not started by this app is answering on port {port} -- if it runs in WSL, stop it there; take over is only offered for a smind.exe"),
        _ => format!("the process holding port {port} is not a sMind daemon (it may be an ssh tunnel or another program) -- take over is only offered for a sMind process"),
    })
}

/// answering_without_owner is true when something answers on the daemon
/// port but no process this app can see owns it. That is what a daemon
/// inside WSL looks like when WSL forwards localhost at the kernel level
/// (no `wslrelay.exe`, no Windows listener at all), found by running the
/// real flow: the IP Helper table has no row, yet /healthz answers.
fn answering_without_owner(reachable: bool, port_owner: Option<u32>) -> bool {
    reachable && port_owner.is_none()
}

/// effective_managed_state is `managed::classify` plus the Windows case
/// above: a port that answers with no visible owner is `Unmanaged` (never
/// `NotRunning`, which would invite Install onto an occupied port).
fn effective_managed_state(platform: Platform, reachable: bool, record: Option<&ManagedRecord>, port_owner: Option<u32>, managed_by: Option<&str>) -> ManagedState {
    // An OS service owns the daemon: whatever the app recorded (say, a
    // take-over from before the app knew about services), it must not offer
    // to restart, update or kill it -- the service would just start it again.
    if reachable && managed_by.is_some() {
        return ManagedState::Unmanaged;
    }
    if platform == Platform::Windows && answering_without_owner(reachable, port_owner) {
        return ManagedState::Unmanaged;
    }
    managed::classify(record, port_owner)
}

// The Windows-only helpers below are pure so their unit tests run on every
// platform; outside tests they are compiled only where they are used.

/// windows_install_conflict_hint is the AC6 half of install's
/// port-conflict refusal on Windows: the base refusal comes from
/// `assert_safe_to_install`; this adds the WSL possibility and names the
/// port the Windows daemon needs free.
#[cfg(any(target_os = "windows", test))]
fn windows_install_conflict_hint(port: u16) -> String {
    format!("If that daemon runs in WSL, stop it inside WSL first -- installing the Windows daemon needs port {port} free.")
}

/// windows_update_steps is the pure ordering of install/update on
/// Windows: a running exe cannot be replaced, so a managed running
/// daemon is stopped strictly before the binary is installed -- and a
/// failed stop aborts before anything on disk is touched.
#[cfg(any(target_os = "windows", test))]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum WindowsUpdateStep {
    StopRunningDaemon,
    InstallBinary,
    StartDaemon,
}

#[cfg(any(target_os = "windows", test))]
fn windows_update_plan(stop_result: Option<Result<(), String>>) -> Result<&'static [WindowsUpdateStep], String> {
    const WITH_STOP: &[WindowsUpdateStep] = &[WindowsUpdateStep::StopRunningDaemon, WindowsUpdateStep::InstallBinary, WindowsUpdateStep::StartDaemon];
    const NO_STOP: &[WindowsUpdateStep] = &[WindowsUpdateStep::InstallBinary, WindowsUpdateStep::StartDaemon];
    match stop_result {
        None => Ok(NO_STOP),
        Some(Ok(())) => Ok(WITH_STOP),
        Some(Err(e)) => Err(format!("could not stop the running daemon before replacing it (the binary was left untouched): {e}")),
    }
}

/// The AC8 message shown when Git for Windows is missing: worktrees need
/// git, and setup/teardown scripts need its `sh`.
#[cfg(any(target_os = "windows", test))]
const GIT_MISSING_MESSAGE: &str =
    "Git for Windows was not found. Worktrees need git, and setup scripts need its sh. Install Git for Windows and restart smind.";

/// git_lookup finds `git.exe` (AC8): on PATH, then in the standard
/// Git for Windows install dirs. Pure over injected env values and an
/// `exists` closure so the search order is unit-testable.
#[cfg(any(target_os = "windows", test))]
fn git_lookup(getenv: impl Fn(&str) -> Option<String>, exists: impl Fn(&Path) -> bool) -> Option<PathBuf> {
    if let Some(path) = getenv("PATH") {
        for dir in std::env::split_paths(&path) {
            let candidate = dir.join("git.exe");
            if exists(&candidate) {
                return Some(candidate);
            }
        }
    }
    for key in ["ProgramFiles", "ProgramFiles(x86)"] {
        if let Some(program_files) = getenv(key).filter(|s| !s.is_empty()) {
            let candidate = Path::new(&program_files).join("Git").join("cmd").join("git.exe");
            if exists(&candidate) {
                return Some(candidate);
            }
        }
    }
    if let Some(local) = getenv("LOCALAPPDATA").filter(|s| !s.is_empty()) {
        let candidate = Path::new(&local).join("Programs").join("Git").join("cmd").join("git.exe");
        if exists(&candidate) {
            return Some(candidate);
        }
    }
    None
}

/// git_prerequisite_fields maps "was git found" to the DTO's AC8 fields:
/// found -> `(Some(true), None)`; missing -> the message; non-Windows
/// (`None`) -> no fields at all.
#[cfg(any(target_os = "windows", test))]
fn git_prerequisite_fields(found_git: Option<bool>) -> (Option<bool>, Option<String>) {
    match found_git {
        None => (None, None),
        Some(true) => (Some(true), None),
        Some(false) => (Some(false), Some(GIT_MISSING_MESSAGE.to_string())),
    }
}

/// windows_git_prerequisites is compute_status's AC8 half on Windows.
#[cfg(target_os = "windows")]
fn windows_git_prerequisites() -> (Option<bool>, Option<String>) {
    let found = git_lookup(|k| std::env::var(k).ok(), |p| p.exists()).is_some();
    git_prerequisite_fields(Some(found))
}

// ---------------------------------------------------------------------------
// Shared: port ownership, status, and the four commands
// ---------------------------------------------------------------------------

fn find_port_owner(platform: Platform, port: u16) -> Option<u32> {
    match platform {
        Platform::Macos => native::find_port_owner(port).ok().flatten(),
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_native::find_port_owner(port).ok().flatten()
            }
            #[cfg(not(target_os = "windows"))]
            {
                None
            }
        }
        Platform::Unsupported => None,
    }
}

/// managed_record_exists is the upgrade rule's second signal (ADR-0031 D7): an
/// earlier version of the app installed and recorded a managed daemon.
pub(crate) fn managed_record_exists(app: &AppHandle) -> bool {
    match detect_platform() {
        Platform::Macos => macos_layout(app).ok().and_then(|l| managed::load(&l.state_path)).is_some(),
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_layout().ok().and_then(|l| managed::load(&l.state_path)).is_some()
            }
            #[cfg(not(target_os = "windows"))]
            {
                false
            }
        }
        Platform::Unsupported => false,
    }
}

/// binary_installed answers "is the managed binary on disk right now" --
/// `restart`'s precondition (`managed::assert_restartable`) and
/// `compute_status`'s `binary_installed` field share this one check.
fn binary_installed(app: &AppHandle, platform: Platform) -> bool {
    match platform {
        Platform::Macos => macos_layout(app).ok().map(|l| l.bin_path.exists()).unwrap_or(false),
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_layout().map(|l| l.bin_path.exists()).unwrap_or(false)
            }
            #[cfg(not(target_os = "windows"))]
            {
                false
            }
        }
        Platform::Unsupported => false,
    }
}

/// macos_exe_matches resolves `pid`'s real command via `ps` and compares
/// it against `expected` -- either a previously-recorded `exe_path`
/// (pre-kill checks) or the managed `Layout`'s own binary path (post-
/// start checks); macOS always knows the managed path as a concrete
/// literal, so one function serves both purposes.
fn macos_exe_matches(pid: u32, expected: &Path) -> bool {
    native::exe_path_for_pid(pid).ok().flatten().map(|p| native::exe_matches(&p, expected)).unwrap_or(false)
}

fn log_path_display(app: &AppHandle, platform: Platform) -> Option<String> {
    match platform {
        Platform::Macos => macos_layout(app).ok().map(|l| l.log_path.display().to_string()),
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_layout().ok().map(|l| l.log_path.display().to_string())
            }
            #[cfg(not(target_os = "windows"))]
            {
                None
            }
        }
        Platform::Unsupported => None,
    }
}

async fn compute_status(app: &AppHandle, state: &DesktopState) -> DaemonStatus {
    let platform = detect_platform();
    let app_version = app_version_string(app);
    let base_url = local_base_url(state);
    let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);

    let Health { reachable, version: daemon_version, service: local_service, managed_by } = probe_health(&base_url).await;

    let record = match platform {
        Platform::Macos => macos_layout(app).ok().and_then(|l| managed::load(&l.state_path)),
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_layout().ok().and_then(|l| managed::load(&l.state_path))
            }
            #[cfg(not(target_os = "windows"))]
            {
                None
            }
        }
        Platform::Unsupported => None,
    };

    let port_owner = find_port_owner(platform, port);
    let managed_state = effective_managed_state(platform, reachable, record.as_ref(), port_owner, managed_by.as_deref());

    // AC6: on Windows, an unmanaged port owner whose image is not an
    // identifiable smind.exe is most likely the WSL relay forwarding a
    // daemon inside a distro -- flagged so the UI can explain it and
    // hide take-over.
    let owner_image: Option<String> = match platform {
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                port_owner.and_then(|pid| windows_native::exe_path_for_pid(pid).ok().flatten())
            }
            #[cfg(not(target_os = "windows"))]
            {
                None
            }
        }
        _ => None,
    };
    let wsl_daemon_suspected = platform == Platform::Windows
        && reachable
        && managed_state == ManagedState::Unmanaged
        && classify_unmanaged(platform, owner_image.as_deref(), managed_by.as_deref()) == UnmanagedKind::ForeignOrWsl;

    let (git_found, prerequisite_message) = match platform {
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_git_prerequisites()
            }
            #[cfg(not(target_os = "windows"))]
            {
                (None, None)
            }
        }
        _ => (None, None),
    };

    let comparison = daemon_version.as_deref().map(|d| version::compare(&app_version, d)).unwrap_or(version::Comparison::Unknown);

    DaemonStatus {
        platform,
        reachable,
        daemon_version,
        app_version,
        comparison,
        managed_state,
        managed_by,
        pid: port_owner,
        installed_path: record.as_ref().map(|r| r.exe_path.clone()),
        log_path: log_path_display(app, platform),
        binary_installed: binary_installed(app, platform),
        wsl_daemon_suspected,
        git_found,
        prerequisite_message,
        local_service,
        port_occupied: port_owner.is_some() && !reachable,
    }
}

/// refuse_service_managed is the backstop behind the UI hiding its buttons:
/// install/update/restart stop the running daemon, which a service-managed
/// one must never have done to it by the app (ADR-0031 P10).
async fn refuse_service_managed(platform: Platform, base_url: &url::Url, port: u16) -> Result<(), String> {
    match probe_health(base_url).await.managed_by.as_deref() {
        Some(m) => Err(take_over_refusal(platform, None, Some(m), port).unwrap_or_default()),
        None => Ok(()),
    }
}

async fn install_or_update(app: &AppHandle, state: &DesktopState) -> Result<DaemonStatus, String> {
    let platform = detect_platform();
    let app_version = app_version_string(app);
    let base_url = local_base_url(state);
    let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);
    refuse_service_managed(platform, &base_url, port).await?;

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

            // M2: the daemon ships inside the app, so there is no release
            // download here.
            debug_assert_eq!(install_source(platform), InstallSource::Bundled);
            emit_progress(app, "installing", "Installing the bundled daemon…");
            install_bundled_binary(&bundled_daemon_path_for_app()?, &layout.bin_path)?;

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

            let child = spawn_native_daemon(&layout).await?;
            drop(child); // detached (its own process group); tracked by pid below, not by this handle.

            // Don't trust "whoever now owns the port" on its own -- verify
            // it's really the binary just installed (and, since /healthz
            // answered, that it reports the version just installed)
            // before saving a ManagedRecord. This is what would have
            // caught Bug 1: if the new process failed to bind because the
            // port was already taken, the port's owner is still the old
            // occupant, whose exe path won't match.
            let daemon_version = wait_for_healthz(&base_url).await;
            let port_owner_after = native::find_port_owner(port).ok().flatten();
            let exe_matches_after = port_owner_after.map(|pid| macos_exe_matches(pid, &layout.bin_path)).unwrap_or(false);
            let pid = managed::verify_fresh_start(port_owner_after, exe_matches_after, daemon_version.as_deref(), &app_version)?;

            let record = ManagedRecord { pid, version: app_version.clone(), installed_at: now_unix_seconds(), exe_path: layout.bin_path.display().to_string() };
            managed::save(&layout.state_path, &record).map_err(|e| e.to_string())?;
        }
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                let layout = windows_layout()?;
                let existing_record = managed::load(&layout.state_path);

                // Bug 1, same guard as the macOS branch above -- plus the
                // AC6 hint, since on Windows the most likely squatter is
                // the relay forwarding a WSL daemon's port.
                let port_owner_before = windows_native::find_port_owner(port).ok().flatten();
                if answering_without_owner(probe_health(&base_url).await.reachable, port_owner_before) {
                    return Err(format!("a daemon is already answering on port {port}, but no Windows process owns it. {}", windows_install_conflict_hint(port)));
                }
                if let Err(e) = managed::assert_safe_to_install(existing_record.as_ref(), port_owner_before, port) {
                    return Err(format!("{e}. {}", windows_install_conflict_hint(port)));
                }

                // A running exe cannot be replaced on Windows: a managed
                // daemon that is still running is stopped first, and a
                // failed stop aborts before the binary is touched
                // (`windows_update_plan`).
                let stop_result = if let Some(record) = &existing_record {
                    if port_owner_before == Some(record.pid) {
                        emit_progress(app, "stopping", "Stopping the running daemon so it can be replaced…");
                        let token = windows_native::read_token(&windows_native::token_path()).unwrap_or_default();
                        Some(windows_native::stop(&reqwest::Client::new(), &layout, port, &token, std::time::Duration::from_secs(10)).await)
                    } else {
                        None
                    }
                } else {
                    None
                };
                windows_update_plan(stop_result)?;

                // The daemon ships inside the installer, like macOS: copy
                // the sidecar next to the app exe into the managed layout.
                debug_assert_eq!(install_source(platform), InstallSource::Bundled);
                emit_progress(app, "installing", "Installing the bundled daemon…");
                install_bundled_binary(&bundled_daemon_path_for_app()?, &layout.bin_path)?;

                emit_progress(app, "starting", "Starting the daemon…");
                let child = windows_native::spawn_detached(&layout, &app_version).map_err(|e| e.to_string())?;
                drop(child); // detached (its own process group); tracked by pid below, not by this handle.

                // Bug 1's post-start half, same as the macOS branch:
                // whoever now owns the port must really be the binary
                // just installed (and, since /healthz answered, report
                // its version) before a ManagedRecord is saved.
                let daemon_version = wait_for_healthz(&base_url).await;
                let port_owner_after = windows_native::find_port_owner(port).ok().flatten();
                let exe_matches_after = port_owner_after.map(|pid| windows_exe_matches(pid, &layout.bin_path)).unwrap_or(false);
                let pid = managed::verify_fresh_start(port_owner_after, exe_matches_after, daemon_version.as_deref(), &app_version)?;

                let record = ManagedRecord { pid, version: app_version.clone(), installed_at: now_unix_seconds(), exe_path: layout.bin_path.display().to_string() };
                managed::save(&layout.state_path, &record).map_err(|e| e.to_string())?;
            }
            #[cfg(not(target_os = "windows"))]
            {
                return Err("this platform isn't supported for app-managed daemon install".to_string());
            }
        }
        Platform::Unsupported => {
            return Err("this platform isn't supported for app-managed daemon install (an unsupported host)".to_string());
        }
    }

    emit_progress(app, "installing", "Done.");
    Ok(compute_status(app, state).await)
}

async fn restart(app: &AppHandle, state: &DesktopState) -> Result<DaemonStatus, String> {
    let platform = detect_platform();
    let base_url = local_base_url(state);
    let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);
    refuse_service_managed(platform, &base_url, port).await?;

    let record = match platform {
        Platform::Macos => macos_layout(app).ok().and_then(|l| managed::load(&l.state_path)),
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_layout().ok().and_then(|l| managed::load(&l.state_path))
            }
            #[cfg(not(target_os = "windows"))]
            {
                None
            }
        }
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
    managed::assert_restartable(binary_installed(app, platform))?;

    // Re-check immediately before signalling: refuse unless the recorded
    // pid is still the real port owner (closes a PID-reuse race) *and*
    // really our binary (Bug 2's guard -- a bare port-ownership match
    // isn't enough on its own, since the pid could have been reused by an
    // unrelated process, including the user's own unmanaged daemon).
    let live_owner = find_port_owner(platform, port);
    // Compare against this record's own exe_path -- for a normal managed
    // daemon that's already the managed bin's path, and for one reached
    // via take-over it's the adopted process's real path, so a take-over
    // followed directly by Restart still passes this check correctly.
    let exe_matches = match platform {
        Platform::Macos => macos_exe_matches(record.pid, Path::new(&record.exe_path)),
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_exe_matches(record.pid, Path::new(&record.exe_path))
            }
            #[cfg(not(target_os = "windows"))]
            {
                false
            }
        }
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
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                // The graceful path (ADR-0030): POST /api/shutdown with
                // the daemon's token, then wait for /healthz to stop
                // answering; windows_native::stop only ever terminates
                // the recorded pid behind the same identity checks.
                let layout = windows_layout()?;
                let token = windows_native::read_token(&windows_native::token_path()).unwrap_or_default();
                windows_native::stop(&reqwest::Client::new(), &layout, port, &token, std::time::Duration::from_secs(10)).await?;
            }
            #[cfg(not(target_os = "windows"))]
            {
                return Err("this platform isn't supported for app-managed daemon restart".to_string());
            }
        }
        Platform::Unsupported => unreachable!(),
    }

    emit_progress(app, "starting", "Starting the daemon…");
    match platform {
        Platform::Macos => {
            let layout = macos_layout(app)?;
            let child = spawn_native_daemon(&layout).await?;
            drop(child);

            let _ = wait_for_healthz(&base_url).await;
            let port_owner_after = native::find_port_owner(port).ok().flatten();
            let exe_matches_after = port_owner_after.map(|pid| macos_exe_matches(pid, &layout.bin_path)).unwrap_or(false);
            let pid = managed::verify_started_identity(port_owner_after, exe_matches_after)?;

            // `spawn_detached` always execs `layout.bin_path` -- refresh
            // exe_path to that (never inherit the old record's, which for
            // a take-over-then-restart would still be the adopted path).
            let new_record = ManagedRecord { pid, exe_path: layout.bin_path.display().to_string(), installed_at: now_unix_seconds(), ..record };
            managed::save(&layout.state_path, &new_record).map_err(|e| e.to_string())?;
        }
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                let layout = windows_layout()?;
                let child = windows_native::spawn_detached(&layout, &record.version).map_err(|e| e.to_string())?;
                drop(child); // detached (its own process group); tracked by pid below, not by this handle.

                let _ = wait_for_healthz(&base_url).await;
                let port_owner_after = windows_native::find_port_owner(port).ok().flatten();
                let exe_matches_after = port_owner_after.map(|pid| windows_exe_matches(pid, &layout.bin_path)).unwrap_or(false);
                let pid = managed::verify_started_identity(port_owner_after, exe_matches_after)?;

                // `spawn_detached` always execs `layout.bin_path` --
                // refresh exe_path to that, same reasoning as the macOS
                // branch.
                let new_record = ManagedRecord { pid, exe_path: layout.bin_path.display().to_string(), installed_at: now_unix_seconds(), ..record };
                managed::save(&layout.state_path, &new_record).map_err(|e| e.to_string())?;
            }
            #[cfg(not(target_os = "windows"))]
            {
                return Err("this platform isn't supported for app-managed daemon restart".to_string());
            }
        }
        Platform::Unsupported => unreachable!(),
    }

    Ok(compute_status(app, state).await)
}

async fn take_over(app: &AppHandle, state: &DesktopState) -> Result<DaemonStatus, String> {
    let platform = detect_platform();
    let base_url = local_base_url(state);
    let port = base_url.port_or_known_default().unwrap_or(DEFAULT_PORT);

    let health = probe_health(&base_url).await;
    // A service-managed daemon is never adopted, before any process is
    // inspected (and whatever owns the port).
    if let Some(message) = health.managed_by.as_deref().and_then(|m| take_over_refusal(platform, None, Some(m), port)) {
        return Err(message);
    }

    let Some(owner) = find_port_owner(platform, port) else {
        // A daemon forwarded from WSL answers with no visible owner: say
        // that, not "nothing is listening".
        if answering_without_owner(health.reachable, None) {
            if let Some(message) = take_over_refusal(platform, None, None, port) {
                return Err(message);
            }
        }
        return Err("nothing is listening on the configured port -- there is no daemon to take over".to_string());
    };

    // Resolve the adopted process's real executable path up front: this
    // becomes the record's own identity check from now on
    // (safe_to_kill/verify_started_identity compare against it), so a
    // take-over that can't resolve it must refuse outright rather than
    // store a placeholder that could never pass an identity check again
    // -- which would make Update/Restart permanently refuse afterwards.
    let exe_path = match platform {
        Platform::Macos => native::exe_path_for_pid(owner).ok().flatten(),
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                windows_native::exe_path_for_pid(owner).ok().flatten()
            }
            #[cfg(not(target_os = "windows"))]
            {
                None
            }
        }
        Platform::Unsupported => None,
    };
    let Some(exe_path) = exe_path else {
        return Err(format!(
            "could not resolve the executable for pid {owner} -- refusing to take over management (it may have already exited, or you may lack permission to inspect it)"
        ));
    };

    // AC6 (Windows): only a smind.exe owner is take-over eligible --
    // anything else (typically the WSL relay) is refused with an
    // explanation instead of being adopted.
    if let Some(message) = take_over_refusal(platform, Some(&exe_path), health.managed_by.as_deref(), port) {
        return Err(message);
    }

    let record = managed::take_over(owner, "unknown", &exe_path, &now_unix_seconds());
    match platform {
        Platform::Macos => {
            let layout = macos_layout(app)?;
            managed::save(&layout.state_path, &record).map_err(|e| e.to_string())?;
        }
        Platform::Windows => {
            #[cfg(target_os = "windows")]
            {
                let layout = windows_layout()?;
                managed::save(&layout.state_path, &record).map_err(|e| e.to_string())?;
            }
            #[cfg(not(target_os = "windows"))]
            {
                return Err("this platform isn't supported for app-managed daemon takeover".to_string());
            }
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
    guard_management(&state)?;
    install_or_update(&app, &state).await
}

#[tauri::command]
pub async fn daemon_update(app: AppHandle, state: tauri::State<'_, DesktopState>) -> Result<DaemonStatus, String> {
    guard_management(&state)?;
    install_or_update(&app, &state).await
}

#[tauri::command]
pub async fn daemon_restart(app: AppHandle, state: tauri::State<'_, DesktopState>) -> Result<DaemonStatus, String> {
    guard_management(&state)?;
    restart(&app, &state).await
}

#[tauri::command]
pub async fn take_over_daemon(app: AppHandle, state: tauri::State<'_, DesktopState>) -> Result<DaemonStatus, String> {
    guard_management(&state)?;
    take_over(&app, &state).await
}

/// connection_version reports version skew for *any* saved connection by
/// id, not just the local one -- the banner (AC7) needs this for the
/// "remote/url/relay connections just get a notice" case, since the
/// loopback proxy doesn't forward `/healthz` at all.
#[tauri::command]
pub async fn connection_version(app: AppHandle, state: tauri::State<'_, DesktopState>, id: String) -> Result<ConnectionVersionInfo, String> {
    let conn = {
        let registry = state.proxy.registry.lock().unwrap();
        registry.list().iter().find(|c| c.id == id).cloned()
    }
    .ok_or_else(|| format!("no connection with id {id:?}"))?;
    let app_version = app_version_string(&app);
    // An ssh connection is probed through its tunnel's local end; with no
    // tunnel running it is simply unreachable.
    let Some(base_url) = state.proxy.upstream_base(&conn) else {
        return Ok(ConnectionVersionInfo { reachable: false, daemon_version: None, app_version, comparison: version::Comparison::Unknown, managed_by: None });
    };
    let url = url::Url::parse(&base_url).map_err(|e| e.to_string())?;
    let Health { reachable, version: daemon_version, managed_by, .. } = probe_health(&url).await;
    let comparison = daemon_version.as_deref().map(|d| version::compare(&app_version, d)).unwrap_or(version::Comparison::Unknown);
    Ok(ConnectionVersionInfo { reachable, daemon_version, app_version, comparison, managed_by })
}

// build.rs shares this file; `cargo test` doesn't run tests in build scripts.
#[cfg(test)]
#[path = "../build_sidecar.rs"]
mod build_sidecar;

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    use smind_daemon_client::daemon_manager::managed::ManagedState;
    use smind_daemon_client::daemon_manager::version::Comparison;

    #[test]
    fn failed_restart_never_reports_managed_running() {
        // A failed start returns Err before the record-saving code runs
        // (`?` in restart), and status is computed from the live port: with
        // nothing listening, classify is NotRunning even if a stale record
        // still names the old pid.
        let stale = ManagedRecord { pid: 4242, version: "0.1.0".into(), installed_at: "0".into(), exe_path: "/x".into() };
        assert_eq!(managed::classify(Some(&stale), None), ManagedState::NotRunning);
    }

    fn layout_in(dir: &Path) -> native::Layout {
        native::Layout::new(&dir.join("managed-daemon"))
    }

    fn fake_bundle(dir: &Path, contents: &[u8]) -> std::path::PathBuf {
        let exe_dir = dir.join("smind.app/Contents/MacOS");
        fs::create_dir_all(&exe_dir).unwrap();
        let bundled = bundled_daemon_path(&exe_dir);
        fs::write(&bundled, contents).unwrap();
        bundled
    }

    #[test]
    fn macos_install_sources_bundled_binary() {
        let tmp = tempfile::tempdir().unwrap();
        let bundled = fake_bundle(tmp.path(), b"#!/bin/sh\necho daemon\n");
        let layout = layout_in(tmp.path());

        // The install takes the bundle path and a layout only -- it has no
        // HTTP client or release URLs to download from.
        install_bundled_binary(&bundled, &layout.bin_path).unwrap();

        assert_eq!(fs::read(&layout.bin_path).unwrap(), b"#!/bin/sh\necho daemon\n");
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = fs::metadata(&layout.bin_path).unwrap().permissions().mode();
            assert_eq!(mode & 0o111, 0o111, "installed daemon must be executable, got {mode:o}");
        }
        assert_eq!(install_source(Platform::Macos), InstallSource::Bundled);
        // Replacing an existing install (an update) works and leaves no staging file behind.
        fs::write(&bundled, b"v2").unwrap();
        install_bundled_binary(&bundled, &layout.bin_path).unwrap();
        assert_eq!(fs::read(&layout.bin_path).unwrap(), b"v2");
        let leftovers: Vec<_> = fs::read_dir(layout.bin_path.parent().unwrap()).unwrap().map(|e| e.unwrap().file_name()).collect();
        assert_eq!(leftovers, vec![std::ffi::OsString::from("smind")]);
    }

    #[test]
    fn macos_install_missing_bundled_binary_is_error() {
        let tmp = tempfile::tempdir().unwrap();
        let layout = layout_in(tmp.path());
        let missing = bundled_daemon_path(&tmp.path().join("nowhere"));

        let err = install_bundled_binary(&missing, &layout.bin_path).unwrap_err();
        assert!(err.contains("bundled daemon"), "unclear error: {err}");
        assert!(err.contains(&missing.display().to_string()), "error should name the path: {err}");
        assert!(!err.to_lowercase().contains("download"), "no download fallback: {err}");
        assert!(!layout.bin_path.exists());

        // An empty file is the dev-build placeholder (build.rs): same clear error, nothing installed.
        let empty = fake_bundle(tmp.path(), b"");
        let err = install_bundled_binary(&empty, &layout.bin_path).unwrap_err();
        assert!(err.contains("empty"), "unclear error: {err}");
        assert!(!layout.bin_path.exists());
    }

    #[test]
    fn macos_autostart_when_unreachable() {
        use Comparison::*;
        use LaunchAction::*;
        use ManagedState::*;

        // No daemon reachable and nothing on the port -> install + start.
        assert_eq!(launch_decision(false, NotRunning, Unknown), InstallAndStart);
        // Managed and older -> the UI offers the update; Rust never restarts.
        assert_eq!(launch_decision(true, Managed, Older), OfferUpdate);
        // Managed and same/newer (or unversioned dev build) -> no-op.
        for c in [Same, Newer, Unknown] {
            assert_eq!(launch_decision(true, Managed, c), NoOp, "managed + {c:?}");
        }
        // Unmanaged (any comparison) -> never touched, banner state only.
        for c in [Older, Same, Newer, Unknown] {
            assert_eq!(launch_decision(true, Unmanaged, c), UnmanagedBanner, "unmanaged + {c:?}");
        }
        // Something holds the port but doesn't answer: if it's ours it may
        // still be starting -- leave it; if it isn't ours, leave it alone too.
        assert_eq!(launch_decision(false, Managed, Unknown), NoOp);
        assert_eq!(launch_decision(false, Unmanaged, Unknown), UnmanagedBanner);
        // Healthy daemon we can't attribute to a port owner (lsof blind,
        // or a non-local URL): nothing to do.
        assert_eq!(launch_decision(true, NotRunning, Older), NoOp);
    }

    const ONBOARDED: AppSettings = AppSettings { onboarding_completed: true, manage_local_daemon: true };

    #[test]
    fn auto_start_noop_until_onboarded() {
        let pending = AppSettings { onboarding_completed: false, manage_local_daemon: true };
        for state in [ManagedState::NotRunning, ManagedState::Managed, ManagedState::Unmanaged] {
            assert_eq!(startup_decision(pending, true, false, state, Comparison::Unknown), LaunchAction::NoOp, "{state:?}");
        }
    }

    #[test]
    fn auto_start_noop_when_manage_local_daemon_false() {
        let off = AppSettings { onboarding_completed: true, manage_local_daemon: false };
        assert_eq!(startup_decision(off, true, false, ManagedState::NotRunning, Comparison::Unknown), LaunchAction::NoOp);
        assert_eq!(startup_decision(off, true, true, ManagedState::Managed, Comparison::Older), LaunchAction::NoOp);
        assert!(ensure_managed(off).unwrap_err().contains("turned off in Settings"));
        assert!(ensure_managed(ONBOARDED).is_ok());
    }

    #[test]
    fn auto_start_runs_when_managed_and_local_selected() {
        assert_eq!(startup_decision(ONBOARDED, true, false, ManagedState::NotRunning, Comparison::Unknown), LaunchAction::InstallAndStart);
        assert_eq!(startup_decision(ONBOARDED, true, true, ManagedState::Managed, Comparison::Older), LaunchAction::OfferUpdate);
        // Another connection selected: still nothing.
        assert_eq!(startup_decision(ONBOARDED, false, false, ManagedState::NotRunning, Comparison::Unknown), LaunchAction::NoOp);
    }

    #[test]
    fn launch_decision_offers_update_instead_of_restarting() {
        use Comparison::*;
        use LaunchAction::*;
        use ManagedState::*;

        // An older managed daemon is never restarted by auto_start; the UI
        // decides once it knows the run state.
        assert_eq!(launch_decision(true, Managed, Older), OfferUpdate);
        // Nothing serving still installs and starts the bundled daemon.
        assert_eq!(launch_decision(false, NotRunning, Older), InstallAndStart);
        assert_eq!(launch_decision(false, NotRunning, Unknown), InstallAndStart);
    }

    #[test]
    fn daemon_start_waits_for_slow_binder() {
        let rt = tokio::runtime::Builder::new_current_thread().enable_time().build().unwrap();
        // The daemon answers on the 4th probe (a first start creating its DB).
        let mut calls = 0;
        let got = rt.block_on(poll_until_ready(
            || {
                calls += 1;
                let ready = calls >= 4;
                async move { ready.then_some("0.9.1".to_string()) }
            },
            10,
            std::time::Duration::from_millis(1),
        ));
        assert_eq!(got.as_deref(), Some("0.9.1"));
        assert_eq!(calls, 4);

        // Never answering gives up after exactly `attempts` probes.
        let mut calls = 0;
        let got: Option<()> = rt.block_on(poll_until_ready(
            || {
                calls += 1;
                async { None }
            },
            5,
            std::time::Duration::from_millis(1),
        ));
        assert!(got.is_none());
        assert_eq!(calls, 5);
    }

    #[test]
    fn platform_for_windows_is_native_and_never_probes_wsl() {
        assert_eq!(platform_for("windows"), Platform::Windows);
        assert_eq!(platform_for("macos"), Platform::Macos);
        // Linux (WSLg included) is not a desktop-managed host; the mapping
        // is pure over the OS name -- nothing is probed at runtime.
        assert_eq!(platform_for("linux"), Platform::Unsupported);
    }

    #[test]
    fn classify_wsl_forwarded_port_is_not_takeover_eligible() {
        use UnmanagedKind::*;
        // The relay that forwards a WSL daemon's port.
        assert_eq!(classify_unmanaged(Platform::Windows, Some(r"C:\Windows\system32\wslrelay.exe"), None), ForeignOrWsl);
        // No owner visible (gone, or access denied).
        assert_eq!(classify_unmanaged(Platform::Windows, None, None), ForeignOrWsl);
        // An unrelated app.
        assert_eq!(classify_unmanaged(Platform::Windows, Some(r"C:\Tools\other.exe"), None), ForeignOrWsl);
        // A smind.exe anywhere is eligible, whatever the case.
        assert_eq!(classify_unmanaged(Platform::Windows, Some(r"C:\Users\x\AppData\Local\smind\bin\SMIND.EXE"), None), NativeSmind);
        assert_eq!(classify_unmanaged(Platform::Windows, Some(r"D:\dev\smind.exe"), None), NativeSmind);
        // ...but a name that merely contains it is not.
        assert_eq!(classify_unmanaged(Platform::Windows, Some(r"C:\Windows\smind.exe.bak"), None), ForeignOrWsl);
    }

    /// ADR-0031 P10: `/healthz` `managedBy` means an OS service owns the
    /// daemon -- never take-over eligible, even when the port owner is an
    /// identifiable smind binary.
    #[test]
    fn classify_unmanaged_service_when_healthz_managed_by_present() {
        use UnmanagedKind::*;
        assert_eq!(classify_unmanaged(Platform::Windows, Some(r"C:\Users\x\AppData\Local\smind\bin\smind.exe"), Some("taskscheduler")), Service);
        assert_eq!(classify_unmanaged(Platform::Macos, Some("/usr/local/bin/smind"), Some("systemd")), Service);
        assert_eq!(classify_unmanaged(Platform::Macos, None, Some("launchd")), Service);
        // An empty value is "not managed", like an absent one.
        assert_eq!(classify_unmanaged(Platform::Macos, None, Some("")), ForeignOrWsl);
    }

    /// A take-over recorded before the app knew about services must not make a
    /// service-managed daemon look app-managed (and so restartable).
    #[test]
    fn a_service_managed_daemon_is_unmanaged_even_with_a_matching_record() {
        let record = ManagedRecord { pid: 7, version: "0.12.0".into(), installed_at: "0".into(), exe_path: "/x/smind".into() };
        for platform in [Platform::Macos, Platform::Windows] {
            assert_eq!(effective_managed_state(platform, true, Some(&record), Some(7), None), ManagedState::Managed);
            assert_eq!(effective_managed_state(platform, true, Some(&record), Some(7), Some("launchd")), ManagedState::Unmanaged);
        }
        // Not answering: nothing to be managed by anyone.
        assert_eq!(effective_managed_state(Platform::Macos, false, None, None, Some("launchd")), ManagedState::NotRunning);
    }

    #[test]
    fn take_over_refused_for_a_service_managed_daemon_on_every_platform() {
        for platform in [Platform::Macos, Platform::Windows, Platform::Unsupported] {
            let err = take_over_refusal(platform, Some("/usr/local/bin/smind"), Some("systemd"), 4648).expect("service-managed must be refused");
            assert!(err.contains("managed by systemd") && err.contains("smind service"), "{err}");
        }
        let err = take_over_refusal(Platform::Windows, None, Some("taskscheduler"), 4648).unwrap();
        assert!(err.contains("Task Scheduler"), "{err}");
        // A container is not a service manager: no `smind service` advice.
        let err = take_over_refusal(Platform::Macos, Some("/usr/local/bin/smind"), Some("docker"), 4648).unwrap();
        assert!(err.contains("managed by Docker") && !err.contains("smind service"), "{err}");
        // Absent managedBy: a smind owner is still eligible.
        assert!(take_over_refusal(Platform::Macos, Some("/x/smind"), None, 4648).is_none());
    }

    #[test]
    fn windows_take_over_refused_for_foreign_owner() {
        let err = take_over_refusal(Platform::Windows, Some(r"C:\Windows\system32\wslrelay.exe"), None, 4648).unwrap();
        assert!(err.contains("not started by this app"), "{err}");
        assert!(err.contains("WSL"), "{err}");
        assert!(err.contains("4648"), "{err}");
        assert!(err.contains("smind.exe"), "{err}");
        // An unresolvable owner is refused too.
        assert!(take_over_refusal(Platform::Windows, None, None, 4648).is_some());
        // A real smind.exe owner is eligible: no refusal.
        assert!(take_over_refusal(Platform::Windows, Some(r"C:\Users\x\AppData\Local\smind\bin\smind.exe"), None, 4648).is_none());
    }

    /// Regression: the AC6 gate classifies by the `smind.exe` file name, so
    /// applying it off Windows would refuse every macOS take-over (the
    /// macOS daemon binary is plain `smind`).
    #[test]
    fn take_over_refusal_never_applies_off_windows() {
        assert!(take_over_refusal(Platform::Macos, Some("/Applications/smind.app/Contents/MacOS/smind"), None, 4648).is_none());
        assert!(take_over_refusal(Platform::Unsupported, Some("/usr/bin/other"), None, 4648).is_none());
    }

    #[test]
    fn take_over_refused_for_non_smind_owner_on_macos() {
        for owner in ["/usr/bin/ssh", "/usr/local/bin/other", "/opt/smind-helper", "/x/smind.exe"] {
            let err = take_over_refusal(Platform::Macos, Some(owner), None, 4648).unwrap_or_else(|| panic!("{owner} must be refused"));
            assert!(err.contains("not a sMind daemon") && err.contains("4648"), "{err}");
        }
        assert!(take_over_refusal(Platform::Macos, None, None, 4648).is_some());
    }

    #[test]
    fn take_over_allowed_for_smind_owner_on_macos() {
        assert!(take_over_refusal(Platform::Macos, Some("/Users/x/bin/smind"), None, 4648).is_none());
        assert!(take_over_refusal(Platform::Macos, Some("/Users/x/Library/Application Support/ai.smind.desktop/managed-daemon/bin/smind"), None, 4648).is_none());
    }

    /// Found in the AC10 run on a real machine: a daemon inside WSL answers
    /// on the port with no Windows owner at all. It must read as Unmanaged
    /// (never NotRunning, which offers Install onto an occupied port), and
    /// only on Windows.
    #[test]
    fn answering_with_no_visible_owner_is_unmanaged_on_windows_only() {
        assert!(answering_without_owner(true, None));
        assert!(!answering_without_owner(false, None));
        assert!(!answering_without_owner(true, Some(7)));

        assert_eq!(effective_managed_state(Platform::Windows, true, None, None, None), ManagedState::Unmanaged);
        // Nothing answering: still plain NotRunning.
        assert_eq!(effective_managed_state(Platform::Windows, false, None, None, None), ManagedState::NotRunning);
        // A visible owner keeps the normal decision table.
        let record = ManagedRecord { pid: 7, version: "0.10.0".into(), installed_at: "0".into(), exe_path: r"C:\x\smind.exe".into() };
        assert_eq!(effective_managed_state(Platform::Windows, true, Some(&record), Some(7), None), ManagedState::Managed);
        // Other platforms never use the Windows rule.
        assert_eq!(effective_managed_state(Platform::Macos, true, None, None, None), ManagedState::NotRunning);
    }

    #[test]
    fn take_over_refusal_covers_an_unowned_answering_port() {
        let err = take_over_refusal(Platform::Windows, None, None, 4648).expect("an ownerless daemon must be refused");
        assert!(err.contains("WSL"), "{err}");
    }

    #[test]
    fn windows_update_stops_running_daemon_before_replacing_binary() {
        use WindowsUpdateStep::*;
        // A managed running daemon: stop strictly before the install.
        let plan = windows_update_plan(Some(Ok(()))).unwrap();
        assert_eq!(plan, [StopRunningDaemon, InstallBinary, StartDaemon]);
        assert!(plan.iter().position(|s| *s == StopRunningDaemon).unwrap() < plan.iter().position(|s| *s == InstallBinary).unwrap());
        // Nothing running: never stopped.
        assert_eq!(windows_update_plan(None).unwrap(), [InstallBinary, StartDaemon]);
        // A failed stop aborts before the binary is touched.
        let err = windows_update_plan(Some(Err("timeout".to_string()))).unwrap_err();
        assert!(err.contains("left untouched"), "{err}");
    }

    #[test]
    fn install_refused_on_port_conflict_names_the_port() {
        // The existing assert_safe_to_install refusal plus the AC6 hint:
        // what the user sees must name the port conflict and the WSL
        // possibility.
        let base = managed::assert_safe_to_install(None, Some(999), 4648).unwrap_err();
        let msg = format!("{base}. {}", windows_install_conflict_hint(4648));
        assert!(msg.contains("port 4648"), "{msg}");
        assert!(msg.contains("WSL"), "{msg}");
    }

    #[test]
    fn daemon_status_reports_missing_git() {
        // Nothing on PATH and none of the install dirs: git is reported
        // missing, with the AC8 message -- but as a prerequisite, not an
        // error (install/start still work without it).
        let found = git_lookup(|_| None, |_| false);
        assert_eq!(found, None);
        let (git_found, message) = git_prerequisite_fields(Some(found.is_some()));
        assert_eq!(git_found, Some(false));
        let message = message.unwrap();
        assert!(message.contains("Git for Windows was not found"), "{message}");
        assert!(message.contains("sh"), "{message}");
        // Found (or non-Windows): no message.
        assert_eq!(git_prerequisite_fields(Some(true)), (Some(true), None));
        assert_eq!(git_prerequisite_fields(None), (None, None));
    }

    // Windows path semantics (drive letters, `\` separators) only hold on a
    // Windows host; the code under test only runs there.
    #[cfg(windows)]
    #[test]
    fn git_lookup_finds_program_files_cmd_git() {
        let installed = PathBuf::from(r"C:\Program Files\Git\cmd\git.exe");
        let found = git_lookup(
            |k| match k {
                "ProgramFiles" => Some(r"C:\Program Files".to_string()),
                "PATH" => Some(r"C:\Windows\System32".to_string()),
                _ => None,
            },
            |p| *p == installed,
        );
        assert_eq!(found, Some(installed));

        // PATH wins over the install dirs when it carries git.
        let on_path = PathBuf::from(r"C:\tools\git.exe");
        let found = git_lookup(|k| (k == "PATH").then(|| r"C:\tools".to_string()), |p| *p == on_path);
        assert_eq!(found, Some(on_path));
    }

    #[test]
    fn windows_install_uses_bundled_source() {
        assert_eq!(install_source(Platform::Windows), InstallSource::Bundled);
        assert_eq!(install_source(Platform::Macos), InstallSource::Bundled);
    }

    #[test]
    fn bundled_daemon_path_is_platform_named() {
        let dir = Path::new("app-dir");
        let expected = if cfg!(windows) { "smind.exe" } else { "smind" };
        assert_eq!(bundled_daemon_path(dir), dir.join(expected));
    }

    /// The Windows layout (`bin\smind.exe`, same shape as
    /// `windows_native::Layout::new`) driven through the shared copy: the
    /// staging file is `smind.exe.new` and nothing is left behind.
    #[test]
    fn windows_install_sources_bundled_binary() {
        let tmp = tempfile::tempdir().unwrap();
        let bundled = tmp.path().join("app").join("smind.exe");
        fs::create_dir_all(bundled.parent().unwrap()).unwrap();
        fs::write(&bundled, b"MZ new").unwrap();
        let bin_path = tmp.path().join("smind").join("bin").join("smind.exe");

        install_bundled_binary(&bundled, &bin_path).unwrap();
        assert_eq!(fs::read(&bin_path).unwrap(), b"MZ new");

        // An update replaces the existing exe, leaving no staging file.
        fs::write(&bundled, b"MZ newer").unwrap();
        install_bundled_binary(&bundled, &bin_path).unwrap();
        assert_eq!(fs::read(&bin_path).unwrap(), b"MZ newer");
        let leftovers: Vec<_> = fs::read_dir(bin_path.parent().unwrap()).unwrap().map(|e| e.unwrap().file_name()).collect();
        assert_eq!(leftovers, vec![std::ffi::OsString::from("smind.exe")]);
    }

    #[test]
    fn windows_install_missing_bundled_binary_is_error() {
        let tmp = tempfile::tempdir().unwrap();
        let bin_path = tmp.path().join("smind").join("bin").join("smind.exe");
        let missing = tmp.path().join("nowhere").join("smind.exe");

        let err = install_bundled_binary(&missing, &bin_path).unwrap_err();
        assert!(err.contains("bundled daemon"), "unclear error: {err}");
        assert!(err.contains(&missing.display().to_string()), "error should name the path: {err}");
        assert!(!err.to_lowercase().contains("download"), "no download fallback: {err}");
        assert!(!bin_path.exists());

        let empty = tmp.path().join("empty-smind.exe");
        fs::write(&empty, b"").unwrap();
        let err = install_bundled_binary(&empty, &bin_path).unwrap_err();
        assert!(err.contains("empty"), "unclear error: {err}");
        assert!(!bin_path.exists());
        assert!(err.contains(stage_task_hint()));
    }

    #[test]
    fn no_wsl_exe_in_desktop_sources() {
        // AC1's grep guard: no desktop crate may invoke the Windows
        // subsystem for Linux. The needles are built with concat! so this
        // test file itself never matches them.
        let needles = [concat!("ws", "l.exe"), concat!("wsl", "path"), concat!("wsl", ".localhost")];
        let desktop_dir = Path::new(env!("CARGO_MANIFEST_DIR")).parent().unwrap().to_path_buf();
        let mut offenders: Vec<String> = Vec::new();
        fn walk(dir: &Path, needles: &[&str], offenders: &mut Vec<String>) {
            for entry in fs::read_dir(dir).expect("smind desktop: readable source tree") {
                let entry = entry.unwrap();
                let path = entry.path();
                let name = entry.file_name().to_string_lossy().to_string();
                if path.is_dir() {
                    if name == "target" || name == "node_modules" {
                        continue;
                    }
                    walk(&path, needles, offenders);
                } else if name.ends_with(".rs") {
                    let src = fs::read_to_string(&path).unwrap_or_default().to_lowercase();
                    for needle in needles {
                        if src.contains(needle) {
                            offenders.push(format!("{} matches {:?}", path.display(), needle));
                        }
                    }
                }
            }
        }
        walk(&desktop_dir, &needles, &mut offenders);
        assert!(offenders.is_empty(), "desktop Rust sources must not drive the Windows subsystem for Linux: {offenders:?}");
    }
}
