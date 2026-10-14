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
    /// Whether the relay's TLS certificate is pinned by the link (false =
    /// CA-verified). The desktop transport only dials a pinned relay today,
    /// so a link without a fingerprint is rejected rather than previewed.
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
    if offer.relay_fingerprint.trim().is_empty() {
        return Err(
            "pairing: invalid offer: missing relay certificate fingerprint (the desktop app connects only to a relay it can pin)"
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
    relay::pairing_store::save(app_data_dir, &conn.id, &pairing).map_err(|e| e.to_string())?;

    Ok(conn)
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
    use smind_daemon_client::proxy::connections::connection_id_for_relay;
    use smind_daemon_client::proxy::ConnectionKind;

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
        offer(0x11, "ws-1", "https://relay.example.test:7401", "deadbeef", &[0xab; 32])
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
        assert_eq!(p.key_fingerprint, "ERER ERER \u{2026} ERER ERER");
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
        let l = link(&offer(0x11, "ws-1", "https://r.test:7401", "", &[1; 32]));
        let err = build_relay_preview(&l, &[], None).unwrap_err();
        assert!(err.contains("fingerprint"), "{err}");
        let l = link(&offer(0x11, "ws-1", "https://r.test:7401", "ab", &[]));
        assert!(build_relay_preview(&l, &[], None).unwrap_err().contains("secret"));
        let l = link(&offer(0x11, "", "https://r.test:7401", "ab", &[1; 32]));
        assert!(build_relay_preview(&l, &[], None).unwrap_err().contains("relay host id"));
        let junk = "https://x/pair#offer=notbase64!!";
        let err = build_relay_preview(junk, &[], None).unwrap_err();
        assert!(!err.contains("notbase64"), "{err}");
        assert!(build_relay_preview("  ", &[], None).is_err());
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

        let new_key = offer(0x22, "ws-1", &o.relay, "deadbeef", &[0xab; 32]);
        let p = build_relay_preview(&link(&new_key), &list, Some(dir.path())).unwrap();
        assert_eq!(p.kind, RelayPreviewKind::ChangedConnection);

        let new_relay = offer(0x11, "ws-1", "https://other.example.test:7401", "deadbeef", &[0xab; 32]);
        let p = build_relay_preview(&link(&new_relay), &list, Some(dir.path())).unwrap();
        assert_eq!(p.kind, RelayPreviewKind::ChangedConnection);

        let other_host = offer(0x11, "ws-2", &o.relay, "deadbeef", &[0xab; 32]);
        let p = build_relay_preview(&link(&other_host), &list, Some(dir.path())).unwrap();
        assert_eq!(p.kind, RelayPreviewKind::NewHost);
    }
}
