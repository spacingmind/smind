//! Release asset naming and URL resolution (AC2/AC6). The only place a
//! download URL is built -- always the fixed `spacingmind/smind` repo,
//! always the *app's own* version (never "latest", never user input).

use std::fmt;

const REPO: &str = "spacingmind/smind";

/// asset_name matches exactly what `.github/workflows/release-please.yml`'s
/// `build-binaries` job produces: `smind_<version>_<os>_<arch>.tar.gz`,
/// where `os`/`arch` are the release's own vocabulary (`linux`/`darwin`,
/// `amd64`/`arm64`), not Rust's `std::env::consts` spelling.
pub fn asset_name(version: &str, os: &str, arch: &str) -> String {
    format!("smind_{version}_{os}_{arch}.tar.gz")
}

/// native_target maps the current process's `std::env::consts::{OS,ARCH}`
/// to the release's `(os, arch)` vocabulary, for the macOS native path.
/// The WSL2 path does not use this -- it asks the distro itself (`wsl`
/// module), since the Windows host's own arch is irrelevant.
pub fn native_target(os: &str, arch: &str) -> Result<(&'static str, &'static str), String> {
    let release_os = match os {
        "macos" => "darwin",
        other => return Err(format!("unsupported native OS {other:?}")),
    };
    let release_arch = match arch {
        "x86_64" => "amd64",
        "aarch64" => "arm64",
        other => return Err(format!("unsupported native arch {other:?}")),
    };
    Ok((release_os, release_arch))
}

/// linux_target maps a WSL2 distro's own `uname -m` output to the
/// release's arch vocabulary. The OS side is always `linux` for WSL2.
pub fn linux_target(uname_m: &str) -> Result<&'static str, String> {
    match uname_m.trim() {
        "x86_64" => Ok("amd64"),
        "aarch64" | "arm64" => Ok("arm64"),
        other => Err(format!("unsupported WSL distro arch {other:?}")),
    }
}

#[derive(Debug, Clone)]
pub struct ReleaseUrls {
    pub tarball: String,
    pub checksums: String,
    pub asset_name: String,
}

/// release_urls builds the two fixed download URLs for `version` (the
/// app's own version, with or without a leading `v`) and this asset's
/// `(os, arch)`. No caller ever supplies a URL directly.
pub fn release_urls(version: &str, os: &str, arch: &str) -> ReleaseUrls {
    let tag = if version.starts_with('v') { version.to_string() } else { format!("v{version}") };
    let bare = tag.trim_start_matches('v');
    let name = asset_name(bare, os, arch);
    let base = format!("https://github.com/{REPO}/releases/download/{tag}");
    ReleaseUrls { tarball: format!("{base}/{name}"), checksums: format!("{base}/checksums.txt"), asset_name: name }
}

#[derive(Debug, Clone)]
pub enum ReleaseError {
    /// The version itself has no published release (e.g. a dev build).
    NoRelease { version: String },
    /// The release exists but has no asset for this platform.
    NoAsset { version: String, asset: String },
    Network(String),
    ChecksumMismatch { asset: String },
    Io(String),
}

impl fmt::Display for ReleaseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ReleaseError::NoRelease { version } => {
                write!(f, "no published release for version {version:?} -- this looks like a dev build")
            }
            ReleaseError::NoAsset { version, asset } => {
                write!(f, "release {version:?} has no asset {asset:?} for this platform")
            }
            ReleaseError::Network(e) => write!(f, "network error fetching release asset: {e}"),
            ReleaseError::ChecksumMismatch { asset } => {
                write!(f, "checksum mismatch for {asset:?} -- refusing to install a corrupted download")
            }
            ReleaseError::Io(e) => write!(f, "io error installing release asset: {e}"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn asset_name_matches_release_workflow_shape() {
        assert_eq!(asset_name("0.7.0", "linux", "amd64"), "smind_0.7.0_linux_amd64.tar.gz");
        assert_eq!(asset_name("0.7.0", "linux", "arm64"), "smind_0.7.0_linux_arm64.tar.gz");
        assert_eq!(asset_name("0.7.0", "darwin", "amd64"), "smind_0.7.0_darwin_amd64.tar.gz");
        assert_eq!(asset_name("0.7.0", "darwin", "arm64"), "smind_0.7.0_darwin_arm64.tar.gz");
    }

    #[test]
    fn native_target_maps_macos() {
        assert_eq!(native_target("macos", "x86_64").unwrap(), ("darwin", "amd64"));
        assert_eq!(native_target("macos", "aarch64").unwrap(), ("darwin", "arm64"));
        assert!(native_target("windows", "x86_64").is_err());
        assert!(native_target("macos", "riscv64").is_err());
    }

    #[test]
    fn linux_target_maps_wsl_arch() {
        assert_eq!(linux_target("x86_64").unwrap(), "amd64");
        assert_eq!(linux_target("aarch64\n").unwrap(), "arm64");
        assert!(linux_target("mips").is_err());
    }

    #[test]
    fn release_urls_use_fixed_repo_and_app_version() {
        let urls = release_urls("0.7.0", "darwin", "arm64");
        assert_eq!(
            urls.tarball,
            "https://github.com/spacingmind/smind/releases/download/v0.7.0/smind_0.7.0_darwin_arm64.tar.gz"
        );
        assert_eq!(
            urls.checksums,
            "https://github.com/spacingmind/smind/releases/download/v0.7.0/checksums.txt"
        );
        assert_eq!(urls.asset_name, "smind_0.7.0_darwin_arm64.tar.gz");
    }

    #[test]
    fn release_urls_tolerate_v_prefix_input() {
        let urls = release_urls("v0.7.0", "linux", "amd64");
        assert!(urls.tarball.contains("/v0.7.0/"));
        assert!(!urls.asset_name.starts_with('v'));
    }
}
