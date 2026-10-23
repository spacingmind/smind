//! The loopback proxy's port from the previous launch, kept in the app data
//! dir so the webview origin (and with it every localStorage preference) stays
//! the same across launches. See `smind_daemon_client::proxy::serve_preferring`.

use std::path::{Path, PathBuf};

pub const PORT_FILE: &str = "proxy-port";

/// The saved port; a missing, unparsable or privileged (< 1024) value is
/// "no preference", never an error.
pub fn load(path: &Path) -> Option<u16> {
    let port: u16 = std::fs::read_to_string(path).ok()?.trim().parse().ok()?;
    (port >= 1024).then_some(port)
}

/// save records the port the proxy actually bound. Best effort: a failed write
/// only means the next launch picks a fresh port.
pub fn save(path: &Path, port: u16) {
    if load(path) == Some(port) {
        return;
    }
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    if let Err(e) = std::fs::write(path, port.to_string()) {
        log::warn!("could not remember the proxy port {port}: {e}");
    }
}

pub fn path_in(app_data_dir: &Path) -> PathBuf {
    app_data_dir.join(PORT_FILE)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_and_ignores_garbage() {
        let dir = tempfile::tempdir().unwrap();
        let path = path_in(dir.path());
        assert_eq!(load(&path), None);
        save(&path, 51234);
        assert_eq!(load(&path), Some(51234));
        std::fs::write(&path, "not a port").unwrap();
        assert_eq!(load(&path), None);
        std::fs::write(&path, "80").unwrap();
        assert_eq!(load(&path), None, "privileged ports are never reused");
        std::fs::write(&path, "99999").unwrap();
        assert_eq!(load(&path), None);
    }

    #[test]
    fn save_creates_the_directory() {
        let dir = tempfile::tempdir().unwrap();
        let path = path_in(&dir.path().join("nested"));
        save(&path, 40001);
        assert_eq!(load(&path), Some(40001));
    }
}
