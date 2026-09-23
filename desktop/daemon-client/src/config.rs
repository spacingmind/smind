//! Daemon URL resolution: default http://127.0.0.1:4648, overridable
//! via SMIND_DAEMON_URL.

use std::fmt;
use url::Url;

pub const DEFAULT_DAEMON_URL: &str = "http://127.0.0.1:4648";
pub const ENV_DAEMON_URL: &str = "SMIND_DAEMON_URL";

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

impl Config {
    /// ws_url builds the /ws?token=... upgrade URL from the base.
    pub fn ws_url(&self, token: &str) -> Result<Url, String> {
        let mut url = self.daemon_url.clone();
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
    fn derived_urls() {
        let cfg = Config { daemon_url: daemon_url_from("http://127.0.0.1:4648").unwrap() };
        assert_eq!(cfg.ws_url("t").unwrap().as_str(), "http://127.0.0.1:4648/ws?token=t");
        assert_eq!(cfg.token_url().as_str(), "http://127.0.0.1:4648/api/token");
        assert_eq!(cfg.healthz_url().as_str(), "http://127.0.0.1:4648/healthz");
    }
}
