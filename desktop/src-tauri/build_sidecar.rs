//! The pure half of build.rs's sidecar placeholder, in its own file so
//! `build.rs` (`#[path]`) and the crate's test build
//! (`daemon_manager.rs`) share one definition -- `cargo test` never runs
//! tests declared inside a build script.

use std::path::PathBuf;

/// sidecar_placeholder_name is the file a dev build must have under
/// `binaries/` for tauri-build to accept the `externalBin` entry
/// `binaries/smind` (`tauri.{macos,windows}.conf.json`): `smind-<triple>`,
/// plus `.exe` on Windows. `None` means "write nothing": release builds
/// never get a placeholder (a real bundle must not silently ship without a
/// daemon), and other platforms have no sidecar.
pub fn sidecar_placeholder_name(target_os: &str, profile: &str, target: &str) -> Option<PathBuf> {
    if profile == "release" {
        return None;
    }
    let ext = match target_os {
        "macos" => "",
        "windows" => ".exe",
        _ => return None,
    };
    Some(PathBuf::from("binaries").join(format!("smind-{target}{ext}")))
}

#[cfg(test)]
mod tests {
    use super::{sidecar_placeholder_name as name, PathBuf};

    #[test]
    fn sidecar_placeholder_name() {
        let win = "x86_64-pc-windows-msvc";
        assert_eq!(name("windows", "debug", win), Some(PathBuf::from("binaries/smind-x86_64-pc-windows-msvc.exe")));
        assert_eq!(name("macos", "debug", "aarch64-apple-darwin"), Some(PathBuf::from("binaries/smind-aarch64-apple-darwin")));
        assert_eq!(name("windows", "release", win), None, "release never writes a placeholder");
        assert_eq!(name("macos", "release", "aarch64-apple-darwin"), None);
        assert_eq!(name("linux", "debug", "x86_64-unknown-linux-gnu"), None);
    }
}
