//! Per-launch secret + request gate (AC3): a random secret (>=128 bits)
//! generated once per launch, exchanged for an HttpOnly/SameSite=Strict
//! cookie via a `?k=` query param on the first request, after which
//! every other request must carry that cookie or be rejected with 403.
//! Pure logic, no I/O, so it's unit-testable without a running server.

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;

/// The cookie name the proxy sets and checks.
pub const COOKIE_NAME: &str = "smind_desktop";

/// SECRET_BYTES is 32 bytes (256 bits), well over the >=128 bit floor
/// AC3 requires.
const SECRET_BYTES: usize = 32;

/// generate_secret returns a fresh, cryptographically random,
/// base64url (no padding) encoded secret.
pub fn generate_secret() -> String {
    let mut buf = [0u8; SECRET_BYTES];
    getrandom::fill(&mut buf).expect("smind desktop: system RNG unavailable");
    URL_SAFE_NO_PAD.encode(buf)
}

/// constant_time_eq compares two strings without leaking timing
/// information about where they first differ -- used for both the `?k=`
/// exchange and the cookie check, since either is effectively a
/// password compare.
pub fn constant_time_eq(a: &str, b: &str) -> bool {
    let (a, b) = (a.as_bytes(), b.as_bytes());
    if a.len() != b.len() {
        return false;
    }
    let mut diff: u8 = 0;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

/// build_set_cookie renders the `Set-Cookie` header value for `secret`.
pub fn build_set_cookie(secret: &str) -> String {
    format!("{COOKIE_NAME}={secret}; HttpOnly; SameSite=Strict; Path=/")
}

/// extract_cookie pulls this proxy's cookie value out of a raw `Cookie`
/// request header (which may carry other cookies too, `; `-separated).
pub fn extract_cookie(cookie_header: &str) -> Option<&str> {
    cookie_header.split(';').find_map(|pair| {
        let pair = pair.trim();
        let (name, value) = pair.split_once('=')?;
        (name == COOKIE_NAME).then_some(value)
    })
}

/// GateOutcome is what the gate decides for one inbound request, given
/// the per-launch secret, its `Cookie` header (if any), and its `?k=`
/// query param (if any).
#[derive(Debug, PartialEq, Eq)]
pub enum GateOutcome {
    /// No valid cookie and no valid `?k=`: reject with 403. Covers every
    /// request -- assets, `/api/*`, and `/ws` alike.
    Forbidden,
    /// A valid `?k=` on a fresh request: set the cookie and redirect to
    /// the same path with `k` stripped.
    ExchangeSecret,
    /// A valid cookie: let the request through.
    Pass,
}

/// evaluate is the gate's whole decision, factored out of any HTTP
/// framework so it's directly unit-testable.
pub fn evaluate(secret: &str, cookie_header: Option<&str>, query_k: Option<&str>) -> GateOutcome {
    if let Some(cookie_header) = cookie_header {
        if let Some(value) = extract_cookie(cookie_header) {
            if constant_time_eq(value, secret) {
                return GateOutcome::Pass;
            }
        }
    }
    if let Some(k) = query_k {
        if constant_time_eq(k, secret) {
            return GateOutcome::ExchangeSecret;
        }
    }
    GateOutcome::Forbidden
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn secret_is_long_enough_and_url_safe() {
        let s = generate_secret();
        // base64url with no padding: 32 bytes -> 43 chars, each from the
        // URL-safe alphabet (no '+', '/', or '=').
        assert_eq!(s.len(), 43);
        assert!(s.chars().all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_'));
    }

    #[test]
    fn two_secrets_differ() {
        assert_ne!(generate_secret(), generate_secret());
    }

    #[test]
    fn constant_time_eq_matches_and_rejects() {
        assert!(constant_time_eq("abc", "abc"));
        assert!(!constant_time_eq("abc", "abd"));
        assert!(!constant_time_eq("abc", "abcd"));
        assert!(!constant_time_eq("", "a"));
        assert!(constant_time_eq("", ""));
    }

    #[test]
    fn set_cookie_shape() {
        let header = build_set_cookie("s3cr3t");
        assert_eq!(header, "smind_desktop=s3cr3t; HttpOnly; SameSite=Strict; Path=/");
    }

    #[test]
    fn extract_cookie_finds_ours_among_others() {
        assert_eq!(extract_cookie("smind_desktop=abc"), Some("abc"));
        assert_eq!(extract_cookie("foo=bar; smind_desktop=abc; baz=qux"), Some("abc"));
        assert_eq!(extract_cookie("foo=bar"), None);
        assert_eq!(extract_cookie(""), None);
    }

    #[test]
    fn gate_rejects_with_no_cookie_and_no_k() {
        assert_eq!(evaluate("secret", None, None), GateOutcome::Forbidden);
    }

    #[test]
    fn gate_rejects_wrong_cookie() {
        assert_eq!(
            evaluate("secret", Some("smind_desktop=wrong"), None),
            GateOutcome::Forbidden
        );
    }

    #[test]
    fn gate_passes_valid_cookie() {
        assert_eq!(
            evaluate("secret", Some("smind_desktop=secret"), None),
            GateOutcome::Pass
        );
    }

    #[test]
    fn gate_exchanges_valid_k_with_no_cookie() {
        assert_eq!(evaluate("secret", None, Some("secret")), GateOutcome::ExchangeSecret);
    }

    #[test]
    fn gate_rejects_wrong_k() {
        assert_eq!(evaluate("secret", None, Some("wrong")), GateOutcome::Forbidden);
    }

    #[test]
    fn valid_cookie_wins_over_absent_k() {
        assert_eq!(
            evaluate("secret", Some("smind_desktop=secret"), Some("anything-else")),
            GateOutcome::Pass
        );
    }
}
