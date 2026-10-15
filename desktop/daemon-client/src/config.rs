//! Daemon URL resolution: default http://127.0.0.1:4648, overridable
//! via SMIND_DAEMON_URL.

use std::fmt;
use url::Url;

pub const DEFAULT_DAEMON_URL: &str = "http://127.0.0.1:4648";
pub const ENV_DAEMON_URL: &str = "SMIND_DAEMON_URL";

#[derive(Clone)]
pub struct Config {
    pub daemon_url: Url,
}

impl fmt::Display for Config {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.daemon_url)
    }
}

/// daemon_url resolves the daemon base URL from the SMIND_DAEMON_URL
/// env var, falling back to the local default. Empty/unset means
/// default; a value that isn't a valid absolute http(s) URL is an error
/// (silently substituting the default would hide a typo from the user).
pub fn daemon_url() -> Result<Url, String> {
    let raw = std::env::var(ENV_DAEMON_URL).unwrap_or_default();
    daemon_url_from(raw)
}

pub fn daemon_url_from(override_value: impl AsRef<str>) -> Result<Url, String> {
    let raw = override_value.as_ref().trim();
    if raw.is_empty() {
        return Url::parse(DEFAULT_DAEMON_URL)
            .map_err(|e| format!("invalid default daemon URL: {e}"));
    }
    let url = Url::parse(raw).map_err(|e| format!("invalid {ENV_DAEMON_URL} {raw:?}: {e}"))?;
    match url.scheme() {
        "http" | "https" => Ok(url),
        s => Err(format!("invalid {ENV_DAEMON_URL} {raw:?}: scheme must be http(s), got {s}")),
    }
}

/// local_token_path_from locates the token file the local daemon wrote
/// (`internal/auth`, mirrored from `internal/config.Dir`): `$SMIND_HOME/token`,
/// else `<home>/.smind/token`, with the legacy `<home>/.spacingmind` winning only
/// when `.smind` does not exist yet. `None` when no home directory is known.
pub fn local_token_path_from(
    smind_home: Option<&str>,
    home: Option<&str>,
    exists: impl Fn(&std::path::Path) -> bool,
) -> Option<std::path::PathBuf> {
    use std::path::Path;
    if let Some(h) = smind_home.filter(|h| !h.is_empty()) {
        return Some(Path::new(h).join("token"));
    }
    let home = Path::new(home.filter(|h| !h.is_empty())?);
    let smind = home.join(".smind");
    let legacy = home.join(".spacingmind");
    Some(if !exists(&smind) && exists(&legacy) { legacy.join("token") } else { smind.join("token") })
}

/// read_local_token reads the local daemon's token file. A daemon with a
/// password does not serve `/api/token` (ADR-0031 D8), but the same OS user can
/// always read this file, which is how the CLI authenticates too.
pub fn read_local_token() -> Option<String> {
    let home = std::env::var("HOME").or_else(|_| std::env::var("USERPROFILE")).ok();
    let smind_home = std::env::var("SMIND_HOME").ok();
    let path = local_token_path_from(smind_home.as_deref(), home.as_deref(), |p| p.exists())?;
    let token = std::fs::read_to_string(path).ok()?.trim().to_string();
    if token.is_empty() { None } else { Some(token) }
}

impl Config {
    /// is_loopback_daemon reports whether the daemon URL points at this machine,
    /// the only case where the local token file is the right credential.
    pub fn is_loopback_daemon(&self) -> bool {
        match self.daemon_url.host() {
            Some(url::Host::Domain(d)) => d.eq_ignore_ascii_case("localhost"),
            Some(url::Host::Ipv4(ip)) => ip.is_loopback(),
            Some(url::Host::Ipv6(ip)) => ip.is_loopback(),
            None => false,
        }
    }

    /// ws_url builds the ws:// (or wss://) /ws?token=... upgrade URL
    /// from the http(s) base: tungstenite rejects http:// URLs.
    pub fn ws_url(&self, token: &str) -> Result<Url, String> {
        let mut url = self.daemon_url.clone();
        let scheme = match url.scheme() {
            "https" => "wss",
            _ => "ws",
        };
        url.set_scheme(scheme)
            .map_err(|_| "cannot set ws scheme".to_string())?;
        url.set_path("/ws");
        url.query_pairs_mut().clear().append_pair("token", token);
        Ok(url)
    }

    pub fn token_url(&self) -> Url {
        let mut url = self.daemon_url.clone();
        url.set_path("/api/token");
        url.set_query(None);
        url
    }

    pub fn healthz_url(&self) -> Url {
        let mut url = self.daemon_url.clone();
        url.set_path("/healthz");
        url.set_query(None);
        url
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_url_when_env_empty() {
        let url = daemon_url_from("").unwrap();
        assert_eq!(url.as_str(), "http://127.0.0.1:4648/");
    }

    #[test]
    fn env_override_wins() {
        let url = daemon_url_from("http://localhost:9999").unwrap();
        assert_eq!(url.as_str(), "http://localhost:9999/");
    }

    #[test]
    fn override_with_trailing_slash() {
        let url = daemon_url_from("http://127.0.0.1:4648/").unwrap();
        assert_eq!(url.host_str().unwrap(), "127.0.0.1");
        assert_eq!(url.port_or_known_default().unwrap(), 4648);
    }

    #[test]
    fn invalid_override_is_error() {
        assert!(daemon_url_from("not a url").is_err());
        assert!(daemon_url_from("ftp://x").is_err());
        assert!(daemon_url_from("127.0.0.1:4648").is_err()); // no scheme
    }

    #[test]
    fn local_token_path_follows_smind_home_then_home() {
        use std::path::{Path, PathBuf};
        let none = |_: &Path| false;
        assert_eq!(local_token_path_from(Some("/data"), Some("/home/u"), none), Some(PathBuf::from("/data/token")));
        assert_eq!(local_token_path_from(None, Some("/home/u"), none), Some(PathBuf::from("/home/u/.smind/token")));
        assert_eq!(local_token_path_from(Some(""), Some("/home/u"), none), Some(PathBuf::from("/home/u/.smind/token")));
        assert_eq!(local_token_path_from(None, None, none), None);
        // The legacy directory only wins while ~/.smind does not exist.
        let legacy_only = |p: &Path| p.ends_with(".spacingmind");
        assert_eq!(local_token_path_from(None, Some("/home/u"), legacy_only), Some(PathBuf::from("/home/u/.spacingmind/token")));
        let both = |_: &Path| true;
        assert_eq!(local_token_path_from(None, Some("/home/u"), both), Some(PathBuf::from("/home/u/.smind/token")));
    }

    #[test]
    fn token_file_is_only_for_a_loopback_daemon() {
        for (url, want) in [
            ("http://127.0.0.1:4648", true),
            ("http://localhost:4648", true),
            ("http://[::1]:4648", true),
            ("http://192.168.1.10:4648", false),
            ("https://smind.example.com", false),
        ] {
            let cfg = Config { daemon_url: daemon_url_from(url).unwrap() };
            assert_eq!(cfg.is_loopback_daemon(), want, "{url}");
        }
    }

    #[test]
    fn derived_urls() {
        let cfg = Config { daemon_url: daemon_url_from("http://127.0.0.1:4648").unwrap() };
        assert_eq!(cfg.ws_url("t").unwrap().as_str(), "ws://127.0.0.1:4648/ws?token=t");
        let tls_cfg = Config { daemon_url: daemon_url_from("https://example.net").unwrap() };
        assert_eq!(tls_cfg.ws_url("t").unwrap().as_str(), "wss://example.net/ws?token=t");
        assert_eq!(cfg.token_url().as_str(), "http://127.0.0.1:4648/api/token");
        assert_eq!(cfg.healthz_url().as_str(), "http://127.0.0.1:4648/healthz");
    }
}
