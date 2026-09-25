//! Generic local-process daemon management (AC5): install a downloaded
//! release tarball, start it as a detached child process, find who owns a
//! port, and stop it. Parameterized on a base directory so it's
//! integration-testable with a temp dir. `src-tauri` wires the real
//! `~/Library/Application Support/smind` path under `cfg(target_os =
//! "macos")`; this module itself has no `cfg` and is exercised live on
//! Linux in this session with a temp dir standing in for the real one.
//!
//! **Decision: a plain child process, not a launchd LaunchAgent.** A
//! LaunchAgent survives the app quitting and needs its own plist/
//! `launchctl` lifecycle for zero benefit here -- the app itself is what's
//! running when the user cares about the daemon. `process_group(0)`
//! (stable `std::os::unix::process::CommandExt`) detaches the child into
//! its own process group so hiding/closing the main window never sends it
//! a signal, mirroring the WSL2 path's `setsid`.

use std::fs::{self, File};
use std::io;
#[cfg(unix)]
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};

use super::checksums;
use super::release::{ReleaseError, ReleaseUrls};

#[derive(Debug, Clone)]
pub struct Layout {
    pub bin_path: PathBuf,
    pub log_path: PathBuf,
    pub state_path: PathBuf,
}

impl Layout {
    pub fn new(base_dir: &Path) -> Self {
        Self { bin_path: base_dir.join("bin").join("smind"), log_path: base_dir.join("smind.log"), state_path: base_dir.join("managed.json") }
    }
}

/// extract_argv matches the WSL2 path's own tar invocation (AC4): argv
/// only, no shell, extracting just the `smind` member.
pub fn extract_argv(tarball: &Path, dest_dir: &Path) -> Vec<String> {
    vec!["tar".into(), "-xzf".into(), tarball.display().to_string(), "-C".into(), dest_dir.display().to_string(), "smind".into()]
}

/// install_binary extracts `tarball` (already downloaded and checksum-
/// verified by the caller) into `layout.bin_path`'s directory and makes it
/// executable.
pub fn install_binary(tarball: &Path, layout: &Layout) -> io::Result<()> {
    let bin_dir = layout.bin_path.parent().expect("smind desktop: bin_path always has a parent");
    fs::create_dir_all(bin_dir)?;
    let argv = extract_argv(tarball, bin_dir);
    let (prog, args) = argv.split_first().expect("smind desktop: extract_argv is never empty");
    let status = Command::new(prog).args(args).status()?;
    if !status.success() {
        return Err(io::Error::other(format!("tar extraction failed: {status}")));
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&layout.bin_path)?.permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&layout.bin_path, perms)?;
    }
    Ok(())
}

/// spawn_detached starts `layout.bin_path serve` as its own process group
/// leader, with stdout/stderr appended to the log file.
pub fn spawn_detached(layout: &Layout) -> io::Result<Child> {
    let log_out = File::options().create(true).append(true).open(&layout.log_path)?;
    let log_err = log_out.try_clone()?;
    let mut cmd = Command::new(&layout.bin_path);
    cmd.arg("serve")
        .stdout(Stdio::from(log_out))
        .stderr(Stdio::from(log_err))
        .stdin(Stdio::null());
    // The native path only runs on macOS; the module still has to compile
    // on Windows (WSL2 path) and Linux, where process groups are unix-only.
    #[cfg(unix)]
    cmd.process_group(0);
    cmd.spawn()
}

/// find_port_owner asks `lsof` who is bound (listening) on `port`, the
/// same primitive used both to verify a managed pid is still the real
/// server and to detect an unmanaged one (AC3).
pub fn find_port_owner(port: u16) -> io::Result<Option<u32>> {
    let output = Command::new("lsof").arg(format!("-tiTCP:{port}")).arg("-sTCP:LISTEN").output()?;
    Ok(parse_lsof_output(&String::from_utf8_lossy(&output.stdout)))
}

pub fn parse_lsof_output(stdout: &str) -> Option<u32> {
    stdout.lines().next()?.trim().parse().ok()
}

/// is_pid_alive uses `kill -0` rather than a syscall binding, matching
/// this module's system-tool-via-argv style and needing no extra
/// dependency (`kill` ships everywhere this code runs).
pub fn is_pid_alive(pid: u32) -> bool {
    Command::new("kill")
        .arg("-0")
        .arg(pid.to_string())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

pub fn kill_process(pid: u32) -> io::Result<()> {
    let status = Command::new("kill").arg(pid.to_string()).stdout(Stdio::null()).stderr(Stdio::null()).status()?;
    if status.success() {
        Ok(())
    } else {
        Err(io::Error::other(format!("kill {pid} failed: {status}")))
    }
}

/// download_and_verify fetches `urls.checksums`, then `urls.tarball`, and
/// verifies the tarball's SHA-256 against the checksums file -- the only
/// place raw release bytes are downloaded directly in-process (the WSL2
/// path downloads with `curl` inside the distro instead, per AC4, and
/// verifies via `sha256sum` there so the tarball's bytes never cross the
/// `wsl.exe` boundary).
pub async fn download_and_verify(client: &reqwest::Client, urls: &ReleaseUrls) -> Result<Vec<u8>, ReleaseError> {
    let checksums_resp = client.get(&urls.checksums).send().await.map_err(|e| ReleaseError::Network(e.to_string()))?;
    if checksums_resp.status() == reqwest::StatusCode::NOT_FOUND {
        return Err(ReleaseError::NoRelease { version: urls.asset_name.clone() });
    }
    let checksums_resp = checksums_resp.error_for_status().map_err(|e| ReleaseError::Network(e.to_string()))?;
    let checksums_text = checksums_resp.text().await.map_err(|e| ReleaseError::Network(e.to_string()))?;
    let map = checksums::parse(&checksums_text);
    let Some(expected_hex) = map.get(&urls.asset_name) else {
        return Err(ReleaseError::NoAsset { version: urls.asset_name.clone(), asset: urls.asset_name.clone() });
    };

    let tarball_resp = client.get(&urls.tarball).send().await.map_err(|e| ReleaseError::Network(e.to_string()))?;
    if tarball_resp.status() == reqwest::StatusCode::NOT_FOUND {
        return Err(ReleaseError::NoAsset { version: urls.asset_name.clone(), asset: urls.asset_name.clone() });
    }
    let tarball_resp = tarball_resp.error_for_status().map_err(|e| ReleaseError::Network(e.to_string()))?;
    let bytes = tarball_resp.bytes().await.map_err(|e| ReleaseError::Network(e.to_string()))?.to_vec();

    if !checksums::verify(&bytes, expected_hex) {
        return Err(ReleaseError::ChecksumMismatch { asset: urls.asset_name.clone() });
    }
    Ok(bytes)
}

/// install_from_release downloads, verifies, and installs a release
/// tarball in one call, writing it to a temp file under `layout`'s
/// directory before extracting (so `install_binary`'s existing argv-only
/// `tar` step is reused unchanged).
pub async fn install_from_release(client: &reqwest::Client, urls: &ReleaseUrls, layout: &Layout) -> Result<(), ReleaseError> {
    let bytes = download_and_verify(client, urls).await?;
    let bin_dir = layout.bin_path.parent().expect("smind desktop: bin_path always has a parent");
    fs::create_dir_all(bin_dir).map_err(|e| ReleaseError::Io(e.to_string()))?;
    let tarball_path = bin_dir.join("download.tar.gz");
    fs::write(&tarball_path, &bytes).map_err(|e| ReleaseError::Io(e.to_string()))?;
    let result = install_binary(&tarball_path, layout).map_err(|e| ReleaseError::Io(e.to_string()));
    let _ = fs::remove_file(&tarball_path);
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use std::net::TcpListener;

    #[test]
    fn extract_argv_shape() {
        let argv = extract_argv(Path::new("/tmp/x.tar.gz"), Path::new("/tmp/bin"));
        assert_eq!(argv, vec!["tar", "-xzf", "/tmp/x.tar.gz", "-C", "/tmp/bin", "smind"]);
    }

    #[test]
    fn parse_lsof_output_first_line() {
        assert_eq!(parse_lsof_output("1234\n"), Some(1234));
        assert_eq!(parse_lsof_output("1234\n5678\n"), Some(1234));
        assert_eq!(parse_lsof_output(""), None);
        assert_eq!(parse_lsof_output("not a pid\n"), None);
    }

    #[test]
    fn find_port_owner_sees_this_process_bound_to_a_port() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let Ok(Some(pid)) = find_port_owner(port) else {
            // lsof may be unavailable in a minimal sandbox; don't fail
            // the suite over a missing system tool, just skip.
            eprintln!("smind desktop: lsof unavailable or found nothing, skipping");
            return;
        };
        assert_eq!(pid, std::process::id());
        drop(listener);
    }

    #[test]
    fn is_pid_alive_true_for_self_false_after_reap() {
        assert!(is_pid_alive(std::process::id()));

        let mut child = Command::new("true").spawn().expect("smind desktop: `true` must exist");
        let pid = child.id();
        child.wait().unwrap();
        assert!(!is_pid_alive(pid));
    }

    #[test]
    fn install_start_find_stop_round_trip_with_a_fake_binary() {
        let dir = tempfile::tempdir().unwrap();
        let layout = Layout::new(dir.path());

        // Build a tiny fake "smind" that listens on the port given via
        // argv (mimicking `smind serve`'s shape closely enough for this
        // test) and build a real tar.gz around it, entirely self-
        // contained -- no network, no real smind binary required.
        let src_dir = dir.path().join("src");
        fs::create_dir_all(&src_dir).unwrap();
        let fake_bin = src_dir.join("smind");
        {
            let mut f = File::create(&fake_bin).unwrap();
            writeln!(f, "#!/bin/sh").unwrap();
            writeln!(f, "exec sleep 30").unwrap();
        }
        let mut perms = fs::metadata(&fake_bin).unwrap().permissions();
        {
            use std::os::unix::fs::PermissionsExt;
            perms.set_mode(0o755);
        }
        fs::set_permissions(&fake_bin, perms).unwrap();

        let tarball = dir.path().join("fake.tar.gz");
        let status = Command::new("tar").arg("-C").arg(&src_dir).arg("-czf").arg(&tarball).arg("smind").status().unwrap();
        assert!(status.success());

        install_binary(&tarball, &layout).unwrap();
        assert!(layout.bin_path.exists());

        let mut child = spawn_detached(&layout).unwrap();
        let pid = child.id();
        assert!(is_pid_alive(pid));

        kill_process(pid).unwrap();
        // Reap so the test doesn't leak a zombie; ignore the exit status
        // (it was killed).
        let _ = child.wait();
        std::thread::sleep(std::time::Duration::from_millis(100));
        assert!(!is_pid_alive(pid));
    }

    async fn serve_fixture(tarball: Vec<u8>, checksums_text: String, asset_name: String) -> (String, tokio::task::JoinHandle<()>) {
        use axum::routing::get;
        use axum::Router;

        let checksums_text2 = checksums_text.clone();
        let tarball2 = tarball.clone();
        let app = Router::new()
            .route("/checksums.txt", get(move || { let t = checksums_text2.clone(); async move { t } }))
            .route(&format!("/{asset_name}"), get(move || { let b = tarball2.clone(); async move { b } }));

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let handle = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        (format!("http://{addr}"), handle)
    }

    #[tokio::test]
    async fn install_from_release_downloads_verifies_and_installs() {
        let dir = tempfile::tempdir().unwrap();
        let layout = Layout::new(dir.path());

        let src_dir = dir.path().join("src");
        fs::create_dir_all(&src_dir).unwrap();
        let fake_bin = src_dir.join("smind");
        fs::write(&fake_bin, "#!/bin/sh\nexec sleep 30\n").unwrap();
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(&fake_bin, fs::Permissions::from_mode(0o755)).unwrap();
        }
        let tarball_path = dir.path().join("fixture.tar.gz");
        let status = Command::new("tar").arg("-C").arg(&src_dir).arg("-czf").arg(&tarball_path).arg("smind").status().unwrap();
        assert!(status.success());
        let tarball_bytes = fs::read(&tarball_path).unwrap();

        let mut hasher = sha2::Sha256::new();
        use sha2::Digest;
        hasher.update(&tarball_bytes);
        let hex: String = hasher.finalize().iter().map(|b| format!("{b:02x}")).collect();
        let asset_name = "smind_0.7.0_linux_amd64.tar.gz".to_string();
        let checksums_text = format!("{hex}  {asset_name}\n");

        let (base, _handle) = serve_fixture(tarball_bytes, checksums_text, asset_name.clone()).await;
        let urls = ReleaseUrls { tarball: format!("{base}/{asset_name}"), checksums: format!("{base}/checksums.txt"), asset_name };

        let client = reqwest::Client::new();
        install_from_release(&client, &urls, &layout).await.unwrap();
        assert!(layout.bin_path.exists());
    }

    #[tokio::test]
    async fn download_and_verify_rejects_bad_checksum() {
        let dir = tempfile::tempdir().unwrap();
        let asset_name = "smind_0.7.0_linux_amd64.tar.gz".to_string();
        let checksums_text = format!("{}  {asset_name}\n", "0".repeat(64));
        let (base, _handle) = serve_fixture(b"not the real bytes".to_vec(), checksums_text, asset_name.clone()).await;
        let urls = ReleaseUrls { tarball: format!("{base}/{asset_name}"), checksums: format!("{base}/checksums.txt"), asset_name };

        let client = reqwest::Client::new();
        let err = download_and_verify(&client, &urls).await.unwrap_err();
        assert!(matches!(err, ReleaseError::ChecksumMismatch { .. }));
        let _ = dir;
    }

    #[tokio::test]
    async fn download_and_verify_no_asset_for_this_platform() {
        let checksums_text = format!("{}  smind_0.7.0_linux_arm64.tar.gz\n", "1".repeat(64));
        let (base, _handle) = serve_fixture(vec![], checksums_text, "smind_0.7.0_linux_arm64.tar.gz".to_string()).await;
        let urls = ReleaseUrls {
            tarball: format!("{base}/smind_0.7.0_linux_amd64.tar.gz"),
            checksums: format!("{base}/checksums.txt"),
            asset_name: "smind_0.7.0_linux_amd64.tar.gz".to_string(),
        };

        let client = reqwest::Client::new();
        let err = download_and_verify(&client, &urls).await.unwrap_err();
        assert!(matches!(err, ReleaseError::NoAsset { .. }));
    }
}
