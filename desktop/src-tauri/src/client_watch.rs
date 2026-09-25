//! Spawns (and, on a connection switch, respawns) the daemon-client
//! watcher: the same connection ADR-0012 already drives the tray's
//! pending count and OS notifications from, now started against
//! whichever connection is currently selected (AC7) rather than a
//! single fixed `SMIND_DAEMON_URL`.

use tauri::AppHandle;
use url::Url;

use smind_daemon_client as dclient;
use smind_daemon_client::{ClientEvent, Config, WorkspaceCache};

use crate::notify;
use crate::state::DesktopState;
use crate::tray::Tray;
use std::sync::Arc;

/// spawn starts the watcher against `cfg` and returns its task handle.
pub fn spawn(app: &AppHandle, tray: Arc<Tray>, cache: WorkspaceCache, proxy_url: Url, cfg: Config) -> tauri::async_runtime::JoinHandle<()> {
    let handle = app.clone();
    tauri::async_runtime::spawn(async move {
        dclient::client::run(cfg, cache.clone(), move |event| match event {
            ClientEvent::Notification(n) => {
                tray.on_permission_pending(n.task_id, &n.request_id);
                let ctx = notify::ClickContext { app: handle.clone(), cache: cache.clone(), proxy_url: proxy_url.clone() };
                notify::show(ctx, n.title, n.body, n.task_id);
            }
            ClientEvent::RunRunning { task_id } => tray.on_run_running(task_id),
            ClientEvent::Reconnected => tray.on_reconnected(),
        })
        .await;
    })
}

/// restart aborts the currently-running watcher (if any) and starts a
/// fresh one against `state`'s newly-selected connection -- called by
/// the `connections_select` command. Reusing the same `state.cache`
/// keeps any already-resolved task->workspace lookups valid across the
/// switch (they're a plain taskId->workspaceId map, not tied to any one
/// connection).
pub fn restart(app: &AppHandle, state: &DesktopState, daemon_url: Url) {
    if let Some(old) = state.client_task.lock().unwrap().take() {
        old.abort();
    }
    let cfg = Config { daemon_url };
    let handle = spawn(app, state.tray.clone(), state.cache.clone(), state.proxy_url.clone(), cfg);
    *state.client_task.lock().unwrap() = Some(handle);
}
