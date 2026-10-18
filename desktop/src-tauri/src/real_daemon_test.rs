//! Opt-in end-to-end test of the desktop's P4 (daemon password) paths against
//! a REAL `smind serve`, not a stub written from the docs. Skips cleanly
//! unless `SMIND_REAL_DAEMON_BIN` points at a built smind:
//!
//! ```text
//! T=$(mktemp -d)
//! (cd <repo> && go build -o $T/smind ./cmd/smind)
//! cd desktop/src-tauri
//! SMIND_REAL_DAEMON_BIN=$T/smind cargo test real_daemon -- --nocapture
//! ```
//!
//! Each daemon runs with its own `SMIND_HOME` (a temp dir) and a free
//! loopback port (`SMIND_LISTEN`); the password daemon gets `SMIND_PASSWORD`.
//! Everything here is loopback and kills the daemons it started.

use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::Arc;
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::Message;
use url::Url;

use smind_daemon_client::client::{self, ClientEvent, WatcherAuth};
use smind_daemon_client::proxy::{ProxyState, Registry};
use smind_daemon_client::{Config, WorkspaceCache};

const PASSWORD: &str = "correct-horse-battery";

struct Daemon {
    child: Child,
    home: tempfile::TempDir,
    url: Url,
}

impl Daemon {
    async fn start(bin: &str, password: Option<&str>) -> Daemon {
        let port = TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port();
        let home = tempfile::tempdir().unwrap();
        let mut cmd = Command::new(bin);
        cmd.arg("serve")
            .env("SMIND_HOME", home.path())
            .env("SMIND_LISTEN", format!("127.0.0.1:{port}"))
            .env_remove("SMIND_PASSWORD")
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null());
        if let Some(p) = password {
            cmd.env("SMIND_PASSWORD", p);
        }
        let child = cmd.spawn().expect("spawn the real smind");
        let url: Url = format!("http://127.0.0.1:{port}").parse().unwrap();
        let daemon = Daemon { child, home, url };
        let http = reqwest::Client::new();
        for _ in 0..100 {
            if http.get(format!("{}/healthz", daemon.url)).send().await.is_ok() {
                return daemon;
            }
            tokio::time::sleep(Duration::from_millis(100)).await;
        }
        panic!("the real daemon did not come up");
    }

    fn token_file(&self) -> PathBuf {
        self.home.path().join("token")
    }
}

impl Drop for Daemon {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

struct Proxy {
    state: Arc<ProxyState>,
    base: String,
    cookie: String,
    http: reqwest::Client,
}

async fn proxy_for(local: &Url, file_token: Option<PathBuf>) -> Proxy {
    let state = ProxyState::new("real-daemon-secret-123456".into(), Registry::new(local), Box::new(crate::assets::EmbeddedAssets));
    state.set_local_token_source(move || file_token.as_deref().and_then(read_token));
    let (port, fut) = smind_daemon_client::proxy::serve(state.clone()).await.unwrap();
    tokio::spawn(fut);
    let http = reqwest::Client::builder().redirect(reqwest::redirect::Policy::none()).build().unwrap();
    let base = format!("http://127.0.0.1:{port}");
    let resp = http.get(format!("{base}/?k=real-daemon-secret-123456")).send().await.unwrap();
    let cookie = resp.headers().get("set-cookie").unwrap().to_str().unwrap().split(';').next().unwrap().to_string();
    Proxy { state, base, cookie, http }
}

fn read_token(path: &Path) -> Option<String> {
    Some(std::fs::read_to_string(path).ok()?.trim().to_string()).filter(|t| !t.is_empty())
}

impl Proxy {
    async fn get(&self, path: &str) -> reqwest::Response {
        self.http.get(format!("{}{path}", self.base)).header("cookie", &self.cookie).send().await.unwrap()
    }

    /// Opens /ws through the proxy and proves the upgrade is really
    /// authenticated: the daemon answers an events.subscribe.
    async fn ws_subscribes(&self, query: &str) -> Result<(), String> {
        self.ws_subscribes_offering(query, None).await.map(|_| ())
    }

    /// Like `ws_subscribes`, offering a WebSocket subprotocol; returns the
    /// subprotocol the proxy answered with.
    async fn ws_subscribes_offering(&self, query: &str, protocol: Option<&str>) -> Result<Option<String>, String> {
        let mut req = format!("{}/ws{query}", self.base.replace("http://", "ws://")).into_client_request().unwrap();
        req.headers_mut().insert("cookie", self.cookie.parse().unwrap());
        if let Some(p) = protocol {
            req.headers_mut().insert("sec-websocket-protocol", p.parse().unwrap());
        }
        let (mut ws, resp) = tokio_tungstenite::connect_async(req).await.map_err(|e| e.to_string())?;
        let selected = resp.headers().get("sec-websocket-protocol").map(|v| v.to_str().unwrap().to_string());
        ws.send(Message::Text(client::subscribe_message().to_string().into())).await.map_err(|e| e.to_string())?;
        let reply = tokio::time::timeout(Duration::from_secs(5), ws.next()).await.map_err(|_| "no answer from the daemon".to_string())?;
        match reply {
            Some(Ok(m)) if m.is_text() => Ok(selected),
            other => Err(format!("unexpected: {other:?}")),
        }
    }
}

/// watcher_connects runs the real `client::run_with_auth` loop against `cfg`
/// and reports whether it authenticated and subscribed (`Reconnected`).
async fn watcher_connects(cfg: Config, auth: WatcherAuth) -> bool {
    let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel();
    let task = tokio::spawn(client::run_with_auth(cfg, auth, WorkspaceCache::new(), move |ev| {
        if matches!(ev, ClientEvent::Reconnected) {
            let _ = tx.send(());
        }
    }));
    let ok = tokio::time::timeout(Duration::from_secs(8), rx.recv()).await.is_ok();
    task.abort();
    ok
}

#[tokio::test]
async fn real_daemon_password_and_no_password_flows() {
    let Ok(bin) = std::env::var("SMIND_REAL_DAEMON_BIN") else {
        eprintln!("real_daemon: SMIND_REAL_DAEMON_BIN not set; skipping");
        return;
    };
    // The watcher's local flow reads $SMIND_HOME/token; the two daemons below
    // are told apart by pointing it at the right one before each use.
    let secured = Daemon::start(&bin, Some(PASSWORD)).await;
    let open = Daemon::start(&bin, None).await;
    let secured_token = read_token(&secured.token_file()).expect("the daemon wrote its token file");

    // --- probe ---------------------------------------------------------
    let p = crate::probe::probe(secured.url.as_str()).await;
    assert!(p.reachable && p.auth_required && p.loopback, "{p:?}");
    assert_eq!(p.service.as_deref(), Some("smind"));
    let p = crate::probe::probe(open.url.as_str()).await;
    assert!(p.reachable && !p.auth_required, "{p:?}");

    // --- add a url connection with a password -----------------------------
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("connections.json");
    let local_dummy: Url = "http://127.0.0.1:9".parse().unwrap();
    let remote_proxy = proxy_for(&local_dummy, None).await;
    let err = crate::commands::add_url_connection(&remote_proxy.state, &path, "Secured", secured.url.as_str(), Some("not-the-password".into())).await.unwrap_err();
    assert_eq!(err, "wrong password");
    assert!(!path.exists(), "nothing saved after a wrong password");
    let conn = crate::commands::add_url_connection(&remote_proxy.state, &path, "Secured", secured.url.as_str(), Some(PASSWORD.into())).await.expect("login against the real daemon");
    let session = remote_proxy.state.session(&conn.id).expect("a session is held");
    let on_disk = std::fs::read_to_string(&path).unwrap();
    assert!(!on_disk.contains(PASSWORD) && !on_disk.contains(&session) && !on_disk.contains(&secured_token));
    // A daemon without a password refuses login (403 -> clear message).
    let err = crate::commands::add_url_connection(&remote_proxy.state, &dir.path().join("x.json"), "", open.url.as_str(), Some(PASSWORD.into())).await.unwrap_err();
    assert_eq!(err, "this daemon does not use a password");

    // --- session connection through the real proxy router ----------------
    remote_proxy.state.registry.lock().unwrap().select(&conn.id).unwrap();
    let body: serde_json::Value = remote_proxy.get("/api/token").await.json().await.unwrap();
    let placeholder = body["token"].as_str().unwrap().to_string();
    assert!(placeholder != session && placeholder != secured_token, "the webview got a placeholder");
    remote_proxy.ws_subscribes(&format!("?token={placeholder}")).await.expect("proxy injected the session bearer");
    // The watcher authenticates with the held session as a bearer.
    let held = session.clone();
    assert!(watcher_connects(Config { daemon_url: secured.url.clone() }, WatcherAuth::with_session(move || Some(held.clone()))).await, "session-held watcher");
    // After the session is gone the daemon's own refusal reaches the webview.
    remote_proxy.state.clear_session(&conn.id);
    assert_eq!(remote_proxy.get("/api/token").await.status(), 403);
    assert!(remote_proxy.ws_subscribes(&format!("?token={placeholder}")).await.is_err(), "no credential, no upgrade");
    // A browser-side login (the webview's own session token) rides as the
    // `smind.bearer.<token>` subprotocol: forwarded upstream, the daemon's
    // selection echoed back (a browser aborts the socket otherwise).
    let login: serde_json::Value = reqwest::Client::new()
        .post(format!("{}/api/login", secured.url))
        .json(&serde_json::json!({ "password": PASSWORD }))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    let browser_session = login["token"].as_str().unwrap();
    // The webview's own login form posts /api/login through the proxy with the
    // proxy origin as its Origin; the proxy drops it, so the daemon's
    // same-origin rule sees an Origin-less request and the post succeeds.
    let via_proxy = remote_proxy
        .http
        .post(format!("{}/api/login", remote_proxy.base))
        .header("cookie", &remote_proxy.cookie)
        .header("origin", &remote_proxy.base)
        .json(&serde_json::json!({ "password": PASSWORD }))
        .send()
        .await
        .unwrap();
    assert_eq!(via_proxy.status(), 200, "webview login through the proxy");
    // The webview offers ["smind", "smind.bearer.<token>"]; the real daemon
    // selects "smind" (never the token) and the proxy must echo exactly that.
    let offered = format!("smind, smind.bearer.{browser_session}");
    let selected = remote_proxy.ws_subscribes_offering("", Some(&offered)).await.expect("subprotocol bearer through the proxy");
    assert_eq!(selected.as_deref(), Some("smind"), "the daemon's selected subprotocol is echoed");

    // --- local connection against the password daemon ---------------------
    let local = proxy_for(&secured.url, Some(secured.token_file())).await;
    let body: serde_json::Value = local.get("/api/token").await.json().await.unwrap();
    let placeholder = body["token"].as_str().unwrap().to_string();
    assert_ne!(placeholder, secured_token, "the token file's token never reaches the webview");
    local.ws_subscribes(&format!("?token={placeholder}")).await.expect("proxy injected the token-file bearer");
    // Without a readable token file the real 403 passes through and /ws is refused.
    let no_file = proxy_for(&secured.url, None).await;
    assert_eq!(no_file.get("/api/token").await.status(), 403);
    assert!(no_file.ws_subscribes("?token=whatever").await.is_err());
    // The watcher's local flow (develop's P4 client): /api/token 403 -> token file bearer.
    std::env::set_var("SMIND_HOME", secured.home.path());
    assert!(watcher_connects(Config { daemon_url: secured.url.clone() }, WatcherAuth::default()).await, "local watcher vs password daemon");

    // --- the same flows against a daemon WITHOUT a password ----------------
    let open_token = read_token(&open.token_file()).unwrap();
    let local_open = proxy_for(&open.url, Some(open.token_file())).await;
    let body: serde_json::Value = local_open.get("/api/token").await.json().await.unwrap();
    assert_eq!(body["token"], open_token.as_str(), "an older-style flow: the real token is served");
    local_open.ws_subscribes(&format!("?token={open_token}")).await.expect("query-token flow still works");
    std::env::set_var("SMIND_HOME", open.home.path());
    assert!(watcher_connects(Config { daemon_url: open.url.clone() }, WatcherAuth::default()).await, "local watcher vs open daemon");
}
