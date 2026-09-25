//! The connection list (AC4): a built-in, non-removable `local` entry
//! plus any number of arbitrary-URL entries, persisted as JSON in the
//! app data dir and remembering the last-selected entry. Pure logic, no
//! Tauri types, so it's unit-testable (including the persistence round
//! trip) without a webview.

use std::fs;
use std::path::Path;

use serde::{Deserialize, Serialize};
use url::Url;

/// The built-in local entry's fixed id. Never removable (AC4).
pub const LOCAL_ID: &str = "local";

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Connection {
    pub id: String,
    pub kind: ConnectionKind,
    pub label: String,
    /// An `http(s)://host[:port]` base URL with no path/query -- see
    /// `validate_base_url`.
    #[serde(rename = "baseUrl")]
    pub base_url: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ConnectionKind {
    Local,
    Url,
}

/// validate_base_url accepts only an absolute `http(s)://host[:port]`
/// URL: no `file:`/`javascript:`/other scheme, and a host must be
/// present (rejects garbage that still happens to parse, like a bare
/// path). A trailing path/query/fragment is stripped rather than
/// rejected, so pasting a full daemon URL (e.g. copied from a browser
/// tab) still works.
pub fn validate_base_url(raw: &str) -> Result<Url, String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err("URL is required".to_string());
    }
    let mut url = Url::parse(trimmed).map_err(|e| format!("invalid URL {trimmed:?}: {e}"))?;
    match url.scheme() {
        "http" | "https" => {}
        s => return Err(format!("invalid URL {trimmed:?}: scheme must be http(s), got {s}")),
    }
    if url.host_str().is_none() {
        return Err(format!("invalid URL {trimmed:?}: missing host"));
    }
    url.set_path("");
    url.set_query(None);
    url.set_fragment(None);
    Ok(url)
}

fn connection_id_for_url(url: &Url) -> String {
    format!("url:{}", url.as_str().trim_end_matches('/'))
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct StoredRegistry {
    connections: Vec<Connection>,
    #[serde(rename = "selectedId")]
    selected_id: String,
}

/// Registry holds the connection list, in-memory, plus which one is
/// currently selected. The built-in `local` entry always exists at
/// index 0 and can never be removed or have its id reused by a `url`
/// entry.
#[derive(Debug, Clone)]
pub struct Registry {
    connections: Vec<Connection>,
    selected_id: String,
}

impl Registry {
    /// new builds a fresh registry with only the built-in local entry
    /// selected, given the resolved local daemon URL (the
    /// `SMIND_DAEMON_URL` override, or the default).
    pub fn new(local_base_url: &Url) -> Self {
        Self {
            connections: vec![local_connection(local_base_url)],
            selected_id: LOCAL_ID.to_string(),
        }
    }

    pub fn list(&self) -> &[Connection] {
        &self.connections
    }

    pub fn current(&self) -> &Connection {
        self.connections
            .iter()
            .find(|c| c.id == self.selected_id)
            // selected_id is only ever set to an id that exists in
            // connections (see select/remove), and the local entry is
            // never removed, so this always finds something.
            .expect("smind desktop: selected connection missing from registry")
    }

    pub fn selected_id(&self) -> &str {
        &self.selected_id
    }

    /// add validates `raw_url`, then inserts or (if the same base URL is
    /// already saved) updates the label of an existing `url` entry.
    /// Returns the resulting connection.
    pub fn add(&mut self, label: &str, raw_url: &str) -> Result<Connection, String> {
        let url = validate_base_url(raw_url)?;
        let id = connection_id_for_url(&url);
        let label = if label.trim().is_empty() { url.as_str().trim_end_matches('/').to_string() } else { label.trim().to_string() };
        let conn = Connection { id: id.clone(), kind: ConnectionKind::Url, label, base_url: url.as_str().trim_end_matches('/').to_string() };
        if let Some(existing) = self.connections.iter_mut().find(|c| c.id == id) {
            *existing = conn.clone();
        } else {
            self.connections.push(conn.clone());
        }
        Ok(conn)
    }

    /// remove deletes a `url` entry by id. Errors for the local entry or
    /// an unknown id. Removing the currently-selected entry falls back
    /// to selecting `local`.
    pub fn remove(&mut self, id: &str) -> Result<(), String> {
        if id == LOCAL_ID {
            return Err("the local connection can't be removed".to_string());
        }
        let before = self.connections.len();
        self.connections.retain(|c| c.id != id);
        if self.connections.len() == before {
            return Err(format!("no connection with id {id:?}"));
        }
        if self.selected_id == id {
            self.selected_id = LOCAL_ID.to_string();
        }
        Ok(())
    }

    /// select switches the current connection. Errors for an unknown id.
    pub fn select(&mut self, id: &str) -> Result<(), String> {
        if !self.connections.iter().any(|c| c.id == id) {
            return Err(format!("no connection with id {id:?}"));
        }
        self.selected_id = id.to_string();
        Ok(())
    }

    /// load reads the registry from `path`, falling back to a fresh
    /// registry (built-in local only) if the file is missing, unreadable,
    /// or malformed -- a corrupt connections file must never prevent the
    /// app from starting. `local_base_url` is always used for the local
    /// entry's base URL, even if the file has a stale one (the env var
    /// or default may have changed since it was last saved).
    pub fn load(path: &Path, local_base_url: &Url) -> Self {
        let Ok(data) = fs::read_to_string(path) else {
            return Self::new(local_base_url);
        };
        let Ok(stored) = serde_json::from_str::<StoredRegistry>(&data) else {
            return Self::new(local_base_url);
        };
        let mut connections: Vec<Connection> =
            stored.connections.into_iter().filter(|c| c.id != LOCAL_ID).collect();
        connections.insert(0, local_connection(local_base_url));
        let selected_id = if connections.iter().any(|c| c.id == stored.selected_id) {
            stored.selected_id
        } else {
            LOCAL_ID.to_string()
        };
        Self { connections, selected_id }
    }

    /// save writes the registry to `path` as JSON, creating parent
    /// directories as needed.
    pub fn save(&self, path: &Path) -> std::io::Result<()> {
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)?;
        }
        let stored = StoredRegistry { connections: self.connections.clone(), selected_id: self.selected_id.clone() };
        let data = serde_json::to_string_pretty(&stored).expect("smind desktop: registry serializes");
        fs::write(path, data)
    }
}

fn local_connection(local_base_url: &Url) -> Connection {
    Connection {
        id: LOCAL_ID.to_string(),
        kind: ConnectionKind::Local,
        label: "Local".to_string(),
        base_url: local_base_url.as_str().trim_end_matches('/').to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn local_url() -> Url {
        Url::parse("http://127.0.0.1:4648").unwrap()
    }

    #[test]
    fn new_registry_has_only_local_selected() {
        let reg = Registry::new(&local_url());
        assert_eq!(reg.list().len(), 1);
        assert_eq!(reg.current().id, LOCAL_ID);
        assert_eq!(reg.current().base_url, "http://127.0.0.1:4648");
    }

    #[test]
    fn validate_base_url_accepts_http_https() {
        assert!(validate_base_url("http://example.com:9000").is_ok());
        assert!(validate_base_url("https://example.com").is_ok());
    }

    #[test]
    fn validate_base_url_strips_path_query_fragment() {
        let url = validate_base_url("http://example.com:9000/foo?x=y#z").unwrap();
        assert_eq!(url.as_str(), "http://example.com:9000/");
    }

    #[test]
    fn validate_base_url_rejects_bad_schemes() {
        assert!(validate_base_url("file:///etc/passwd").is_err());
        assert!(validate_base_url("javascript:alert(1)").is_err());
        assert!(validate_base_url("ftp://example.com").is_err());
    }

    #[test]
    fn validate_base_url_rejects_garbage_and_empty() {
        assert!(validate_base_url("").is_err());
        assert!(validate_base_url("not a url").is_err());
        assert!(validate_base_url("   ").is_err());
    }

    #[test]
    fn add_then_select_then_current() {
        let mut reg = Registry::new(&local_url());
        let conn = reg.add("Tunnel", "http://example.com:9000").unwrap();
        assert_eq!(reg.list().len(), 2);
        reg.select(&conn.id).unwrap();
        assert_eq!(reg.current().id, conn.id);
        assert_eq!(reg.current().label, "Tunnel");
    }

    #[test]
    fn add_rejects_invalid_url() {
        let mut reg = Registry::new(&local_url());
        assert!(reg.add("bad", "not a url").is_err());
        assert_eq!(reg.list().len(), 1);
    }

    #[test]
    fn add_same_url_twice_updates_in_place() {
        let mut reg = Registry::new(&local_url());
        reg.add("First", "http://example.com:9000").unwrap();
        reg.add("Second", "http://example.com:9000/").unwrap();
        assert_eq!(reg.list().len(), 2);
        assert_eq!(reg.list()[1].label, "Second");
    }

    #[test]
    fn cannot_remove_local() {
        let mut reg = Registry::new(&local_url());
        assert!(reg.remove(LOCAL_ID).is_err());
        assert_eq!(reg.list().len(), 1);
    }

    #[test]
    fn remove_unknown_errors() {
        let mut reg = Registry::new(&local_url());
        assert!(reg.remove("url:http://nope").is_err());
    }

    #[test]
    fn removing_selected_falls_back_to_local() {
        let mut reg = Registry::new(&local_url());
        let conn = reg.add("Tunnel", "http://example.com:9000").unwrap();
        reg.select(&conn.id).unwrap();
        reg.remove(&conn.id).unwrap();
        assert_eq!(reg.current().id, LOCAL_ID);
    }

    #[test]
    fn select_unknown_errors() {
        let mut reg = Registry::new(&local_url());
        assert!(reg.select("nope").is_err());
        assert_eq!(reg.current().id, LOCAL_ID);
    }

    #[test]
    fn persistence_round_trip() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");

        let mut reg = Registry::new(&local_url());
        let conn = reg.add("Tunnel", "http://example.com:9000").unwrap();
        reg.select(&conn.id).unwrap();
        reg.save(&path).unwrap();

        let loaded = Registry::load(&path, &local_url());
        assert_eq!(loaded.list().len(), 2);
        assert_eq!(loaded.current().id, conn.id);
        assert_eq!(loaded.current().label, "Tunnel");
    }

    #[test]
    fn load_missing_file_falls_back_to_fresh() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("does-not-exist.json");
        let reg = Registry::load(&path, &local_url());
        assert_eq!(reg.list().len(), 1);
        assert_eq!(reg.current().id, LOCAL_ID);
    }

    #[test]
    fn load_malformed_file_falls_back_to_fresh() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        fs::write(&path, "not json").unwrap();
        let reg = Registry::load(&path, &local_url());
        assert_eq!(reg.list().len(), 1);
    }

    #[test]
    fn load_ignores_a_stale_local_entry_and_uses_current_url() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        let stale = StoredRegistry {
            connections: vec![Connection {
                id: LOCAL_ID.to_string(),
                kind: ConnectionKind::Local,
                label: "Local".to_string(),
                base_url: "http://127.0.0.1:9999".to_string(),
            }],
            selected_id: LOCAL_ID.to_string(),
        };
        fs::write(&path, serde_json::to_string(&stale).unwrap()).unwrap();

        let loaded = Registry::load(&path, &local_url());
        assert_eq!(loaded.list().len(), 1);
        assert_eq!(loaded.current().base_url, "http://127.0.0.1:4648");
    }

    #[test]
    fn load_falls_back_to_local_if_selected_entry_is_gone() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        let stale = StoredRegistry { connections: vec![], selected_id: "url:http://gone".to_string() };
        fs::write(&path, serde_json::to_string(&stale).unwrap()).unwrap();

        let loaded = Registry::load(&path, &local_url());
        assert_eq!(loaded.current().id, LOCAL_ID);
    }
}
