//! Spawns (and, on a connection switch, respawns) the daemon-client
//! watcher: the same connection ADR-0012 already drives the tray's
//! pending count and OS notifications from, now started against
//! whichever connection is currently selected (AC7) rather than a
//! single fixed `SMIND_DAEMON_URL` -- including a `relay`-kind
//! connection, whose transport this module also starts/stops.

use std::path::Path;
use std::sync::Arc;

use tauri::AppHandle;
use url::Url;

use smind_daemon_client as dclient;
use smind_daemon_client::proxy::{Connection, ConnectionKind, ProxyState};
use smind_daemon_client::relay::client::RelayHandle;
use smind_daemon_client::{relay, ClientEvent, Config, WorkspaceCache};

use crate::notify;
use crate::state::DesktopState;
use crate::tray::Tray;

/// spawn starts the watcher against `cfg` (a plain dialable daemon,
/// `local`/`url`-kind) and returns its task handle.
pub fn spawn(
    app: &AppHandle,
    tray: Arc<Tray>,
    cache: WorkspaceCache,
    proxy_url: Url,
    cfg: Config,
) -> tauri::async_runtime::JoinHandle<()> {
    let handle = app.clone();
    tauri::async_runtime::spawn(async move {
        dclient::client::run(cfg, cache.clone(), move |event| {
            on_event(&handle, &tray, &cache, &proxy_url, event)
        })
        .await;
    })
}

/// spawn_over_relay is `spawn`'s counterpart for a `relay`-kind
/// connection: the watcher rides the same shared tunnel `relay_handle`
/// gives the proxy's `/ws` bridge (AC5/AC7), rather than dialing its own
/// WS connection.
pub fn spawn_over_relay(
    app: &AppHandle,
    tray: Arc<Tray>,
    cache: WorkspaceCache,
    proxy_url: Url,
    relay_handle: RelayHandle,
) -> tauri::async_runtime::JoinHandle<()> {
    let handle = app.clone();
    tauri::async_runtime::spawn(async move {
        dclient::client::run_over_relay(relay_handle, cache.clone(), move |event| {
            on_event(&handle, &tray, &cache, &proxy_url, event)
        })
        .await;
    })
}

fn on_event(
    app: &AppHandle,
    tray: &Arc<Tray>,
    cache: &WorkspaceCache,
    proxy_url: &Url,
    event: ClientEvent,
) {
    match event {
        ClientEvent::Notification(n) => {
            tray.on_permission_pending(n.task_id, &n.request_id);
            let ctx = notify::ClickContext {
                app: app.clone(),
                cache: cache.clone(),
                proxy_url: proxy_url.clone(),
            };
            notify::show(ctx, n.title, n.body, n.task_id);
        }
        ClientEvent::RunRunning { task_id } => tray.on_run_running(task_id),
        ClientEvent::Reconnected => tray.on_reconnected(),
    }
}

/// activate starts fresh watcher (+ relay transport, if `conn` is
/// `relay`-kind) tasks for `conn`, installing the relay handle into
/// `proxy` (AC5's `/ws` bridge reads it from there) if any. Returns the
/// new task handles for the caller to store; it does not abort anything
/// -- see `restart`, which does that before calling this.
fn activate(
    app: &AppHandle,
    proxy: &Arc<ProxyState>,
    tray: Arc<Tray>,
    cache: WorkspaceCache,
    proxy_url: Url,
    conn: &Connection,
    connections_path: &Path,
) -> (
    Option<tauri::async_runtime::JoinHandle<()>>,
    Option<tokio::task::JoinHandle<()>>,
) {
    if conn.kind != ConnectionKind::Relay {
        proxy.set_relay(None);
        let daemon_url: Url = conn
            .base_url
            .parse()
            .expect("smind desktop: saved connection URL is well-formed");
        let client_task = spawn(app, tray, cache, proxy_url, Config { daemon_url });
        return (Some(client_task), None);
    }

    let Some(app_data_dir) = connections_path.parent() else {
        eprintln!("smind desktop: connections path {connections_path:?} has no parent directory");
        proxy.set_relay(None);
        return (None, None);
    };
    match relay::pairing_store::load(app_data_dir, &conn.id) {
        Ok(Some(pairing)) => {
            let (relay_handle, relay_task) = relay::client::spawn(pairing);
            proxy.set_relay(Some(relay_handle.clone()));
            let client_task = spawn_over_relay(app, tray, cache, proxy_url, relay_handle);
            (Some(client_task), Some(relay_task))
        }
        Ok(None) => {
            eprintln!(
                "smind desktop: relay connection {:?} has no pairing on disk; not connecting",
                conn.id
            );
            proxy.set_relay(None);
            (None, None)
        }
        Err(e) => {
            eprintln!(
                "smind desktop: relay connection {:?}: load pairing: {e}",
                conn.id
            );
            proxy.set_relay(None);
            (None, None)
        }
    }
}

/// restart aborts the currently-running watcher and relay transport (if
/// any) and starts fresh ones for `state`'s newly-selected connection --
/// called by the `connections_select` command. Reusing the same
/// `state.cache` keeps any already-resolved task->workspace lookups
/// valid across the switch (they're a plain taskId->workspaceId map, not
/// tied to any one connection).
pub fn restart(app: &AppHandle, state: &DesktopState, conn: &Connection) {
    if let Some(old) = state.client_task.lock().unwrap().take() {
        old.abort();
    }
    if let Some(old) = state.relay_task.lock().unwrap().take() {
        old.abort();
    }
    let (client_task, relay_task) = activate(
        app,
        &state.proxy,
        state.tray.clone(),
        state.cache.clone(),
        state.proxy_url.clone(),
        conn,
        &state.connections_path,
    );
    *state.client_task.lock().unwrap() = client_task;
    *state.relay_task.lock().unwrap() = relay_task;
}

/// spawn_initial is `restart`'s startup-time counterpart: there is
/// nothing to abort yet, and the caller (still assembling
/// `DesktopState`) wants the task handles back directly rather than
/// stored through it.
pub fn spawn_initial(
    app: &AppHandle,
    proxy: &Arc<ProxyState>,
    tray: Arc<Tray>,
    cache: WorkspaceCache,
    proxy_url: Url,
    conn: &Connection,
    connections_path: &Path,
) -> (
    Option<tauri::async_runtime::JoinHandle<()>>,
    Option<tokio::task::JoinHandle<()>>,
) {
    activate(app, proxy, tray, cache, proxy_url, conn, connections_path)
}
