//! Pairing offer: parses the same pairing URL format
//! `mobile/src/relay/pairing.ts` accepts and `internal/relay/pairing`
//! (Go) produces — `<base>#offer=<base64url(JSON)>` — entirely in Rust,
//! with no network round trip needed to validate a pasted URL.
//!
//! Wire-compatible with `internal/relay/pairing/offer.go`: same JSON key
//! names, same unpadded base64url encoding, same "fragment only, never a
//! query string" rule (a URL fragment is never sent to a server).

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use serde::{Deserialize, Serialize};

pub const OFFER_VERSION: i64 = 1;
pub const FRAGMENT_KEY: &str = "offer";
pub const PUBLIC_KEY_SIZE: usize = 32;
pub const DEFAULT_PAIR_URL: &str = "https://spacingmind.sh/pair";

#[derive(Debug, PartialEq, Eq)]
pub enum PairingError {
    /// A JSON/base64/URL syntax error, or a field with the wrong shape.
    Malformed(String),
    /// Well-formed but missing something pairing needs (daemon id, a
    /// valid 32-byte public key, or a parseable relay URL).
    Invalid(String),
    /// The offer's `v` field isn't `OFFER_VERSION`.
    UnsupportedVersion(i64),
}

impl std::fmt::Display for PairingError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            PairingError::Malformed(s) => write!(f, "pairing: invalid offer: {s}"),
            PairingError::Invalid(s) => write!(f, "pairing: invalid offer: {s}"),
            PairingError::UnsupportedVersion(v) => {
                write!(f, "pairing: invalid offer: unsupported offer version {v}")
            }
        }
    }
}

impl std::error::Error for PairingError {}

/// Offer is what a device needs to start an E2EE session with a daemon,
/// mirroring `internal/relay/pairing.Offer` field for field.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Offer {
    /// The admission daemon-key-id (NOT the relay workspace id).
    pub daemon_id: String,
    /// The daemon's long-lived X25519 public key, 32 raw bytes.
    pub public_key: [u8; 32],
    /// The relay's grpc-web endpoint URL, e.g. `https://host:7401` — see
    /// `crate::relay::client::derive_native_grpc_address` for why the
    /// desktop client doesn't dial this address directly.
    pub relay: String,
    /// Relay workspace id this device must Admit under.
    pub workspace_id: String,
    /// Hex SHA-256 of the relay's pinned TLS cert, empty if unset.
    pub relay_fingerprint: String,
    /// Raw workspace admission secret.
    pub secret: Vec<u8>,
}

#[derive(Serialize, Deserialize)]
struct WireOffer {
    v: i64,
    id: String,
    pk: String,
    relay: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    fp: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    sec: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    ws: String,
}

impl Offer {
    /// validate mirrors `Offer.Validate` (Go): non-empty daemon id, a
    /// valid 32-byte public key (trivially true here, the type already
    /// enforces the length) and a non-empty, URL-parseable relay
    /// endpoint. `workspace_id`/`relay_fingerprint`/`secret` are not
    /// required here either, matching Go's back-compat note — but a real
    /// relay connection can't Admit without a workspace id and secret.
    pub fn validate(&self) -> Result<(), PairingError> {
        if self.daemon_id.trim().is_empty() {
            return Err(PairingError::Invalid("missing daemon ID".into()));
        }
        if self.relay.trim().is_empty() {
            return Err(PairingError::Invalid("missing relay endpoint".into()));
        }
        url::Url::parse(&self.relay)
            .map_err(|e| PairingError::Invalid(format!("relay endpoint {:?}: {e}", self.relay)))?;
        Ok(())
    }

    /// encode_payload returns the unpadded base64url JSON blob that goes
    /// after `#offer=`.
    pub fn encode_payload(&self) -> Result<String, PairingError> {
        self.validate()?;
        let wire = WireOffer {
            v: OFFER_VERSION,
            id: self.daemon_id.clone(),
            pk: URL_SAFE_NO_PAD.encode(self.public_key),
            relay: self.relay.clone(),
            fp: self.relay_fingerprint.clone(),
            sec: URL_SAFE_NO_PAD.encode(&self.secret),
            ws: self.workspace_id.clone(),
        };
        let data = serde_json::to_vec(&wire)
            .map_err(|e| PairingError::Malformed(format!("encode offer: {e}")))?;
        Ok(URL_SAFE_NO_PAD.encode(data))
    }

    /// decode_payload parses a base64url offer payload, mirroring
    /// `DecodePayload` (Go).
    pub fn decode_payload(payload: &str) -> Result<Offer, PairingError> {
        let data = URL_SAFE_NO_PAD
            .decode(payload)
            .map_err(|e| PairingError::Malformed(format!("decode payload: {e}")))?;
        let wire: WireOffer = serde_json::from_slice(&data)
            .map_err(|e| PairingError::Malformed(format!("parse payload: {e}")))?;
        if wire.v != OFFER_VERSION {
            return Err(PairingError::UnsupportedVersion(wire.v));
        }
        let pk = URL_SAFE_NO_PAD
            .decode(&wire.pk)
            .map_err(|e| PairingError::Malformed(format!("decode public key: {e}")))?;
        let public_key: [u8; PUBLIC_KEY_SIZE] = pk.try_into().map_err(|pk: Vec<u8>| {
            PairingError::Invalid(format!(
                "e2ee: invalid X25519 public key: got {} bytes, want {PUBLIC_KEY_SIZE}",
                pk.len()
            ))
        })?;
        let secret = if wire.sec.is_empty() {
            Vec::new()
        } else {
            URL_SAFE_NO_PAD
                .decode(&wire.sec)
                .map_err(|e| PairingError::Malformed(format!("decode secret: {e}")))?
        };
        let offer = Offer {
            daemon_id: wire.id,
            public_key,
            relay: wire.relay,
            relay_fingerprint: wire.fp,
            secret,
            workspace_id: wire.ws,
        };
        offer.validate()?;
        Ok(offer)
    }

    /// url renders the offer as a pairing deep link, payload in the
    /// fragment. An empty base uses DEFAULT_PAIR_URL.
    pub fn url(&self, base: &str) -> Result<String, PairingError> {
        let base = if base.trim().is_empty() {
            DEFAULT_PAIR_URL
        } else {
            base
        };
        if base.contains('#') {
            return Err(PairingError::Invalid(format!(
                "base URL {base:?} already has a fragment"
            )));
        }
        let payload = self.encode_payload()?;
        Ok(format!("{base}#{FRAGMENT_KEY}={payload}"))
    }

    /// parse_url extracts an offer from a pairing deep link. Only the
    /// fragment is consulted — an offer smuggled into the query string
    /// is rejected, mirroring `ParseURL` (Go).
    pub fn parse_url(raw: &str) -> Result<Offer, PairingError> {
        let fragment = raw
            .split_once('#')
            .map(|(_, frag)| frag)
            .ok_or_else(|| PairingError::Invalid("URL has no fragment".into()))?;
        let payload = url::form_urlencoded::parse(fragment.as_bytes())
            .find(|(k, _)| k == FRAGMENT_KEY)
            .map(|(_, v)| v.into_owned())
            .ok_or_else(|| {
                PairingError::Invalid(format!("URL has no {FRAGMENT_KEY:?} fragment parameter"))
            })?;
        Offer::decode_payload(&payload)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Exact fixture from internal/relay/pairing/fixture_test.go, also
    // hardcoded in mobile/src/relay/__tests__/pairing.test.ts — proves
    // this parser/encoder is wire-compatible with both, not just
    // internally consistent.
    const FIXTURE_OFFER_URL: &str = "https://spacingmind.sh/pair#offer=eyJ2IjoxLCJpZCI6ImRhZW1vbi1maXh0dXJlLTEiLCJwayI6IkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkUiLCJyZWxheSI6Imh0dHBzOi8vcmVsYXkuZXhhbXBsZS50ZXN0Ojc0MDEiLCJmcCI6ImRlYWRiZWVmIiwic2VjIjoicTZ1cnE2dXJxNnVycTZ1cnE2dXJxNnVycTZ1cnE2dXJxNnVycTZ1cnE2cyIsIndzIjoid3MtZml4dHVyZS0xIn0";

    fn fixture_offer() -> Offer {
        Offer {
            daemon_id: "daemon-fixture-1".into(),
            public_key: [0x11; 32],
            relay: "https://relay.example.test:7401".into(),
            relay_fingerprint: "deadbeef".into(),
            secret: vec![0xab; 32],
            workspace_id: "ws-fixture-1".into(),
        }
    }

    #[test]
    fn encodes_the_exact_go_fixture_url() {
        let offer = fixture_offer();
        assert_eq!(offer.url("").unwrap(), FIXTURE_OFFER_URL);
    }

    #[test]
    fn parses_the_exact_go_fixture_url() {
        let got = Offer::parse_url(FIXTURE_OFFER_URL).unwrap();
        assert_eq!(got, fixture_offer());
    }

    #[test]
    fn round_trips() {
        let offer = fixture_offer();
        let url = offer.url("https://example.com/p").unwrap();
        let got = Offer::parse_url(&url).unwrap();
        assert_eq!(got, offer);
    }

    #[test]
    fn rejects_wrong_version() {
        let mut offer = fixture_offer();
        offer.workspace_id = "x".into();
        let payload = offer.encode_payload().unwrap();
        let data = URL_SAFE_NO_PAD.decode(&payload).unwrap();
        let mut v: serde_json::Value = serde_json::from_slice(&data).unwrap();
        v["v"] = serde_json::json!(2);
        let bad_payload = URL_SAFE_NO_PAD.encode(serde_json::to_vec(&v).unwrap());
        let err = Offer::decode_payload(&bad_payload).unwrap_err();
        assert_eq!(err, PairingError::UnsupportedVersion(2));
    }

    #[test]
    fn rejects_missing_fields() {
        let mut offer = fixture_offer();
        offer.daemon_id = "".into();
        assert!(offer.encode_payload().is_err());

        offer = fixture_offer();
        offer.relay = "".into();
        assert!(offer.encode_payload().is_err());
    }

    #[test]
    fn rejects_short_public_key() {
        // A short/garbage `pk` field can't decode to [u8; 32].
        let payload = URL_SAFE_NO_PAD.encode(
            serde_json::to_vec(&serde_json::json!({
                "v": 1,
                "id": "d",
                "pk": URL_SAFE_NO_PAD.encode([1u8, 2, 3]),
                "relay": "https://relay.example.test:7401",
            }))
            .unwrap(),
        );
        assert!(Offer::decode_payload(&payload).is_err());
    }

    #[test]
    fn rejects_offer_in_query_string_not_fragment() {
        let offer = fixture_offer();
        let payload = offer.encode_payload().unwrap();
        let url = format!("https://spacingmind.sh/pair?offer={payload}");
        assert!(Offer::parse_url(&url).is_err());
    }
}
