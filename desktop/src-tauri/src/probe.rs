//! ADR-0031 D7 (b): `connections_probe(url)` -- what a daemon URL answers,
//! without saving anything. Nothing here is a secret: the probe sends no
//! credentials, strips any `user:pass@` from the URL before dialing, and its
//! error text never echoes the URL.

use std::time::Duration;

use serde::{Deserialize, Serialize};
use url::{Host, Url};

use smind_daemon_client::proxy::connections::validate_base_url;

const PROBE_TIMEOUT: Duration = Duration::from_secs(3);

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProbeResult {
    pub reachable: bool,
    pub version: Option<String>,
    pub service: Option<String>,
    pub auth_required: bool,
    pub loopback: bool,
    pub error: Option<String>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Healthz {
    service: Option<String>,
    version: Option<String>,
    #[serde(default)]
    auth_required: bool,
}

impl ProbeResult {
    fn unreachable(loopback: bool, error: impl Into<String>) -> Self {
        ProbeResult { reachable: false, version: None, service: None, auth_required: false, loopback, error: Some(error.into()) }
    }
}

/// is_loopback_host: `localhost`, `*.localhost`, or a loopback IP.
fn is_loopback_host(url: &Url) -> bool {
    match url.host() {
        Some(Host::Domain(d)) => {
            let d = d.trim_end_matches('.').to_ascii_lowercase();
            d == "localhost" || d.ends_with(".localhost")
        }
        Some(Host::Ipv4(ip)) => ip.is_loopback(),
        Some(Host::Ipv6(ip)) => ip.is_loopback(),
        None => false,
    }
}

/// without_userinfo drops `user:pass@` so credentials typed into a URL are
/// neither sent as Basic auth nor able to surface in a message.
pub(crate) fn without_userinfo(mut url: Url) -> Url {
    let _ = url.set_username("");
    let _ = url.set_password(None);
    url
}

/// describe_request_error is a user-facing reason that never carries the URL
/// (reqwest's own Display does).
pub(crate) fn describe_request_error(e: &reqwest::Error) -> String {
    if e.is_timeout() {
        "timed out".to_string()
    } else if e.is_connect() {
        "could not connect (is the daemon running and the address reachable?)".to_string()
    } else {
        "the request failed".to_string()
    }
}

pub async fn probe(raw_url: &str) -> ProbeResult {
    let base = match validate_base_url(raw_url) {
        Ok(url) => without_userinfo(url),
        // validate_base_url echoes what was typed, which may hold a password
        // or a pairing secret: report only the reason.
        Err(e) if e.contains("relay pairing link") || e.contains("must not contain credentials") => {
            return ProbeResult::unreachable(false, e)
        }
        Err(_) => return ProbeResult::unreachable(false, "not a valid http(s) daemon URL"),
    };
    let loopback = is_loopback_host(&base);
    let mut url = base;
    url.set_path("/healthz");
    let client = match reqwest::Client::builder().redirect(reqwest::redirect::Policy::none()).timeout(PROBE_TIMEOUT).build() {
        Ok(c) => c,
        Err(_) => return ProbeResult::unreachable(loopback, "could not start the request"),
    };
    let resp = match client.get(url).send().await {
        Ok(r) => r,
        Err(e) => return ProbeResult::unreachable(loopback, describe_request_error(&e)),
    };
    let status = resp.status();
    // Anything that answers HTTP is reachable; whether it is smind is the
    // `service` field's job (the UI refuses anything but "smind").
    let body = resp.json::<Healthz>().await.ok();
    ProbeResult {
        reachable: true,
        version: body.as_ref().and_then(|h| h.version.clone()),
        service: body.as_ref().and_then(|h| h.service.clone()),
        auth_required: body.as_ref().is_some_and(|h| h.auth_required),
        loopback,
        error: (!status.is_success()).then(|| format!("the address answered HTTP {}", status.as_u16())),
    }
}

#[tauri::command]
pub async fn connections_probe(url: String) -> Result<ProbeResult, String> {
    Ok(probe(&url).await)
}

#[cfg(test)]
pub(crate) mod test_support {
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::sync::{Arc, Mutex};

    /// One canned reply per request; records each raw request. Plain std so
    /// the desktop crate needs no HTTP server dev-dependency.
    pub fn stub(replies: Vec<&'static str>) -> (String, Arc<Mutex<Vec<String>>>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap();
        let seen = Arc::new(Mutex::new(Vec::new()));
        let log = seen.clone();
        std::thread::spawn(move || {
            for reply in replies {
                let Ok((mut sock, _)) = listener.accept() else { return };
                let mut buf = vec![0u8; 8192];
                let mut got = 0;
                // Read until the headers (and a short JSON body) have arrived.
                loop {
                    let n = sock.read(&mut buf[got..]).unwrap_or(0);
                    got += n;
                    let text = String::from_utf8_lossy(&buf[..got]).to_string();
                    let done = text.find("\r\n\r\n").is_some_and(|i| {
                        let len = text[..i]
                            .to_ascii_lowercase()
                            .lines()
                            .find_map(|l| l.strip_prefix("content-length:").map(|v| v.trim().parse::<usize>().unwrap_or(0)))
                            .unwrap_or(0);
                        got >= i + 4 + len
                    });
                    if n == 0 || done {
                        break;
                    }
                }
                log.lock().unwrap().push(String::from_utf8_lossy(&buf[..got]).to_string());
                let _ = sock.write_all(reply.as_bytes());
            }
        });
        (format!("http://{addr}"), seen)
    }
}

#[cfg(test)]
mod tests {
    use super::test_support::stub;
    use super::*;

    fn ok_json(body: &'static str) -> String {
        format!("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len())
    }

    fn leak(s: String) -> &'static str {
        Box::leak(s.into_boxed_str())
    }

    #[tokio::test]
    async fn connections_probe_reports_reachable_version_service_and_auth() {
        let (url, seen) = stub(vec![leak(ok_json(r#"{"status":"ok","service":"smind","version":"0.11.1","authRequired":true}"#))]);
        let got = probe(&format!("{url}/some/path?x=1")).await;
        assert_eq!(
            got,
            ProbeResult { reachable: true, version: Some("0.11.1".into()), service: Some("smind".into()), auth_required: true, loopback: true, error: None }
        );
        let req = seen.lock().unwrap()[0].clone();
        assert!(req.starts_with("GET /healthz "), "{req}");
    }

    #[tokio::test]
    async fn connections_probe_rejects_non_smind_healthz() {
        let (url, _) = stub(vec![leak(ok_json(r#"{"service":"nginx","version":"1.0"}"#))]);
        let got = probe(&url).await;
        assert!(got.reachable);
        assert_eq!(got.service.as_deref(), Some("nginx"));
        assert!(!got.auth_required);

        // Not JSON at all: reachable, no service.
        let (url, _) = stub(vec!["HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nhi"]);
        let got = probe(&url).await;
        assert!(got.reachable);
        assert_eq!(got.service, None);
    }

    #[tokio::test]
    async fn connections_probe_unreachable_is_not_an_error() {
        // A port nothing listens on.
        let port = std::net::TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port();
        let got = connections_probe(format!("http://127.0.0.1:{port}")).await.expect("never Err");
        assert!(!got.reachable);
        assert!(got.loopback);
        let err = got.error.expect("an explanation");
        assert!(!err.contains("127.0.0.1"), "{err}");

        let creds = connections_probe("http://user:hunter2@127.0.0.1:1".to_string()).await.unwrap();
        assert!(!creds.reachable);
        assert_eq!(creds.error.as_deref(), Some("a daemon URL must not contain credentials"));

        let bad = connections_probe("ftp://user:hunter2@host".to_string()).await.unwrap();
        assert!(!bad.reachable);
        assert!(!bad.error.unwrap().contains("hunter2"));
        let link = connections_probe("https://relay.example/#offer=abc".to_string()).await.unwrap();
        assert!(link.error.unwrap().contains("relay pairing link"));
    }

    #[test]
    fn loopback_hosts() {
        for (u, want) in [
            ("http://localhost:4648", true),
            ("http://app.localhost", true),
            ("http://127.0.0.1:1", true),
            ("http://[::1]:1", true),
            ("http://192.168.1.5:4648", false),
            ("http://localhost.evil.com", false),
        ] {
            assert_eq!(is_loopback_host(&Url::parse(u).unwrap()), want, "{u}");
        }
    }
}
