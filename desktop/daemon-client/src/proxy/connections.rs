//! The connection list (AC4): a built-in, non-removable `local` entry
//! plus any number of arbitrary-URL entries, persisted as JSON in the
//! app data dir and remembering the last-selected entry. Pure logic, no
//! Tauri types, so it's unit-testable (including the persistence round
//! trip) without a webview.

use std::collections::HashMap;
use std::fs;
use std::path::Path;

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
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
    Relay,
    /// ADR-0031 D7 (b2): a daemon reached through `ssh -N -L`; `base_url` is
    /// the `ssh://` URI (`SshTarget::to_uri`), the tunnel's local end lives
    /// in `ProxyState`.
    Ssh,
}

/// SshTarget is what "Connect over SSH" asks for: where to ssh, and the
/// daemon's (loopback) port on that host. Mirrors Paseo's remote-SSH host
/// (`protocol/src/ssh-transport.ts`: validateSshHost/validatePort, the
/// `ssh://[user@]host[:sshPort]?daemonPort=N` shape).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SshTarget {
    /// `host` or `user@host`.
    pub host: String,
    pub ssh_port: Option<u16>,
    pub daemon_port: u16,
}

fn validate_port(port: u16, label: &str) -> Result<u16, String> {
    if port == 0 {
        return Err(format!("{label} must be between 1 and 65535"));
    }
    Ok(port)
}

impl SshTarget {
    /// validate trims the host and rejects what ssh could misread as an
    /// option or split into several arguments: empty, whitespace or control
    /// characters, a leading `-`.
    pub fn validate(&self) -> Result<SshTarget, String> {
        let host = self.host.trim();
        if host.is_empty() {
            return Err("SSH host is required".to_string());
        }
        // ssh_config aliases and DNS names are ASCII; refusing the rest also
        // keeps percent-encoding out of argv and the stored URI.
        if !host.is_ascii() {
            return Err("SSH host must be ASCII".to_string());
        }
        if host.chars().any(|c| c.is_whitespace() || c.is_control()) || host.starts_with('-') {
            return Err("SSH host is invalid".to_string());
        }
        let (user, name) = match host.rsplit_once('@') {
            Some((user, name)) => (Some(user), name),
            None => (None, host),
        };
        // An IPv6 literal is accepted bare (`::1`) or bracketed (`[::1]`) and
        // kept bare for ssh's argv and the connection id; only the stored URI
        // brackets it.
        let name = name.strip_prefix('[').and_then(|n| n.strip_suffix(']')).unwrap_or(name);
        if name.is_empty() || name.starts_with('-') || name.contains(['[', ']']) || user.is_some_and(str::is_empty) {
            return Err("SSH host is invalid".to_string());
        }
        let host = match user {
            Some(user) => format!("{user}@{name}"),
            None => name.to_string(),
        };
        Ok(SshTarget {
            host,
            ssh_port: self.ssh_port.map(|p| validate_port(p, "SSH port")).transpose()?,
            daemon_port: validate_port(self.daemon_port, "daemon port")?,
        })
    }

    /// to_uri is the stored `base_url`: `ssh://[user@]host[:sshPort]?daemonPort=N`.
    pub fn to_uri(&self) -> Result<String, String> {
        let (user, host) = match self.host.rsplit_once('@') {
            Some((u, h)) => (Some(u), h),
            None => (None, self.host.as_str()),
        };
        let mut url = Url::parse("ssh://placeholder").expect("static URL");
        let host = if host.contains(':') { format!("[{host}]") } else { host.to_string() };
        url.set_host(Some(&host)).map_err(|_| "SSH host is invalid".to_string())?;
        if let Some(user) = user {
            url.set_username(user).map_err(|_| "SSH host is invalid".to_string())?;
        }
        let _ = url.set_port(self.ssh_port);
        url.query_pairs_mut().append_pair("daemonPort", &self.daemon_port.to_string());
        Ok(url.to_string())
    }

    /// from_uri parses a stored `ssh://` base URL back.
    pub fn from_uri(raw: &str) -> Result<SshTarget, String> {
        let url = Url::parse(raw).map_err(|_| "invalid SSH connection URL".to_string())?;
        if url.scheme() != "ssh" || url.password().is_some() {
            return Err("invalid SSH connection URL".to_string());
        }
        let hostname = url.host_str().unwrap_or_default();
        let hostname = hostname.strip_prefix('[').and_then(|h| h.strip_suffix(']')).unwrap_or(hostname);
        let user = percent_decode(url.username());
        let host = if user.is_empty() { hostname.to_string() } else { format!("{user}@{hostname}") };
        let daemon_port = url
            .query_pairs()
            .find(|(k, _)| k == "daemonPort")
            .and_then(|(_, v)| v.parse::<u16>().ok())
            .ok_or_else(|| "invalid SSH connection URL".to_string())?;
        SshTarget { host, ssh_port: url.port(), daemon_port }.validate()
    }

    /// connection_id is deterministic, so re-adding the same target updates
    /// the entry instead of duplicating it.
    pub fn connection_id(&self) -> String {
        let port = self.ssh_port.map_or("default".to_string(), |p| p.to_string());
        format!("ssh:{}:{port}:{}", self.host, self.daemon_port)
    }
}

fn percent_decode(s: &str) -> String {
    url::form_urlencoded::parse(format!("x={}", s.replace('+', "%2B")).as_bytes())
        .next()
        .map(|(_, v)| v.into_owned())
        .unwrap_or_default()
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
    // A relay pairing link carries the relay's admission secret in its
    // fragment; stripping the fragment below would save a bogus direct
    // connection to the link's pairing host. Never echo the link.
    if trimmed
        .split_once('#')
        .is_some_and(|(_, frag)| url::form_urlencoded::parse(frag.as_bytes()).any(|(k, _)| k == "offer"))
    {
        return Err("that is a relay pairing link, not a daemon URL: use \"Add via relay link\"".to_string());
    }
    let mut url = Url::parse(trimmed).map_err(|e| format!("invalid URL {trimmed:?}: {e}"))?;
    match url.scheme() {
        "http" | "https" => {}
        s => {
            return Err(format!(
                "invalid URL {trimmed:?}: scheme must be http(s), got {s}"
            ))
        }
    }
    if url.host_str().is_none() {
        return Err(format!("invalid URL {trimmed:?}: missing host"));
    }
    // Never echo the URL here: it carries the credential being refused.
    if !url.username().is_empty() || url.password().is_some() {
        return Err("a daemon URL must not contain credentials".to_string());
    }
    url.set_path("");
    url.set_query(None);
    url.set_fragment(None);
    Ok(url)
}

fn connection_id_for_url(url: &Url) -> String {
    format!("url:{}", url.as_str().trim_end_matches('/'))
}

/// connection_id_for_relay derives a stable id from the (workspace,
/// relay address) pair a pairing offer resolves to, so re-pairing the
/// same workspace/relay updates the existing entry in place rather than
/// duplicating it -- mirroring `connection_id_for_url`'s dedup-by-id
/// behavior.
pub fn connection_id_for_relay(workspace_id: &str, relay_native_addr: &str) -> String {
    format!("relay:{workspace_id}@{relay_native_addr}")
}

/// The file as raw JSON: known entries are parsed into `Connection`s,
/// everything this build does not understand (unknown kinds, unknown
/// fields) is kept verbatim so a newer app version's data survives a
/// round trip through an older one.
#[cfg(test)]
#[derive(Debug, Clone, Serialize, Deserialize)]
struct StoredRegistry {
    connections: Vec<Connection>,
    #[serde(rename = "selectedId")]
    selected_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct RawRegistry {
    #[serde(default)]
    connections: Vec<Value>,
    #[serde(rename = "selectedId")]
    selected_id: Option<String>,
    #[serde(flatten)]
    extra: Map<String, Value>,
}

/// A display-only summary of an entry whose `kind` this build does not
/// know. Carries only `id`/`label`/`kind` -- never `baseUrl` or other
/// fields, which can hold credentials.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct UnsupportedConnection {
    pub id: Option<String>,
    pub label: Option<String>,
    pub kind: Option<String>,
}

fn string_field(v: &Value, key: &str) -> Option<String> {
    v.get(key).and_then(Value::as_str).map(str::to_string)
}

const KNOWN_ENTRY_KEYS: [&str; 4] = ["id", "kind", "label", "baseUrl"];

/// Registry holds the connection list, in-memory, plus which one is
/// currently selected. The built-in `local` entry always exists at
/// index 0 and can never be removed or have its id reused by a `url`
/// entry.
#[derive(Debug, Clone)]
pub struct Registry {
    connections: Vec<Connection>,
    selected_id: String,
    unsupported: Vec<Value>,
    entry_extras: HashMap<String, Map<String, Value>>,
    top_extra: Map<String, Value>,
    preserved_selection: Option<String>,
}

impl Registry {
    /// new builds a fresh registry with only the built-in local entry
    /// selected, given the resolved local daemon URL (the
    /// `SMIND_DAEMON_URL` override, or the default).
    pub fn new(local_base_url: &Url) -> Self {
        Self {
            connections: vec![local_connection(local_base_url)],
            selected_id: LOCAL_ID.to_string(),
            unsupported: Vec::new(),
            entry_extras: HashMap::new(),
            top_extra: Map::new(),
            preserved_selection: None,
        }
    }

    /// unsupported lists the stored entries this build could not parse
    /// (unknown kind, or a known kind with an invalid shape). They are
    /// kept verbatim and written back on `save`; they never appear in
    /// `list()`/`current()`.
    pub fn unsupported(&self) -> Vec<UnsupportedConnection> {
        self.unsupported
            .iter()
            .map(|v| UnsupportedConnection {
                id: string_field(v, "id"),
                label: string_field(v, "label"),
                kind: string_field(v, "kind"),
            })
            .collect()
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
        let label = if label.trim().is_empty() {
            url.as_str().trim_end_matches('/').to_string()
        } else {
            label.trim().to_string()
        };
        let conn = Connection {
            id: id.clone(),
            kind: ConnectionKind::Url,
            label,
            base_url: url.as_str().trim_end_matches('/').to_string(),
        };
        if let Some(existing) = self.connections.iter_mut().find(|c| c.id == id) {
            *existing = conn.clone();
        } else {
            self.connections.push(conn.clone());
        }
        Ok(conn)
    }

    /// add_ssh inserts or updates the `ssh` connection for `target`.
    pub fn add_ssh(&mut self, label: &str, target: &SshTarget) -> Result<Connection, String> {
        let target = target.validate()?;
        let label = label.trim();
        let conn = Connection {
            id: target.connection_id(),
            kind: ConnectionKind::Ssh,
            label: if label.is_empty() { target.host.clone() } else { label.to_string() },
            base_url: target.to_uri()?,
        };
        if let Some(existing) = self.connections.iter_mut().find(|c| c.id == conn.id) {
            *existing = conn.clone();
        } else {
            self.connections.push(conn.clone());
        }
        Ok(conn)
    }

    /// add_relay inserts or (if the same workspace+relay-address pairing
    /// already exists) updates a `relay` connection's display entry.
    /// Unlike `add`, no URL validation happens here -- the pairing URL
    /// itself is parsed/validated by `crate::relay::pairing::Offer`
    /// before this is ever called. This method only manages the
    /// non-secret display entry: `connections.json` never carries relay
    /// key material (the admission secret / daemon public key are
    /// persisted separately, keyed by this connection's id, via
    /// `crate::relay::pairing_store`).
    pub fn add_relay(
        &mut self,
        label: &str,
        workspace_id: &str,
        relay_native_addr: &str,
    ) -> Connection {
        let id = connection_id_for_relay(workspace_id, relay_native_addr);
        let label = if label.trim().is_empty() {
            format!("Relay ({workspace_id})")
        } else {
            label.trim().to_string()
        };
        let conn = Connection {
            id: id.clone(),
            kind: ConnectionKind::Relay,
            label,
            base_url: format!("relay://{workspace_id}@{relay_native_addr}"),
        };
        if let Some(existing) = self.connections.iter_mut().find(|c| c.id == id) {
            *existing = conn.clone();
        } else {
            self.connections.push(conn.clone());
        }
        conn
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
        self.entry_extras.remove(id);
        if self.selected_id == id {
            self.selected_id = LOCAL_ID.to_string();
            self.preserved_selection = None;
        }
        Ok(())
    }

    /// select switches the current connection. Errors for an unknown id.
    pub fn select(&mut self, id: &str) -> Result<(), String> {
        if !self.connections.iter().any(|c| c.id == id) {
            return Err(format!("no connection with id {id:?}"));
        }
        self.selected_id = id.to_string();
        self.preserved_selection = None;
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
        let Ok(raw) = serde_json::from_str::<RawRegistry>(&data) else {
            return Self::new(local_base_url);
        };
        let mut connections: Vec<Connection> = Vec::new();
        let mut unsupported: Vec<Value> = Vec::new();
        let mut entry_extras: HashMap<String, Map<String, Value>> = HashMap::new();
        for v in raw.connections {
            match serde_json::from_value::<Connection>(v.clone()) {
                // The local entry is always rebuilt from the current
                // local_base_url; a stored one is dropped.
                Ok(c) if c.id != LOCAL_ID => {
                    if let Some(obj) = v.as_object() {
                        let extras: Map<String, Value> = obj
                            .iter()
                            .filter(|(k, _)| !KNOWN_ENTRY_KEYS.contains(&k.as_str()))
                            .map(|(k, v)| (k.clone(), v.clone()))
                            .collect();
                        if !extras.is_empty() {
                            entry_extras.insert(c.id.clone(), extras);
                        }
                    }
                    connections.push(c);
                }
                Ok(_) => {}
                Err(_) => unsupported.push(v),
            }
        }
        connections.insert(0, local_connection(local_base_url));
        let stored_selected = raw.selected_id.unwrap_or_default();
        // If the stored selection points at an entry we cannot use, keep
        // the selection local in memory but remember it, so save() does
        // not silently rewrite the user's choice.
        let preserved_selection = (!connections.iter().any(|c| c.id == stored_selected)
            && unsupported
                .iter()
                .any(|v| v.get("id").and_then(Value::as_str) == Some(stored_selected.as_str())))
        .then(|| stored_selected.clone());
        let selected_id = if connections.iter().any(|c| c.id == stored_selected) {
            stored_selected
        } else {
            LOCAL_ID.to_string()
        };
        Self {
            connections,
            selected_id,
            unsupported,
            entry_extras,
            top_extra: raw.extra,
            preserved_selection,
        }
    }

    /// save writes the registry to `path` as JSON, creating parent
    /// directories as needed. Unknown top-level fields, unknown fields
    /// on known entries, and unparsed entries are written back verbatim,
    /// so the file survives a round trip through an older build. Known
    /// entries come first (in registry order), unsupported ones after.
    pub fn save(&self, path: &Path) -> std::io::Result<()> {
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)?;
        }
        let mut root = Value::Object(self.top_extra.clone());
        let connections: Vec<Value> = self
            .connections
            .iter()
            .map(|conn| {
                let mut obj = serde_json::to_value(conn)
                    .expect("smind desktop: connection serializes")
                    .as_object()
                    .expect("smind desktop: connection serializes to an object")
                    .clone();
                if let Some(extras) = self.entry_extras.get(&conn.id) {
                    for (k, v) in extras {
                        obj.entry(k.clone()).or_insert_with(|| v.clone());
                    }
                }
                Value::Object(obj)
            })
            .chain(self.unsupported.iter().cloned())
            .collect();
        root["connections"] = Value::Array(connections);
        root["selectedId"] =
            Value::String(self.preserved_selection.clone().unwrap_or(self.selected_id.clone()));
        let data =
            serde_json::to_string_pretty(&root).expect("smind desktop: registry serializes");
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
    fn validate_base_url_rejects_relay_pairing_links_without_echoing_them() {
        let err = validate_base_url("https://spacingmind.sh/pair#offer=c2VjcmV0").unwrap_err();
        assert!(err.contains("Add via relay link"), "{err}");
        assert!(!err.contains("c2VjcmV0"), "{err}");
        assert!(validate_base_url("http://example.com:9000/#z").is_ok());
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
    fn add_relay_then_select_then_current() {
        let mut reg = Registry::new(&local_url());
        let conn = reg.add_relay("My Desktop", "ws-1", "relay.example.test:7400");
        assert_eq!(conn.kind, ConnectionKind::Relay);
        assert_eq!(reg.list().len(), 2);
        reg.select(&conn.id).unwrap();
        assert_eq!(reg.current().id, conn.id);
        assert_eq!(reg.current().label, "My Desktop");
    }

    #[test]
    fn add_relay_same_pairing_twice_updates_in_place() {
        let mut reg = Registry::new(&local_url());
        reg.add_relay("First", "ws-1", "relay.example.test:7400");
        reg.add_relay("Second", "ws-1", "relay.example.test:7400");
        assert_eq!(reg.list().len(), 2);
        assert_eq!(reg.list()[1].label, "Second");
    }

    #[test]
    fn relay_connection_can_be_removed_like_url() {
        let mut reg = Registry::new(&local_url());
        let conn = reg.add_relay("My Desktop", "ws-1", "relay.example.test:7400");
        reg.select(&conn.id).unwrap();
        reg.remove(&conn.id).unwrap();
        assert_eq!(reg.current().id, LOCAL_ID);
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
    fn validate_base_url_rejects_userinfo_without_echoing_it() {
        for raw in ["http://user:hunter2@host:4648", "https://tok@host", "http://:hunter2@host"] {
            let err = validate_base_url(raw).unwrap_err();
            assert_eq!(err, "a daemon URL must not contain credentials");
            assert!(!err.contains("hunter2"));
            let mut reg = Registry::new(&local_url());
            assert!(reg.add("x", raw).is_err());
            assert_eq!(reg.list().len(), 1);
        }
    }

    fn target(host: &str, ssh_port: Option<u16>, daemon_port: u16) -> SshTarget {
        SshTarget { host: host.to_string(), ssh_port, daemon_port }
    }

    #[test]
    fn ssh_host_starting_with_dash_rejected() {
        for host in ["-oProxyCommand=x", "-p", "  -x", "user@-host"] {
            assert_eq!(target(host, None, 4648).validate().unwrap_err(), "SSH host is invalid", "{host}");
        }
    }

    #[test]
    fn ssh_host_with_whitespace_rejected() {
        for host in ["a b", "host\tx", "ho\nst", "ho\u{7}st", "user name@host"] {
            assert_eq!(target(host, None, 4648).validate().unwrap_err(), "SSH host is invalid", "{host:?}");
        }
        assert_eq!(target("  ", None, 4648).validate().unwrap_err(), "SSH host is required");
        assert_eq!(target("  lab  ", None, 4648).validate().unwrap().host, "lab", "trimmed");
    }

    #[test]
    fn ssh_port_out_of_range_rejected() {
        assert!(target("lab", Some(0), 4648).validate().unwrap_err().contains("SSH port"));
        assert!(target("lab", None, 0).validate().unwrap_err().contains("daemon port"));
        assert!(target("lab", Some(65535), 1).validate().is_ok());
    }

    #[test]
    fn ssh_connection_round_trips_through_registry_json() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        let mut reg = Registry::new(&local_url());
        let t = target("me@lab.example", Some(2222), 4648);
        let conn = reg.add_ssh("", &t).unwrap();
        assert_eq!(conn.id, "ssh:me@lab.example:2222:4648");
        assert_eq!(conn.kind, ConnectionKind::Ssh);
        assert_eq!(conn.label, "me@lab.example");
        assert_eq!(conn.base_url, "ssh://me@lab.example:2222?daemonPort=4648");
        reg.select(&conn.id).unwrap();
        reg.save(&path).unwrap();
        assert!(fs::read_to_string(&path).unwrap().contains(r#""kind": "ssh""#));

        let loaded = Registry::load(&path, &local_url());
        assert_eq!(loaded.current(), &conn);
        assert_eq!(SshTarget::from_uri(&loaded.current().base_url).unwrap(), t);

        // Same target again dedupes (label updates); a different port is a new entry.
        let mut reg = loaded;
        reg.add_ssh("Lab", &t).unwrap();
        assert_eq!(reg.list().len(), 2);
        assert_eq!(reg.list()[1].label, "Lab");
        reg.add_ssh("", &target("me@lab.example", None, 4648)).unwrap();
        assert_eq!(reg.list().len(), 3);
        assert_eq!(reg.list()[2].id, "ssh:me@lab.example:default:4648");
        assert_eq!(SshTarget::from_uri("ssh://[::1]?daemonPort=9").unwrap().host, "::1");
    }

    #[test]
    fn ssh_ipv6_hosts_are_normalised_to_the_bare_form() {
        for raw in ["::1", "[::1]", " [::1] "] {
            let t = target(raw, Some(22), 4648).validate().unwrap();
            assert_eq!(t.host, "::1", "{raw}");
            assert_eq!(t.connection_id(), "ssh:::1:22:4648");
            // Brackets only inside the stored URI, and it round-trips bare.
            let uri = t.to_uri().unwrap();
            assert_eq!(uri, "ssh://[::1]:22?daemonPort=4648");
            assert_eq!(SshTarget::from_uri(&uri).unwrap(), t);
        }
        let with_user = target("me@[2001:db8::7]", None, 9).validate().unwrap();
        assert_eq!(with_user.host, "me@2001:db8::7");
        assert_eq!(with_user.to_uri().unwrap(), "ssh://me@[2001:db8::7]?daemonPort=9");
        assert_eq!(SshTarget::from_uri(&with_user.to_uri().unwrap()).unwrap(), with_user);
        // Only brackets, unbalanced or stray brackets: rejected.
        for bad in ["[]", "me@[]", "[::1", "::1]", "a[b]c", "[[::1]]"] {
            assert_eq!(target(bad, None, 9).validate().unwrap_err(), "SSH host is invalid", "{bad}");
        }
    }

    #[test]
    fn ssh_non_ascii_host_rejected() {
        for host in ["lab\u{e9}", "me@h\u{f6}st", "\u{4e3b}\u{673a}", "ho\u{a0}st"] {
            assert_eq!(target(host, None, 9).validate().unwrap_err(), "SSH host must be ASCII", "{host:?}");
        }
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
        let stale = StoredRegistry {
            connections: vec![],
            selected_id: "url:http://gone".to_string(),
        };
        fs::write(&path, serde_json::to_string(&stale).unwrap()).unwrap();

        let loaded = Registry::load(&path, &local_url());
        assert_eq!(loaded.current().id, LOCAL_ID);
    }
    fn write_raw(path: &Path, json: &str) {
        fs::write(path, json).unwrap();
    }

    fn read_json(path: &Path) -> Value {
        serde_json::from_str(&fs::read_to_string(path).unwrap()).unwrap()
    }

    #[test]
    fn registry_round_trips_unknown_kind_verbatim() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        let unknown = serde_json::json!({"id":"vpn:1","kind":"wireguard","label":"Office","baseUrl":"wg://x","extra":{"a":[1,2]}});
        write_raw(&path, &serde_json::json!({
            "connections": [
                {"id":"url:http://example.com:9000","kind":"url","label":"T","baseUrl":"http://example.com:9000"},
                unknown.clone()
            ],
            "selectedId": "local"
        }).to_string());

        let reg = Registry::load(&path, &local_url());
        assert!(reg.list().iter().all(|c| c.kind != ConnectionKind::Url || c.id != "vpn:1"));
        assert!(!reg.list().iter().any(|c| c.id == "vpn:1"));
        assert_eq!(
            reg.unsupported(),
            vec![UnsupportedConnection { id: Some("vpn:1".into()), label: Some("Office".into()), kind: Some("wireguard".into()) }]
        );

        let out = dir.path().join("out.json");
        reg.save(&out).unwrap();
        let saved = read_json(&out);
        let conns = saved["connections"].as_array().unwrap();
        assert!(conns.iter().any(|c| *c == unknown), "{conns:?}");
    }

    #[test]
    fn registry_round_trips_unknown_fields_in_known_entries() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        write_raw(&path, &serde_json::json!({
            "connections": [
                {"id":"url:http://example.com:9000","kind":"url","label":"T","baseUrl":"http://example.com:9000",
                 "futureField":{"x":1},"pinned":true}
            ],
            "selectedId": "local"
        }).to_string());
        let reg = Registry::load(&path, &local_url());
        let out = dir.path().join("out.json");
        reg.save(&out).unwrap();
        let saved_entry = &read_json(&out)["connections"][1];
        assert_eq!(saved_entry["futureField"], serde_json::json!({"x":1}));
        assert_eq!(saved_entry["pinned"], serde_json::json!(true));
        assert_eq!(saved_entry["baseUrl"], "http://example.com:9000");
        assert_eq!(saved_entry["label"], "T");
        assert_eq!(saved_entry["kind"], "url");
    }

    #[test]
    fn registry_round_trips_unknown_top_level_fields() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        write_raw(&path, &serde_json::json!({
            "connections": [], "selectedId": "local", "version": 2, "futureTop": {"k": "v"}
        }).to_string());
        let reg = Registry::load(&path, &local_url());
        let out = dir.path().join("out.json");
        reg.save(&out).unwrap();
        let saved = read_json(&out);
        assert_eq!(saved["version"], serde_json::json!(2));
        assert_eq!(saved["futureTop"], serde_json::json!({"k":"v"}));
    }

    #[test]
    fn registry_keeps_a_selection_that_points_at_an_unsupported_entry_until_the_user_selects() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        write_raw(&path, &serde_json::json!({
            "connections": [
                {"id":"vpn:1","kind":"wireguard","label":"Office","baseUrl":"wg://x"}
            ],
            "selectedId": "vpn:1"
        }).to_string());
        let mut reg = Registry::load(&path, &local_url());
        assert_eq!(reg.current().id, LOCAL_ID);

        let out = dir.path().join("out.json");
        reg.save(&out).unwrap();
        assert_eq!(read_json(&out)["selectedId"], "vpn:1");

        reg.select(LOCAL_ID).unwrap();
        reg.save(&out).unwrap();
        assert_eq!(read_json(&out)["selectedId"], "local");
    }

    #[test]
    fn registry_keeps_unsupported_entries_through_add_remove_and_select() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        let unknown = serde_json::json!({"id":"vpn:1","kind":"wireguard","label":"Office","baseUrl":"wg://x"});
        write_raw(&path, &serde_json::json!({
            "connections": [unknown.clone()], "selectedId": "local"
        }).to_string());
        let mut reg = Registry::load(&path, &local_url());
        let conn = reg.add("x", "http://example.com:9000").unwrap();
        reg.select(&conn.id).unwrap();
        reg.remove(&conn.id).unwrap();
        let out = dir.path().join("out.json");
        reg.save(&out).unwrap();
        assert!(
            read_json(&out)["connections"].as_array().unwrap().contains(&unknown)
        );
    }

    #[test]
    fn entry_with_a_known_kind_but_an_invalid_shape_is_kept_verbatim() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        let bad = serde_json::json!({"id":"url:z","kind":"url","label":"z"});
        write_raw(&path, &serde_json::json!({
            "connections": [bad.clone()], "selectedId": "local"
        }).to_string());
        let reg = Registry::load(&path, &local_url());
        assert!(!reg.list().iter().any(|c| c.id == "url:z"));
        assert_eq!(reg.unsupported().len(), 1);
        let out = dir.path().join("out.json");
        reg.save(&out).unwrap();
        assert!(read_json(&out)["connections"].as_array().unwrap().contains(&bad));
    }

    #[test]
    fn unsupported_reports_only_id_label_and_kind() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        write_raw(&path, &serde_json::json!({
            "connections": [
                {"id":"vpn:1","kind":"wireguard","label":"Office","baseUrl":"wg://sekrit/hunter2"}
            ],
            "selectedId": "local"
        }).to_string());
        let reg = Registry::load(&path, &local_url());
        let json = serde_json::to_string(&reg.unsupported()).unwrap();
        assert!(!json.contains("wg://"), "{json}");
        assert!(!json.contains("hunter2"), "{json}");
    }

    #[test]
    fn corrupt_file_is_never_overwritten_by_load() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        fs::write(&path, "{not json").unwrap();
        let _reg = Registry::load(&path, &local_url());
        assert_eq!(fs::read_to_string(&path).unwrap(), "{not json");
    }

    #[test]
    fn corrupt_file_is_replaced_only_by_an_explicit_save() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        fs::write(&path, "{not json").unwrap();
        let reg = Registry::load(&path, &local_url());
        reg.save(&path).unwrap();
        let saved = read_json(&path);
        let conns = saved["connections"].as_array().unwrap();
        assert_eq!(conns.len(), 1);
        assert_eq!(conns[0]["id"], "local");
        assert_eq!(saved["selectedId"], "local");
    }

    #[test]
    fn a_top_level_that_is_not_an_object_is_treated_as_corrupt() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("connections.json");
        fs::write(&path, "[1,2]").unwrap();
        let reg = Registry::load(&path, &local_url());
        assert_eq!(reg.list().len(), 1);
        assert!(reg.unsupported().is_empty());
    }
}
