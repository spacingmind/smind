//! ADR-0031 D7: the Rust-owned `app-settings.json` (`onboardingCompleted`,
//! `manageLocalDaemon`). SCAFFOLD: the command shapes are fixed by the P5
//! contract (`web/packages/ui/src/lib/platform.ts`); the bodies are filled in
//! by the P5 Rust work.

use serde::{Deserialize, Serialize};

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

#[tauri::command]
pub fn app_settings_get() -> Result<AppSettings, String> {
    Err("app_settings_get: not implemented yet".to_string())
}

#[tauri::command]
pub fn app_settings_set(_patch: AppSettingsPatch) -> Result<AppSettings, String> {
    Err("app_settings_set: not implemented yet".to_string())
}
