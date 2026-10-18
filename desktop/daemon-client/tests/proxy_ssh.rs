//! ADR-0031 D7 (b2): an `ssh`-kind connection is served through its tunnel's
//! local end; with no tunnel the proxy says so (502) instead of guessing.

use std::net::SocketAddr;

use axum::routing::get;
use axum::Router;
use tokio::net::TcpListener;
use url::Url;

use smind_daemon_client::proxy::{server::AssetSource, server::ProxyState, Registry, SshTarget};

const SECRET: &str = "test-secret-1234567890";

struct EmptyAssets;
impl AssetSource for EmptyAssets {
    fn get(&self, _path: &str) -> Option<(Vec<u8>, String)> {
        None
    }
}

async fn spawn_stub() -> SocketAddr {
    let app = Router::new().route("/api/ping", get(|| async { "pong from the far side" }));
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let _ = axum::serve(listener, app).await;
    });
    addr
}

async fn proxy_with_ssh_connection() -> (std::sync::Arc<ProxyState>, String, String, reqwest::Client, String) {
    let local = Url::parse("http://127.0.0.1:9").unwrap();
    let state = ProxyState::new(SECRET.to_string(), Registry::new(&local), Box::new(EmptyAssets));
    let id = {
        let mut reg = state.registry.lock().unwrap();
        let conn = reg.add_ssh("Lab", &SshTarget { host: "me@lab".into(), ssh_port: None, daemon_port: 4648 }).unwrap();
        reg.select(&conn.id).unwrap();
        conn.id
    };
    let (port, fut) = smind_daemon_client::proxy::serve(state.clone()).await.unwrap();
    tokio::spawn(fut);
    let http = reqwest::Client::builder().redirect(reqwest::redirect::Policy::none()).build().unwrap();
    let resp = http.get(format!("http://127.0.0.1:{port}/?k={SECRET}")).send().await.unwrap();
    let cookie = resp.headers().get("set-cookie").unwrap().to_str().unwrap().split(';').next().unwrap().to_string();
    (state, id, format!("http://127.0.0.1:{port}"), http, cookie)
}

#[tokio::test]
async fn proxy_maps_ssh_connection_to_tunnel_url() {
    let (state, id, base, http, cookie) = proxy_with_ssh_connection().await;
    let stub = spawn_stub().await;
    state.set_ssh_local(&id, format!("http://{stub}"));
    let resp = http.get(format!("{base}/api/ping")).header("cookie", &cookie).send().await.unwrap();
    assert_eq!(resp.status(), 200);
    assert_eq!(resp.text().await.unwrap(), "pong from the far side");
    let conn = state.registry.lock().unwrap().current().clone();
    assert_eq!(state.upstream_base(&conn), Some(format!("http://{stub}")));
}

#[tokio::test]
async fn proxy_ssh_without_tunnel_is_502() {
    let (state, id, base, http, cookie) = proxy_with_ssh_connection().await;
    let resp = http.get(format!("{base}/api/ping")).header("cookie", &cookie).send().await.unwrap();
    assert_eq!(resp.status(), 502);
    let text = resp.text().await.unwrap();
    assert!(text.contains("SSH tunnel for Lab is not running") && text.contains("switch to it again"), "{text}");
    // /ws answers the same, before any upgrade.
    let ws = http.get(format!("{base}/ws")).header("cookie", &cookie).send().await.unwrap();
    assert_ne!(ws.status(), 200);
    // And clearing a mapping returns to 502.
    state.set_ssh_local(&id, "http://127.0.0.1:9".into());
    state.clear_ssh_local(&id);
    let again = http.get(format!("{base}/api/ping")).header("cookie", &cookie).send().await.unwrap();
    assert_eq!(again.status(), 502);
}

/// The tunnel is up (ssh owns the port) but the daemon behind it is down: the
/// far end of the forward closes every connection. The proxy must answer 502
/// for the API and /ws, so the UI shows the connection as unreachable rather
/// than connected.
#[tokio::test]
async fn a_tunnel_whose_daemon_is_down_reads_as_unreachable() {
    let (state, id, base, http, cookie) = proxy_with_ssh_connection().await;
    let dead = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = dead.local_addr().unwrap();
    tokio::spawn(async move {
        loop {
            // Accept and hang up, like ssh forwarding to a closed remote port.
            let _ = dead.accept().await;
        }
    });
    state.set_ssh_local(&id, format!("http://{addr}"));
    for path in ["/api/token", "/api/ping"] {
        let resp = http.get(format!("{base}{path}")).header("cookie", &cookie).send().await.unwrap();
        assert_eq!(resp.status(), 502, "{path}");
    }
    let ws = http.get(format!("{base}/ws")).header("cookie", &cookie).send().await.unwrap();
    assert_ne!(ws.status(), 200);
    assert_ne!(ws.status(), 101);
}
