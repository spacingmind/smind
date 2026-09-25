//! The six commands the bundled UI's connection picker calls (AC5),
//! exposed only to the loopback proxy origin via
//! `capabilities/proxy.json`. Every command validates its own input in
//! Rust (`Registry::add`/`remove`/`select` and `validate_base_url` do
//! the real work; this module is just the Tauri-facing surface plus
//! persistence and, for `connections_select`, restarting the
//! daemon-client watcher against the newly-selected connection).

use tauri::{AppHandle, State};

use smind_daemon_client::proxy::Connection;

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
pub fn connections_add(state: State<'_, DesktopState>, label: String, url: String) -> Result<Connection, String> {
    let mut registry = state.proxy.registry.lock().unwrap();
    let conn = registry.add(&label, &url)?;
    registry.save(&state.connections_path).map_err(|e| e.to_string())?;
    Ok(conn)
}

#[tauri::command]
pub fn connections_remove(state: State<'_, DesktopState>, id: String) -> Result<(), String> {
    let mut registry = state.proxy.registry.lock().unwrap();
    registry.remove(&id)?;
    registry.save(&state.connections_path).map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
pub fn connections_select(app: AppHandle, state: State<'_, DesktopState>, id: String) -> Result<Connection, String> {
    let conn = {
        let mut registry = state.proxy.registry.lock().unwrap();
        registry.select(&id)?;
        registry.save(&state.connections_path).map_err(|e| e.to_string())?;
        registry.current().clone()
    };
    let daemon_url = url::Url::parse(&conn.base_url).map_err(|e| e.to_string())?;
    client_watch::restart(&app, &state, daemon_url);
    Ok(conn)
}

#[tauri::command]
pub fn open_external(app: AppHandle, url: String) -> Result<(), String> {
    let parsed = url::Url::parse(&url).map_err(|e| format!("invalid URL {url:?}: {e}"))?;
    if parsed.scheme() != "http" && parsed.scheme() != "https" {
        return Err(format!("only http(s) URLs can be opened externally, got scheme {:?}", parsed.scheme()));
    }
    use tauri_plugin_opener::OpenerExt;
    app.opener().open_url(url, None::<&str>).map_err(|e| e.to_string())
}
