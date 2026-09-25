//! Integration test for the loopback proxy (AC2 Test Scenario): starts
//! the real proxy against an in-process stub HTTP+WS server standing in
//! for a smind daemon, and asserts `/api/token` passthrough, a `/ws`
//! echo round trip, that the stub sees no `Origin` header, and that
//! re-pointing the registry to a second stub works without restarting
//! the server.

use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use axum::extract::ws::{Message, WebSocketUpgrade};
use axum::extract::State;
use axum::http::HeaderMap;
use axum::routing::get;
use axum::Router;
use futures_util::{SinkExt, StreamExt};
use tokio::net::TcpListener;
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::Message as TMessage;
use url::Url;

use smind_daemon_client::proxy::{server::AssetSource, server::ProxyState, Registry};

/// A stub "daemon": GET /api/token returns a fixed token, GET /ws echoes
/// every text message back once. saw_origin flips true if any request
/// arrived with an Origin header -- the proxy must never let that
/// happen.
struct Stub {
    addr: SocketAddr,
    saw_origin: Arc<AtomicBool>,
}

async fn spawn_stub(token: &'static str) -> Stub {
    let saw_origin = Arc::new(AtomicBool::new(false));
    let state = (token, saw_origin.clone());

    let app = Router::new()
        .route(
            "/api/token",
            get(move |State((token, saw_origin)): State<(&'static str, Arc<AtomicBool>)>, headers: HeaderMap| async move {
                if headers.contains_key(axum::http::header::ORIGIN) {
                    saw_origin.store(true, Ordering::SeqCst);
                }
                axum::Json(serde_json::json!({ "token": token }))
            }),
        )
        .route(
            "/ws",
            get(
                |State((_token, saw_origin)): State<(&'static str, Arc<AtomicBool>)>,
                 headers: HeaderMap,
                 ws: WebSocketUpgrade| async move {
                    if headers.contains_key(axum::http::header::ORIGIN) {
                        saw_origin.store(true, Ordering::SeqCst);
                    }
                    ws.on_upgrade(|mut socket| async move {
                        while let Some(Ok(msg)) = socket.next().await {
                            if matches!(msg, Message::Close(_)) {
                                break;
                            }
                            if socket.send(msg).await.is_err() {
                                break;
                            }
                        }
                    })
                },
            ),
        )
        .with_state(state);

    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let _ = axum::serve(listener, app).await;
    });
    Stub { addr, saw_origin }
}

struct EmptyAssets;
impl AssetSource for EmptyAssets {
    fn get(&self, _path: &str) -> Option<(Vec<u8>, String)> {
        None
    }
}

/// exchange performs the `?k=` -> cookie exchange against `base` and
/// returns the cookie value to use on subsequent requests.
async fn exchange_secret(client: &reqwest::Client, base: &str, secret: &str) -> String {
    let resp = client
        .get(format!("{base}/?k={secret}"))
        .send()
        .await
        .expect("exchange request");
    assert!(resp.status().is_redirection(), "expected a redirect from the k= exchange, got {}", resp.status());
    let set_cookie = resp
        .headers()
        .get(axum::http::header::SET_COOKIE)
        .expect("Set-Cookie header present")
        .to_str()
        .unwrap()
        .to_string();
    // "smind_desktop=<value>; HttpOnly; ..." -- just the name=value pair.
    set_cookie.split(';').next().unwrap().to_string()
}

#[tokio::test]
async fn proxies_token_ws_and_reroutes_with_no_origin_leaked() {
    let stub_a = spawn_stub("tok-a").await;
    let local_url = Url::parse(&format!("http://{}", stub_a.addr)).unwrap();

    let real_secret = "test-secret-1234567890";
    let state = ProxyState::new(real_secret.to_string(), Registry::new(&local_url), Box::new(EmptyAssets));
    let (port, server_fut) = smind_daemon_client::proxy::serve(state.clone()).await.unwrap();
    tokio::spawn(server_fut);

    let base = format!("http://127.0.0.1:{port}");
    let http = reqwest::Client::builder().redirect(reqwest::redirect::Policy::none()).build().unwrap();

    // No cookie, no k: every kind of request is rejected.
    assert_eq!(http.get(format!("{base}/api/token")).send().await.unwrap().status(), 403);
    assert_eq!(http.get(&base).send().await.unwrap().status(), 403);

    // The `?k=` exchange issues the cookie.
    let cookie = exchange_secret(&http, &base, real_secret).await;

    // /api/token passthrough, with an inbound Origin the proxy must drop.
    let resp = http
        .get(format!("{base}/api/token"))
        .header(axum::http::header::COOKIE, &cookie)
        .header(axum::http::header::ORIGIN, "http://evil.example")
        .send()
        .await
        .unwrap();
    assert_eq!(resp.status(), 200);
    let body: serde_json::Value = resp.json().await.unwrap();
    assert_eq!(body["token"], "tok-a");
    assert!(!stub_a.saw_origin.load(Ordering::SeqCst), "stub should never see an Origin header");

    // /ws echo round trip, through the proxy, with the gate cookie.
    let ws_url = format!("ws://127.0.0.1:{port}/ws?token=tok-a");
    let mut req = ws_url.into_client_request().unwrap();
    req.headers_mut().insert(axum::http::header::COOKIE, cookie.parse().unwrap());
    let (mut ws, _resp) = tokio_tungstenite::connect_async(req).await.expect("ws connect through proxy");
    ws.send(TMessage::text("hello")).await.unwrap();
    let echoed = ws.next().await.unwrap().unwrap();
    assert_eq!(echoed.into_text().unwrap(), "hello");
    ws.close(None).await.ok();
    assert!(!stub_a.saw_origin.load(Ordering::SeqCst), "stub ws upgrade should never see an Origin header either");

    // Re-point to a second stub without restarting the proxy.
    let stub_b = spawn_stub("tok-b").await;
    {
        let mut registry = state.registry.lock().unwrap();
        let conn = registry.add("Stub B", &format!("http://{}", stub_b.addr)).unwrap();
        registry.select(&conn.id).unwrap();
    }
    let resp = http
        .get(format!("{base}/api/token"))
        .header(axum::http::header::COOKIE, &cookie)
        .send()
        .await
        .unwrap();
    let body: serde_json::Value = resp.json().await.unwrap();
    assert_eq!(body["token"], "tok-b");
}
