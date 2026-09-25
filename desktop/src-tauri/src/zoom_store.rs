//! On-disk persistence for the View menu's zoom level (quick-wins AC2).
//! The clamping/formatting logic itself lives in
//! `smind_daemon_client::zoom`, which is unit-tested without a webview;
//! this module is just the IO + applying it to a real window.

use std::path::PathBuf;

use tauri::{AppHandle, Manager};

use smind_daemon_client::zoom;

const FILE_NAME: &str = "zoom.txt";

fn state_path(app: &AppHandle) -> Option<PathBuf> {
    app.path().app_config_dir().ok().map(|dir| dir.join(FILE_NAME))
}

/// load reads the persisted zoom level, defaulting to 100% if there is
/// none yet or the file is unreadable/corrupt.
pub fn load(app: &AppHandle) -> f64 {
    state_path(app)
        .and_then(|path| std::fs::read_to_string(path).ok())
        .map(|raw| zoom::parse(&raw))
        .unwrap_or(zoom::DEFAULT)
}

/// save persists `level`, creating the app config directory if needed.
/// Best-effort: a failed write only costs the next launch its saved
/// zoom, not a crash.
pub fn save(app: &AppHandle, level: f64) {
    let Some(path) = state_path(app) else { return };
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    let _ = std::fs::write(path, zoom::serialize(level));
}
