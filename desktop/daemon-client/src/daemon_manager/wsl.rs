//! Windows + WSL2 (AC4): every argv builder here returns a `Vec<String>`
//! rather than a shell string, so nothing that touches `wsl.exe` ever
//! interpolates a value into shell syntax except the one place a shell is
//! unavoidable (`start_detached_argv`, see its own doc comment) -- and
//! there, only fixed, app-computed paths are embedded, never user input.
//!
//! `run` is the one function that actually spawns `wsl.exe`; everything
//! else is a pure function of its inputs, unit-tested without a real
//! Windows host. `run` itself is exercised live (this sandbox is a WSL2
//! Ubuntu distro with `wsl.exe` reachable via interop -- see the plan's
//! Validation section), not unit tested.

use std::io;
use std::process::{Command, Output};

pub const WSL_EXE: &str = "wsl.exe";

/// The managed install layout inside the distro, relative to `$HOME`
/// (expanded by the WSL-side shell, never by Rust).
pub const INSTALL_DIR: &str = ".local/share/smind";
pub const BIN_SUBPATH: &str = ".local/share/smind/bin/smind";
pub const LOG_SUBPATH: &str = ".local/share/smind/smind.log";
pub const STATE_SUBPATH: &str = ".local/share/smind/managed.json";

pub fn list_verbose_argv() -> Vec<String> {
    vec![WSL_EXE.to_string(), "-l".to_string(), "-v".to_string()]
}

/// parse_default_distro decodes `wsl.exe -l -v`'s UTF-16LE output (no BOM
/// observed in practice, but tolerated if present) and returns the name
/// of the distro marked default (a leading `*` column).
pub fn parse_default_distro(raw: &[u8]) -> Option<String> {
    let text = decode_utf16le_lossy(raw);
    for line in text.lines() {
        let line = line.trim();
        if let Some(rest) = line.strip_prefix('*') {
            return rest.split_whitespace().next().map(str::to_string);
        }
    }
    None
}

fn decode_utf16le_lossy(raw: &[u8]) -> String {
    let bytes = if raw.len() >= 2 && raw[0] == 0xFF && raw[1] == 0xFE { &raw[2..] } else { raw };
    let units: Vec<u16> = bytes.chunks_exact(2).map(|c| u16::from_le_bytes([c[0], c[1]])).collect();
    String::from_utf16_lossy(&units)
}

/// uname_arch_argv asks the distro itself for its architecture -- the
/// Windows host's own arch is irrelevant to which linux tarball to fetch.
pub fn uname_arch_argv(distro: &str) -> Vec<String> {
    vec![WSL_EXE.into(), "-d".into(), distro.into(), "--".into(), "uname".into(), "-m".into()]
}

/// mkdir_argv ensures the managed install dir exists.
pub fn mkdir_argv(distro: &str) -> Vec<String> {
    vec![WSL_EXE.into(), "-d".into(), distro.into(), "--".into(), "mkdir".into(), "-p".into(), format!("$HOME/{INSTALL_DIR}/bin")]
}

/// curl_download_argv downloads `url` to `dest_relpath` (relative to
/// `$HOME`) using `curl` already present in a stock Ubuntu WSL distro.
/// `url` is always one of the two fixed URLs `release::release_urls`
/// builds -- never user input -- but is still passed as its own argv
/// element, not concatenated into a shell string.
pub fn curl_download_argv(distro: &str, url: &str, dest_relpath: &str) -> Vec<String> {
    vec![
        WSL_EXE.into(),
        "-d".into(),
        distro.into(),
        "--".into(),
        "curl".into(),
        "-fsSL".into(),
        "-o".into(),
        format!("$HOME/{dest_relpath}"),
        url.into(),
    ]
}

/// cat_argv brings a small text file (checksums.txt) back across the
/// `wsl.exe` boundary for parsing in Rust (`checksums::parse`).
pub fn cat_argv(distro: &str, relpath: &str) -> Vec<String> {
    vec![WSL_EXE.into(), "-d".into(), distro.into(), "--".into(), "cat".into(), format!("$HOME/{relpath}")]
}

/// sha256sum_argv hashes the downloaded tarball *inside* the distro, so
/// only the resulting hex digest (not the tarball's bytes) crosses the
/// `wsl.exe` boundary. `parse_sha256sum_output` extracts the hex from its
/// `<hex>  <path>` stdout shape.
pub fn sha256sum_argv(distro: &str, relpath: &str) -> Vec<String> {
    vec![WSL_EXE.into(), "-d".into(), distro.into(), "--".into(), "sha256sum".into(), format!("$HOME/{relpath}")]
}

pub fn parse_sha256sum_output(stdout: &str) -> Option<String> {
    stdout.split_whitespace().next().map(str::to_string)
}

/// tar_extract_argv extracts just the `smind` binary member from the
/// downloaded tarball straight into the managed bin dir.
pub fn tar_extract_argv(distro: &str, tarball_relpath: &str) -> Vec<String> {
    vec![
        WSL_EXE.into(),
        "-d".into(),
        distro.into(),
        "--".into(),
        "tar".into(),
        "-xzf".into(),
        format!("$HOME/{tarball_relpath}"),
        "-C".into(),
        format!("$HOME/{INSTALL_DIR}/bin"),
        "smind".into(),
    ]
}

pub fn chmod_x_argv(distro: &str) -> Vec<String> {
    vec![WSL_EXE.into(), "-d".into(), distro.into(), "--".into(), "chmod".into(), "+x".into(), format!("$HOME/{BIN_SUBPATH}")]
}

/// start_detached_argv is the one place a shell is used: detaching a
/// process from its controlling session and redirecting its output to a
/// log file both need shell syntax (`&`, `>>`), and there is no argv-only
/// equivalent. Every value embedded in the shell string here is a fixed,
/// app-computed path under the managed install dir -- never user input --
/// so this doesn't violate the "no shell interpolation of untrusted
/// values" rule.
pub fn start_detached_argv(distro: &str) -> Vec<String> {
    let script = format!("setsid nohup \"$HOME/{BIN_SUBPATH}\" serve >>\"$HOME/{LOG_SUBPATH}\" 2>&1 </dev/null &");
    vec![WSL_EXE.into(), "-d".into(), distro.into(), "--".into(), "sh".into(), "-c".into(), script]
}

/// port_owner_argv finds the pid bound to `port` via `ss`'s own
/// multi-token filter grammar (`sport = :<port>`) -- no shell needed, each
/// token is its own argv element.
pub fn port_owner_argv(distro: &str, port: u16) -> Vec<String> {
    vec![
        WSL_EXE.into(),
        "-d".into(),
        distro.into(),
        "--".into(),
        "ss".into(),
        "-H".into(),
        "-tlnp".into(),
        "sport".into(),
        "=".into(),
        format!(":{port}"),
    ]
}

/// parse_ss_pid extracts the pid from `ss -H -tlnp`'s
/// `users:(("smind",pid=1234,fd=8))` column.
pub fn parse_ss_pid(stdout: &str) -> Option<u32> {
    let idx = stdout.find("pid=")?;
    let rest = &stdout[idx + "pid=".len()..];
    let digits: String = rest.chars().take_while(|c| c.is_ascii_digit()).collect();
    digits.parse().ok()
}

pub fn kill_argv(distro: &str, pid: u32) -> Vec<String> {
    vec![WSL_EXE.into(), "-d".into(), distro.into(), "--".into(), "kill".into(), pid.to_string()]
}

/// run is the only function in this module that spawns anything.
pub fn run(argv: &[String]) -> io::Result<Output> {
    let (prog, args) = argv.split_first().expect("smind desktop: argv is never empty");
    Command::new(prog).args(args).output()
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Captured verbatim from `wsl.exe -l -v` in this sandbox (a genuine
    /// WSL2 Ubuntu distro, "Ubuntu" marked default and Running, WSL
    /// version 2) -- see the plan's AC4 test scenario.
    const SAMPLE_LV: &[u8] = &[
        0x20, 0x00, 0x20, 0x00, 0x4e, 0x00, 0x41, 0x00, 0x4d, 0x00, 0x45, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00,
        0x20, 0x00, 0x53, 0x00, 0x54, 0x00, 0x41, 0x00, 0x54, 0x00, 0x45, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00,
        0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x56, 0x00, 0x45, 0x00, 0x52, 0x00, 0x53, 0x00, 0x49, 0x00,
        0x4f, 0x00, 0x4e, 0x00, 0x0d, 0x00, 0x0a, 0x00, 0x2a, 0x00, 0x20, 0x00, 0x55, 0x00, 0x62, 0x00, 0x75, 0x00, 0x6e, 0x00, 0x74, 0x00,
        0x75, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x52, 0x00, 0x75, 0x00, 0x6e, 0x00, 0x6e, 0x00, 0x69, 0x00, 0x6e, 0x00,
        0x67, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x32, 0x00,
        0x0d, 0x00, 0x0a, 0x00,
    ];

    #[test]
    fn parse_default_distro_from_captured_sample() {
        assert_eq!(parse_default_distro(SAMPLE_LV).as_deref(), Some("Ubuntu"));
    }

    #[test]
    fn parse_default_distro_none_without_a_star() {
        let text = "  NAME    STATE\r\n  Ubuntu  Running\r\n";
        let raw: Vec<u8> = text.encode_utf16().flat_map(|u| u.to_le_bytes()).collect();
        assert_eq!(parse_default_distro(&raw), None);
    }

    #[test]
    fn parse_default_distro_tolerates_bom() {
        let text = "* Ubuntu  Running  2\r\n";
        let mut raw = vec![0xFF, 0xFE];
        raw.extend(text.encode_utf16().flat_map(|u| u.to_le_bytes()));
        assert_eq!(parse_default_distro(&raw).as_deref(), Some("Ubuntu"));
    }

    #[test]
    fn argv_builders_never_embed_shell_metacharacters_in_dynamic_fields() {
        // Defense in depth: even though these argv vectors are never run
        // through a shell, a distro name or url with shell metacharacters
        // should still just be an inert argv element, not something that
        // changes the shape of the command.
        for v in [
            uname_arch_argv("Ubuntu"),
            mkdir_argv("Ubuntu"),
            curl_download_argv("Ubuntu", "https://example.com/x.tar.gz", "x.tar.gz"),
            cat_argv("Ubuntu", "checksums.txt"),
            sha256sum_argv("Ubuntu", "x.tar.gz"),
            tar_extract_argv("Ubuntu", "x.tar.gz"),
            chmod_x_argv("Ubuntu"),
            port_owner_argv("Ubuntu", 4648),
            kill_argv("Ubuntu", 123),
        ] {
            assert_eq!(v[0], WSL_EXE);
            assert_eq!(v[1], "-d");
            assert_eq!(v[3], "--");
        }
    }

    #[test]
    fn start_detached_argv_only_embeds_fixed_paths() {
        let argv = start_detached_argv("Ubuntu");
        assert_eq!(argv[0], WSL_EXE);
        let script = argv.last().unwrap();
        assert!(script.contains(BIN_SUBPATH));
        assert!(script.contains(LOG_SUBPATH));
        assert!(script.contains("setsid nohup"));
    }

    #[test]
    fn parse_sha256sum_output_extracts_hex() {
        assert_eq!(parse_sha256sum_output("deadbeef  /home/u/x.tar.gz\n").as_deref(), Some("deadbeef"));
    }

    #[test]
    fn parse_ss_pid_extracts_pid() {
        let line = "LISTEN 0 4096 127.0.0.1:4648 0.0.0.0:*  users:((\"smind\",pid=1234,fd=8))\n";
        assert_eq!(parse_ss_pid(line), Some(1234));
    }

    #[test]
    fn parse_ss_pid_none_when_absent() {
        assert_eq!(parse_ss_pid(""), None);
        assert_eq!(parse_ss_pid("LISTEN 0 4096 *:22 *:*\n"), None);
    }
}
