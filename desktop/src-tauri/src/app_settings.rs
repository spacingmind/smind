//! ADR-0031 D7: the Rust-owned `app-settings.json` (`onboardingCompleted`,
//! `manageLocalDaemon`) next to `connections.json`. The command shapes are
//! fixed by the P5 contract (`web/packages/ui/src/lib/platform.ts`).

use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use tauri::State;

use crate::state::DesktopState;

pub const SETTINGS_FILE: &str = "app-settings.json";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AppSettings {
    pub onboarding_completed: bool,
    pub manage_local_daemon: bool,
}

/// A partial update: only the named fields change.
#[derive(Debug, Clone, Copy, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AppSettingsPatch {
    pub onboarding_completed: Option<bool>,
    pub manage_local_daemon: Option<bool>,
}

impl AppSettings {
    /// First run: nothing chosen yet, and the app manages the local daemon
    /// once the user picks "run here".
    pub const FIRST_RUN: AppSettings = AppSettings { onboarding_completed: false, manage_local_daemon: true };
    const UPGRADED: AppSettings = AppSettings { onboarding_completed: true, manage_local_daemon: true };

    pub fn apply(self, patch: AppSettingsPatch) -> AppSettings {
        AppSettings {
            onboarding_completed: patch.onboarding_completed.unwrap_or(self.onboarding_completed),
            manage_local_daemon: patch.manage_local_daemon.unwrap_or(self.manage_local_daemon),
        }
    }
}

/// load reads the settings file; a missing or unparsable file is first run
/// (never a panic, and a corrupt file is left alone until the next `save`).
pub fn load(path: &Path) -> AppSettings {
    std::fs::read(path).ok().and_then(|raw| serde_json::from_slice(&raw).ok()).unwrap_or(AppSettings::FIRST_RUN)
}

/// save writes atomically (temp file + rename).
pub fn save(path: &Path, settings: &AppSettings) -> Result<(), String> {
    let dir = path.parent().ok_or_else(|| "settings path has no parent directory".to_string())?;
    std::fs::create_dir_all(dir).map_err(|e| format!("create {}: {e}", dir.display()))?;
    let json = serde_json::to_vec_pretty(settings).map_err(|e| e.to_string())?;
    let tmp = path.with_extension("json.tmp");
    std::fs::write(&tmp, json).map_err(|e| format!("write {}: {e}", tmp.display()))?;
    std::fs::rename(&tmp, path).map_err(|e| {
        let _ = std::fs::remove_file(&tmp);
        format!("replace {}: {e}", path.display())
    })
}

/// load_or_upgrade is the startup evaluation: an absent file on an install
/// that already has saved connections or a managed-daemon record is an
/// upgrade, not a first run -- it is written as onboarded so those installs
/// keep today's behaviour and never see first-run.
pub fn load_or_upgrade(path: &Path, has_connections: bool, has_managed_record: bool) -> AppSettings {
    if !path.exists() && (has_connections || has_managed_record) {
        let upgraded = AppSettings::UPGRADED;
        if let Err(e) = save(path, &upgraded) {
            log::warn!("could not persist upgraded app settings: {e}");
        }
        return upgraded;
    }
    load(path)
}

pub fn settings_path(connections_path: &Path) -> PathBuf {
    connections_path.parent().map(Path::to_path_buf).unwrap_or_default().join(SETTINGS_FILE)
}

#[tauri::command]
pub fn app_settings_get(state: State<'_, DesktopState>) -> Result<AppSettings, String> {
    Ok(*state.app_settings.lock().unwrap())
}

#[tauri::command]
pub fn app_settings_set(state: State<'_, DesktopState>, patch: AppSettingsPatch) -> Result<AppSettings, String> {
    let mut current = state.app_settings.lock().unwrap();
    let next = current.apply(patch);
    save(&state.app_settings_path, &next)?;
    *current = next;
    Ok(next)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn path(dir: &tempfile::TempDir) -> PathBuf {
        dir.path().join(SETTINGS_FILE)
    }

    #[test]
    fn app_settings_missing_file_means_first_run() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(load_or_upgrade(&path(&dir), false, false), AppSettings::FIRST_RUN);
        assert!(!path(&dir).exists(), "first run writes nothing until a set");
    }

    #[test]
    fn app_settings_corrupt_file_falls_back_to_first_run() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(path(&dir), b"{not json").unwrap();
        // Even with saved connections: the file exists, so no upgrade rule.
        assert_eq!(load_or_upgrade(&path(&dir), true, true), AppSettings::FIRST_RUN);
        assert_eq!(std::fs::read(path(&dir)).unwrap(), b"{not json", "a corrupt file is not overwritten");
    }

    #[test]
    fn upgrade_rule_existing_connections_marks_onboarded() {
        let dir = tempfile::tempdir().unwrap();
        let got = load_or_upgrade(&path(&dir), true, false);
        assert_eq!(got, AppSettings { onboarding_completed: true, manage_local_daemon: true });
        assert_eq!(load(&path(&dir)), got, "persisted");
    }

    #[test]
    fn upgrade_rule_existing_managed_record_marks_onboarded() {
        let dir = tempfile::tempdir().unwrap();
        let got = load_or_upgrade(&path(&dir), false, true);
        assert_eq!(got, AppSettings { onboarding_completed: true, manage_local_daemon: true });
        assert_eq!(load(&path(&dir)), got);
    }

    #[test]
    fn app_settings_patch_only_changes_named_fields() {
        let dir = tempfile::tempdir().unwrap();
        let p = path(&dir);
        let first = AppSettings::FIRST_RUN.apply(AppSettingsPatch { manage_local_daemon: Some(false), ..Default::default() });
        assert_eq!(first, AppSettings { onboarding_completed: false, manage_local_daemon: false });
        save(&p, &first).unwrap();
        let second = load(&p).apply(AppSettingsPatch { onboarding_completed: Some(true), ..Default::default() });
        assert_eq!(second, AppSettings { onboarding_completed: true, manage_local_daemon: false });
        assert_eq!(AppSettings::FIRST_RUN.apply(AppSettingsPatch::default()), AppSettings::FIRST_RUN);
        assert!(!p.with_extension("json.tmp").exists());
    }
}
