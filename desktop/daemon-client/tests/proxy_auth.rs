//! ADR-0031 D8 proxy auth: the loopback proxy forwards/echoes `smind.bearer.*`
//! WebSocket subprotocols and attaches its own credential (held login
//! session, or -- for the `local` connection only -- the daemon token file)
//! upstream, never handing it to the webview.

use std::net::SocketAddr;
use std::sync::{Arc, Mutex};

use axum::extract::ws::{Message, WebSocketUpgrade};
use axum::extract::{RawQuery, State};
use axum::http::HeaderMap;
use axum::response::IntoResponse;
use axum::routing::get;
use axum::Router;
use futures_util::{SinkExt, StreamExt};
use tokio::net::TcpListener;
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::Message as TMessage;
use url::Url;

use smind_daemon_client::proxy::{server::AssetSource, server::ProxyState, Registry};

const SECRET: &str = "test-secret-1234567890";

/// What the stub daemon saw on one request.
#[derive(Debug, Clone)]
struct Seen {
    path: &'static str,
    authorization: Option<String>,
    protocols: Option<String>,
    query: Option<String>,
}

type Log = Arc<Mutex<Vec<Seen>>>;

fn record(log: &Log, path: &'static str, headers: &HeaderMap, query: Option<String>) {
    let h = |n: &str| headers.get(n).and_then(|v| v.to_str().ok()).map(str::to_string);
    log.lock().unwrap().push(Seen { path, authorization: h("authorization"), protocols: h("sec-websocket-protocol"), query });
}

/// A stub daemon: /api/token serves "daemon-token" (403 when `protected`, like
/// a password daemon), /api/ping answers ok, and
/// /ws selects the first offered `smind.bearer.*` protocol (like the real
/// daemon) then echoes text frames.
async fn spawn_stub(protected: bool) -> (SocketAddr, Log) {
    spawn_stub_as(protected, protected).await
}

/// `smind_healthz`: whether /healthz identifies a password-protected smind.
async fn spawn_stub_as(protected: bool, smind_healthz: bool) -> (SocketAddr, Log) {
    let log: Log = Arc::new(Mutex::new(Vec::new()));
    let app = Router::new()
        .route(
            "/api/token",
            get(move |State(log): State<Log>, headers: HeaderMap| async move {
                record(&log, "/api/token", &headers, None);
                if protected {
                    return (axum::http::StatusCode::FORBIDDEN, axum::Json(serde_json::json!({ "error": "password configured" })));
                }
                (axum::http::StatusCode::OK, axum::Json(serde_json::json!({ "token": "daemon-token" })))
            }),
        )
        .route(
            "/healthz",
            get(move || async move {
                if smind_healthz {
                    axum::Json(serde_json::json!({ "service": "smind", "version": "0.1.0", "authRequired": true }))
                } else {
                    axum::Json(serde_json::json!({ "service": "nginx" }))
                }
            }),
        )
        .route(
            "/api/ping",
            get(|State(log): State<Log>, headers: HeaderMap, RawQuery(q): RawQuery| async move {
                record(&log, "/api/ping", &headers, q);
                if headers.get("authorization").and_then(|v| v.to_str().ok()) == Some("Bearer expired-session") {
                    return (axum::http::StatusCode::UNAUTHORIZED, "session expired");
                }
                (axum::http::StatusCode::OK, "ok")
            }),
        )
        .route(
            "/ws",
            get(|State(log): State<Log>, headers: HeaderMap, RawQuery(q): RawQuery, ws: WebSocketUpgrade| async move {
                record(&log, "/ws", &headers, q);
                if headers.get("authorization").and_then(|v| v.to_str().ok()) == Some("Bearer expired-session") {
                    return axum::http::StatusCode::UNAUTHORIZED.into_response();
                }
                let offered: Vec<String> = headers
                    .get("sec-websocket-protocol")
                    .and_then(|v| v.to_str().ok())
                    .map(|v| v.split(',').map(|p| p.trim().to_string()).filter(|p| p.starts_with("smind.bearer.")).collect())
                    .unwrap_or_default();
                ws.protocols(offered).on_upgrade(|mut socket| async move {
                    while let Some(Ok(msg)) = socket.next().await {
                        if matches!(msg, Message::Close(_)) || socket.send(msg).await.is_err() {
                            break;
                        }
                    }
                }).into_response()
            }),
        )
        .with_state(log.clone());
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let _ = axum::serve(listener, app).await;
    });
    (addr, log)
}

struct EmptyAssets;
impl AssetSource for EmptyAssets {
    fn get(&self, _path: &str) -> Option<(Vec<u8>, String)> {
        None
    }
}

struct Harness {
    state: Arc<ProxyState>,
    port: u16,
    cookie: String,
    http: reqwest::Client,
    stub: Log,
}

/// Proxy whose built-in `local` connection points at a stub daemon.
async fn harness() -> Harness {
    harness_with(false).await
}

async fn harness_with(protected: bool) -> Harness {
    harness_as(protected, protected).await
}

async fn harness_as(protected: bool, smind_healthz: bool) -> Harness {
    let (addr, stub) = spawn_stub_as(protected, smind_healthz).await;
    let local = Url::parse(&format!("http://{addr}")).unwrap();
    let state = ProxyState::new(SECRET.to_string(), Registry::new(&local), Box::new(EmptyAssets));
    state.set_local_token_source(|| None);
    let (port, fut) = smind_daemon_client::proxy::serve(state.clone()).await.unwrap();
    tokio::spawn(fut);
    let http = reqwest::Client::builder().redirect(reqwest::redirect::Policy::none()).build().unwrap();
    let resp = http.get(format!("http://127.0.0.1:{port}/?k={SECRET}")).send().await.unwrap();
    let cookie = resp.headers().get("set-cookie").unwrap().to_str().unwrap().split(';').next().unwrap().to_string();
    Harness { state, port, cookie, http, stub }
}

impl Harness {
    async fn get(&self, path: &str) -> reqwest::Response {
        self.http.get(format!("http://127.0.0.1:{}{path}", self.port)).header("cookie", &self.cookie).send().await.unwrap()
    }

    /// Opens /ws through the proxy offering `protocols`; returns the socket
    /// and the subprotocol the proxy answered with.
    async fn ws(&self, query: &str, protocols: &[&str]) -> (tokio_tungstenite::WebSocketStream<tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>>, Option<String>) {
        let mut req = format!("ws://127.0.0.1:{}/ws{query}", self.port).into_client_request().unwrap();
        req.headers_mut().insert("cookie", self.cookie.parse().unwrap());
        if !protocols.is_empty() {
            req.headers_mut().insert("sec-websocket-protocol", protocols.join(", ").parse().unwrap());
        }
        let (ws, resp) = tokio_tungstenite::connect_async(req).await.expect("ws connect through proxy");
        let selected = resp.headers().get("sec-websocket-protocol").map(|v| v.to_str().unwrap().to_string());
        (ws, selected)
    }

    /// Adds a url connection pointing at a second stub, selects it, and
    /// returns (its id, its stub log).
    async fn select_url_connection(&self) -> (String, Log) {
        let (addr, log) = spawn_stub(false).await;
        let mut reg = self.state.registry.lock().unwrap();
        let conn = reg.add("remote", &format!("http://{addr}")).unwrap();
        reg.select(&conn.id).unwrap();
        (conn.id, log)
    }
}

async fn echo_roundtrip(ws: &mut tokio_tungstenite::WebSocketStream<tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>>) {
    ws.send(TMessage::text("hello")).await.unwrap();
    assert_eq!(ws.next().await.unwrap().unwrap().into_text().unwrap(), "hello");
}

#[tokio::test]
async fn ws_forwards_bearer_subprotocol_and_echoes_selected() {
    let h = harness().await;
    let (mut ws, selected) = h.ws("", &["other.proto", "smind.bearer.abc123"]).await;
    assert_eq!(selected.as_deref(), Some("smind.bearer.abc123"), "the browser aborts unless the selection is echoed");
    echo_roundtrip(&mut ws).await;
    let ws_seen = |h: &Harness| h.stub.lock().unwrap().iter().filter(|s| s.path == "/ws").cloned().collect::<Vec<_>>();
    let seen = ws_seen(&h)[0].clone();
    assert_eq!(seen.protocols.as_deref(), Some("smind.bearer.abc123"), "only smind.bearer.* is forwarded");
    assert_eq!(seen.authorization, None, "no credential of the proxy's own with nothing configured");

    // No offered protocol: nothing selected, still connects (unchanged behaviour).
    let (mut ws, selected) = h.ws("?token=t", &[]).await;
    assert_eq!(selected, None);
    echo_roundtrip(&mut ws).await;
    assert_eq!(ws_seen(&h)[1].query.as_deref(), Some("token=t"), "without a credential the query is passed through");
}

#[tokio::test]
async fn local_connection_with_403_token_falls_back_to_injected_bearer() {
    let h = harness_with(true).await;
    h.state.set_local_token_source(|| Some("file-token".to_string()));

    // The daemon refuses /api/token (403): the answer is synthesized, and
    // /api/* then carries the bearer.
    let body: serde_json::Value = h.get("/api/token").await.json().await.unwrap();
    let placeholder = body["token"].as_str().unwrap().to_string();
    assert!(!placeholder.is_empty() && placeholder != "daemon-token" && placeholder != "file-token");
    assert_eq!(h.stub.lock().unwrap()[0].path, "/api/token");
    assert_eq!(h.stub.lock().unwrap()[0].authorization, None, "the token fetch itself carries no credential");
    assert_eq!(h.get("/api/ping?token=zzz&x=1").await.status(), 200);
    let ping = h.stub.lock().unwrap()[1].clone();
    assert_eq!(ping.authorization.as_deref(), Some("Bearer file-token"));
    assert_eq!(ping.query.as_deref(), Some("x=1"), "the placeholder token param is stripped");

    // /ws: Authorization injected, ?token= dropped.
    let (mut ws, selected) = h.ws(&format!("?token={placeholder}"), &[]).await;
    assert_eq!(selected, None);
    echo_roundtrip(&mut ws).await;
    let seen = h.stub.lock().unwrap()[2].clone();
    assert_eq!(seen.path, "/ws");
    assert_eq!(seen.authorization.as_deref(), Some("Bearer file-token"));
    assert_eq!(seen.query, None);

    // The token file is read per attempt: a restarted daemon's new token is used.
    h.state.set_local_token_source(|| Some("rotated".to_string()));
    let (_ws, _) = h.ws("", &[]).await;
    assert_eq!(h.stub.lock().unwrap()[3].authorization.as_deref(), Some("Bearer rotated"));
}

#[tokio::test]
async fn local_connection_with_403_and_no_token_file_passes_the_403_through() {
    let h = harness_with(true).await;
    assert_eq!(h.get("/api/token").await.status(), 403);
}

#[tokio::test]
async fn token_file_is_never_used_for_a_403_that_is_not_a_password_smind() {
    // A 403 on /api/token from something whose /healthz is not a
    // password-protected smind: the 403 passes through and no bearer is sent.
    let h = harness_as(true, false).await;
    h.state.set_local_token_source(|| Some("file-token".to_string()));
    assert_eq!(h.get("/api/token").await.status(), 403);
    assert_eq!(h.get("/api/ping").await.status(), 200);
    let (_ws, _) = h.ws("", &[]).await;
    for seen in h.stub.lock().unwrap().iter() {
        assert_eq!(seen.authorization, None, "{seen:?}");
    }
}

#[tokio::test]
async fn local_connection_without_password_keeps_token_query_flow() {
    let h = harness().await;
    // A readable token file changes nothing for a daemon that serves /api/token.
    h.state.set_local_token_source(|| Some("file-token".to_string()));
    let body: serde_json::Value = h.get("/api/token").await.json().await.unwrap();
    assert_eq!(body["token"], "daemon-token", "passthrough, not synthesized");
    h.get("/api/ping?token=daemon-token").await;
    let (mut ws, _) = h.ws("?token=daemon-token", &[]).await;
    echo_roundtrip(&mut ws).await;
    let log = h.stub.lock().unwrap();
    assert_eq!(log[1].query.as_deref(), Some("token=daemon-token"));
    assert_eq!(log[2].query.as_deref(), Some("token=daemon-token"), "the token query still reaches an older daemon");
    for seen in log.iter() {
        assert_eq!(seen.authorization, None, "{seen:?}");
    }
}

#[tokio::test]
async fn local_connection_without_token_file_is_unchanged() {
    let h = harness().await;
    let body: serde_json::Value = h.get("/api/token").await.json().await.unwrap();
    assert_eq!(body["token"], "daemon-token", "passthrough");
    assert_eq!(h.stub.lock().unwrap()[0].authorization, None);
}

#[tokio::test]
async fn url_connection_never_gets_local_token_file_bearer() {
    let h = harness().await;
    h.state.set_local_token_source(|| Some("file-token".to_string()));
    let (_id, remote) = h.select_url_connection().await;

    let body: serde_json::Value = h.get("/api/token").await.json().await.unwrap();
    assert_eq!(body["token"], "daemon-token", "passed through, not synthesized");
    h.get("/api/ping").await;
    let (_ws, _) = h.ws("?token=t", &[]).await;
    for seen in remote.lock().unwrap().iter() {
        assert_eq!(seen.authorization, None, "{seen:?}");
    }
    assert_eq!(remote.lock().unwrap().len(), 3);
    assert!(h.stub.lock().unwrap().is_empty(), "the local stub was never used");
}

#[tokio::test]
async fn session_connection_injects_session_bearer_and_synthesizes_token() {
    let h = harness().await;
    h.state.set_local_token_source(|| Some("file-token".to_string()));
    let (id, remote) = h.select_url_connection().await;
    h.state.set_session(&id, "session-1".to_string());

    let body: serde_json::Value = h.get("/api/token").await.json().await.unwrap();
    assert_ne!(body["token"], "daemon-token");
    assert!(remote.lock().unwrap().is_empty());

    // An inbound Authorization from the webview is replaced, not trusted.
    let resp = h
        .http
        .get(format!("http://127.0.0.1:{}/api/ping?token=x", h.port))
        .header("cookie", &h.cookie)
        .header("authorization", "Bearer webview-made-this-up")
        .send()
        .await
        .unwrap();
    assert_eq!(resp.status(), 200);
    let ping = remote.lock().unwrap()[0].clone();
    assert_eq!(ping.authorization.as_deref(), Some("Bearer session-1"));
    assert_eq!(ping.query, None);

    let (mut ws, _) = h.ws("?token=placeholder", &[]).await;
    echo_roundtrip(&mut ws).await;
    let seen = remote.lock().unwrap()[1].clone();
    assert_eq!(seen.authorization.as_deref(), Some("Bearer session-1"));
    assert_eq!(seen.query, None);

    // Removing the session stops the injection.
    h.state.clear_session(&id);
    h.get("/api/ping").await;
    assert_eq!(remote.lock().unwrap().last().unwrap().authorization, None);
}

#[tokio::test]
async fn token_never_reaches_the_webview() {
    let h = harness_with(true).await;
    h.state.set_local_token_source(|| Some("file-token".to_string()));
    let mut surfaces: Vec<String> = Vec::new();

    for round in 0..2 {
        let secret = if round == 0 {
            "file-token"
        } else {
            let (id, _) = h.select_url_connection().await;
            h.state.set_session(&id, "session-secret".to_string());
            "session-secret"
        };
        for path in ["/api/token", "/api/ping"] {
            let resp = h.get(path).await;
            surfaces.push(format!("{:?}", resp.headers()));
            surfaces.push(resp.text().await.unwrap());
        }
        // Even when the browser offers a bearer subprotocol of its own, the
        // answer echoes the browser's value, never the proxy's credential.
        let (mut ws, selected) = h.ws("?token=placeholder", &["smind.bearer.browser-side"]).await;
        echo_roundtrip(&mut ws).await;
        assert_eq!(selected.as_deref(), Some("smind.bearer.browser-side"));
        surfaces.push(selected.unwrap());
        for s in &surfaces {
            assert!(!s.contains(secret), "a proxy credential reached the webview");
        }
    }
}

#[tokio::test]
async fn expired_session_is_cleared_so_the_webview_sees_the_daemons_refusal() {
    let h = harness().await;
    let (id, _remote) = h.select_url_connection().await;

    // /api: the daemon answers 401 to the session -> it is dropped.
    h.state.set_session(&id, "expired-session".to_string());
    let body: serde_json::Value = h.get("/api/token").await.json().await.unwrap();
    assert!(body["token"].as_str().is_some(), "synthesized while the session is held");
    assert_eq!(h.get("/api/ping").await.status(), 401, "the daemon's answer passes through");
    assert_eq!(h.state.session(&id), None);
    // Now /api/token reaches the daemon instead of looping on a dead placeholder.
    let token: serde_json::Value = h.get("/api/token").await.json().await.unwrap();
    assert_eq!(token["token"], "daemon-token");

    // /ws: a refused handshake drops the session too.
    h.state.set_session(&id, "expired-session".to_string());
    let mut req = format!("ws://127.0.0.1:{}/ws", h.port).into_client_request().unwrap();
    req.headers_mut().insert("cookie", h.cookie.parse().unwrap());
    assert!(tokio_tungstenite::connect_async(req).await.is_err());
    assert_eq!(h.state.session(&id), None);

    // A healthy session is never dropped.
    h.state.set_session(&id, "session-ok".to_string());
    assert_eq!(h.get("/api/ping").await.status(), 200);
    assert_eq!(h.state.session(&id).as_deref(), Some("session-ok"));
}
