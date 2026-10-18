//! ADR-0023: desktop auto-update plumbing (Rust side). The shell checks,
//! downloads and installs; the webview can only ask Rust to check and to
//! install the update Rust already found. Linux (deb-only) has no updater:
//! `app_update_check` reports `unsupported` and install rejects.

use serde::Serialize;
use std::sync::Mutex;

pub const NOTES_CAP: usize = 4000;

#[cfg(test)]
const TAURI_CONF: &str = include_str!("../tauri.conf.json");
#[cfg(test)]
const UPDATER_CONF: &str = include_str!("../tauri.updater.conf.json");

/// U2.2: an update is offered only if its version is strictly greater
/// than the running one AND has no pre-release part. Non-semver input is
/// rejected (never a panic, never offered).
pub fn is_offerable(current: &str, candidate: &str) -> bool {
    match (
        current.parse::<semver::Version>(),
        candidate.parse::<semver::Version>(),
    ) {
        (Ok(current), Ok(candidate)) => candidate > current && candidate.pre.is_empty(),
        _ => false,
    }
}

/// Cap release notes at 4000 chars (cut on a char boundary, marked with
/// an ellipsis); empty notes become None.
pub fn cap_notes(notes: Option<String>) -> Option<String> {
    let mut notes = notes?;
    if notes.chars().count() > NOTES_CAP {
        notes = format!("{}…", notes.chars().take(NOTES_CAP).collect::<String>());
    }
    if notes.is_empty() {
        None
    } else {
        Some(notes)
    }
}

/// Whether the updater exists on the given target OS.
// Pure, platform-independent helper pinned by tests; the non-test
// builds on macOS/Windows use the plugin directly.
#[allow(dead_code)]
pub fn updater_supported(os: &str) -> bool {
    matches!(os, "macos" | "windows")
}

/// The `app_update_check` result, serialized per the interface contract:
/// `{ "kind": "unsupported" }`, `{ "kind": "upToDate", ... }`,
/// `{ "kind": "available", ... }`.
#[derive(Debug, Serialize)]
#[serde(tag = "kind", rename_all = "camelCase")]
pub enum AppUpdateCheck {
    // Constructed only on non-updater targets in app_update_check; the
    // variant is part of the wire contract everywhere.
    #[cfg_attr(any(target_os = "macos", target_os = "windows"), allow(dead_code))]
    Unsupported,
    #[serde(rename_all = "camelCase")]
    UpToDate { current_version: String },
    #[serde(rename_all = "camelCase")]
    Available {
        version: String,
        current_version: String,
        notes: Option<String>,
        date: Option<String>,
    },
}

/// The update held from the last `available` check. `take` moves it out
/// (one install per check) and errors per the contract when empty.
pub struct PendingUpdate<T> {
    inner: Mutex<Option<T>>,
}

impl<T> PendingUpdate<T> {
    pub fn new() -> Self {
        Self {
            inner: Mutex::new(None),
        }
    }

    pub fn set(&self, update: T) {
        *self.inner.lock().unwrap() = Some(update);
    }

    /// Put an update back (a failed install must stay retryable).
    pub fn restore(&self, update: T) {
        self.set(update);
    }

    pub fn take(&self) -> Result<T, String> {
        self.inner
            .lock()
            .unwrap()
            .take()
            .ok_or_else(|| "No update is pending; check for updates again".to_string())
    }
}

impl<T> Default for PendingUpdate<T> {
    fn default() -> Self {
        Self::new()
    }
}

/// Settle an install attempt: a failure puts the update back so the
/// UI's retry doesn't hit "No update is pending".
pub fn settle_install<T>(
    pending: &PendingUpdate<T>,
    update: T,
    result: Result<(), String>,
) -> Result<(), String> {
    if result.is_err() {
        pending.restore(update);
    }
    result
}

/// RAII in-progress guard: `begin` fails with the contract's error while
/// an install runs; the flag clears when the guard drops (an AtomicBool,
/// not a held lock, so the install future stays Send).
pub struct InstallInProgress {
    flag: std::sync::atomic::AtomicBool,
}

pub struct InstallGuard<'a> {
    flag: &'a std::sync::atomic::AtomicBool,
}

impl Drop for InstallGuard<'_> {
    fn drop(&mut self) {
        self.flag.store(false, std::sync::atomic::Ordering::SeqCst);
    }
}

impl InstallInProgress {
    pub fn new() -> Self {
        Self {
            flag: std::sync::atomic::AtomicBool::new(false),
        }
    }

    /// Err with the contract's error while an install runs (no claim).
    pub fn ensure_idle(&self) -> Result<(), String> {
        if self.flag.load(std::sync::atomic::Ordering::SeqCst) {
            return Err("An update is already in progress".to_string());
        }
        Ok(())
    }

    pub fn begin(&self) -> Result<InstallGuard<'_>, String> {
        if self.flag.swap(true, std::sync::atomic::Ordering::SeqCst) {
            return Err("An update is already in progress".to_string());
        }
        Ok(InstallGuard { flag: &self.flag })
    }
}

impl Default for InstallInProgress {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(any(target_os = "macos", target_os = "windows"))]
use tauri_plugin_updater::Update;

/// Managed state: the pending update from the last check plus the
/// in-progress guard. On Linux (no updater) the slot just stays empty.
#[derive(Default)]
pub struct UpdateState {
    #[cfg(any(target_os = "macos", target_os = "windows"))]
    pub pending: PendingUpdate<Update>,
    pub install: InstallInProgress,
}

/// The check, over an already configured builder (production passes
/// `app.updater_builder()`; the e2e tests pass one aimed at a local feed).
/// A successful `available` result is stored as the pending update.
#[cfg(any(target_os = "macos", target_os = "windows"))]
pub(crate) async fn check_with(
    builder: tauri_plugin_updater::UpdaterBuilder,
    current_version: &str,
    state: &UpdateState,
) -> Result<AppUpdateCheck, String> {
    state.install.ensure_idle()?;
    let updater = builder.build().map_err(|e| e.to_string())?;
    match updater.check().await {
        Ok(Some(update)) => {
            log::info!("smind desktop: update available: {}", update.version);
            let result = AppUpdateCheck::Available {
                version: update.version.clone(),
                current_version: update.current_version.clone(),
                notes: cap_notes(update.body.clone()),
                date: update.date.map(|d| d.to_string()),
            };
            // An install may have started while the check ran.
            state.install.ensure_idle()?;
            state.pending.set(update);
            Ok(result)
        }
        Ok(None) => Ok(AppUpdateCheck::UpToDate {
            current_version: current_version.to_string(),
        }),
        Err(e) => {
            log::info!("smind desktop: update check failed: {e}");
            Err(e.to_string())
        }
    }
}

/// Download, verify the signature and install the pending update, reporting
/// progress through `emit` (the `app-update-progress` payloads). The caller
/// holds the in-progress guard (`state.install.begin()`) for as long as the
/// install and the relaunch take. `installing` is emitted only after the
/// signature check passed, so a refused download never shows it. A failure
/// leaves the update pending so the UI's retry still finds it. The
/// platform's relaunch is the caller's job: macOS restarts the app after
/// this returns, Windows' installer exits the app itself.
#[cfg(any(target_os = "macos", target_os = "windows"))]
pub(crate) async fn install_pending(
    state: &UpdateState,
    _guard: &InstallGuard<'_>,
    emit: &(dyn Fn(serde_json::Value) + Send + Sync),
) -> Result<(), String> {
    let update = state.pending.take()?;
    let result = async {
        let bytes = update
            .download(
                |downloaded, total| {
                    emit(serde_json::json!({
                        "stage": "downloading",
                        "downloaded": downloaded,
                        "total": total,
                    }));
                },
                || {},
            )
            .await?;
        emit(serde_json::json!({ "stage": "installing" }));
        update.install(bytes)
    }
    .await
    .map_err(|e| {
        log::info!("smind desktop: update install failed: {e}");
        e.to_string()
    });
    settle_install(&state.pending, update, result)
}

#[tauri::command]
pub async fn app_update_check(
    app: tauri::AppHandle,
    state: tauri::State<'_, UpdateState>,
) -> Result<AppUpdateCheck, String> {
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        let _ = (app, state);
        Ok(AppUpdateCheck::Unsupported)
    }
    #[cfg(any(target_os = "macos", target_os = "windows"))]
    {
        use tauri_plugin_updater::UpdaterExt;

        check_with(
            app.updater_builder(),
            &app.package_info().version.to_string(),
            &state,
        )
        .await
    }
}

#[tauri::command]
pub async fn app_update_install(
    app: tauri::AppHandle,
    state: tauri::State<'_, UpdateState>,
) -> Result<(), String> {
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        let _ = (app, state);
        Err("Updates are not supported on this platform".to_string())
    }
    #[cfg(any(target_os = "macos", target_os = "windows"))]
    {
        use tauri::Emitter;

        let guard = state.install.begin()?;
        install_pending(&state, &guard, &|payload| {
            let _ = app.emit("app-update-progress", payload);
        })
        .await?;
        // Windows: the NSIS installer exits the app itself. macOS: the
        // call never resolves -- restart does not return.
        #[cfg(target_os = "macos")]
        app.restart();
        #[cfg(not(target_os = "macos"))]
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::Engine;
    use serde_json::{json, Value};

    #[test]
    fn version_newer_stable_only() {
        assert!(is_offerable("0.10.0", "0.10.1"));
        assert!(is_offerable("0.10.0", "0.11.0"));
        assert!(is_offerable("0.10.0", "1.0.0"));
        assert!(!is_offerable("0.10.0", "0.10.0"));
        assert!(!is_offerable("0.10.0", "0.9.9"));
        assert!(!is_offerable("0.10.0", "0.11.0-rc.1"));
        assert!(!is_offerable("0.10.0", "0.11.0-dev+abc"));
        // Non-semver remote versions are rejected, not a panic.
        assert!(!is_offerable("0.10.0", "not-a-version"));
        assert!(!is_offerable("not-a-version", "0.11.0"));
    }

    #[test]
    fn notes_are_capped() {
        assert_eq!(cap_notes(None), None);
        assert_eq!(cap_notes(Some(String::new())), None);
        assert_eq!(
            cap_notes(Some("short".to_string())),
            Some("short".to_string())
        );
        let long: String = "é".repeat(NOTES_CAP + 10);
        let capped = cap_notes(Some(long)).expect("capped notes are Some");
        assert_eq!(capped.chars().count(), NOTES_CAP + 1); // cut + ellipsis
        assert!(capped.ends_with('…'));
    }

    #[test]
    fn check_result_serializes_to_contract() {
        assert_eq!(
            serde_json::to_value(AppUpdateCheck::Unsupported).unwrap(),
            json!({"kind": "unsupported"})
        );
        assert_eq!(
            serde_json::to_value(AppUpdateCheck::UpToDate {
                current_version: "0.10.0".to_string(),
            })
            .unwrap(),
            json!({"kind": "upToDate", "currentVersion": "0.10.0"})
        );
        assert_eq!(
            serde_json::to_value(AppUpdateCheck::Available {
                version: "0.11.0".to_string(),
                current_version: "0.10.0".to_string(),
                notes: Some("notes".to_string()),
                date: Some("2026-10-11T00:00:00Z".to_string()),
            })
            .unwrap(),
            json!({
                "kind": "available",
                "version": "0.11.0",
                "currentVersion": "0.10.0",
                "notes": "notes",
                "date": "2026-10-11T00:00:00Z",
            })
        );
    }

    #[test]
    fn install_without_pending_is_error() {
        let pending: PendingUpdate<u8> = PendingUpdate::new();
        assert_eq!(
            pending.take(),
            Err("No update is pending; check for updates again".to_string())
        );
        pending.set(1);
        assert_eq!(pending.take(), Ok(1));
        // A second take after one succeeded is empty again.
        assert!(pending.take().is_err());
    }

    #[test]
    fn failed_install_keeps_update_pending() {
        let pending: PendingUpdate<u8> = PendingUpdate::new();
        pending.set(7);
        let update = pending.take().unwrap();
        let err = settle_install(&pending, update, Err("boom".to_string()));
        assert_eq!(err, Err("boom".to_string()));
        assert_eq!(pending.take(), Ok(7), "retry still finds the update");
        // Success does not restore it.
        assert_eq!(settle_install(&pending, 7, Ok(())), Ok(()));
        assert!(pending.take().is_err());
    }

    #[test]
    fn check_rejected_while_install_in_progress() {
        let install = InstallInProgress::new();
        assert!(install.ensure_idle().is_ok());
        let _guard = install.begin().unwrap();
        assert_eq!(
            install.ensure_idle(),
            Err("An update is already in progress".to_string())
        );
    }

    #[test]
    fn install_rejects_concurrent_call() {
        let install = InstallInProgress::new();
        {
            let _guard = install.begin().expect("first begin succeeds");
            assert_eq!(
                install.begin().err(),
                Some("An update is already in progress".to_string())
            );
        }
        // Released on drop.
        assert!(install.begin().is_ok());
    }

    #[test]
    fn updater_supported_by_platform() {
        assert!(updater_supported("macos"));
        assert!(updater_supported("windows"));
        assert!(!updater_supported("linux"));
    }

    #[test]
    fn updater_config_pins_key_and_endpoint() {
        let conf: Value = serde_json::from_str(TAURI_CONF).expect("tauri.conf.json parses");
        let updater = &conf["plugins"]["updater"];
        let pubkey = updater["pubkey"].as_str().expect("pubkey is a string");
        assert!(!pubkey.is_empty());
        let decoded = base64::engine::general_purpose::STANDARD
            .decode(pubkey)
            .expect("pubkey base64-decodes");
        let text = String::from_utf8(decoded).expect("pubkey decodes to UTF-8 text");
        assert!(
            text.starts_with("untrusted comment:"),
            "pubkey is a minisign public key"
        );
        let endpoints: Vec<&str> = updater["endpoints"]
            .as_array()
            .expect("endpoints array")
            .iter()
            .filter_map(Value::as_str)
            .collect();
        assert_eq!(
            endpoints,
            vec!["https://github.com/spacingmind/smind/releases/latest/download/latest.json"],
            "exactly one endpoint, the stable-channel latest.json"
        );
        assert!(
            updater.get("dangerousInsecureTransportProtocol").is_none(),
            "dangerousInsecureTransportProtocol must never be committed"
        );
    }

    #[test]
    fn create_updater_artifacts_opt_in_only() {
        let conf: Value = serde_json::from_str(TAURI_CONF).expect("tauri.conf.json parses");
        let create = conf["bundle"]["createUpdaterArtifacts"].clone();
        assert!(
            create.is_null() || create == Value::Bool(false),
            "base config must not ask for updater artifacts (keyless builds)"
        );
        for (name, src) in [
            (
                "tauri.macos.conf.json",
                include_str!("../tauri.macos.conf.json"),
            ),
            (
                "tauri.windows.conf.json",
                include_str!("../tauri.windows.conf.json"),
            ),
        ] {
            let c: Value =
                serde_json::from_str(src).unwrap_or_else(|e| panic!("{name} parses: {e}"));
            let create = c["bundle"]["createUpdaterArtifacts"].clone();
            assert!(
                create.is_null() || create == Value::Bool(false),
                "{name} must not ask for updater artifacts"
            );
        }
        let updater_conf: Value =
            serde_json::from_str(UPDATER_CONF).expect("tauri.updater.conf.json parses");
        assert_eq!(
            updater_conf["bundle"]["createUpdaterArtifacts"],
            Value::Bool(true),
            "the opt-in merge config asks for updater artifacts"
        );
    }
}
