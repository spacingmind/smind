//! Small, bounded HTTP helpers for decisions made from a daemon's answer
//! (healthz, login): a short timeout, no redirects, and a response body that
//! is read with a limit instead of trusting the peer to be small.

use std::time::Duration;

/// The largest healthz/login body any caller will read.
pub const MAX_BODY_BYTES: usize = 64 * 1024;

/// client is a no-redirect client with an overall `timeout`.
pub fn client(timeout: Duration) -> reqwest::Client {
    reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(timeout)
        .build()
        .unwrap_or_default()
}

/// read_limited reads `resp`'s body, giving up (`None`) as soon as it
/// exceeds `limit` bytes or the read fails.
pub async fn read_limited(mut resp: reqwest::Response, limit: usize) -> Option<Vec<u8>> {
    if resp.content_length().is_some_and(|n| n > limit as u64) {
        return None;
    }
    let mut body = Vec::new();
    while let Ok(Some(chunk)) = resp.chunk().await {
        if body.len() + chunk.len() > limit {
            return None;
        }
        body.extend_from_slice(&chunk);
    }
    Some(body)
}

/// is_password_smind confirms that what answers at `base_url` really is a
/// password-protected smind daemon (`/healthz`: service "smind", authRequired
/// true) before the daemon token file is attached to anything it says.
/// `client` should come from `client()` (short timeout, no redirects).
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
    read_json::<Healthz>(resp, MAX_BODY_BYTES).await.is_some_and(|h| h.service.as_deref() == Some("smind") && h.auth_required)
}

/// read_json is `read_limited` + JSON decode, `None` on any failure.
pub async fn read_json<T: serde::de::DeserializeOwned>(resp: reqwest::Response, limit: usize) -> Option<T> {
    serde_json::from_slice(&read_limited(resp, limit).await?).ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};

    fn serve_once(body: Vec<u8>) -> String {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap();
        std::thread::spawn(move || {
            let Ok((mut sock, _)) = listener.accept() else { return };
            let mut buf = [0u8; 1024];
            let _ = sock.read(&mut buf);
            // Chunked, no Content-Length: the limit must hold without one.
            let _ = sock.write_all(b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n");
            for part in body.chunks(8192) {
                if sock.write_all(format!("{:x}\r\n", part.len()).as_bytes()).is_err()
                    || sock.write_all(part).is_err()
                    || sock.write_all(b"\r\n").is_err()
                {
                    return;
                }
            }
            let _ = sock.write_all(b"0\r\n\r\n");
        });
        format!("http://{addr}/")
    }

    #[tokio::test]
    async fn read_limited_stops_at_the_limit_even_without_content_length() {
        let c = client(Duration::from_secs(3));
        let small = c.get(serve_once(b"{\"a\":1}".to_vec())).send().await.unwrap();
        assert_eq!(read_limited(small, 1024).await.unwrap(), b"{\"a\":1}");
        let big = c.get(serve_once(vec![b'x'; 300 * 1024])).send().await.unwrap();
        assert_eq!(read_limited(big, MAX_BODY_BYTES).await, None);
    }
}
