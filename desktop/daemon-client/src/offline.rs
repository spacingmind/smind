//! Query-string formatting for `desktop/ui/offline.html`'s live retry
//! state (quick-wins AC5). Delivered to the already-loaded page via
//! `WebviewWindow::eval` (not a re-navigate: the local asset origin
//! differs by platform -- `tauri://localhost` on Linux/macOS,
//! `http://tauri.localhost` on Windows -- so eval-ing a small update
//! into the current page avoids depending on that), but shaped exactly
//! like a URL query string so the page parses it with the same
//! `URLSearchParams` it already needs for a plain reload.

use url::Url;

pub struct OfflineState {
    pub daemon_url: String,
    pub attempt: u32,
    pub next_retry_secs: u64,
}

impl OfflineState {
    /// to_query_string returns the `?...` suffix (including the leading
    /// `?`), percent-encoded via the `url` crate so a daemon URL's `:`
    /// and `/` survive `URLSearchParams` parsing on the other side.
    pub fn to_query_string(&self) -> String {
        let mut url = Url::parse("http://offline.invalid/").unwrap();
        url.query_pairs_mut()
            .append_pair("daemon", &self.daemon_url)
            .append_pair("attempt", &self.attempt.to_string())
            .append_pair("next", &self.next_retry_secs.to_string());
        format!("?{}", url.query().unwrap_or_default())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn formats_query_string() {
        let s = OfflineState {
            daemon_url: "http://127.0.0.1:4648".to_string(),
            attempt: 3,
            next_retry_secs: 2,
        };
        assert_eq!(s.to_query_string(), "?daemon=http%3A%2F%2F127.0.0.1%3A4648&attempt=3&next=2");
    }

    #[test]
    fn parseable_by_url_query_pairs() {
        let s = OfflineState { daemon_url: "http://localhost:9999".to_string(), attempt: 1, next_retry_secs: 2 };
        let q = s.to_query_string();
        let parsed = Url::parse(&format!("http://offline.invalid/{q}")).unwrap();
        let pairs: std::collections::HashMap<_, _> = parsed.query_pairs().into_owned().collect();
        assert_eq!(pairs.get("daemon").map(String::as_str), Some("http://localhost:9999"));
        assert_eq!(pairs.get("attempt").map(String::as_str), Some("1"));
        assert_eq!(pairs.get("next").map(String::as_str), Some("2"));
    }
}
