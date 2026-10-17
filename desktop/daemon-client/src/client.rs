//! The connection loop: fetch the daemon token over HTTP, open /ws,
//! subscribe to permission.pending, and reconnect with backoff. Lives
//! here (not in the tauri crate) so it compiles and is exercisable
//! without webkit2gtk; the shell passes in a notification callback.

use std::sync::Arc;
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use tokio_tungstenite::connect_async;
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::Message;

use crate::backoff::Backoff;
use crate::cache::WorkspaceCache;
use crate::config::Config;
use crate::protocol;

/// A connection counts as stable (and resets the backoff schedule)
/// after it has stayed up this long without an error.
const STABLE_AFTER: Duration = Duration::from_secs(30);
const HTTP_TIMEOUT: Duration = Duration::from_secs(2);
const SUBSCRIBE_ID: &str = "events-sub-1";

/// A one-shot resolution has this long to get an answer before giving up.
const RESOLVE_TIMEOUT: Duration = Duration::from_secs(3);

type TokenSource = Arc<dyn Fn() -> Option<String> + Send + Sync>;

/// WatcherAuth is how the watcher authenticates to a password-protected
/// daemon (ADR-0031 D8), which refuses `GET /api/token`. The default (no
/// credentials) is the plain token-query flow every older daemon uses.
/// Credentials are only ever sent as an `Authorization: Bearer` header,
/// never in the URL, and never logged.
#[derive(Clone, Default)]
pub struct WatcherAuth {
    /// A held login session token for this connection, read per attempt
    /// (it is cleared when the session expires).
    pub session: Option<TokenSource>,
    /// The built-in local connection: when `/api/token` answers 403 and
    /// `/healthz` confirms a password-protected smind, use the daemon token
    /// file (read per attempt; local-by-address only).
    pub local_token_file: Option<TokenSource>,
}

impl WatcherAuth {
    /// local is the auth for the built-in `local` connection.
    pub fn local() -> Self {
        Self { session: None, local_token_file: Some(Arc::new(crate::proxy::local_token::read)) }
    }

    pub fn with_session(session: impl Fn() -> Option<String> + Send + Sync + 'static) -> Self {
        Self { session: Some(Arc::new(session)), local_token_file: None }
    }
}

/// How the /ws upgrade is authenticated.
enum Credential {
    /// The daemon token from `/api/token`, as `?token=`.
    Query(String),
    /// A bearer token, as the Authorization header.
    Bearer(String),
}

enum TokenError {
    Forbidden,
    Other(String),
}

/// credential picks what to authenticate with for one attempt: a held
/// session first, else the daemon's own token, else (403, local only, a
/// confirmed password smind) the token file.
async fn credential(cfg: &Config, client: &reqwest::Client, auth: &WatcherAuth) -> Result<Credential, String> {
    if let Some(token) = auth.session.as_ref().and_then(|s| s()) {
        return Ok(Credential::Bearer(token));
    }
    match fetch_token(cfg, client).await {
        Ok(token) => Ok(Credential::Query(token)),
        Err(TokenError::Other(e)) => Err(e),
        Err(TokenError::Forbidden) => {
            let refused = || "token fetch: the daemon refused (password-protected) and no credential is available".to_string();
            let Some(source) = &auth.local_token_file else { return Err(refused()) };
            if !crate::proxy::local_token::is_local_by_address(&cfg.daemon_url).await
                || !crate::proxy::local_token::is_password_smind(&crate::proxy::bounded::client(HTTP_TIMEOUT), cfg.daemon_url.as_str()).await
            {
                return Err(refused());
            }
            source().map(Credential::Bearer).ok_or_else(refused)
        }
    }
}

/// connect_authed opens /ws with whichever credential applies.
async fn connect_authed(
    cfg: &Config,
    client: &reqwest::Client,
    auth: &WatcherAuth,
) -> Result<(tokio_tungstenite::WebSocketStream<tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>>, String), String> {
    let (request, label) = match credential(cfg, client, auth).await? {
        Credential::Query(token) => {
            let url = cfg.ws_url(&token)?;
            let label = url_label(&url);
            (url.as_str().into_client_request().map_err(|e| format!("ws request: {e}"))?, label)
        }
        Credential::Bearer(token) => {
            let url = cfg.ws_base_url()?;
            let label = url_label(&url);
            let mut request = url.as_str().into_client_request().map_err(|e| format!("ws request: {e}"))?;
            let mut value = tokio_tungstenite::tungstenite::http::HeaderValue::from_str(&format!("Bearer {token}"))
                .map_err(|_| "ws request: unusable credential".to_string())?;
            value.set_sensitive(true);
            request.headers_mut().insert(tokio_tungstenite::tungstenite::http::header::AUTHORIZATION, value);
            (request, label)
        }
    };
    let (ws, _resp) = connect_async(request).await.map_err(|e| format!("ws connect: {e}"))?;
    Ok((ws, label))
}

/// url_label is what may be logged of a /ws URL: scheme, host, port and
/// path, never the query (which can carry the daemon token).
fn url_label(url: &url::Url) -> String {
    let host = url.host_str().unwrap_or_default();
    match url.port() {
        Some(port) => format!("{}://{host}:{port}{}", url.scheme(), url.path()),
        None => format!("{}://{host}{}", url.scheme(), url.path()),
    }
}

/// healthz_ok reports whether the daemon answers GET /healthz.
pub async fn healthz_ok(cfg: &Config) -> bool {
    let client = reqwest::Client::new();
    match client
        .get(cfg.healthz_url())
        .timeout(HTTP_TIMEOUT)
        .send()
        .await
    {
        Ok(resp) => resp.status().is_success(),
        Err(_) => false,
    }
}

/// resolve_workspace_id opens its own short-lived connection to answer
/// one task.get, for a `smind://task/<id>` deep link (quick-wins AC6)
/// that arrives with no workspaceId and isn't already in the
/// long-lived connection's cache. Best-effort: None on any failure or
/// timeout, same as a cache miss.
pub async fn resolve_workspace_id(cfg: &Config, task_id: i64) -> Option<i64> {
    let client = reqwest::Client::new();
    let (ws, _label) = connect_authed(cfg, &client, &WatcherAuth::default()).await.ok()?;
    let (mut tx, mut rx) = ws.split();

    let req_id = protocol::task_get_request_id(task_id);
    let req = protocol::task_get_message(&req_id, task_id).to_string();
    tx.send(Message::Text(req.into())).await.ok()?;

    let deadline = tokio::time::sleep(RESOLVE_TIMEOUT);
    tokio::pin!(deadline);
    loop {
        tokio::select! {
            _ = &mut deadline => return None,
            msg = rx.next() => {
                let m = msg?.ok()?;
                if !(m.is_text() || m.is_binary()) {
                    continue;
                }
                let text = m.into_text().ok()?;
                let parsed = protocol::parse_server_message(&text)?;
                if let (Some(id), Some(result)) = (parsed.id, parsed.result) {
                    if protocol::task_get_response_task_id(&id) == Some(task_id) {
                        return protocol::task_get_workspace_id(&result);
                    }
                }
            }
        }
    }
}

/// subscribe_message builds the events.subscribe request the client
/// sends right after the /ws upgrade. run.status is subscribed
/// alongside permission.pending so quick-wins AC4's tray attention can
/// clear a task's pending entry when its run (re)starts -- see
/// `crate::attention`.
pub fn subscribe_message() -> serde_json::Value {
    serde_json::json!({
        "id": SUBSCRIBE_ID,
        "method": "events.subscribe",
        "params": { "topics": ["permission.pending", "run.status"] }
    })
}

/// RESOLVED_SUBSCRIBE_ID identifies the separate permission.resolved
/// subscribe request.
const RESOLVED_SUBSCRIBE_ID: &str = "subscribe-permission-resolved";

/// subscribe_resolved_message subscribes to permission.resolved on its own:
/// an older daemon rejects unknown topics, and if that were in the first
/// message it would drop the whole subscription. Its failure is ignored.
pub fn subscribe_resolved_message() -> serde_json::Value {
    serde_json::json!({
        "id": RESOLVED_SUBSCRIBE_ID,
        "method": "events.subscribe",
        "params": { "topics": ["permission.resolved"] }
    })
}

/// subscribe_messages is every subscribe request sent after connecting,
/// in order: the original one, then the separate permission.resolved one.
pub fn subscribe_messages() -> Vec<serde_json::Value> {
    vec![subscribe_message(), subscribe_resolved_message()]
}

/// ClientEvent is what `run` hands to its callback: a notification to
/// show, a run that just (re)started (clears that task's tray
/// attention), a permission that was resolved, a run that ended, or a
/// fresh connection (resets it).
#[derive(Debug, PartialEq, Eq)]
pub enum ClientEvent {
    Notification(protocol::Notification),
    RunRunning { task_id: i64 },
    RunEnded { task_id: i64 },
    PermissionResolved { request_id: String },
    Reconnected,
}

/// attention_event maps a run.status / permission.resolved event to the
/// ClientEvent the tray cares about (None for anything else).
fn attention_event(event: &protocol::EventNotification) -> Option<ClientEvent> {
    match event.topic.as_str() {
        "run.status" => {
            let rs = protocol::run_status(&event.payload)?;
            Some(if rs.status == "running" {
                ClientEvent::RunRunning { task_id: rs.task_id }
            } else {
                ClientEvent::RunEnded { task_id: rs.task_id }
            })
        }
        "permission.resolved" => {
            let p = protocol::permission_resolved(&event.payload)?;
            Some(ClientEvent::PermissionResolved { request_id: p.request_id })
        }
        _ => None,
    }
}

/// run drives the token fetch + WS connection forever, handing each
/// event to on_event and resolving a notification's workspaceId into
/// `cache` in the background (permission.pending carries no workspaceId
/// -- see `crate::cache`). Reconnects with exponential backoff (reset
/// after a stable connection), refetching the token and resubscribing
/// on every attempt.
pub async fn run(
    cfg: Config,
    cache: WorkspaceCache,
    on_event: impl Fn(ClientEvent) + Send + Sync + 'static,
) {
    run_with_auth(cfg, WatcherAuth::default(), cache, on_event).await
}

/// run_with_auth is `run` for a daemon that may be password-protected.
pub async fn run_with_auth(
    cfg: Config,
    auth: WatcherAuth,
    cache: WorkspaceCache,
    on_event: impl Fn(ClientEvent) + Send + Sync + 'static,
) {
    let client = reqwest::Client::new();
    let mut backoff = Backoff::default();
    loop {
        let stable = run_once(&cfg, &client, &auth, &cache, &on_event).await;
        if stable {
            backoff.reset();
        }
        tokio::time::sleep(backoff.next()).await;
    }
}

/// run_once performs one connect/subscribe/serve cycle. Returns true
/// if the connection stayed up long enough to count as stable (so the
/// caller resets its backoff) before ending.
async fn run_once(
    cfg: &Config,
    client: &reqwest::Client,
    auth: &WatcherAuth,
    cache: &WorkspaceCache,
    on_event: &impl Fn(ClientEvent),
) -> bool {
    match try_run_once(cfg, client, auth, cache, on_event).await {
        Ok(stable) => stable,
        Err(err) => {
            eprintln!("smind desktop: daemon connection error: {err}");
            false
        }
    }
}

async fn try_run_once(
    cfg: &Config,
    client: &reqwest::Client,
    auth: &WatcherAuth,
    cache: &WorkspaceCache,
    on_event: &impl Fn(ClientEvent),
) -> Result<bool, String> {
    let (ws, label) = connect_authed(cfg, client, auth).await?;
    eprintln!("smind desktop: ws connected to {label}");
    let (mut tx, mut rx) = ws.split();

    let [sub, sub_resolved] = [subscribe_message(), subscribe_resolved_message()].map(|m| m.to_string());
    tx.send(Message::Text(sub.into()))
        .await
        .map_err(|e| format!("ws send subscribe: {e}"))?;
    // Separate message: an older daemon rejects the unknown topic, which
    // must not affect the subscription above. A send failure here means
    // the socket is gone, which the read loop below notices anyway.
    let _ = tx.send(Message::Text(sub_resolved.into())).await;
    eprintln!("smind desktop: subscribed to permission.pending, run.status");
    on_event(ClientEvent::Reconnected);

    let mut stable = false;
    let stable_timer = tokio::time::sleep(STABLE_AFTER);
    tokio::pin!(stable_timer);
    loop {
        tokio::select! {
            _ = &mut stable_timer, if !stable => {
                stable = true;
            }
            msg = rx.next() => {
                match msg {
                    Some(Ok(m)) if m.is_text() || m.is_binary() => {
                        let text = m.into_text().map_err(|e| format!("ws text: {e}"))?;
                        let Some(parsed) = protocol::parse_server_message(text.as_str()) else {
                            continue;
                        };
                        if let Some(event) = parsed.event {
                            if let Some(n) = protocol::notification_for(&event) {
                                // quick-wins AC3: resolve the notification's
                                // workspaceId in the background, over this
                                // same connection, so a later click can
                                // build a route without blocking on it.
                                let req_id = protocol::task_get_request_id(n.task_id);
                                let req = protocol::task_get_message(&req_id, n.task_id).to_string();
                                let _ = tx.send(Message::Text(req.into())).await;
                                on_event(ClientEvent::Notification(n));
                            } else if let Some(ev) = attention_event(&event) {
                                on_event(ev);
                            }
                        } else if let (Some(id), Some(result)) = (parsed.id, parsed.result) {
                            if let Some(task_id) = protocol::task_get_response_task_id(&id) {
                                if let Some(workspace_id) = protocol::task_get_workspace_id(&result) {
                                    cache.insert(task_id, workspace_id);
                                }
                            }
                        }
                    }
                    Some(Ok(_)) => {} // ping/pong frames; tungstenite answers pings itself
                    Some(Err(e)) => {
                        eprintln!("smind desktop: ws error: {e}");
                        return Ok(stable);
                    }
                    None => {
                        eprintln!("smind desktop: ws closed by daemon");
                        return Ok(stable);
                    }
                }
            }
        }
    }
}

/// run_over_relay is `run`'s counterpart for a `relay`-kind connection
/// (AC7): the relay transport (`crate::relay::client::spawn`) already
/// owns reconnect/backoff/resume internally, so this just (re)subscribes
/// on every transition to `Connected` (each one is a fresh logical
/// wsapi connection daemon-side, exactly like a fresh `/ws` upgrade --
/// see `crate::relay::client::run`'s doc comment) and parses inbound
/// messages the same way `try_run_once` does. Kept as a short, separate
/// function rather than unified with the WS path: the two transports'
/// reconnection ownership differs enough (this one is entirely internal
/// to `handle`) that sharing one generic loop would need more
/// abstraction than the ~30 lines it would save.
pub async fn run_over_relay(
    handle: crate::relay::client::RelayHandle,
    cache: WorkspaceCache,
    on_event: impl Fn(ClientEvent) + Send + Sync + 'static,
) {
    let mut status_rx = handle.status();
    let mut inbound = handle.subscribe();
    loop {
        tokio::select! {
            changed = status_rx.changed() => {
                if changed.is_err() {
                    return; // every RelayHandle sender dropped
                }
                if *status_rx.borrow() == crate::relay::client::Status::Connected {
                    let [sub, sub_resolved] = [subscribe_message(), subscribe_resolved_message()].map(|m| m.to_string());
                    if handle.send(sub.into_bytes()).await.is_err() {
                        return;
                    }
                    // Separate and best-effort, see `subscribe_resolved_message`.
                    let _ = handle.send(sub_resolved.into_bytes()).await;
                    on_event(ClientEvent::Reconnected);
                }
            }
            msg = inbound.recv() => {
                match msg {
                    Ok(bytes) => {
                        let Ok(text) = String::from_utf8(bytes) else { continue };
                        handle_relay_message(&text, &cache, &on_event, &handle).await;
                    }
                    Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => continue,
                    Err(tokio::sync::broadcast::error::RecvError::Closed) => return,
                }
            }
        }
    }
}

/// handle_relay_message mirrors `try_run_once`'s inner per-message match
/// (see that function's comments for the rationale of each branch), with
/// `handle.send` standing in for the WS `tx.send`.
async fn handle_relay_message(
    text: &str,
    cache: &WorkspaceCache,
    on_event: &impl Fn(ClientEvent),
    handle: &crate::relay::client::RelayHandle,
) {
    let Some(parsed) = protocol::parse_server_message(text) else {
        return;
    };
    if let Some(event) = parsed.event {
        if let Some(n) = protocol::notification_for(&event) {
            let req_id = protocol::task_get_request_id(n.task_id);
            let req = protocol::task_get_message(&req_id, n.task_id).to_string();
            let _ = handle.send(req.into_bytes()).await;
            on_event(ClientEvent::Notification(n));
        } else if let Some(ev) = attention_event(&event) {
            on_event(ev);
        }
    } else if let (Some(id), Some(result)) = (parsed.id, parsed.result) {
        if let Some(task_id) = protocol::task_get_response_task_id(&id) {
            if let Some(workspace_id) = protocol::task_get_workspace_id(&result) {
                cache.insert(task_id, workspace_id);
            }
        }
    }
}

async fn fetch_token(cfg: &Config, client: &reqwest::Client) -> Result<String, TokenError> {
    #[derive(serde::Deserialize)]
    struct TokenResp {
        token: String,
    }
    let resp = client
        .get(cfg.token_url())
        .timeout(HTTP_TIMEOUT)
        .send()
        .await
        .map_err(|e| TokenError::Other(format!("token fetch: {}", e.without_url())))?;
    if resp.status() == reqwest::StatusCode::FORBIDDEN {
        return Err(TokenError::Forbidden);
    }
    let body: TokenResp = crate::proxy::bounded::read_json(resp, crate::proxy::bounded::MAX_BODY_BYTES)
        .await
        .ok_or_else(|| TokenError::Other("token decode: not a token answer".to_string()))?;
    Ok(body.token)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn subscribe_message_shape() {
        let msg = subscribe_message();
        assert_eq!(msg["method"], "events.subscribe");
        assert_eq!(msg["params"]["topics"][0], "permission.pending");
        assert_eq!(msg["params"]["topics"][1], "run.status");
        assert!(msg.get("id").is_some());
        // Round-trips through the envelope parser without being mistaken
        // for a server event.
        let parsed = crate::protocol::parse_server_message(&msg.to_string()).unwrap();
        assert!(parsed.event.is_none());
    }

    #[test]
    fn subscribe_messages_put_permission_resolved_in_its_own_message() {
        let msgs = subscribe_messages();
        assert_eq!(msgs.len(), 2);
        assert_eq!(msgs[0], subscribe_message());
        assert_eq!(msgs[0]["params"]["topics"], serde_json::json!(["permission.pending", "run.status"]));
        assert_eq!(msgs[1]["method"], "events.subscribe");
        assert_eq!(msgs[1]["params"]["topics"], serde_json::json!(["permission.resolved"]));
        assert_ne!(msgs[0]["id"], msgs[1]["id"]);
    }

    fn event(topic: &str, payload: serde_json::Value) -> protocol::EventNotification {
        protocol::EventNotification { topic: topic.to_string(), seq: 1, payload }
    }

    #[test]
    fn run_status_done_emits_run_ended() {
        let done = event("run.status", serde_json::json!({"runId":"r","taskId":5,"status":"done"}));
        assert_eq!(attention_event(&done), Some(ClientEvent::RunEnded { task_id: 5 }));
        let running = event("run.status", serde_json::json!({"runId":"r","taskId":5,"status":"running"}));
        assert_eq!(attention_event(&running), Some(ClientEvent::RunRunning { task_id: 5 }));
        let resolved = event("permission.resolved", serde_json::json!({"runId":"r","taskId":5,"requestId":"q","reason":"human"}));
        assert_eq!(attention_event(&resolved), Some(ClientEvent::PermissionResolved { request_id: "q".into() }));
    }

    /// A password-protected stub daemon: /api/token 403, /healthz a
    /// password smind, /ws upgrades only with `Authorization: Bearer <accept>`
    /// and never with a `?token=` query. Records what each /ws saw.
    type WsSeen = Arc<std::sync::Mutex<Vec<(Option<String>, Option<String>)>>>;

    async fn protected_daemon(accept: &'static str) -> (Config, WsSeen) {
        use axum::extract::ws::WebSocketUpgrade;
        use axum::http::{HeaderMap, StatusCode};
        use axum::response::IntoResponse;
        let seen: WsSeen = Arc::default();
        let log = seen.clone();
        let app = axum::Router::new()
            .route("/api/token", axum::routing::get(|| async { (StatusCode::FORBIDDEN, "password configured") }))
            .route(
                "/healthz",
                axum::routing::get(|| async { axum::Json(serde_json::json!({"service":"smind","authRequired":true})) }),
            )
            .route(
                "/ws",
                axum::routing::get(move |headers: HeaderMap, axum::extract::RawQuery(q): axum::extract::RawQuery, ws: WebSocketUpgrade| async move {
                    let auth = headers.get("authorization").and_then(|v| v.to_str().ok()).map(str::to_string);
                    log.lock().unwrap().push((auth.clone(), q.clone()));
                    if q.is_some() || auth.as_deref() != Some(&format!("Bearer {accept}")) {
                        return StatusCode::UNAUTHORIZED.into_response();
                    }
                    ws.on_upgrade(|_socket| async {}).into_response()
                }),
            );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            let _ = axum::serve(listener, app).await;
        });
        (Config { daemon_url: format!("http://{addr}").parse().unwrap() }, seen)
    }

    #[tokio::test]
    async fn watcher_authenticates_to_protected_daemon_with_token_file_bearer() {
        let (cfg, seen) = protected_daemon("file-token").await;
        let auth = WatcherAuth { session: None, local_token_file: Some(Arc::new(|| Some("file-token".to_string()))) };
        let (_ws, label) = connect_authed(&cfg, &reqwest::Client::new(), &auth).await.expect("authenticated");
        assert_eq!(seen.lock().unwrap()[0], (Some("Bearer file-token".to_string()), None), "bearer header, no query");
        assert!(!label.contains("token") && !label.contains('?'), "the log label must carry no query");
    }

    #[tokio::test]
    async fn watcher_uses_held_session_as_bearer_never_in_the_url() {
        let (cfg, seen) = protected_daemon("session-1").await;
        let auth = WatcherAuth::with_session(|| Some("session-1".to_string()));
        connect_authed(&cfg, &reqwest::Client::new(), &auth).await.expect("authenticated");
        assert_eq!(seen.lock().unwrap()[0], (Some("Bearer session-1".to_string()), None));
        // An expired/cleared session falls back to the token flow, which a
        // password daemon refuses: an error, not a query-token attempt.
        let cleared = WatcherAuth::with_session(|| None);
        let err = connect_authed(&cfg, &reqwest::Client::new(), &cleared).await.map(|_| ()).unwrap_err();
        assert!(err.contains("password-protected"), "{err}");
        assert_eq!(seen.lock().unwrap().len(), 1, "nothing was sent to /ws");
    }

    #[tokio::test]
    async fn watcher_without_credentials_does_not_authenticate() {
        let (cfg, seen) = protected_daemon("x").await;
        let err = connect_authed(&cfg, &reqwest::Client::new(), &WatcherAuth::default()).await.err().unwrap();
        assert!(err.contains("password-protected"), "{err}");
        assert!(seen.lock().unwrap().is_empty());
        // The token file is not trusted for a daemon whose healthz is not a password smind.
        let url_label_check = url_label(&"ws://127.0.0.1:1/ws?token=SECRET".parse().unwrap());
        assert_eq!(url_label_check, "ws://127.0.0.1:1/ws");
    }
}
