//! The connection loop: fetch the daemon token over HTTP, open /ws,
//! subscribe to permission.pending, and reconnect with backoff. Lives
//! here (not in the tauri crate) so it compiles and is exercisable
//! without webkit2gtk; the shell passes in a notification callback.

use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::http::{header::AUTHORIZATION, HeaderValue};
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::{connect_async, MaybeTlsStream, WebSocketStream};

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
    let token = fetch_token(cfg, &client).await.ok()?;
    let ws = connect_ws(cfg, &token).await.ok()?;
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
    let client = reqwest::Client::new();
    let mut backoff = Backoff::default();
    loop {
        let stable = run_once(&cfg, &client, &cache, &on_event).await;
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
    cache: &WorkspaceCache,
    on_event: &impl Fn(ClientEvent),
) -> bool {
    match try_run_once(cfg, client, cache, on_event).await {
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
    cache: &WorkspaceCache,
    on_event: &impl Fn(ClientEvent),
) -> Result<bool, String> {
    let token = fetch_token(cfg, client).await?;
    let ws = connect_ws(cfg, &token).await?;
    // Not the URL: it carries the token.
    eprintln!("smind desktop: ws connected to {}", cfg.daemon_url);
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

/// connect_ws opens /ws with the token in the Authorization header (which a
/// daemon with a password requires) and in the URL (which daemons that predate
/// that read).
async fn connect_ws(cfg: &Config, token: &str) -> Result<WebSocketStream<MaybeTlsStream<tokio::net::TcpStream>>, String> {
    let url = cfg.ws_url(token)?;
    let mut req = url.as_str().into_client_request().map_err(|e| format!("ws request: {e}"))?;
    let bearer = HeaderValue::from_str(&format!("Bearer {token}")).map_err(|e| format!("ws auth header: {e}"))?;
    req.headers_mut().insert(AUTHORIZATION, bearer);
    let (ws, _resp) = connect_async(req).await.map_err(|e| format!("ws connect: {e}"))?;
    Ok(ws)
}

/// fetch_token gets the daemon token from GET /api/token; a daemon with a
/// password refuses that (403, ADR-0031 D8), so for a daemon on this machine it
/// falls back to the token file the same OS user can read.
async fn fetch_token(cfg: &Config, client: &reqwest::Client) -> Result<String, String> {
    match fetch_token_http(cfg, client).await {
        Ok(token) => Ok(token),
        Err(err) => {
            if cfg.is_loopback_daemon() {
                if let Some(token) = crate::config::read_local_token() {
                    return Ok(token);
                }
            }
            Err(err)
        }
    }
}

async fn fetch_token_http(cfg: &Config, client: &reqwest::Client) -> Result<String, String> {
    #[derive(serde::Deserialize)]
    struct TokenResp {
        token: String,
    }
    let resp: TokenResp = client
        .get(cfg.token_url())
        .timeout(HTTP_TIMEOUT)
        .send()
        .await
        .map_err(|e| format!("token fetch: {e}"))?
        .error_for_status()
        .map_err(|e| format!("token fetch: {e}"))?
        .json()
        .await
        .map_err(|e| format!("token decode: {e}"))?;
    Ok(resp.token)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// serves one canned HTTP response per connection on an ephemeral port.
    async fn canned_http(status_line: &'static str, body: &'static str) -> std::net::SocketAddr {
        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            while let Ok((mut sock, _)) = listener.accept().await {
                let mut buf = [0u8; 2048];
                let _ = sock.read(&mut buf).await;
                let resp = format!(
                    "HTTP/1.1 {status_line}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                    body.len()
                );
                let _ = sock.write_all(resp.as_bytes()).await;
            }
        });
        addr
    }

    static ENV_LOCK: tokio::sync::Mutex<()> = tokio::sync::Mutex::const_new(());

    #[tokio::test]
    async fn token_comes_from_the_endpoint_when_it_serves_one() {
        let addr = canned_http("200 OK", r#"{"token":"from-endpoint"}"#).await;
        let cfg = Config { daemon_url: format!("http://{addr}").parse().unwrap() };
        assert_eq!(fetch_token(&cfg, &reqwest::Client::new()).await.unwrap(), "from-endpoint");
    }

    #[tokio::test]
    async fn token_falls_back_to_the_file_when_a_password_daemon_refuses_the_endpoint() {
        let addr = canned_http("403 Forbidden", r#"{"error":"a password is configured"}"#).await;
        let cfg = Config { daemon_url: format!("http://{addr}").parse().unwrap() };
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("token"), "from-file\n").unwrap();
        let _guard = ENV_LOCK.lock().await;
        let prev = std::env::var("SMIND_HOME").ok();
        std::env::set_var("SMIND_HOME", dir.path());
        let got = fetch_token(&cfg, &reqwest::Client::new()).await;
        match prev {
            Some(v) => std::env::set_var("SMIND_HOME", v),
            None => std::env::remove_var("SMIND_HOME"),
        }
        assert_eq!(got.unwrap(), "from-file");
    }

    #[tokio::test]
    async fn a_remote_daemon_never_uses_the_local_token_file() {
        // 127.0.0.2 is loopback, so use a name that is not: the point is the
        // is_loopback_daemon gate, which fetch_token consults before the file.
        let cfg = Config { daemon_url: "http://192.0.2.1:1".parse().unwrap() };
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("token"), "local-secret").unwrap();
        let _guard = ENV_LOCK.lock().await;
        let prev = std::env::var("SMIND_HOME").ok();
        std::env::set_var("SMIND_HOME", dir.path());
        let got = fetch_token(&cfg, &reqwest::Client::new()).await;
        match prev {
            Some(v) => std::env::set_var("SMIND_HOME", v),
            None => std::env::remove_var("SMIND_HOME"),
        }
        assert!(got.is_err(), "a remote daemon must not be handed this machine's token: {got:?}");
    }

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
}
