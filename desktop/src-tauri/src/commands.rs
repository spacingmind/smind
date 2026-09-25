//! The six commands the bundled UI's connection picker calls (AC5),
//! exposed only to the loopback proxy origin via
//! `capabilities/proxy.json`. Every command validates its own input in
//! Rust (`Registry::add`/`remove`/`select` and `validate_base_url` do
//! the real work; this module is just the Tauri-facing surface plus
//! persistence and, for `connections_select`, restarting the
//! daemon-client watcher against the newly-selected connection).

use tauri::{AppHandle, State};

use smind_daemon_client::proxy::Connection;
use smind_daemon_client::relay;

use crate::client_watch;
use crate::state::DesktopState;

#[tauri::command]
pub fn connections_list(state: State<'_, DesktopState>) -> Vec<Connection> {
    state.proxy.registry.lock().unwrap().list().to_vec()
}

#[tauri::command]
pub fn connections_get_current(state: State<'_, DesktopState>) -> Connection {
    state.proxy.registry.lock().unwrap().current().clone()
}

#[tauri::command]
pub fn connections_add(
    state: State<'_, DesktopState>,
    label: String,
    url: String,
) -> Result<Connection, String> {
    let mut registry = state.proxy.registry.lock().unwrap();
    let conn = registry.add(&label, &url)?;
    registry
        .save(&state.connections_path)
        .map_err(|e| e.to_string())?;
    Ok(conn)
}

#[tauri::command]
pub fn connections_remove(state: State<'_, DesktopState>, id: String) -> Result<(), String> {
    let mut registry = state.proxy.registry.lock().unwrap();
    registry.remove(&id)?;
    registry
        .save(&state.connections_path)
        .map_err(|e| e.to_string())?;
    drop(registry);
    // Unpairing deletes the keys: a no-op for local/url ids, since
    // `pairing_store::delete` only ever removes a file that a relay
    // connection's `connections_add_relay` would have created.
    if let Some(dir) = state.connections_path.parent() {
        relay::pairing_store::delete(dir, &id).map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[tauri::command]
pub fn connections_select(
    app: AppHandle,
    state: State<'_, DesktopState>,
    id: String,
) -> Result<Connection, String> {
    let conn = {
        let mut registry = state.proxy.registry.lock().unwrap();
        registry.select(&id)?;
        registry
            .save(&state.connections_path)
            .map_err(|e| e.to_string())?;
        registry.current().clone()
    };
    client_watch::restart(&app, &state, &conn);
    Ok(conn)
}

/// connections_add_relay parses and validates a pairing URL (the same
/// format `mobile/src/relay/*` accepts) entirely in Rust, persists its
/// pairing material (admission secret, daemon public key, relay address/
/// fingerprint) under the app data dir at `0600` (never in
/// `connections.json`), and returns only the non-secret display
/// `Connection` -- no command returns the secret or any key material.
#[tauri::command]
pub fn connections_add_relay(
    state: State<'_, DesktopState>,
    label: String,
    pairing_url: String,
) -> Result<Connection, String> {
    let offer = relay::pairing::Offer::parse_url(&pairing_url).map_err(|e| e.to_string())?;
    let native_addr = relay::client::derive_native_grpc_address(&offer.relay)?;
    let pairing = relay::pairing_store::RelayPairing::from_offer(&offer, native_addr.clone());

    let conn = {
        let mut registry = state.proxy.registry.lock().unwrap();
        let conn = registry.add_relay(&label, &offer.workspace_id, &native_addr);
        registry
            .save(&state.connections_path)
            .map_err(|e| e.to_string())?;
        conn
    };

    let app_data_dir = state
        .connections_path
        .parent()
        .ok_or("smind desktop: connections path has no parent directory")?;
    relay::pairing_store::save(app_data_dir, &conn.id, &pairing).map_err(|e| e.to_string())?;

    Ok(conn)
}

#[tauri::command]
pub fn open_external(app: AppHandle, url: String) -> Result<(), String> {
    let parsed = url::Url::parse(&url).map_err(|e| format!("invalid URL {url:?}: {e}"))?;
    if parsed.scheme() != "http" && parsed.scheme() != "https" {
        return Err(format!(
            "only http(s) URLs can be opened externally, got scheme {:?}",
            parsed.scheme()
        ));
    }
    use tauri_plugin_opener::OpenerExt;
    app.opener()
        .open_url(url, None::<&str>)
        .map_err(|e| e.to_string())
}
