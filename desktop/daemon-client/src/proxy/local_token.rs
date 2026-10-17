//! The local daemon's token file, for the loopback proxy's `local`
//! connection only (ADR-0031 D8): a daemon with a password does not serve
//! `/api/token`, but the same OS user can always read the file the daemon
//! wrote (`internal/auth`), which is also how the CLI authenticates. The
//! proxy attaches it upstream itself; it never reaches the webview.

use std::path::{Path, PathBuf};

use url::{Host, Url};

/// token_path_from locates the token file (`internal/config.Dir`):
/// `$SMIND_HOME/token`, else `<home>/.smind/token`, with the legacy
/// `<home>/.spacingmind` winning only when `.smind` does not exist yet.
/// `None` when no home directory is known.
pub fn token_path_from(smind_home: Option<&str>, home: Option<&str>, exists: impl Fn(&Path) -> bool) -> Option<PathBuf> {
    if let Some(h) = smind_home.filter(|h| !h.is_empty()) {
        return Some(Path::new(h).join("token"));
    }
    let home = Path::new(home.filter(|h| !h.is_empty())?);
    let smind = home.join(".smind");
    let legacy = home.join(".spacingmind");
    Some(if !exists(&smind) && exists(&legacy) { legacy.join("token") } else { smind.join("token") })
}

/// read reads the token file afresh (it changes when the daemon is
/// restarted with a new token); `None` when absent, unreadable or empty.
pub fn read() -> Option<String> {
    let home = std::env::var("HOME").or_else(|_| std::env::var("USERPROFILE")).ok();
    let smind_home = std::env::var("SMIND_HOME").ok();
    let path = token_path_from(smind_home.as_deref(), home.as_deref(), |p| p.exists())?;
    let token = std::fs::read_to_string(path).ok()?.trim().to_string();
    (!token.is_empty()).then_some(token)
}

/// is_local_by_address is the gate before the token file is used for a
/// daemon URL: true only when the daemon is on this machine by *address*,
/// not by spelling. An IP literal must be loopback; the name `localhost`
/// must resolve and every address it resolves to must be loopback; any
/// other host name is remote without being looked up. (`SMIND_DAEMON_URL`
/// can point the built-in `local` entry at another machine.)
pub async fn is_local_by_address(url: &Url) -> bool {
    match url.host() {
        Some(Host::Ipv4(ip)) => ip.is_loopback(),
        Some(Host::Ipv6(ip)) => ip.is_loopback(),
        Some(Host::Domain(d)) if d.eq_ignore_ascii_case("localhost") => {
            let port = url.port_or_known_default().unwrap_or(80);
            match tokio::net::lookup_host((d, port)).await {
                Ok(addrs) => {
                    let addrs: Vec<_> = addrs.collect();
                    !addrs.is_empty() && addrs.iter().all(|a| a.ip().is_loopback())
                }
                Err(_) => false,
            }
        }
        _ => false,
    }
}

/// is_password_smind confirms that what answers at `base_url` really is a
/// password-protected smind daemon (`/healthz`: service "smind", authRequired
/// true) before the token file is ever attached to anything it says. Short
/// timeout (the caller's `client`), bounded body.
pub async fn is_password_smind(client: &reqwest::Client, base_url: &str) -> bool {
    #[derive(serde::Deserialize)]
    #[serde(rename_all = "camelCase")]
    struct Healthz {
        service: Option<String>,
        #[serde(default)]
        auth_required: bool,
    }
    let url = format!("{}/healthz", base_url.trim_end_matches('/'));
    let Ok(resp) = client.get(url).send().await else { return false };
    if !resp.status().is_success() {
        return false;
    }
    crate::proxy::bounded::read_json::<Healthz>(resp, crate::proxy::bounded::MAX_BODY_BYTES)
        .await
        .is_some_and(|h| h.service.as_deref() == Some("smind") && h.auth_required)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn token_path_follows_smind_home_then_dot_smind_then_legacy() {
        let none = |_: &Path| false;
        assert_eq!(token_path_from(Some("/h"), Some("/u"), none), Some(PathBuf::from("/h/token")));
        assert_eq!(token_path_from(None, Some("/u"), none), Some(PathBuf::from("/u/.smind/token")));
        assert_eq!(token_path_from(Some(""), Some("/u"), none), Some(PathBuf::from("/u/.smind/token")));
        assert_eq!(token_path_from(None, Some("/u"), |p| p.ends_with(".spacingmind")), Some(PathBuf::from("/u/.spacingmind/token")));
        assert_eq!(token_path_from(None, Some("/u"), |_| true), Some(PathBuf::from("/u/.smind/token")));
        assert_eq!(token_path_from(None, None, none), None);
    }

    #[tokio::test]
    async fn only_loopback_addresses_are_local() {
        for (u, want) in [
            ("http://127.0.0.1:4648", true),
            ("http://[::1]:4648", true),
            ("http://localhost:4648", true),
            ("http://192.168.1.10:4648", false),
            ("https://smind.example.com", false),
            ("http://[::ffff:127.0.0.1]:1", false),
        ] {
            assert_eq!(is_local_by_address(&Url::parse(u).unwrap()).await, want, "{u}");
        }
    }
}
