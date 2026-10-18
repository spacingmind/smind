//! Hermetic end-to-end test of the in-app update (ADR-0023): the real updater
//! plugin, wired the way `lib.rs` wires it, talks over real HTTP to a local
//! feed (`latest.json` + a signed artifact) and the real `check_with` /
//! `install_pending` cores run against it. The signing key is generated here,
//! per test run; no real key or secret is ever read.
//!
//! Not covered here (still manual, see docs/plans/active/desktop-update-e2e.md):
//! the webview window, the menu click and `app.restart()`.

use crate::updater::{check_with, install_pending, AppUpdateCheck, UpdateState};
use base64::Engine;
use std::collections::HashMap;
use std::io::{Read, Write};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::sync::{Arc, Mutex};
use std::thread;
use tauri::test::{mock_builder, mock_context, noop_assets, MockRuntime};
use tauri::{App, Manager};
use tauri_plugin_updater::UpdaterExt;

/// Version the mock app reports as running.
pub const RUNNING: &str = "0.10.0";

fn b64(s: &str) -> String {
    base64::engine::general_purpose::STANDARD.encode(s)
}

/// A throwaway minisign key pair in the formats `tauri.conf.json` and
/// `latest.json` carry: base64 of the full public key / signature file text.
pub struct TestKey {
    pair: minisign::KeyPair,
}

impl TestKey {
    pub fn generate() -> Self {
        Self {
            pair: minisign::KeyPair::generate_unencrypted_keypair().expect("generate test key"),
        }
    }

    /// The `plugins.updater.pubkey` value for this key.
    pub fn pubkey(&self) -> String {
        b64(&self.pair.pk.to_box().expect("pubkey box").to_string())
    }

    /// The `latest.json` signature for `data`; `signed_version` goes in the
    /// trusted comment like the Tauri CLI does (`None` = no version recorded).
    pub fn sign(&self, data: &[u8], signed_version: Option<&str>) -> String {
        let comment = match signed_version {
            Some(v) => format!("timestamp:1700000000\tfile:smind.app.tar.gz\tversion:{v}"),
            None => "timestamp:1700000000\tfile:smind.app.tar.gz".to_string(),
        };
        let sig = minisign::sign(
            Some(&self.pair.pk),
            &self.pair.sk,
            data,
            Some(&comment),
            Some("smind e2e test signature"),
        )
        .expect("sign");
        b64(&sig.to_string())
    }
}

/// A `.app.tar.gz` holding `smind.app/Contents/{Info.plist,MacOS/smind-desktop}`
/// where Info.plist is just `marker` (what the install test reads back).
pub fn app_tarball(marker: &str) -> Vec<u8> {
    let gz = flate2::write::GzEncoder::new(Vec::new(), flate2::Compression::fast());
    let mut tar = tar::Builder::new(gz);
    for (path, body) in [
        ("smind.app/Contents/Info.plist", marker.as_bytes()),
        ("smind.app/Contents/MacOS/smind-desktop", b"#!/bin/sh\n".as_slice()),
    ] {
        let mut header = tar::Header::new_gnu();
        header.set_size(body.len() as u64);
        header.set_mode(0o755);
        header.set_cksum();
        tar.append_data(&mut header, path, body).expect("tar entry");
    }
    tar.into_inner().expect("tar").finish().expect("gzip")
}

/// `<os>-<arch>` as the plugin looks it up in `platforms`.
fn platform_key() -> String {
    let os = if cfg!(target_os = "macos") { "darwin" } else { "windows" };
    let arch = if cfg!(target_arch = "aarch64") { "aarch64" } else { "x86_64" };
    format!("{os}-{arch}")
}

/// What the feed serves: `latest.json` pointing at `/smind.app.tar.gz`.
pub struct Release {
    pub version: String,
    pub notes: String,
    pub pub_date: String,
    pub signature: String,
    pub artifact: Vec<u8>,
}

/// A tiny HTTP/1.1 server on 127.0.0.1 with an access log (`"GET /path"`).
pub struct Feed {
    pub addr: SocketAddr,
    log: Arc<Mutex<Vec<String>>>,
}

impl Feed {
    /// Start a server whose routes (path -> body, anything else is a 404) may
    /// refer to its own address, which is only known after the bind.
    pub fn start(routes: impl FnOnce(SocketAddr) -> HashMap<String, Vec<u8>>) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind feed");
        let addr = listener.local_addr().unwrap();
        let routes = routes(addr);
        let log = Arc::new(Mutex::new(Vec::new()));
        let served = log.clone();
        thread::spawn(move || {
            for stream in listener.incoming().flatten() {
                let (routes, served) = (routes.clone(), served.clone());
                thread::spawn(move || handle(stream, &routes, &served));
            }
        });
        Self { addr, log }
    }

    /// Serve a release: `/latest.json` plus `/smind.app.tar.gz`.
    pub fn serve_release(release: &Release) -> Self {
        Self::start(|addr| {
            let manifest = serde_json::json!({
                "version": release.version,
                "notes": release.notes,
                "pub_date": release.pub_date,
                "platforms": {
                    platform_key(): {
                        "url": format!("http://{addr}/smind.app.tar.gz"),
                        "signature": release.signature,
                    },
                },
            });
            HashMap::from([
                ("/latest.json".to_string(), manifest.to_string().into_bytes()),
                ("/smind.app.tar.gz".to_string(), release.artifact.clone()),
            ])
        })
    }

    /// `http://127.0.0.1:<port>/latest.json`
    pub fn latest_url(&self) -> String {
        format!("http://{}/latest.json", self.addr)
    }

    /// The requests served so far, in order, as `"GET /path"`.
    pub fn requests(&self) -> Vec<String> {
        self.log.lock().unwrap().clone()
    }
}

fn handle(mut stream: TcpStream, routes: &HashMap<String, Vec<u8>>, log: &Mutex<Vec<String>>) {
    let mut buf = [0u8; 4096];
    let n = stream.read(&mut buf).unwrap_or(0);
    let head = String::from_utf8_lossy(&buf[..n]);
    let mut parts = head.lines().next().unwrap_or("").split_whitespace();
    let (method, path) = (parts.next().unwrap_or(""), parts.next().unwrap_or(""));
    log.lock().unwrap().push(format!("{method} {path}"));
    let (status, body) = match routes.get(path) {
        Some(body) => ("200 OK", body.as_slice()),
        None => ("404 Not Found", b"not found".as_slice()),
    };
    let _ = write!(
        stream,
        "HTTP/1.1 {status}\r\nContent-Length: {}\r\nContent-Type: application/octet-stream\r\nConnection: close\r\n\r\n",
        body.len()
    );
    let _ = stream.write_all(body);
}

/// A mock-runtime app that has the updater plugin registered exactly as
/// production registers it (`lib.rs::updater_plugin`, so the same version
/// comparator), configured with `pubkey` and the local `endpoint`.
pub fn test_app(pubkey: &str, endpoint: &str) -> App<MockRuntime> {
    let mut context = mock_context(noop_assets());
    context.package_info_mut().version = RUNNING.parse().unwrap();
    context.config_mut().plugins.0.insert(
        "updater".to_string(),
        serde_json::json!({
            "pubkey": pubkey,
            "endpoints": [endpoint],
            "dangerousInsecureTransportProtocol": true,
        }),
    );
    let app = mock_builder()
        .plugin(crate::updater_plugin::<MockRuntime>())
        .build(context)
        .expect("build mock app");
    app.manage(UpdateState::default());
    app
}

#[cfg(test)]
mod tests {
    use super::*;

    fn release(key: &TestKey, version: &str, artifact: Vec<u8>) -> Release {
        Release {
            version: version.to_string(),
            notes: "## 0.10.1\n\n* fixes".to_string(),
            pub_date: "2026-10-12T09:00:00Z".to_string(),
            signature: key.sign(&artifact, Some(version)),
            artifact,
        }
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn check_reports_available_and_downloads_nothing() {
        let key = TestKey::generate();
        let feed = Feed::serve_release(&release(&key, "0.10.1", app_tarball("new")));
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        let got = check_with(app.updater_builder(), RUNNING, &state)
            .await
            .expect("check");
        match got {
            AppUpdateCheck::Available { version, notes, .. } => {
                assert_eq!(version, "0.10.1");
                assert!(notes.is_some());
            }
            other => panic!("expected Available, got {other:?}"),
        }
        assert_eq!(feed.requests(), vec!["GET /latest.json"]);
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn same_version_is_up_to_date() {
        let key = TestKey::generate();
        let feed = Feed::serve_release(&release(&key, "0.10.0", app_tarball("new")));
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        let got = check_with(app.updater_builder(), RUNNING, &state)
            .await
            .expect("check");
        assert!(matches!(
            got,
            AppUpdateCheck::UpToDate { ref current_version } if current_version == "0.10.0"
        ));
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn older_version_is_not_offered() {
        let key = TestKey::generate();
        let feed = Feed::serve_release(&release(&key, "0.9.9", app_tarball("new")));
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        let got = check_with(app.updater_builder(), RUNNING, &state)
            .await
            .expect("check");
        assert!(matches!(
            got,
            AppUpdateCheck::UpToDate { ref current_version } if current_version == "0.10.0"
        ));
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn prerelease_version_is_not_offered() {
        let key = TestKey::generate();
        let feed = Feed::serve_release(&release(&key, "0.11.0-rc.1", app_tarball("new")));
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        let got = check_with(app.updater_builder(), RUNNING, &state)
            .await
            .expect("check");
        assert!(matches!(
            got,
            AppUpdateCheck::UpToDate { ref current_version } if current_version == "0.10.0"
        ));
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn dead_endpoint_is_an_error() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        drop(listener);
        let app = test_app(&TestKey::generate().pubkey(), &format!("http://127.0.0.1:{port}/latest.json"));
        let state = app.state::<UpdateState>();
        let err = check_with(app.updater_builder(), RUNNING, &state)
            .await
            .expect_err("dead endpoint must error");
        assert!(!err.is_empty());
        assert!(state.pending.take().is_err());
    }


    #[tokio::test(flavor = "multi_thread")]
    async fn available_result_matches_the_ui_fixture() {
        const FIXTURE: &str =
            include_str!("../../../web/packages/ui/src/lib/__fixtures__/app-update-available.json");
        let fixture: serde_json::Value = serde_json::from_str(FIXTURE).unwrap();
        let key = TestKey::generate();
        let feed = Feed::serve_release(&Release {
            version: fixture["version"].as_str().unwrap().to_string(),
            notes: fixture["notes"].as_str().unwrap().to_string(),
            pub_date: "2026-10-12T09:00:00Z".to_string(),
            signature: key.sign(&app_tarball("new"), Some("0.10.1")),
            artifact: app_tarball("new"),
        });
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        let got = check_with(app.updater_builder(), RUNNING, &state)
            .await
            .expect("check");
        assert_eq!(serde_json::to_value(&got).unwrap(), fixture);
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn download_verifies_a_good_signature() {
        let key = TestKey::generate();
        let good = release(&key, "0.10.1", app_tarball("new"));
        let feed = Feed::serve_release(&good);
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        check_with(app.updater_builder(), RUNNING, &state).await.expect("check");
        let update = state.pending.take().unwrap();
        let bytes = update.download(|_, _| {}, || {}).await.expect("download");
        assert_eq!(bytes, good.artifact);
        assert_eq!(feed.requests(), vec!["GET /latest.json", "GET /smind.app.tar.gz"]);
    }

    async fn refused(feed: Feed, app: App<MockRuntime>) -> String {
        let state = app.state::<UpdateState>();
        let update = state.pending.take().unwrap();
        let err = update
            .download(|_, _| {}, || {})
            .await
            .expect_err("refused")
            .to_string();
        assert_eq!(
            feed.requests(),
            vec!["GET /latest.json", "GET /smind.app.tar.gz"]
        );
        err
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn tampered_artifact_is_refused() {
        let key = TestKey::generate();
        let good = release(&key, "0.10.1", app_tarball("new"));
        let mut tampered = good.artifact.clone();
        let last = tampered.len() - 1;
        tampered[last] ^= 0xff;
        let feed = Feed::serve_release(&Release { artifact: tampered, ..good });
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        check_with(app.updater_builder(), RUNNING, &state).await.expect("check");
        let err = refused(feed, app).await;
        assert!(err.to_lowercase().contains("signature verification failed"));
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn signature_from_another_key_is_refused() {
        let signing_key = TestKey::generate();
        let verifying_key = TestKey::generate();
        let feed = Feed::serve_release(&release(&signing_key, "0.10.1", app_tarball("new")));
        let app = test_app(&verifying_key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        check_with(app.updater_builder(), RUNNING, &state).await.expect("check");
        let err = refused(feed, app).await;
        assert!(err.to_lowercase().contains("different key"));
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn artifact_signed_for_another_version_is_refused() {
        let key = TestKey::generate();
        let artifact = app_tarball("new");
        let feed = Feed::serve_release(&Release {
            signature: key.sign(&artifact, Some("0.9.0")),
            ..release(&key, "0.10.1", artifact.clone())
        });
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        check_with(app.updater_builder(), RUNNING, &state).await.expect("check");
        let err = refused(feed, app).await;
        assert!(err.to_lowercase().contains("signed for version"));
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn undecodable_signature_is_refused() {
        let key = TestKey::generate();
        let feed = Feed::serve_release(&Release {
            signature: "!!!not-base64!!!".to_string(),
            ..release(&key, "0.10.1", app_tarball("new"))
        });
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let state = app.state::<UpdateState>();
        check_with(app.updater_builder(), RUNNING, &state).await.expect("check");
        let err = refused(feed, app).await;
        assert!(err.to_lowercase().contains("invalid symbol"));
    }

    /// A fake installed app the plugin can replace: `<tmp>/smind.app/Contents/...`.
    #[cfg(target_os = "macos")]
    fn installed_app(dir: &std::path::Path, marker: &str) -> std::path::PathBuf {
        let exe = dir.join("smind.app/Contents/MacOS/smind-desktop");
        std::fs::create_dir_all(exe.parent().unwrap()).unwrap();
        std::fs::write(&exe, "#!/bin/sh\n").unwrap();
        std::fs::write(dir.join("smind.app/Contents/Info.plist"), marker).unwrap();
        exe
    }

    #[cfg(target_os = "macos")]
    #[tokio::test(flavor = "multi_thread")]
    async fn refused_install_leaves_the_bundle_and_keeps_the_update_pending() {
        let key = TestKey::generate();
        let good = release(&key, "0.10.1", app_tarball("new"));
        let mut tampered = good.artifact.clone();
        let last = tampered.len() - 1;
        tampered[last] ^= 0xff;
        let feed = Feed::serve_release(&Release { artifact: tampered, ..good });
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let dir = tempfile::tempdir().unwrap();
        let exe = installed_app(dir.path(), "old");
        let state = app.state::<UpdateState>();
        check_with(app.updater_builder().executable_path(&exe), RUNNING, &state)
            .await
            .expect("check");
        let events = Mutex::new(Vec::new());
        let err = install_pending(&state, &|p| events.lock().unwrap().push(p))
            .await
            .expect_err("tampered artifact must be refused");
        assert!(err.to_lowercase().contains("signature"));
        let plist = std::fs::read_to_string(dir.path().join("smind.app/Contents/Info.plist")).unwrap();
        assert_eq!(plist, "old");
        assert!(exe.exists());
        assert!(state.pending.take().is_ok(), "failed install stays retryable");
        // The plugin runs its `on_download_finish` hook (our "installing"
        // event) before verifying the signature, so an "installing" event is
        // emitted even for a refused artifact; what must hold is that the
        // bundle is untouched and the update stays retryable.
        let events = events.into_inner().unwrap();
        let last_download = events
            .iter()
            .rev()
            .find(|e| e["stage"] == "downloading")
            .unwrap();
        assert!(last_download["downloaded"].as_u64().unwrap() > 0);
    }

    #[cfg(target_os = "macos")]
    #[tokio::test(flavor = "multi_thread")]
    async fn install_replaces_the_bundle_and_reports_progress() {
        let key = TestKey::generate();
        let feed = Feed::serve_release(&release(&key, "0.10.1", app_tarball("new")));
        let app = test_app(&key.pubkey(), &feed.latest_url());
        let dir = tempfile::tempdir().unwrap();
        let exe = installed_app(dir.path(), "old");
        let state = app.state::<UpdateState>();
        check_with(app.updater_builder().executable_path(&exe), RUNNING, &state)
            .await
            .expect("check");
        let events = Mutex::new(Vec::new());
        install_pending(&state, &|p| events.lock().unwrap().push(p))
            .await
            .expect("install");
        let plist = std::fs::read_to_string(dir.path().join("smind.app/Contents/Info.plist")).unwrap();
        assert_eq!(plist, "new");
        let events = events.into_inner().unwrap();
        assert_eq!(events.last().unwrap()["stage"], "installing");
        assert_eq!(events[0]["stage"], "downloading");
        let last_download = events
            .iter()
            .rev()
            .find(|e| e["stage"] == "downloading")
            .unwrap();
        assert!(last_download["downloaded"].as_u64().unwrap() > 0);
        assert_eq!(feed.requests(), vec!["GET /latest.json", "GET /smind.app.tar.gz"]);
        assert!(state.pending.take().is_err());
    }
}
