//! Client-side half of relay admission (ADR-0011), byte-compatible with
//! `internal/relay/admission/admission.go`: the daemon<->relay auth layer,
//! independent of the E2EE handshake. Pure logic only — the actual
//! `AdmitChallenge`/`Admit` RPCs are driven from `crate::relay::client`.

use hmac::{Hmac, KeyInit, Mac};
use sha2::{Digest, Sha256};

pub const PROTOCOL_VERSION: u32 = 1;
pub const NONCE_SIZE: usize = 32;

/// hash_secret maps a raw admission secret to the form presented in the
/// HMAC transcript — plain SHA-256, matching `HashSecret` (Go): the
/// secret is already 256 random bits, so a slow KDF would only add
/// latency.
pub fn hash_secret(secret: &[u8]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(secret);
    hasher.finalize().into()
}

pub fn random_nonce() -> [u8; NONCE_SIZE] {
    let mut nonce = [0u8; NONCE_SIZE];
    getrandom::fill(&mut nonce).expect("getrandom: admission nonce");
    nonce
}

/// The exact canonical transcript inputs `ComputeHMAC` (Go) hashes, in
/// order.
pub struct Transcript<'a> {
    pub protocol_version: u32,
    pub workspace_id: &'a str,
    pub client_nonce: &'a [u8],
    pub server_nonce: &'a [u8],
    pub daemon_key_id: &'a str,
}

/// compute_hmac reproduces `ComputeHMAC` (Go) exactly: HMAC-SHA256(key,
/// BE32(protocol_version) || LP(workspace_id) || client_nonce ||
/// server_nonce || LP(daemon_key_id)), where LP is a 4-byte big-endian
/// length prefix followed by the UTF-8 bytes — length-prefixing is what
/// makes the concatenation unambiguous (a workspace_id/daemon_key_id
/// split can't produce a colliding transcript).
pub fn compute_hmac(key: &[u8], t: &Transcript<'_>) -> [u8; 32] {
    let mut mac = Hmac::<Sha256>::new_from_slice(key).expect("HMAC accepts any key length");
    mac.update(&t.protocol_version.to_be_bytes());
    write_lp_string(&mut mac, t.workspace_id);
    mac.update(t.client_nonce);
    mac.update(t.server_nonce);
    write_lp_string(&mut mac, t.daemon_key_id);
    mac.finalize().into_bytes().into()
}

fn write_lp_string(mac: &mut Hmac<Sha256>, s: &str) {
    mac.update(&(s.len() as u32).to_be_bytes());
    mac.update(s.as_bytes());
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Mirrors `TestComputeHMACCanonicalForm` (Go): the same byte inputs
    /// must produce the same HMAC here, and a nonce/string-boundary
    /// shuffle that changes the actual bytes must change the output —
    /// length-prefixing must make the transcript unambiguous.
    #[test]
    fn canonical_form_matches_inputs_go_uses() {
        let key = b"k";
        let t = Transcript {
            protocol_version: 1,
            workspace_id: "ab",
            client_nonce: &[1, 2, 3],
            server_nonce: &[4, 5, 6],
            daemon_key_id: "c",
        };
        let mac1 = compute_hmac(key, &t);
        let mac2 = compute_hmac(key, &t);
        assert_eq!(mac1, mac2, "deterministic for identical inputs");

        // Shuffle workspace_id/daemon_key_id so the raw concatenated
        // bytes without length-prefixing would collide ("ab"+"c" vs
        // "a"+"bc"); with length-prefixing they must NOT collide.
        let t2 = Transcript {
            protocol_version: 1,
            workspace_id: "a",
            client_nonce: &[1, 2, 3],
            server_nonce: &[4, 5, 6],
            daemon_key_id: "bc",
        };
        assert_ne!(compute_hmac(key, &t), compute_hmac(key, &t2));

        // Swapping which nonce is which must also change the output.
        let t3 = Transcript {
            protocol_version: 1,
            workspace_id: "ab",
            client_nonce: &[4, 5, 6],
            server_nonce: &[1, 2, 3],
            daemon_key_id: "c",
        };
        assert_ne!(compute_hmac(key, &t), compute_hmac(key, &t3));
    }

    #[test]
    fn hash_secret_is_sha256() {
        let secret = [0x42u8; 32];
        let got = hash_secret(&secret);
        let mut hasher = Sha256::new();
        hasher.update(secret);
        let want: [u8; 32] = hasher.finalize().into();
        assert_eq!(got, want);
    }

    #[test]
    fn nonces_are_random_and_correctly_sized() {
        let a = random_nonce();
        let b = random_nonce();
        assert_eq!(a.len(), NONCE_SIZE);
        assert_ne!(a, b);
    }
}
