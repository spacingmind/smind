//! The connection loop: fetch the daemon token over HTTP, open /ws,
//! subscribe to permission.pending, and reconnect with backoff. Lives
//! here (not in the tauri crate) so it compiles and is exercisable
//! without webkit2gtk; the shell passes in a notification callback.

use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use tokio_tungstenite::connect_async;
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

/// healthz_ok reports whether the daemon answers GET /healthz.
pub async fn healthz_ok(cfg: &Config) -> bool {
    let client = reqwest::Client::new();
    match client.get(cfg.healthz_url()).timeout(HTTP_TIMEOUT).send().await {
        Ok(resp) => resp.status().is_success(),
        Err(_) => false,
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

/// ClientEvent is what `run` hands to its callback: a notification to
/// show, a run that just (re)started (clears that task's tray
/// attention), or a fresh connection (resets it).
pub enum ClientEvent {
    Notification(protocol::Notification),
    RunRunning { task_id: i64 },
    Reconnected,
}

/// run drives the token fetch + WS connection forever, handing each
/// event to on_event and resolving a notification's workspaceId into
/// `cache` in the background (permission.pending carries no workspaceId
/// -- see `crate::cache`). Reconnects with exponential backoff (reset
/// after a stable connection), refetching the token and resubscribing
/// on every attempt.
pub async fn run(cfg: Config, cache: WorkspaceCache, on_event: impl Fn(ClientEvent) + Send + Sync + 'static) {
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
    let ws_url = cfg.ws_url(&token)?;
    let (ws, _resp) = connect_async(ws_url.as_str())
        .await
        .map_err(|e| format!("ws connect: {e}"))?;
    eprintln!("smind desktop: ws connected to {}", ws_url.as_str());
    let (mut tx, mut rx) = ws.split();

    let sub = subscribe_message().to_string();
    tx.send(Message::Text(sub.into()))
        .await
        .map_err(|e| format!("ws send subscribe: {e}"))?;
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
                            } else if event.topic == "run.status" {
                                if let Some(rs) = protocol::run_status(&event.payload) {
                                    if rs.status == "running" {
                                        on_event(ClientEvent::RunRunning { task_id: rs.task_id });
                                    }
                                }
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

async fn fetch_token(cfg: &Config, client: &reqwest::Client) -> Result<String, String> {
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
        .json()
        .await
        .map_err(|e| format!("token decode: {e}"))?;
    Ok(resp.token)
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
}
