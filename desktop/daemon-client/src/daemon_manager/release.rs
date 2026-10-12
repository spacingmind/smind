//! Release asset naming and URL resolution (AC2/AC6). The only place a
//! download URL is built -- always the fixed `spacingmind/smind` repo,
//! always the *app's own* version (never "latest", never user input).

use std::fmt;

const REPO: &str = "spacingmind/smind";

/// `IMAGE_FILE_MACHINE_*` values from winnt.h, named locally so the pure
/// mapping below stays testable without importing windows-sys here.
pub const IMAGE_FILE_MACHINE_UNKNOWN: u16 = 0x0000;
pub const IMAGE_FILE_MACHINE_I386: u16 = 0x014c;
pub const IMAGE_FILE_MACHINE_AMD64: u16 = 0x8664;
pub const IMAGE_FILE_MACHINE_ARM64: u16 = 0xaa64;

/// asset_name matches exactly what `.github/workflows/release-please.yml`'s
/// `build-binaries` job produces: `smind_<version>_<os>_<arch>.tar.gz`
/// everywhere except windows, whose release assets are `.zip`. `os`/`arch`
/// are the release's own vocabulary (`linux`/`darwin`/`windows`,
/// `amd64`/`arm64`), not Rust's `std::env::consts` spelling.
pub fn asset_name(version: &str, os: &str, arch: &str) -> String {
    let ext = if os == "windows" { "zip" } else { "tar.gz" };
    format!("smind_{version}_{os}_{arch}.{ext}")
}

/// windows_target_from_os_arch maps the two machines IsWow64Process2
/// reports (the process's own, and the OS's native) to the release's arch
/// vocabulary. The *native* machine decides: an x64 app running under
/// emulation on ARM64 must download the arm64 daemon, which is why
/// `cfg!(target_arch)` can never be used for this. The process machine is
/// only a fallback where native detection returns UNKNOWN.
pub fn windows_target_from_os_arch(process_machine: u16, native_machine: u16) -> Result<&'static str, String> {
    match native_machine {
        IMAGE_FILE_MACHINE_ARM64 => Ok("arm64"),
        IMAGE_FILE_MACHINE_AMD64 => Ok("amd64"),
        IMAGE_FILE_MACHINE_UNKNOWN => match process_machine {
            IMAGE_FILE_MACHINE_ARM64 => Ok("arm64"),
            IMAGE_FILE_MACHINE_AMD64 => Ok("amd64"),
            other => Err(format!("unsupported windows machine {other:#06x}")),
        },
        other => Err(format!("unsupported native windows machine {other:#06x}")),
    }
}

/// native_target maps the current process's `std::env::consts::{OS,ARCH}`
/// to the release's `(os, arch)` vocabulary, for the macOS native path.
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
    fn asset_name_windows_amd64_and_arm64() {
        assert_eq!(asset_name("0.7.0", "windows", "amd64"), "smind_0.7.0_windows_amd64.zip");
        assert_eq!(asset_name("0.7.0", "windows", "arm64"), "smind_0.7.0_windows_arm64.zip");
        // non-windows assets are unchanged
        assert!(asset_name("0.7.0", "darwin", "arm64").ends_with(".tar.gz"));
    }

    #[test]
    fn windows_target_from_os_arch() {
        use super::*;
        // Native x64 process on x64 windows.
        assert_eq!(windows_target_from_os_arch(IMAGE_FILE_MACHINE_AMD64, IMAGE_FILE_MACHINE_AMD64).unwrap(), "amd64");
        // Native arm64 process on arm64 windows.
        assert_eq!(windows_target_from_os_arch(IMAGE_FILE_MACHINE_UNKNOWN, IMAGE_FILE_MACHINE_ARM64).unwrap(), "arm64");
        // x64 app running under emulation on ARM64: the OS machine wins,
        // never the app's own target_arch.
        assert_eq!(windows_target_from_os_arch(IMAGE_FILE_MACHINE_AMD64, IMAGE_FILE_MACHINE_ARM64).unwrap(), "arm64");
        // Native machine undetectable: fall back to the process machine.
        assert_eq!(windows_target_from_os_arch(IMAGE_FILE_MACHINE_ARM64, IMAGE_FILE_MACHINE_UNKNOWN).unwrap(), "arm64");
        assert!(windows_target_from_os_arch(IMAGE_FILE_MACHINE_I386, IMAGE_FILE_MACHINE_UNKNOWN).is_err());
        assert!(windows_target_from_os_arch(IMAGE_FILE_MACHINE_AMD64, 0x01c4).is_err());
    }

    #[test]
    fn release_urls_produce_zip_for_windows() {
        let urls = release_urls("0.7.0", "windows", "amd64");
        assert_eq!(urls.tarball, "https://github.com/spacingmind/smind/releases/download/v0.7.0/smind_0.7.0_windows_amd64.zip");
        assert_eq!(
            urls.checksums,
            "https://github.com/spacingmind/smind/releases/download/v0.7.0/checksums.txt"
        );
        assert_eq!(urls.asset_name, "smind_0.7.0_windows_amd64.zip");
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
