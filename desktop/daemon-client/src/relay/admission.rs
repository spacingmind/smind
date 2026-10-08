//! Client-side half of relay admission (ADR-0011, protocol v2: SCRAM-style
//! proof), byte-compatible with `internal/relay/admission/admission.go`: the
//! daemon<->relay auth layer, independent of the E2EE handshake. Pure logic only — the actual
//! `AdmitChallenge`/`Admit` RPCs are driven from `crate::relay::client`.

use hmac::{Hmac, KeyInit, Mac};
use sha2::{Digest, Sha256};

/// Admission protocol version. v2 is the SCRAM-style proof; v1 (HMAC keyed by
/// the relay's stored hash) is rejected by the relay.
pub const PROTOCOL_VERSION: u32 = 2;
pub const NONCE_SIZE: usize = 32;

/// hash_secret maps a raw admission secret (SCRAM's ClientKey) to the
/// StoredKey the relay persists — plain SHA-256, matching `HashSecret`
/// (Go): the secret is already 256 random bits, so a slow KDF would only
/// add latency.
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

/// The exact canonical transcript inputs `ProofMask` (Go) hashes, in
/// order.
pub struct Transcript<'a> {
    pub protocol_version: u32,
    pub workspace_id: &'a str,
    pub client_nonce: &'a [u8],
    pub server_nonce: &'a [u8],
    pub daemon_key_id: &'a str,
}

/// proof_mask reproduces `ProofMask` (Go) exactly: HMAC-SHA256(stored_key,
/// BE32(protocol_version) || LP(workspace_id) || client_nonce ||
/// server_nonce || LP(daemon_key_id)), where LP is a 4-byte big-endian
/// length prefix followed by the UTF-8 bytes — length-prefixing is what
/// makes the concatenation unambiguous (a workspace_id/daemon_key_id
/// split can't produce a colliding transcript).
pub fn proof_mask(stored_key: &[u8], t: &Transcript<'_>) -> [u8; 32] {
    let mut mac = Hmac::<Sha256>::new_from_slice(stored_key).expect("HMAC accepts any key length");
    mac.update(&t.protocol_version.to_be_bytes());
    write_lp_string(&mut mac, t.workspace_id);
    mac.update(t.client_nonce);
    mac.update(t.server_nonce);
    write_lp_string(&mut mac, t.daemon_key_id);
    mac.finalize().into_bytes().into()
}

/// compute_proof reproduces `ComputeProof` (Go): the SCRAM-style admission
/// proof `ClientKey XOR HMAC-SHA256(StoredKey, transcript)`, where
/// `ClientKey` is the raw 32-byte workspace secret and `StoredKey =
/// SHA-256(ClientKey)`. The relay persists only `StoredKey`, which is not
/// enough to produce this value.
pub fn compute_proof(secret: &[u8], t: &Transcript<'_>) -> [u8; 32] {
    let mut proof = proof_mask(&hash_secret(secret), t);
    for (i, byte) in proof.iter_mut().enumerate() {
        *byte ^= secret.get(i).copied().unwrap_or(0);
    }
    proof
}

fn write_lp_string(mac: &mut Hmac<Sha256>, s: &str) {
    mac.update(&(s.len() as u32).to_be_bytes());
    mac.update(s.as_bytes());
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Mirrors `TestProofMaskCanonicalForm` (Go): the same byte inputs
    /// must produce the same mask here, and a nonce/string-boundary
    /// shuffle that changes the actual bytes must change the output —
    /// length-prefixing must make the transcript unambiguous.
    #[test]
    fn canonical_form_matches_inputs_go_uses() {
        let key = b"k";
        let t = Transcript {
            protocol_version: PROTOCOL_VERSION,
            workspace_id: "ab",
            client_nonce: &[1, 2, 3],
            server_nonce: &[4, 5, 6],
            daemon_key_id: "c",
        };
        let mac1 = proof_mask(key, &t);
        let mac2 = proof_mask(key, &t);
        assert_eq!(mac1, mac2, "deterministic for identical inputs");

        // Shuffle workspace_id/daemon_key_id so the raw concatenated
        // bytes without length-prefixing would collide ("ab"+"c" vs
        // "a"+"bc"); with length-prefixing they must NOT collide.
        let t2 = Transcript {
            protocol_version: PROTOCOL_VERSION,
            workspace_id: "a",
            client_nonce: &[1, 2, 3],
            server_nonce: &[4, 5, 6],
            daemon_key_id: "bc",
        };
        assert_ne!(proof_mask(key, &t), proof_mask(key, &t2));

        // Swapping which nonce is which must also change the output.
        let t3 = Transcript {
            protocol_version: PROTOCOL_VERSION,
            workspace_id: "ab",
            client_nonce: &[4, 5, 6],
            server_nonce: &[1, 2, 3],
            daemon_key_id: "c",
        };
        assert_ne!(proof_mask(key, &t), proof_mask(key, &t3));
    }

    /// The same fixed vector `TestProofKnownVector` (Go) and
    /// `mobile/src/relay/__tests__/admission.test.ts` assert — a drift in
    /// any one implementation's transcript/XOR construction fails that
    /// implementation's own suite.
    #[test]
    fn proof_matches_cross_language_known_vector() {
        let secret = [0x42u8; 32];
        let t = Transcript {
            protocol_version: PROTOCOL_VERSION,
            workspace_id: "ws-vector",
            client_nonce: &[0x01; NONCE_SIZE],
            server_nonce: &[0x02; NONCE_SIZE],
            daemon_key_id: "key-vector",
        };
        let proof = compute_proof(&secret, &t);
        let hex: String = proof.iter().map(|b| format!("{b:02x}")).collect();
        assert_eq!(
            hex,
            "b007e940ba3c295ffd33e247373d4d150370b3702d90d9a4757d435227b37cfb"
        );
    }

    /// The proof is ClientKey XOR mask(StoredKey), so the relay can recover
    /// ClientKey and check its hash — and the bare mask (the v1 value an
    /// attacker holding only the stored hash could compute) is not a proof.
    #[test]
    fn proof_recovers_client_key_and_is_not_the_bare_mask() {
        let secret = [0x42u8; 32];
        let stored_key = hash_secret(&secret);
        let t = Transcript {
            protocol_version: PROTOCOL_VERSION,
            workspace_id: "ws",
            client_nonce: &[7; NONCE_SIZE],
            server_nonce: &[9; NONCE_SIZE],
            daemon_key_id: "k",
        };
        let mask = proof_mask(&stored_key, &t);
        let proof = compute_proof(&secret, &t);
        assert_ne!(proof, mask);
        let mut recovered = proof;
        for (b, m) in recovered.iter_mut().zip(mask.iter()) {
            *b ^= m;
        }
        assert_eq!(recovered, secret);
        assert_eq!(hash_secret(&recovered), stored_key);
    }

    #[test]
    fn protocol_version_is_2() {
        assert_eq!(PROTOCOL_VERSION, 2);
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
