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

/// login_timeout bounds POST /api/login (bcrypt on the daemon is slow by design).
const LOGIN_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(5);

/// login exchanges `password` for a session token (ADR-0031 D8
/// `POST /api/login`). Every error is fixed text or a status/number: neither
/// the password nor the URL is echoed.
async fn login(base: &url::Url, password: &str) -> Result<String, String> {
    #[derive(serde::Deserialize)]
    #[serde(rename_all = "camelCase")]
    struct Reply {
        token: Option<String>,
        retry_after_seconds: Option<u64>,
    }
    let mut url = crate::probe::without_userinfo(base.clone());
    url.set_path("/api/login");
    let client = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(LOGIN_TIMEOUT)
        .build()
        .map_err(|_| "could not start the login request".to_string())?;
    let resp = client
        .post(url)
        .json(&serde_json::json!({ "password": password }))
        .send()
        .await
        .map_err(|e| format!("login failed: {}", crate::probe::describe_request_error(&e)))?;
    let status = resp.status();
    let header_wait = resp.headers().get(reqwest::header::RETRY_AFTER).and_then(|v| v.to_str().ok()).and_then(|v| v.trim().parse::<u64>().ok());
    let body = smind_daemon_client::proxy::bounded::read_json::<Reply>(resp, smind_daemon_client::proxy::bounded::MAX_BODY_BYTES).await;
    match status.as_u16() {
        200 => body.and_then(|r| r.token).filter(|t| !t.is_empty()).ok_or_else(|| "the daemon's login answer had no session token".to_string()),
        401 => Err("wrong password".to_string()),
        429 => {
            let wait = body.and_then(|r| r.retry_after_seconds).or(header_wait);
            Err(match wait {
                Some(n) => format!("too many attempts, retry in {n} s"),
                None => "too many attempts, retry later".to_string(),
            })
        }
        403 => Err("this daemon does not use a password".to_string()),
        code => Err(format!("login failed: the daemon answered HTTP {code}")),
    }
}

/// scrub overwrites the password's bytes before it is dropped (best effort:
/// the request body copies are out of reach).
fn scrub(password: Option<String>) {
    if let Some(p) = password {
        let mut bytes = p.into_bytes();
        bytes.fill(0);
    }
}

/// add_url_connection is `connections_add`: with a password it logs in first
/// and saves the connection only on success, keeping the session token in
/// memory (`ProxyState::set_session`). The password is never stored.
pub(crate) async fn add_url_connection(
    proxy: &smind_daemon_client::proxy::ProxyState,
    path: &std::path::Path,
    label: &str,
    url: &str,
    password: Option<String>,
) -> Result<Connection, String> {
    let password = password.filter(|p| !p.is_empty());
    let result = async {
        let session = match &password {
            Some(p) => {
                let base = smind_daemon_client::proxy::connections::validate_base_url(url)?;
                Some(login(&base, p).await?)
            }
            None => None,
        };
        let conn = {
            let mut registry = proxy.registry.lock().unwrap();
            let conn = registry.add(label, url)?;
            registry.save(path).map_err(|e| e.to_string())?;
            conn
        };
        if let Some(token) = session {
            proxy.set_session(&conn.id, token);
        }
        Ok(conn)
    }
    .await;
    scrub(password);
    result
}

#[tauri::command]
pub async fn connections_add(
    state: State<'_, DesktopState>,
    label: String,
    url: String,
    // ADR-0031 D7 (b): with a password Rust logs in and keeps the session
    // token in memory; the password itself is never kept.
    password: Option<String>,
) -> Result<Connection, String> {
    add_url_connection(&state.proxy, &state.connections_path, &label, &url, password).await
}

/// remove_connection deletes `id` from the registry and everything that
/// belongs to it: a held login session, an ssh tunnel (its child is
/// killed), a relay pairing's keys.
pub fn remove_connection(
    proxy: &smind_daemon_client::proxy::ProxyState,
    tunnels: &crate::ssh_tunnel::SshTunnels,
    path: &std::path::Path,
    id: &str,
) -> Result<(), String> {
    // The in-memory removal is what makes the connection gone; a failure to
    // persist it must not leave its session, tunnel or pairing keys behind, so
    // cleanup always runs and the first error is the one reported.
    let removed = {
        let mut registry = proxy.registry.lock().unwrap();
        registry.remove(id)?;
        registry.save(path).map_err(|e| e.to_string())
    };
    proxy.clear_session(id);
    crate::ssh_tunnel::forget_tunnel(proxy, tunnels, id);
    // Unpairing deletes the keys: a no-op for local/url ids, since
    // `pairing_store::delete` only ever removes a file that a relay
    // connection's `connections_add_relay` would have created.
    let unpaired = match path.parent() {
        Some(dir) => relay::pairing_store::delete(dir, id).map_err(|e| e.to_string()),
        None => Ok(()),
    };
    removed.and(unpaired)
}

#[tauri::command]
pub fn connections_remove(
    app: AppHandle,
    state: State<'_, DesktopState>,
    id: String,
) -> Result<(), String> {
    let was_current = state.proxy.registry.lock().unwrap().current().id == id;
    let result = remove_connection(&state.proxy, &state.ssh_tunnels, &state.connections_path, &id);
    // A removed connection's password goes with it (it only ever lived in memory).
    client_watch::forget_password(&id);
    // Removing the connection that is live falls back to the local one: stop
    // its relay session (and its device key in memory) now instead of leaving
    // it attached until the next switch.
    if was_current {
        let now_current = state.proxy.registry.lock().unwrap().current().clone();
        if now_current.id != id {
            client_watch::restart(&app, &state, &now_current);
        }
    }
    result
}

/// select_connection selects `id`; an `ssh` connection first gets its tunnel
/// (re)started if it is not running, and a failure (ssh's own stderr) leaves
/// the selection unchanged.
pub async fn select_connection(
    proxy: &smind_daemon_client::proxy::ProxyState,
    tunnels: &crate::ssh_tunnel::SshTunnels,
    path: &std::path::Path,
    id: &str,
) -> Result<Connection, String> {
    let existing = proxy.registry.lock().unwrap().list().iter().find(|c| c.id == id).cloned();
    if let Some(conn) = existing.as_ref().filter(|c| c.kind == smind_daemon_client::proxy::ConnectionKind::Ssh) {
        crate::ssh_tunnel::ensure_tunnel(proxy, tunnels, conn).await?;
        // Removed while the tunnel was coming up: do not leave an orphan
        // authenticated ssh child behind.
        if !proxy.registry.lock().unwrap().list().iter().any(|c| c.id == id) {
            crate::ssh_tunnel::forget_tunnel(proxy, tunnels, id);
            return Err("this connection was removed".to_string());
        }
    }
    let mut registry = proxy.registry.lock().unwrap();
    registry.select(id)?;
    registry.save(path).map_err(|e| e.to_string())?;
    Ok(registry.current().clone())
}

#[tauri::command]
pub async fn connections_select(
    app: AppHandle,
    state: State<'_, DesktopState>,
    id: String,
) -> Result<Connection, String> {
    let conn = select_connection(&state.proxy, &state.ssh_tunnels, &state.connections_path, &id).await?;
    client_watch::restart(&app, &state, &conn);
    Ok(conn)
}

/// The longest pairing link either relay command will parse. A real offer
/// (432 bytes of payload, ADR-0031 D6) is far below this; anything bigger is
/// a paste of the wrong thing, rejected before any decoding work.
const MAX_PAIRING_URL_BYTES: usize = 4096;

/// What the confirmation screen shows for a pasted relay link: every field
/// is non-secret. The admission secret and the daemon key bytes never leave
/// Rust -- `key_fingerprint` shows only the ends of the key, to compare with
/// what the daemon prints.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RelayPreview {
    /// The relay's `host:port`, as written in the link.
    pub relay_address: String,
    /// The relay host id the link admits under.
    pub relay_host_id: String,
    pub key_fingerprint: String,
    /// Whether the relay's TLS certificate is pinned by the link (false = CA
    /// mode: the certificate chain and host name are verified against the
    /// system roots, for a relay serving a real certificate).
    pub pinned: bool,
    pub kind: RelayPreviewKind,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub enum RelayPreviewKind {
    /// No saved connection uses this relay host.
    NewHost,
    /// This exact relay host, relay and key are already saved; adding
    /// refreshes the stored link.
    AlreadySaved,
    /// A saved connection has this relay host id but a different key or relay.
    ChangedConnection,
}

/// parse_pairing_link is the one place a pasted link is trimmed, size-limited
/// and validated, for both `connections_preview_relay` and
/// `connections_add_relay`. Its errors never echo the link (it is a
/// credential): only what was wrong with it.
fn parse_pairing_link(
    pairing_url: &str,
) -> Result<(relay::pairing::Offer, String), String> {
    let link = pairing_url.trim();
    if link.is_empty() {
        return Err("paste a relay pairing link".to_string());
    }
    if link.len() > MAX_PAIRING_URL_BYTES {
        return Err(format!(
            "that is not a relay pairing link: it is {} bytes long, the limit is {MAX_PAIRING_URL_BYTES}",
            link.len()
        ));
    }
    let offer = relay::pairing::Offer::parse_url(link).map_err(|e| e.to_string())?;
    if offer.workspace_id.trim().is_empty() {
        return Err("pairing: invalid offer: missing relay host id".to_string());
    }
    if offer.secret.is_empty() {
        return Err("pairing: invalid offer: missing admission secret".to_string());
    }
    // An empty fingerprint is CA mode (ADR-0031 P12c). A non-empty one must be
    // a SHA-256 -- 32 bytes, 64 hex digits -- and is NEVER read as CA mode when
    // it is not: that would silently drop a pin the link asked for.
    let fp = &offer.relay_fingerprint;
    if !fp.is_empty() && (fp.len() != 64 || !fp.bytes().all(|b| b.is_ascii_hexdigit())) {
        return Err(
            "pairing: invalid offer: the relay certificate fingerprint is not a SHA-256 (64 hex digits)"
                .to_string(),
        );
    }
    let native_addr = relay::client::derive_native_grpc_address(&offer.relay)?;
    Ok((offer, native_addr))
}

/// The key's first and last 8 characters (base64url), in groups of four --
/// Paseo's `formatDaemonKeyFingerprint`: key material itself, not a re-hash,
/// so two keys sharing both ends show the same value.
fn format_key_fingerprint(public_key: &[u8; 32]) -> String {
    use base64::engine::general_purpose::URL_SAFE_NO_PAD;
    use base64::Engine;
    let key = URL_SAFE_NO_PAD.encode(public_key);
    let group = |s: &str| {
        s.as_bytes()
            .chunks(4)
            .map(|c| std::str::from_utf8(c).unwrap_or_default())
            .collect::<Vec<_>>()
            .join(" ")
    };
    format!("{} \u{2026} {}", group(&key[..8]), group(&key[key.len() - 8..]))
}

fn relay_address_of(offer_relay: &str) -> String {
    match url::Url::parse(offer_relay) {
        Ok(u) => match (u.host_str(), u.port()) {
            (Some(h), Some(p)) => format!("{h}:{p}"),
            (Some(h), None) => h.to_string(),
            _ => offer_relay.to_string(),
        },
        Err(_) => offer_relay.to_string(),
    }
}

fn build_relay_preview(
    pairing_url: &str,
    connections: &[Connection],
    app_data_dir: Option<&std::path::Path>,
) -> Result<RelayPreview, String> {
    let (offer, native_addr) = parse_pairing_link(pairing_url)?;
    let id = smind_daemon_client::proxy::connections::connection_id_for_relay(&offer.workspace_id, &native_addr);
    let host_prefix = format!("relay://{}@", offer.workspace_id);
    let saved_host = connections.iter().any(|c| c.base_url.starts_with(&host_prefix));
    let kind = if !saved_host {
        RelayPreviewKind::NewHost
    } else {
        let same = connections.iter().any(|c| c.id == id)
            && app_data_dir
                .and_then(|dir| relay::pairing_store::load(dir, &id).ok().flatten())
                .is_some_and(|saved| {
                    saved.daemon_public_key == offer.public_key
                        && saved.relay_fingerprint == offer.relay_fingerprint
                });
        if same {
            RelayPreviewKind::AlreadySaved
        } else {
            RelayPreviewKind::ChangedConnection
        }
    };
    Ok(RelayPreview {
        relay_address: relay_address_of(&offer.relay),
        relay_host_id: offer.workspace_id.clone(),
        key_fingerprint: format_key_fingerprint(&offer.public_key),
        pinned: !offer.relay_fingerprint.is_empty(),
        kind,
    })
}

/// connections_preview_relay validates a pasted relay pairing link and
/// returns only what the confirmation screen may show. It saves nothing and
/// returns no secret or key bytes; errors never contain the link.
#[tauri::command]
pub fn connections_preview_relay(
    state: State<'_, DesktopState>,
    pairing_url: String,
) -> Result<RelayPreview, String> {
    let connections = state.proxy.registry.lock().unwrap().list().to_vec();
    build_relay_preview(&pairing_url, &connections, state.connections_path.parent())
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
    let (offer, native_addr) = parse_pairing_link(&pairing_url)?;
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
    // The pairing and the connection's device key are created together
    // (ADR-0031 P13a): the key is generated once, kept at 0600 next to the
    // pairing, deleted with it, and never returned by any command. If either
    // cannot be written the connection is taken back out of the registry, so
    // the user can simply retry instead of being left with a half-made entry.
    let persisted = relay::pairing_store::save(app_data_dir, &conn.id, &pairing)
        .map_err(|e| e.to_string())
        .and_then(|()| {
            relay::device::load_or_create_key(app_data_dir, &conn.id)
                .map(|_| ())
                .map_err(|e| e.to_string())
        });
    if let Err(e) = persisted {
        let _ = relay::pairing_store::delete(app_data_dir, &conn.id);
        let mut registry = state.proxy.registry.lock().unwrap();
        let _ = registry.remove(&conn.id);
        let _ = registry.save(&state.connections_path);
        return Err(e);
    }

    Ok(conn)
}

/// RelayStatus is what Settings → Connections shows for the selected relay
/// connection. Every field is non-secret: the state name, this device's own
/// id (which the user types into `smind relay devices approve`), and the
/// daemon's refusal code/message.
#[derive(serde::Serialize, Debug, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct RelayStatus {
    pub state: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub device_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub code: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub message: Option<String>,
    /// `passwordLocked`: seconds until the daemon will take another try.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub retry_after_secs: Option<u64>,
}

impl From<&relay::client::Status> for RelayStatus {
    fn from(s: &relay::client::Status) -> Self {
        use relay::client::Status;
        let (device_id, code, message) = match s {
            Status::AwaitingApproval { device_id } | Status::Revoked { device_id } => {
                (Some(device_id.clone()), None, None)
            }
            Status::Refused { code, message } => (None, Some(code.clone()), Some(message.clone())),
            _ => (None, None, None),
        };
        let retry_after_secs = match s {
            Status::PasswordLocked { retry_after_secs } => Some(*retry_after_secs),
            _ => None,
        };
        RelayStatus {
            state: s.state_name().to_string(),
            device_id,
            code,
            message,
            retry_after_secs,
        }
    }
}

/// connections_relay_status reports the live state of the selected relay
/// connection (`None` when the selected connection is not a relay one, or
/// has no live transport).
#[tauri::command]
pub fn connections_relay_status(state: State<'_, DesktopState>) -> Option<RelayStatus> {
    let handle = state.proxy.relay.lock().unwrap().clone()?;
    let status = handle.status().borrow().clone();
    Some(RelayStatus::from(&status))
}

/// connections_relay_set_password hands the relay connection `id` (which must be the selected one) the
/// daemon password (ADR-0031 P13c) and wakes it. The password is typed in the
/// webview and crosses this command once; from here it lives in Rust process
/// memory only (`PasswordCell`: zeroized, redacted Debug), is never written to
/// disk or logs, and is never returned to the webview — the status read-back
/// (`connections_relay_status`) carries state names only. A password the daemon
/// rejects is forgotten by the connection.
#[tauri::command]
pub fn connections_relay_set_password(
    state: State<'_, DesktopState>,
    id: String,
    password: String,
) -> Result<(), String> {
    use smind_daemon_client::relay::password::MAX_PASSWORD_BYTES;
    if password.is_empty() {
        return Err("enter the daemon's password".into());
    }
    if password.len() > MAX_PASSWORD_BYTES {
        return Err("that is too long to be the daemon's password".into());
    }
    // Bound to a connection: the status the user saw may be a moment stale after a
    // switch, and a password typed for one daemon must not go to another.
    if state.proxy.registry.lock().unwrap().current().id != id {
        return Err("that connection is no longer the selected one".into());
    }
    let handle = state
        .proxy
        .relay
        .lock()
        .unwrap()
        .clone()
        .ok_or_else(|| "no relay connection is selected".to_string())?;
    handle.set_password(password);
    Ok(())
}

/// Quits the app. The renderer's Ctrl+Q on Windows/Linux calls this: the
/// window is undecorated there, so the native menu's Ctrl+Q accelerator never
/// dispatches (no menu bar is attached to the window). Same effect as the
/// menu's and the tray's Quit items.
#[tauri::command]
pub fn app_quit(app: AppHandle) {
    app.exit(0);
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn device_key_never_returned_by_ipc() {
        // Every status the UI can see, serialised exactly as the command
        // returns it: only the documented non-secret fields appear, and none
        // is a key, seed or secret.
        use relay::client::Status;
        let all = [
            Status::Connecting,
            Status::Connected,
            Status::Reconnecting,
            Status::AwaitingApproval { device_id: "abcdefghijklmnopqrstuvwxyz".into() },
            Status::Revoked { device_id: "abcdefghijklmnopqrstuvwxyz".into() },
            Status::Refused { code: "too_many_devices".into(), message: "too many".into() },
        ];
        for st in &all {
            let v = serde_json::to_value(RelayStatus::from(st)).unwrap();
            for key in v.as_object().unwrap().keys() {
                assert!(
                    matches!(key.as_str(), "state" | "deviceId" | "code" | "message"),
                    "unexpected field {key} in the relay status"
                );
            }
        }
        // And the key store has no accessor that returns a seed or private key
        // to callers outside the crate: the type is neither Serialize nor Clone
        // (compile-time property); its Debug output is redacted.
        let key = relay::device::DeviceKey::generate();
        let dbg = format!("{key:?}");
        assert!(dbg.contains("redacted"), "{dbg}");
    }

    #[test]
    fn relay_status_names_are_stable() {
        use relay::client::Status;
        let names: Vec<String> = [
            Status::Connecting,
            Status::Connected,
            Status::Reconnecting,
            Status::AwaitingApproval { device_id: "x".into() },
            Status::Revoked { device_id: "x".into() },
            Status::Refused { code: "c".into(), message: "m".into() },
            Status::PasswordRequired,
            Status::IncorrectPassword,
            Status::PasswordLocked { retry_after_secs: 8 },
        ]
        .iter()
        .map(|s| RelayStatus::from(s).state)
        .collect();
        assert_eq!(
            names,
            [
                "connecting",
                "connected",
                "reconnecting",
                "awaitingApproval",
                "revoked",
                "refused",
                "passwordRequired",
                "incorrectPassword",
                "passwordLocked"
            ]
        );
    }

    #[test]
    fn password_lockout_reports_its_wait_and_nothing_secret() {
        use relay::client::Status;
        let locked = RelayStatus::from(&Status::PasswordLocked { retry_after_secs: 8 });
        assert_eq!(locked.retry_after_secs, Some(8));
        let json = serde_json::to_value(&locked).unwrap();
        assert_eq!(json["state"], "passwordLocked");
        assert_eq!(json["retryAfterSecs"], 8);
        // No state carries a password or any field that could.
        for st in [Status::PasswordRequired, Status::IncorrectPassword] {
            let v = serde_json::to_value(RelayStatus::from(&st)).unwrap();
            let keys: Vec<&str> = v.as_object().unwrap().keys().map(String::as_str).collect();
            assert_eq!(keys, ["state"]);
        }
    }
    use smind_daemon_client::proxy::connections::connection_id_for_relay;
    use smind_daemon_client::proxy::ConnectionKind;

    const FP: &str = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff";

    fn offer(key: u8, ws: &str, relay_url: &str, fp: &str, secret: &[u8]) -> relay::pairing::Offer {
        relay::pairing::Offer {
            daemon_id: "daemon-1".into(),
            public_key: [key; 32],
            relay: relay_url.into(),
            workspace_id: ws.into(),
            relay_fingerprint: fp.into(),
            secret: secret.to_vec(),
        }
    }

    fn link(o: &relay::pairing::Offer) -> String {
        o.url("").unwrap()
    }

    fn good() -> relay::pairing::Offer {
        offer(0x11, "ws-1", "https://relay.example.test:7401", FP, &[0xab; 32])
    }

    fn saved(o: &relay::pairing::Offer) -> Connection {
        let addr = relay::client::derive_native_grpc_address(&o.relay).unwrap();
        Connection {
            id: connection_id_for_relay(&o.workspace_id, &addr),
            kind: ConnectionKind::Relay,
            label: "x".into(),
            base_url: format!("relay://{}@{addr}", o.workspace_id),
        }
    }

    #[test]
    fn connections_preview_relay_returns_no_secret_fields() {
        let o = good();
        let p = build_relay_preview(&link(&o), &[], None).unwrap();
        assert_eq!(p.relay_address, "relay.example.test:7401");
        assert_eq!(p.relay_host_id, "ws-1");
        assert!(p.pinned);
        assert_eq!(p.kind, RelayPreviewKind::NewHost);
        let json = serde_json::to_string(&p).unwrap();
        let payload = link(&o).split_once("#offer=").unwrap().1.to_string();
        use base64::engine::general_purpose::URL_SAFE_NO_PAD;
        use base64::Engine;
        assert!(!json.contains(&payload));
        assert!(!json.contains(&URL_SAFE_NO_PAD.encode([0xab; 32])), "admission secret leaked");
        assert!(!json.contains(&URL_SAFE_NO_PAD.encode([0x11; 32])), "full key leaked");
        // Ends of the key only, in groups of four.
        assert_eq!(p.key_fingerprint, "ERER ERER \u{2026} RERE RERE");
    }

    #[test]
    fn connections_preview_relay_rejects_wrong_version() {
        use base64::engine::general_purpose::URL_SAFE_NO_PAD;
        use base64::Engine;
        let payload = URL_SAFE_NO_PAD.encode(br#"{"v":2,"id":"d","pk":"x","relay":"https://r:7401"}"#);
        let err = build_relay_preview(&format!("https://x/pair#offer={payload}"), &[], None).unwrap_err();
        assert!(err.contains("unsupported offer version 2"), "{err}");
    }

    #[test]
    fn connections_preview_relay_rejects_missing_pieces_without_echoing_link() {
        let l = link(&offer(0x11, "ws-1", "https://r.test:7401", "ab", &[1; 32]));
        assert!(build_relay_preview(&l, &[], None).unwrap_err().contains("SHA-256"));
        let l = link(&offer(0x11, "ws-1", "https://r.test:7401", FP, &[]));
        assert!(build_relay_preview(&l, &[], None).unwrap_err().contains("secret"));
        let l = link(&offer(0x11, "", "https://r.test:7401", FP, &[1; 32]));
        assert!(build_relay_preview(&l, &[], None).unwrap_err().contains("relay host id"));
        let junk = "https://x/pair#offer=notbase64!!";
        let err = build_relay_preview(junk, &[], None).unwrap_err();
        assert!(!err.contains("notbase64"), "{err}");
        assert!(build_relay_preview("  ", &[], None).is_err());
    }

    #[test]
    fn connections_preview_relay_accepts_ca_mode_offer_with_pinned_false() {
        let ca = offer(0x11, "ws-1", "https://r.test:7401", "", &[1; 32]);
        let p = build_relay_preview(&link(&ca), &[], None).unwrap();
        assert!(!p.pinned, "no fingerprint = CA mode");
        assert_eq!(p.relay_address, "r.test:7401");
        // The JSON contract is unchanged: `pinned` is a bool.
        assert_eq!(serde_json::to_value(&p).unwrap()["pinned"], false);
        let pinned = build_relay_preview(&link(&good()), &[], None).unwrap();
        assert!(pinned.pinned);
    }

    #[test]
    fn connections_preview_relay_rejects_malformed_fingerprint_never_ca_mode() {
        let valid = FP.to_string();
        for bad in [&valid[..63], &valid[..62], &format!("{valid}00"), &format!("{}zz", &valid[..62]), " ", "deadbeef"] {
            let l = link(&offer(0x11, "ws-1", "https://r.test:7401", bad, &[1; 32]));
            let err = build_relay_preview(&l, &[], None).unwrap_err();
            assert!(err.contains("not a SHA-256"), "{bad:?}: {err}");
        }
    }

    #[test]
    fn connections_add_relay_roundtrips_empty_fingerprint() {
        let dir = tempfile::tempdir().unwrap();
        let ca = offer(0x11, "ws-1", "https://r.test:7401", "", &[0xab; 32]);
        let (parsed, addr) = parse_pairing_link(&link(&ca)).unwrap();
        let conn = saved(&parsed);
        relay::pairing_store::save(dir.path(), &conn.id, &relay::pairing_store::RelayPairing::from_offer(&parsed, addr)).unwrap();
        let loaded = relay::pairing_store::load(dir.path(), &conn.id).unwrap().expect("saved");
        assert_eq!(loaded.relay_fingerprint, "", "CA mode survives the round trip");
        assert_eq!(loaded.workspace_id, "ws-1");
        // Re-pasting the same CA-mode link is AlreadySaved (empty == empty), and
        // switching a saved CA-mode connection to a pinned link is a change.
        let list = [conn];
        let same = build_relay_preview(&link(&ca), &list, Some(dir.path())).unwrap();
        assert_eq!(same.kind, RelayPreviewKind::AlreadySaved);
        let pinned = offer(0x11, "ws-1", "https://r.test:7401", FP, &[0xab; 32]);
        let changed = build_relay_preview(&link(&pinned), &list, Some(dir.path())).unwrap();
        assert_eq!(changed.kind, RelayPreviewKind::ChangedConnection);
    }

    #[test]
    fn connections_preview_relay_rejects_oversized_link() {
        let big = format!("https://x/pair#offer={}", "A".repeat(MAX_PAIRING_URL_BYTES));
        let err = build_relay_preview(&big, &[], None).unwrap_err();
        assert!(err.contains("limit"), "{err}");
        assert!(!err.contains("AAAA"));
    }

    #[test]
    fn connections_preview_relay_flags_changed_key_and_relay() {
        let dir = tempfile::tempdir().unwrap();
        let o = good();
        let conn = saved(&o);
        let addr = relay::client::derive_native_grpc_address(&o.relay).unwrap();
        relay::pairing_store::save(
            dir.path(),
            &conn.id,
            &relay::pairing_store::RelayPairing::from_offer(&o, addr),
        )
        .unwrap();
        let list = [conn];

        let same = build_relay_preview(&link(&o), &list, Some(dir.path())).unwrap();
        assert_eq!(same.kind, RelayPreviewKind::AlreadySaved);

        let new_key = offer(0x22, "ws-1", &o.relay, FP, &[0xab; 32]);
        let p = build_relay_preview(&link(&new_key), &list, Some(dir.path())).unwrap();
        assert_eq!(p.kind, RelayPreviewKind::ChangedConnection);

        let new_relay = offer(0x11, "ws-1", "https://other.example.test:7401", FP, &[0xab; 32]);
        let p = build_relay_preview(&link(&new_relay), &list, Some(dir.path())).unwrap();
        assert_eq!(p.kind, RelayPreviewKind::ChangedConnection);

        let other_host = offer(0x11, "ws-2", &o.relay, FP, &[0xab; 32]);
        let p = build_relay_preview(&link(&other_host), &list, Some(dir.path())).unwrap();
        assert_eq!(p.kind, RelayPreviewKind::NewHost);
    }

    // --- connections_add with a password (ADR-0031 D7 b / D8) ---

    use smind_daemon_client::proxy::{ProxyState, Registry};

    fn proxy() -> std::sync::Arc<ProxyState> {
        let local = url::Url::parse("http://127.0.0.1:4648").unwrap();
        ProxyState::new("s".into(), Registry::new(&local), Box::new(crate::assets::EmbeddedAssets))
    }

    fn reply(status: &str, extra: &str, body: &str) -> &'static str {
        Box::leak(
            format!("HTTP/1.1 {status}\r\nContent-Type: application/json\r\n{extra}Content-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len())
                .into_boxed_str(),
        )
    }

    #[tokio::test]
    async fn connections_add_wrong_password_saves_nothing() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        let proxy = proxy();
        let (url, seen) = crate::probe::test_support::stub(vec![reply("401 Unauthorized", "", r#"{"error":"invalid password"}"#)]);
        let err = add_url_connection(&proxy, &path, "Lab", &url, Some("hunter2hunter2".into())).await.unwrap_err();
        assert_eq!(err, "wrong password");
        assert!(!path.exists(), "nothing saved");
        assert_eq!(proxy.registry.lock().unwrap().list().len(), 1);
        let req = seen.lock().unwrap()[0].clone();
        assert!(req.starts_with("POST /api/login "), "{req}");
        assert!(req.to_ascii_lowercase().contains("content-type: application/json"), "{req}");
        assert!(req.contains(r#"{"password":"hunter2hunter2"}"#), "{req}");
    }

    #[tokio::test]
    async fn connections_add_login_errors_are_mapped_without_secrets() {
        let cases = [
            (reply("429 Too Many Requests", "Retry-After: 7\r\n", r#"{"error":"too many attempts","retryAfterSeconds":7}"#), "too many attempts, retry in 7 s"),
            (reply("429 Too Many Requests", "Retry-After: 9\r\n", "nope"), "too many attempts, retry in 9 s"),
            (reply("403 Forbidden", "", r#"{"error":"x"}"#), "this daemon does not use a password"),
            (reply("500 Internal Server Error", "", "{}"), "login failed: the daemon answered HTTP 500"),
        ];
        for (canned, want) in cases {
            let dir = tempfile::tempdir().unwrap();
            let (url, _) = crate::probe::test_support::stub(vec![canned]);
            let err = add_url_connection(&proxy(), &dir.path().join("c.json"), "", &url, Some("hunter2hunter2".into())).await.unwrap_err();
            assert_eq!(err, want);
            assert!(!err.contains("hunter2") && !err.contains("127.0.0.1"));
        }
    }

    #[test]
    fn remove_cleans_up_even_when_saving_the_registry_fails() {
        let dir = tempfile::tempdir().unwrap();
        let p = proxy();
        let conn = p.registry.lock().unwrap().add("Lab", "http://127.0.0.1:9").unwrap();
        p.set_session(&conn.id, "sess".into());
        // An unwritable registry path: its parent is a regular file.
        let blocker = dir.path().join("blocker");
        std::fs::write(&blocker, b"x").unwrap();
        let bad_path = blocker.join("connections.json");

        let tunnels = crate::ssh_tunnel::SshTunnels::default();
        let err = remove_connection(&p, &tunnels, &bad_path, &conn.id).unwrap_err();
        assert!(!err.is_empty());
        assert_eq!(p.registry.lock().unwrap().list().len(), 1, "removed in memory");
        assert_eq!(p.session(&conn.id), None, "session cleared");
        // The pairing keys live next to the registry file: they are deleted too.
        let ok_path = dir.path().join("connections.json");
        let c2 = p.registry.lock().unwrap().add("Lab2", "http://127.0.0.1:10").unwrap();
        let pairing2 = relay::pairing_store::pairing_path(dir.path(), &c2.id);
        std::fs::create_dir_all(pairing2.parent().unwrap()).unwrap();
        std::fs::write(&pairing2, b"keys").unwrap();
        std::fs::create_dir(&ok_path).unwrap(); // saving over a directory fails
        assert!(remove_connection(&p, &tunnels, &ok_path, &c2.id).is_err());
        assert!(!pairing2.exists(), "pairing keys deleted despite the save failure");
    }

    #[tokio::test]
    async fn oversized_login_answer_is_an_error_not_a_buffer() {
        let dir = tempfile::tempdir().unwrap();
        let filler = "x".repeat(200 * 1024);
        let body = format!(r#"{{"token":"sess","pad":"{filler}"}}"#);
        let canned: &'static str = Box::leak(format!("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len()).into_boxed_str());
        let (url, _) = crate::probe::test_support::stub(vec![canned]);
        let proxy = proxy();
        let err = add_url_connection(&proxy, &dir.path().join("c.json"), "", &url, Some("hunter2hunter2".into())).await.unwrap_err();
        assert_eq!(err, "the daemon's login answer had no session token");
        assert_eq!(proxy.registry.lock().unwrap().list().len(), 1, "nothing saved");
    }

    #[tokio::test]
    async fn connections_add_success_keeps_session_in_memory_only() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        let proxy = proxy();
        let (url, _) = crate::probe::test_support::stub(vec![reply("200 OK", "", r#"{"token":"sess-tok-123","expiresAt":"2030-01-01T00:00:00Z"}"#)]);
        let conn = add_url_connection(&proxy, &path, "Lab", &url, Some("hunter2hunter2".into())).await.unwrap();
        assert_eq!(proxy.session(&conn.id).as_deref(), Some("sess-tok-123"));
        let on_disk = std::fs::read_to_string(&path).unwrap();
        assert!(on_disk.contains(&conn.id));
        assert!(!on_disk.contains("hunter2") && !on_disk.contains("sess-tok-123"), "{on_disk}");
        proxy.clear_session(&conn.id);
        assert_eq!(proxy.session(&conn.id), None);
    }

    #[tokio::test]
    async fn connections_add_without_password_does_not_log_in() {
        let dir = tempfile::tempdir().unwrap();
        // No stub is listening: a login attempt would fail.
        let conn = add_url_connection(&proxy(), &dir.path().join("c.json"), "Lab", "http://127.0.0.1:9", Some(String::new())).await.unwrap();
        assert_eq!(conn.kind, ConnectionKind::Url);
    }
}
