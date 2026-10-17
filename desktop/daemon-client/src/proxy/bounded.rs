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

/// read_json is `read_limited` + JSON decode, `None` on any failure.
pub async fn read_json<T: serde::de::DeserializeOwned>(resp: reqwest::Response, limit: usize) -> Option<T> {
    serde_json::from_slice(&read_limited(resp, limit).await?).ok()
}
