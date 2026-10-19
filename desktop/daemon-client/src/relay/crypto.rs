//! E2EE v2 session crypto and wire framing (ADR-0035), byte-compatible with
//! `internal/relay/e2ee/{handshake,session}.go` (Go): a Noise-NK-shaped
//! handshake (`es` and `ee` X25519 outputs, HKDF-SHA256 key schedule with the
//! transcript hash as salt, HMAC key confirmation) and ChaCha20-Poly1305
//! (IETF, RFC 8439) AEAD with 12-byte counter-based nonces.
//!
//! The relay itself never sees any of this: it forwards opaque
//! ciphertext frames between the two ends. There is no older protocol
//! version in this module and no negotiation (ADR-0035 §6).

use chacha20poly1305::aead::Aead;
use chacha20poly1305::{ChaCha20Poly1305, KeyInit, Nonce};
use hkdf::Hkdf;
use hmac::{Hmac, Mac};
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;
use x25519_dalek::{PublicKey, StaticSecret};
use zeroize::Zeroizing;

/// The E2EE wire protocol version. Both ends must match; no negotiation.
pub const PROTOCOL_VERSION: u8 = 2;
/// Bounds a single frame so a malformed length prefix can't force an
/// arbitrarily large allocation.
pub const MAX_FRAME_LEN: usize = 1 << 20;
/// Bounds a frame (type byte plus payload) while a handshake is in progress:
/// the largest legitimate one is M2 (1 + 66 bytes).
pub const MAX_HANDSHAKE_FRAME_LEN: usize = 128;
pub const PUBLIC_KEY_SIZE: usize = 32;
/// M1: version (1) + role (1) + client ephemeral public key (32).
pub const CLIENT_HELLO_LEN: usize = 2 + PUBLIC_KEY_SIZE;
/// M2: version (1) + role (1) + daemon public key (32) + daemon ephemeral (32).
pub const DAEMON_HELLO_LEN: usize = 2 + 2 * PUBLIC_KEY_SIZE;
/// R_d / R_c: an HMAC-SHA256 tag.
pub const READY_LEN: usize = 32;

pub const FRAME_HELLO: u8 = 0x01;
pub const FRAME_READY: u8 = 0x02;
pub const FRAME_DATA: u8 = 0x03;

const LABEL: &str = "smind relay e2ee v2";
const INFO_D2M: &str = "smind relay e2ee v2 daemon->mobile";
const INFO_M2D: &str = "smind relay e2ee v2 mobile->daemon";
const INFO_CONFIRM_D: &str = "smind relay e2ee v2 confirm daemon";
const INFO_CONFIRM_M: &str = "smind relay e2ee v2 confirm mobile";

/// Which end of the session this is. The two ends must differ; the role
/// selects which directional key is used for sending. Every client is
/// `Mobile` (the initiator); the daemon is the responder.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Role {
    Daemon = 1,
    Mobile = 2,
}

/// A per-handshake X25519 secret. There is deliberately no public way to hand
/// one to a channel: `Channel::handshake` draws a fresh one every call, so an
/// initiator key can never be reused across sessions (ADR-0035 §3). The
/// scalar is wiped on drop.
pub(crate) struct EphemeralSecret {
    secret: StaticSecret,
    public: PublicKey,
}

impl EphemeralSecret {
    pub(crate) fn generate() -> Self {
        let mut seed = Zeroizing::new([0u8; 32]);
        getrandom::fill(&mut seed[..]).expect("getrandom: generate X25519 key");
        Self::from_seed(*seed)
    }

    pub(crate) fn from_seed(seed: [u8; 32]) -> Self {
        let secret = StaticSecret::from(seed);
        let public = PublicKey::from(&secret);
        Self { secret, public }
    }

    pub(crate) fn public(&self) -> [u8; PUBLIC_KEY_SIZE] {
        self.public.to_bytes()
    }

    /// X25519 with the RFC 7748 §6.1 all-zero-output check:
    /// x25519-dalek does not error on a low-order peer point, so it is
    /// refused here, explicitly.
    pub(crate) fn dh(
        &self,
        peer: &[u8; PUBLIC_KEY_SIZE],
    ) -> Result<Zeroizing<[u8; 32]>, KeyAgreementError> {
        let shared = self.secret.diffie_hellman(&PublicKey::from(*peer));
        if !shared.was_contributory() {
            return Err(KeyAgreementError);
        }
        Ok(Zeroizing::new(shared.to_bytes()))
    }
}

/// An X25519 exchange produced the all-zero output (a low-order peer point).
#[derive(Debug, PartialEq, Eq)]
pub struct KeyAgreementError;

impl std::fmt::Display for KeyAgreementError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "e2ee: X25519 key agreement failed")
    }
}

impl std::error::Error for KeyAgreementError {}

#[derive(Debug, PartialEq, Eq)]
pub enum SessionError {
    /// The frame's counter didn't match the next expected counter —
    /// either a replay (repeat) or a reordered/dropped frame (skip).
    Replay { got: u64, want: u64 },
    /// AEAD authentication/decryption failed.
    Decrypt,
    /// This side's send counter reached `u64::MAX`; a new session is
    /// required.
    CounterExhausted,
}

impl std::fmt::Display for SessionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            SessionError::Replay { got, want } => {
                write!(
                    f,
                    "e2ee: frame counter replayed or out of order: got counter {got}, want {want}"
                )
            }
            SessionError::Decrypt => write!(f, "e2ee: frame failed to decrypt"),
            SessionError::CounterExhausted => write!(f, "e2ee: frame counter exhausted"),
        }
    }
}

impl std::error::Error for SessionError {}

/// SHA-256(label ‖ daemon_pub ‖ daemon_eph_pub ‖ client_eph_pub): the HKDF salt
/// of every derived key, and the value the device hello signature binds to
/// (Go: `e2ee.TranscriptHash`). Public data.
pub fn transcript_hash(
    daemon_pub: &[u8; PUBLIC_KEY_SIZE],
    daemon_eph_pub: &[u8; PUBLIC_KEY_SIZE],
    client_eph_pub: &[u8; PUBLIC_KEY_SIZE],
) -> [u8; 32] {
    let mut transcript = Sha256::new();
    transcript.update(LABEL.as_bytes());
    transcript.update(daemon_pub);
    transcript.update(daemon_eph_pub);
    transcript.update(client_eph_pub);
    transcript.finalize().into()
}

/// Everything the v2 key schedule (ADR-0035 §2) derives from one handshake.
/// Only `th` is public; the four keys are wiped on drop.
pub struct Schedule {
    pub th: [u8; 32],
    pub d2m: Zeroizing<[u8; 32]>,
    pub m2d: Zeroizing<[u8; 32]>,
    pub confirm_d: Zeroizing<[u8; 32]>,
    pub confirm_m: Zeroizing<[u8; 32]>,
}

/// Runs the key schedule from the two DH outputs (`es` = DH(client eph,
/// daemon static), `ee` = DH(client eph, daemon eph)). Both ends call it with
/// the same inputs, so they arrive at the same keys.
pub fn derive_schedule(
    es: &[u8; 32],
    ee: &[u8; 32],
    daemon_pub: &[u8; PUBLIC_KEY_SIZE],
    daemon_eph_pub: &[u8; PUBLIC_KEY_SIZE],
    client_eph_pub: &[u8; PUBLIC_KEY_SIZE],
) -> Schedule {
    let th = transcript_hash(daemon_pub, daemon_eph_pub, client_eph_pub);
    let mut ikm = Zeroizing::new([0u8; 64]);
    ikm[..32].copy_from_slice(es);
    ikm[32..].copy_from_slice(ee);
    let hk = Hkdf::<Sha256>::new(Some(&th), &ikm[..]);
    let expand = |info: &str| {
        let mut out = Zeroizing::new([0u8; 32]);
        hk.expand(info.as_bytes(), &mut out[..])
            .expect("HKDF expand: 32 bytes is a valid SHA-256 output length");
        out
    };
    Schedule {
        th,
        d2m: expand(INFO_D2M),
        m2d: expand(INFO_M2D),
        confirm_d: expand(INFO_CONFIRM_D),
        confirm_m: expand(INFO_CONFIRM_M),
    }
}

/// The key-confirmation MAC: HMAC-SHA256(confirm key, th).
pub fn confirm_tag(confirm_key: &[u8; 32], th: &[u8; 32]) -> [u8; 32] {
    let mut mac = Hmac::<Sha256>::new_from_slice(confirm_key).expect("HMAC accepts any key length");
    mac.update(th);
    mac.finalize().into_bytes().into()
}

#[derive(Debug, PartialEq, Eq)]
pub enum ReadyError {
    /// An empty ready (the retired v1 shape) or any length other than 32.
    WrongLength(usize),
    /// The MAC did not verify: the two ends did not derive the same keys.
    Mismatch,
}

/// Verifies a ready payload against the expected tag in constant time.
pub fn check_ready(payload: &[u8], expected: &[u8; 32]) -> Result<(), ReadyError> {
    if payload.len() != READY_LEN {
        return Err(ReadyError::WrongLength(payload.len()));
    }
    if bool::from(payload.ct_eq(expected)) {
        Ok(())
    } else {
        Err(ReadyError::Mismatch)
    }
}

/// Holds the two directional ChaCha20-Poly1305 keys derived by the key
/// schedule, plus the per-direction counters that supply nonces and provide
/// replay protection.
///
/// Nonces are never random: each direction's 12-byte nonce is a 4-byte
/// zero prefix followed by the big-endian frame counter, so no nonce is
/// ever reused under a key as long as the counter is monotonic — the
/// same property that makes replayed frames detectable. Keys are unique per
/// handshake (both ends contribute a fresh ephemeral), so restarting the
/// counters at zero in every session never repeats a (key, nonce).
pub struct Session {
    send: ChaCha20Poly1305,
    recv: ChaCha20Poly1305,
    send_counter: u64,
    recv_counter: u64,
}

impl Session {
    pub fn new(role: Role, sched: &Schedule) -> Self {
        let (send_key, recv_key) = if role == Role::Mobile {
            (&sched.m2d, &sched.d2m)
        } else {
            (&sched.d2m, &sched.m2d)
        };
        Session {
            send: ChaCha20Poly1305::new((&**send_key).into()),
            recv: ChaCha20Poly1305::new((&**recv_key).into()),
            send_counter: 0,
            recv_counter: 0,
        }
    }

    /// Encrypts plaintext with the next outbound counter, returning that
    /// counter and the ciphertext.
    pub fn seal(&mut self, plaintext: &[u8]) -> Result<(u64, Vec<u8>), SessionError> {
        if self.send_counter == u64::MAX {
            return Err(SessionError::CounterExhausted);
        }
        let counter = self.send_counter;
        let nonce = counter_nonce(counter);
        let ciphertext = self
            .send
            .encrypt(&Nonce::from(nonce), plaintext)
            .map_err(|_| SessionError::Decrypt)?;
        self.send_counter += 1;
        Ok((counter, ciphertext))
    }

    /// Decrypts a frame, rejecting any counter that isn't exactly the
    /// next one expected from the peer.
    pub fn open(&mut self, counter: u64, ciphertext: &[u8]) -> Result<Vec<u8>, SessionError> {
        if self.recv_counter == u64::MAX {
            return Err(SessionError::CounterExhausted);
        }
        if counter != self.recv_counter {
            return Err(SessionError::Replay {
                got: counter,
                want: self.recv_counter,
            });
        }
        let nonce = counter_nonce(counter);
        let plaintext = self
            .recv
            .decrypt(&Nonce::from(nonce), ciphertext)
            .map_err(|_| SessionError::Decrypt)?;
        self.recv_counter += 1;
        Ok(plaintext)
    }
}

/// Renders a frame counter as a 12-byte ChaCha20-Poly1305 nonce. The
/// leading 4 bytes stay zero (reserved for a future sub-stream id).
fn counter_nonce(counter: u64) -> [u8; 12] {
    let mut nonce = [0u8; 12];
    nonce[4..].copy_from_slice(&counter.to_be_bytes());
    nonce
}

/// Encodes one wire frame: `[4B BE length][1B type][payload]`, where
/// length covers the type byte plus payload — matching
/// `Channel.writeFrame` (Go) exactly, byte for byte.
pub fn encode_frame(frame_type: u8, payload: &[u8]) -> Vec<u8> {
    let mut frame = Vec::with_capacity(4 + 1 + payload.len());
    let len = (1 + payload.len()) as u32;
    frame.extend_from_slice(&len.to_be_bytes());
    frame.push(frame_type);
    frame.extend_from_slice(payload);
    frame
}

#[derive(Debug, PartialEq, Eq)]
pub enum FrameError {
    Truncated,
    ZeroLength,
    TooLarge(u32),
}

impl std::fmt::Display for FrameError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            FrameError::Truncated => write!(f, "e2ee: protocol violation: truncated frame"),
            FrameError::ZeroLength => write!(f, "e2ee: protocol violation: zero-length frame"),
            FrameError::TooLarge(len) => {
                write!(f, "e2ee: protocol violation: frame length {len} exceeds the {MAX_FRAME_LEN} byte limit")
            }
        }
    }
}

impl std::error::Error for FrameError {}

/// Decodes exactly one complete wire frame from `buf` (the entire
/// `relaypb.Frame.payload`, which always carries exactly one whole e2ee
/// frame — `frameConn.Write` in Go never splits or coalesces frames
/// across relay envelopes). Returns `(type, payload)`.
pub fn decode_frame(buf: &[u8]) -> Result<(u8, &[u8]), FrameError> {
    // Like Go's readFrame, judge the 4-byte length header first: a bare
    // header with length 0 or over the limit is a violation, not a truncation.
    if buf.len() < 4 {
        return Err(FrameError::Truncated);
    }
    let len = u32::from_be_bytes(buf[0..4].try_into().unwrap());
    if len == 0 {
        return Err(FrameError::ZeroLength);
    }
    if len as usize > MAX_FRAME_LEN {
        return Err(FrameError::TooLarge(len));
    }
    if buf.len() != 4 + len as usize {
        return Err(FrameError::Truncated);
    }
    Ok((buf[4], &buf[5..]))
}

/// Builds M1, the client hello payload (version, role, client ephemeral).
pub fn client_hello_payload(client_eph_pub: &[u8; PUBLIC_KEY_SIZE]) -> [u8; CLIENT_HELLO_LEN] {
    let mut payload = [0u8; CLIENT_HELLO_LEN];
    payload[0] = PROTOCOL_VERSION;
    payload[1] = Role::Mobile as u8;
    payload[2..].copy_from_slice(client_eph_pub);
    payload
}

/// Builds M2, the daemon hello payload (version, role, static key, ephemeral).
pub fn daemon_hello_payload(
    daemon_pub: &[u8; PUBLIC_KEY_SIZE],
    daemon_eph_pub: &[u8; PUBLIC_KEY_SIZE],
) -> [u8; DAEMON_HELLO_LEN] {
    let mut payload = [0u8; DAEMON_HELLO_LEN];
    payload[0] = PROTOCOL_VERSION;
    payload[1] = Role::Daemon as u8;
    payload[2..2 + PUBLIC_KEY_SIZE].copy_from_slice(daemon_pub);
    payload[2 + PUBLIC_KEY_SIZE..].copy_from_slice(daemon_eph_pub);
    payload
}

#[derive(Debug, PartialEq, Eq)]
pub enum HelloError {
    Empty,
    /// The peer's version byte, read before length or role so a v1 hello
    /// (whatever its shape) is reported as such.
    UnsupportedVersion(u8),
    WrongLength {
        got: usize,
        want: usize,
    },
    WrongRole {
        got: u8,
        want: Role,
    },
}

impl std::fmt::Display for HelloError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            HelloError::Empty => write!(f, "empty hello"),
            HelloError::UnsupportedVersion(v) => write!(
                f,
                "peer speaks protocol version {v}, this end speaks {PROTOCOL_VERSION}"
            ),
            HelloError::WrongLength { got, want } => write!(f, "hello is {got} bytes, want {want}"),
            HelloError::WrongRole { got, want } => {
                write!(f, "hello carries role {got}, want {want:?}")
            }
        }
    }
}

impl std::error::Error for HelloError {}

/// Reads the version byte first, before length or role.
fn check_version(payload: &[u8]) -> Result<(), HelloError> {
    match payload.first() {
        None => Err(HelloError::Empty),
        Some(&v) if v != PROTOCOL_VERSION => Err(HelloError::UnsupportedVersion(v)),
        Some(_) => Ok(()),
    }
}

fn check_shape(payload: &[u8], want_len: usize, want_role: Role) -> Result<(), HelloError> {
    check_version(payload)?;
    if payload.len() != want_len {
        return Err(HelloError::WrongLength {
            got: payload.len(),
            want: want_len,
        });
    }
    if payload[1] != want_role as u8 {
        return Err(HelloError::WrongRole {
            got: payload[1],
            want: want_role,
        });
    }
    Ok(())
}

/// Parses and validates M1 and returns the client ephemeral public key.
pub fn parse_client_hello(payload: &[u8]) -> Result<[u8; PUBLIC_KEY_SIZE], HelloError> {
    check_shape(payload, CLIENT_HELLO_LEN, Role::Mobile)?;
    Ok(payload[2..].try_into().expect("length checked"))
}

/// Parses and validates M2 and returns the daemon static and ephemeral
/// public keys.
#[allow(clippy::type_complexity)]
pub fn parse_daemon_hello(
    payload: &[u8],
) -> Result<([u8; PUBLIC_KEY_SIZE], [u8; PUBLIC_KEY_SIZE]), HelloError> {
    check_shape(payload, DAEMON_HELLO_LEN, Role::Daemon)?;
    Ok((
        payload[2..2 + PUBLIC_KEY_SIZE]
            .try_into()
            .expect("length checked"),
        payload[2 + PUBLIC_KEY_SIZE..]
            .try_into()
            .expect("length checked"),
    ))
}

/// The shared, Go-generated vector file (ADR-0035 §7). Never edited here.
#[cfg(test)]
pub(crate) mod testvec {
    use serde_json::Value;

    pub fn load() -> Value {
        serde_json::from_str(include_str!(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../internal/relay/e2ee/testdata/e2ee_v2_vectors.json"
        )))
        .expect("e2ee_v2_vectors.json parses")
    }

    pub fn hex_decode(s: &str) -> Vec<u8> {
        assert!(s.len().is_multiple_of(2), "odd-length hex {s:?}");
        (0..s.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap())
            .collect()
    }

    pub fn hex_encode(b: &[u8]) -> String {
        b.iter().map(|x| format!("{x:02x}")).collect()
    }

    pub fn bytes(v: &Value, key: &str) -> Vec<u8> {
        hex_decode(v[key].as_str().unwrap_or_else(|| panic!("missing {key}")))
    }

    pub fn arr32(v: &Value, key: &str) -> [u8; 32] {
        bytes(v, key)
            .try_into()
            .unwrap_or_else(|_| panic!("{key} is not 32 bytes"))
    }

    pub fn seed(v: &Value, key: &str) -> [u8; 32] {
        arr32(&v["seeds"], key)
    }
}

#[cfg(test)]
mod tests {
    use super::testvec::*;
    use super::*;

    struct Ref {
        client: EphemeralSecret,
        daemon_static: EphemeralSecret,
        daemon_eph: EphemeralSecret,
    }

    fn reference(v: &serde_json::Value, daemon_eph_seed: &str) -> Ref {
        Ref {
            client: EphemeralSecret::from_seed(seed(v, "client_eph")),
            daemon_static: EphemeralSecret::from_seed(seed(v, "daemon_static")),
            daemon_eph: EphemeralSecret::from_seed(seed(v, daemon_eph_seed)),
        }
    }

    /// Client-side derivation, as `Channel::handshake` does it.
    fn client_schedule(r: &Ref) -> (Zeroizing<[u8; 32]>, Zeroizing<[u8; 32]>, Schedule) {
        let es = r.client.dh(&r.daemon_static.public()).unwrap();
        let ee = r.client.dh(&r.daemon_eph.public()).unwrap();
        let sched = derive_schedule(
            &es,
            &ee,
            &r.daemon_static.public(),
            &r.daemon_eph.public(),
            &r.client.public(),
        );
        (es, ee, sched)
    }

    fn frame_hex(typ: u8, payload: &[u8]) -> String {
        hex_encode(&encode_frame(typ, payload))
    }

    #[test]
    fn key_schedule_vectors() {
        let v = load();
        assert_eq!(v["label"], LABEL);
        let r = reference(&v, "daemon_eph");
        assert_eq!(
            hex_encode(&r.client.public()),
            v["public_keys"]["client_eph"]
        );
        assert_eq!(
            hex_encode(&r.daemon_static.public()),
            v["public_keys"]["daemon_static"]
        );
        assert_eq!(
            hex_encode(&r.daemon_eph.public()),
            v["public_keys"]["daemon_eph"]
        );

        let (es, ee, sched) = client_schedule(&r);
        assert_eq!(*es, arr32(&v, "es"));
        assert_eq!(*ee, arr32(&v, "ee"));
        // th equals the vector, and the daemon's view (its own secrets, the
        // client's public key) derives the identical schedule.
        assert_eq!(sched.th, arr32(&v, "th"));
        assert_eq!(*sched.d2m, arr32(&v, "k_d2m"));
        assert_eq!(*sched.m2d, arr32(&v, "k_m2d"));
        assert_eq!(*sched.confirm_d, arr32(&v, "confirm_d"));
        assert_eq!(*sched.confirm_m, arr32(&v, "confirm_m"));
        let des = r.daemon_static.dh(&r.client.public()).unwrap();
        let dee = r.daemon_eph.dh(&r.client.public()).unwrap();
        assert_eq!((*des, *dee), (*es, *ee));

        // Wire frames, byte for byte.
        let m1 = client_hello_payload(&r.client.public());
        assert_eq!(
            frame_hex(FRAME_HELLO, &m1),
            v["m1_frame_hex"].as_str().unwrap()
        );
        let m2 = daemon_hello_payload(&r.daemon_static.public(), &r.daemon_eph.public());
        assert_eq!(
            frame_hex(FRAME_HELLO, &m2),
            v["m2_frame_hex"].as_str().unwrap()
        );
        let r_d = confirm_tag(&sched.confirm_d, &sched.th);
        assert_eq!(
            frame_hex(FRAME_READY, &r_d),
            v["r_d_frame_hex"].as_str().unwrap()
        );
        let r_c = confirm_tag(&sched.confirm_m, &sched.th);
        assert_eq!(
            frame_hex(FRAME_READY, &r_c),
            v["r_c_frame_hex"].as_str().unwrap()
        );
        assert_eq!(check_ready(&r_d, &r_d), Ok(()));
        assert_eq!(check_ready(&r_c, &r_d), Err(ReadyError::Mismatch));
        assert_eq!(check_ready(&[], &r_d), Err(ReadyError::WrongLength(0)));
    }

    #[test]
    fn transcript_hash_equals_vector_and_is_the_hkdf_salt() {
        let v = load();
        let r = reference(&v, "daemon_eph");
        let th = transcript_hash(
            &r.daemon_static.public(),
            &r.daemon_eph.public(),
            &r.client.public(),
        );
        assert_eq!(th, arr32(&v, "th"));
        // The daemon ephemeral is part of the transcript: another one, another th.
        let other = reference(&v, "daemon_eph_replay");
        assert_ne!(
            th,
            transcript_hash(
                &r.daemon_static.public(),
                &other.daemon_eph.public(),
                &r.client.public()
            )
        );
    }

    #[test]
    fn sealed_data_frames_match_vectors_in_both_directions() {
        let v = load();
        let (_, _, sched) = client_schedule(&reference(&v, "daemon_eph"));
        let mut daemon = Session::new(Role::Daemon, &sched);
        let mut mobile = Session::new(Role::Mobile, &sched);

        for (i, d) in v["data"]["d2m"].as_array().unwrap().iter().enumerate() {
            let pt = bytes(d, "plaintext_hex");
            let (counter, ct) = daemon.seal(&pt).unwrap();
            assert_eq!(counter, i as u64);
            assert_eq!(counter, d["counter"]);
            let mut payload = counter.to_be_bytes().to_vec();
            payload.extend_from_slice(&ct);
            assert_eq!(
                frame_hex(FRAME_DATA, &payload),
                d["frame_hex"].as_str().unwrap()
            );
            assert_eq!(mobile.open(counter, &ct).unwrap(), pt);
        }
        for (i, d) in v["data"]["m2d"].as_array().unwrap().iter().enumerate() {
            let pt = bytes(d, "plaintext_hex");
            let (counter, ct) = mobile.seal(&pt).unwrap();
            assert_eq!(counter, i as u64);
            let mut payload = counter.to_be_bytes().to_vec();
            payload.extend_from_slice(&ct);
            assert_eq!(
                frame_hex(FRAME_DATA, &payload),
                d["frame_hex"].as_str().unwrap()
            );
            assert_eq!(daemon.open(counter, &ct).unwrap(), pt);
        }
    }

    #[test]
    fn replayed_handshake_derives_fresh_keys() {
        let v = load();
        let a = reference(&v, "daemon_eph");
        let b = reference(&v, "daemon_eph_replay");
        // Same client ephemeral, same daemon static key, another daemon ephemeral.
        assert_eq!(a.client.public(), b.client.public());
        let (es_a, ee_a, sa) = client_schedule(&a);
        let (es_b, ee_b, sb) = client_schedule(&b);
        assert_eq!(*es_a, *es_b, "es is the same: only ee changes the keys");
        assert_ne!(*ee_a, *ee_b);
        assert_ne!(sa.th, sb.th);
        assert_ne!(*sa.d2m, *sb.d2m);
        assert_ne!(*sa.m2d, *sb.m2d);
        assert_ne!(*sa.confirm_d, *sb.confirm_d);
        assert_ne!(*sa.confirm_m, *sb.confirm_m);

        let rp = &v["replay"];
        assert_eq!(sb.th, arr32(rp, "th"));
        assert_eq!(*ee_b, arr32(rp, "ee"));
        assert_eq!(*sb.d2m, arr32(rp, "k_d2m"));
        assert_eq!(*sb.m2d, arr32(rp, "k_m2d"));
        assert_eq!(*sb.confirm_d, arr32(rp, "confirm_d"));
        assert_eq!(*sb.confirm_m, arr32(rp, "confirm_m"));
        let m2 = daemon_hello_payload(&b.daemon_static.public(), &b.daemon_eph.public());
        assert_eq!(
            frame_hex(FRAME_HELLO, &m2),
            rp["m2_frame_hex"].as_str().unwrap()
        );
        assert_eq!(
            frame_hex(FRAME_READY, &confirm_tag(&sb.confirm_d, &sb.th)),
            rp["r_d_frame_hex"].as_str().unwrap()
        );
        assert_eq!(
            frame_hex(FRAME_READY, &confirm_tag(&sb.confirm_m, &sb.th)),
            rp["r_c_frame_hex"].as_str().unwrap()
        );

        // No (key, nonce) pair is shared: the same plaintext at counter 0
        // seals to different ciphertexts in the two sessions.
        let mut s1 = Session::new(Role::Daemon, &sa);
        let mut s2 = Session::new(Role::Daemon, &sb);
        assert_ne!(
            s1.seal(b"same plaintext").unwrap().1,
            s2.seal(b"same plaintext").unwrap().1
        );
    }

    #[test]
    fn low_order_points_are_rejected_on_both_dh_outputs() {
        let sk = EphemeralSecret::from_seed([7; 32]);
        let zero = [0u8; 32];
        // The other small-order points (order 1, 2 and 8) of curve25519.
        let mut one = [0u8; 32];
        one[0] = 1;
        let order2 = hex_decode("e0eb7a7c3b41b8ae1656e3faf19fc46ada098deb9c32b1fd866205165f49b800");
        let order8 = hex_decode("5f9c95bca3508c24b1d0b1559c83ef5b04445cc4581c8e86d8224eddd09f1157");
        for p in [zero.to_vec(), one.to_vec(), order2, order8] {
            let p: [u8; 32] = p.try_into().unwrap();
            assert_eq!(
                sk.dh(&p).err(),
                Some(KeyAgreementError),
                "point {}",
                hex_encode(&p)
            );
        }
        let good = EphemeralSecret::from_seed([8; 32]).public();
        assert!(sk.dh(&good).is_ok());
    }

    #[test]
    fn tampered_ciphertext_fails_to_open() {
        let v = load();
        let (_, _, sched) = client_schedule(&reference(&v, "daemon_eph"));
        let mut daemon = Session::new(Role::Daemon, &sched);
        let mut mobile = Session::new(Role::Mobile, &sched);
        let (_, mut ct) = daemon.seal(b"hello").unwrap();
        *ct.last_mut().unwrap() ^= 0xff;
        assert_eq!(mobile.open(0, &ct), Err(SessionError::Decrypt));
    }

    #[test]
    fn repeated_counter_is_rejected_as_replay() {
        let v = load();
        let (_, _, sched) = client_schedule(&reference(&v, "daemon_eph"));
        let mut daemon = Session::new(Role::Daemon, &sched);
        let mut mobile = Session::new(Role::Mobile, &sched);
        let (_, ct) = daemon.seal(b"hello").unwrap();
        mobile.open(0, &ct).unwrap();
        assert_eq!(
            mobile.open(0, &ct),
            Err(SessionError::Replay { got: 0, want: 1 })
        );
    }

    #[test]
    fn hello_parsers_read_the_version_byte_first() {
        let k = [5u8; 32];
        let m1 = client_hello_payload(&k);
        assert_eq!(parse_client_hello(&m1), Ok(k));
        let m2 = daemon_hello_payload(&k, &[6u8; 32]);
        assert_eq!(parse_daemon_hello(&m2), Ok((k, [6u8; 32])));

        // A v1 hello is reported as an unsupported version whatever its length or role.
        for bad in [
            vec![1u8, 1].into_iter().chain(k).collect::<Vec<_>>(),
            vec![1],
            vec![1, 9, 0],
        ] {
            assert_eq!(
                parse_daemon_hello(&bad),
                Err(HelloError::UnsupportedVersion(1))
            );
            assert_eq!(
                parse_client_hello(&bad),
                Err(HelloError::UnsupportedVersion(1))
            );
        }
        assert_eq!(
            parse_daemon_hello(&[3, 1]),
            Err(HelloError::UnsupportedVersion(3))
        );
        assert_eq!(parse_daemon_hello(&[]), Err(HelloError::Empty));
        assert_eq!(
            parse_daemon_hello(&m1),
            Err(HelloError::WrongLength { got: 34, want: 66 })
        );
        let mut wrong_role = m2;
        wrong_role[1] = Role::Mobile as u8;
        assert_eq!(
            parse_daemon_hello(&wrong_role),
            Err(HelloError::WrongRole {
                got: 2,
                want: Role::Daemon
            })
        );
    }

    #[test]
    fn frame_round_trip() {
        let frame = encode_frame(FRAME_DATA, b"payload bytes");
        let (typ, payload) = decode_frame(&frame).unwrap();
        assert_eq!(typ, FRAME_DATA);
        assert_eq!(payload, b"payload bytes");
    }

    #[test]
    fn frame_decode_rejects_zero_length() {
        assert_eq!(
            decode_frame(&hex_decode("0000000000")),
            Err(FrameError::ZeroLength)
        );
    }

    #[test]
    fn frame_decode_rejects_truncated() {
        assert_eq!(
            decode_frame(&hex_decode("000000230101")),
            Err(FrameError::Truncated)
        );
    }

    #[test]
    fn frame_decode_rejects_oversized_length() {
        let mut frame = vec![0u8; 5];
        frame[0..4].copy_from_slice(&((MAX_FRAME_LEN as u32) + 1).to_be_bytes());
        assert_eq!(
            decode_frame(&frame),
            Err(FrameError::TooLarge((MAX_FRAME_LEN as u32) + 1))
        );
    }
}
