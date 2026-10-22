//! Spawns (and, on a connection switch, respawns) the daemon-client
//! watcher: the same connection ADR-0012 already drives the tray's
//! pending count and OS notifications from, now started against
//! whichever connection is currently selected (AC7) rather than a
//! single fixed `SMIND_DAEMON_URL` -- including a `relay`-kind
//! connection, whose transport this module also starts/stops.

use std::collections::HashMap;
use std::path::Path;
use std::sync::{Arc, Mutex, OnceLock};

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
    auth: dclient::client::WatcherAuth,
) -> tauri::async_runtime::JoinHandle<()> {
    let handle = app.clone();
    tauri::async_runtime::spawn(async move {
        dclient::client::run_with_auth(cfg, auth, cache.clone(), move |event| {
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
        ClientEvent::RunEnded { task_id } => tray.on_run_ended(task_id),
        ClientEvent::PermissionResolved { request_id } => tray.on_permission_resolved(&request_id),
        ClientEvent::Reconnected => tray.on_reconnected(),
    }
}

/// The in-memory password cells, keyed by (connection id, daemon public key).
type PasswordCells = Mutex<HashMap<(String, [u8; 32]), relay::password::PasswordCell>>;

/// The daemon passwords entered this run, in memory only, never persisted. Keyed by
/// connection id AND the daemon's public key: re-adding a link with the same relay
/// host id but a different daemon key replaces the connection, and a password the
/// user typed for the old daemon must never be sent to the new one.
fn password_cells() -> &'static PasswordCells {
    static CELLS: OnceLock<PasswordCells> = OnceLock::new();
    CELLS.get_or_init(|| Mutex::new(HashMap::new()))
}

fn password_cell(conn_id: &str, daemon_key: &[u8; 32]) -> relay::password::PasswordCell {
    password_cells()
        .lock()
        .unwrap()
        .entry((conn_id.to_string(), *daemon_key))
        .or_default()
        .clone()
}

/// forget_password drops every in-memory password of a removed connection.
pub fn forget_password(conn_id: &str) {
    password_cells().lock().unwrap().retain(|(id, _), cell| {
        if id == conn_id {
            cell.clear();
            false
        } else {
            true
        }
    });
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
        // An ssh connection is watched through its tunnel's local end; with
        // no tunnel yet there is nothing to watch (it is restarted when the
        // tunnel comes up).
        let Some(base_url) = proxy.upstream_base(conn) else {
            return (None, None);
        };
        let daemon_url: Url = base_url
            .parse()
            .expect("smind desktop: saved connection URL is well-formed");
        // A password-protected daemon refuses /api/token: `local` falls back
        // to the daemon token file inside the client, a url connection uses
        // its held session.
        let auth = match conn.kind {
            ConnectionKind::Url => {
                let (proxy, id) = (proxy.clone(), conn.id.clone());
                dclient::client::WatcherAuth::with_session(move || proxy.session(&id))
            }
            _ => dclient::client::WatcherAuth::default(),
        };
        let client_task = spawn(app, tray, cache, proxy_url, Config { daemon_url }, auth);
        return (Some(client_task), None);
    }

    let Some(app_data_dir) = connections_path.parent() else {
        eprintln!("smind desktop: connections path {connections_path:?} has no parent directory");
        proxy.set_relay(None);
        return (None, None);
    };
    match relay::pairing_store::load(app_data_dir, &conn.id) {
        Ok(Some(pairing)) => {
            // The connection's persistent device key (created at pairing; made
            // here for a pairing saved before device identity existed). If it
            // cannot be read the client does not connect and says why
            // (Settings → Connections shows it): quietly speaking the
            // single-device protocol would only be refused by a current daemon.
            let options = match relay::device::load_or_create_key(app_data_dir, &conn.id) {
                Ok(key) => {
                    let mut options = relay::client::RelayOptions::identity(Arc::new(key));
                    // The daemon password the user entered this run survives the
                    // restart a connection switch or a daemon bounce causes.
                    options.password = password_cell(&conn.id, &pairing.daemon_public_key);
                    options
                }
                Err(e) => {
                    eprintln!(
                        "smind desktop: relay connection {:?}: device key: {e}",
                        conn.id
                    );
                    relay::client::RelayOptions::key_unavailable(format!(
                        "the device key cannot be read ({e}); remove this connection and add the link again"
                    ))
                }
            };
            let (relay_handle, relay_task) = relay::client::spawn(pairing, options);
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

#[cfg(test)]
mod password_tests {
    use super::*;

    #[test]
    fn a_new_daemon_key_under_the_same_connection_id_gets_no_password() {
        let (old_key, new_key) = ([1u8; 32], [2u8; 32]);
        let old = password_cell("relay:test-host@relay.example:7400", &old_key);
        old.set("typed-for-the-old-daemon".into());
        let same = password_cell("relay:test-host@relay.example:7400", &old_key);
        assert!(same.is_set(), "the same daemon keeps its password across restarts of the loop");
        let other = password_cell("relay:test-host@relay.example:7400", &new_key);
        assert!(!other.is_set(), "a different daemon key must start with no password");
        forget_password("relay:test-host@relay.example:7400");
        assert!(!password_cell("relay:test-host@relay.example:7400", &old_key).is_set(), "removal forgets every key's password");
        assert!(!old.is_set());
    }
}
