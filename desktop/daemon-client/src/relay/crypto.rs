//! E2EE session crypto and wire framing, byte-compatible with
//! `internal/relay/e2ee/{handshake,session}.go` (Go) and
//! `mobile/src/relay/e2ee.ts` (TypeScript): X25519 key agreement,
//! HKDF-SHA256 key derivation, ChaCha20-Poly1305 (IETF, RFC 8439) AEAD
//! with 12-byte counter-based nonces, per
//! `docs/decisions/0007-relay-architecture.md` (c)/(d)/(e).
//!
//! The relay itself never sees any of this: it forwards opaque
//! ciphertext frames between the two ends.

use chacha20poly1305::aead::Aead;
use chacha20poly1305::{ChaCha20Poly1305, KeyInit, Nonce};
use hkdf::Hkdf;
use sha2::{Digest, Sha256};
use x25519_dalek::{PublicKey, StaticSecret};

/// The E2EE wire protocol version. Both ends must match; no negotiation.
pub const PROTOCOL_VERSION: u8 = 1;
/// Bounds a single frame so a malformed length prefix can't force an
/// arbitrarily large allocation.
pub const MAX_FRAME_LEN: usize = 1 << 20;
/// protocol version (1) + role (1) + X25519 public key (32).
pub const HELLO_PAYLOAD_LEN: usize = 34;
pub const PUBLIC_KEY_SIZE: usize = 32;

pub const FRAME_HELLO: u8 = 0x01;
pub const FRAME_READY: u8 = 0x02;
pub const FRAME_DATA: u8 = 0x03;

const HKDF_LABEL: &str = "smind relay e2ee v1";

/// Which end of the session this is. The two ends must differ; the role
/// selects which directional key is used for sending.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Role {
    Daemon = 1,
    Mobile = 2,
}

impl Role {
    pub fn from_u8(v: u8) -> Option<Role> {
        match v {
            1 => Some(Role::Daemon),
            2 => Some(Role::Mobile),
            _ => None,
        }
    }
}

/// An X25519 keypair. The desktop client generates a fresh one per
/// connect and never persists it (see the plan's Decisions: ADR-0007(e)
/// already allows falling back to a fresh handshake on process restart).
pub struct KeyPair {
    secret: StaticSecret,
    public: PublicKey,
}

impl KeyPair {
    pub fn generate() -> Self {
        let mut seed = [0u8; 32];
        getrandom::fill(&mut seed).expect("getrandom: generate X25519 keypair");
        Self::from_seed(seed)
    }

    /// from_seed builds a keypair from a raw 32-byte scalar seed —
    /// exposed only for cross-language fixture tests (mirrors Go's
    /// `ecdh.X25519().NewPrivateKey(seed)`, TypeScript has no equivalent
    /// deterministic constructor but shares the same seeded fixture
    /// values via the Go-computed public keys).
    pub fn from_seed(seed: [u8; 32]) -> Self {
        let secret = StaticSecret::from(seed);
        let public = PublicKey::from(&secret);
        Self { secret, public }
    }

    pub fn public(&self) -> [u8; PUBLIC_KEY_SIZE] {
        self.public.to_bytes()
    }
}

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

/// Holds the two directional ChaCha20-Poly1305 keys derived from an
/// X25519 exchange, plus the per-direction counters that supply nonces
/// and provide replay protection.
///
/// Nonces are never random: each direction's 12-byte nonce is a 4-byte
/// zero prefix followed by the big-endian frame counter, so no nonce is
/// ever reused under a key as long as the counter is monotonic — the
/// same property that makes replayed frames detectable.
pub struct Session {
    send: ChaCha20Poly1305,
    recv: ChaCha20Poly1305,
    send_counter: u64,
    recv_counter: u64,
}

impl Session {
    /// Derives both directional keys from an X25519 exchange. `daemon_pub`
    /// and `mobile_pub` are always passed in that order regardless of
    /// which side is deriving, so both ends agree on the HKDF salt.
    pub fn new(
        kp: &KeyPair,
        peer_public: &[u8; PUBLIC_KEY_SIZE],
        role: Role,
        daemon_pub: &[u8; PUBLIC_KEY_SIZE],
        mobile_pub: &[u8; PUBLIC_KEY_SIZE],
    ) -> Self {
        let peer = PublicKey::from(*peer_public);
        let shared = kp.secret.diffie_hellman(&peer);

        let mut transcript = Sha256::new();
        transcript.update(HKDF_LABEL.as_bytes());
        transcript.update(daemon_pub);
        transcript.update(mobile_pub);
        let salt = transcript.finalize();

        let hk = Hkdf::<Sha256>::new(Some(&salt), shared.as_bytes());
        let mut d2m = [0u8; 32];
        hk.expand(format!("{HKDF_LABEL} daemon->mobile").as_bytes(), &mut d2m)
            .expect("HKDF expand: 32 bytes is a valid SHA-256 output length");
        let mut m2d = [0u8; 32];
        hk.expand(format!("{HKDF_LABEL} mobile->daemon").as_bytes(), &mut m2d)
            .expect("HKDF expand: 32 bytes is a valid SHA-256 output length");

        let (send_key, recv_key) = if role == Role::Mobile {
            (m2d, d2m)
        } else {
            (d2m, m2d)
        };
        Session {
            send: ChaCha20Poly1305::new((&send_key).into()),
            recv: ChaCha20Poly1305::new((&recv_key).into()),
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
    if buf.len() < 5 {
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

/// Builds the hello frame payload (protocol version, role, public key).
pub fn hello_payload(role: Role, public_key: &[u8; PUBLIC_KEY_SIZE]) -> Vec<u8> {
    let mut payload = Vec::with_capacity(HELLO_PAYLOAD_LEN);
    payload.push(PROTOCOL_VERSION);
    payload.push(role as u8);
    payload.extend_from_slice(public_key);
    payload
}

#[derive(Debug, PartialEq, Eq)]
pub enum HelloError {
    WrongLength(usize),
    WrongVersion(u8),
    UnknownRole(u8),
    SameRole(Role),
}

/// Parses and validates a hello frame payload against `own_role`
/// (rejecting a peer claiming the same role), mirroring `parseHello`
/// (Go).
pub fn parse_hello(
    payload: &[u8],
    own_role: Role,
) -> Result<(Role, [u8; PUBLIC_KEY_SIZE]), HelloError> {
    if payload.len() != HELLO_PAYLOAD_LEN {
        return Err(HelloError::WrongLength(payload.len()));
    }
    if payload[0] != PROTOCOL_VERSION {
        return Err(HelloError::WrongVersion(payload[0]));
    }
    let peer_role = Role::from_u8(payload[1]).ok_or(HelloError::UnknownRole(payload[1]))?;
    if peer_role == own_role {
        return Err(HelloError::SameRole(peer_role));
    }
    let mut peer_pub = [0u8; PUBLIC_KEY_SIZE];
    peer_pub.copy_from_slice(&payload[2..]);
    Ok((peer_role, peer_pub))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture_seed(b: u8) -> [u8; 32] {
        [b; 32]
    }

    const FIXTURE_DAEMON_PUB_HEX: &str =
        "7b4e909bbe7ffe44c465a220037d608ee35897d31ef972f07f74892cb0f73f13";
    const FIXTURE_MOBILE_PUB_HEX: &str =
        "0faa684ed28867b97f4a6a2dee5df8ce974e76b7018e3f22a1c4cf2678570f20";
    const FIXTURE_D2M_CIPHER0: &str =
        "d6a248cac0f4b322f162664fc9c16b2ef390e3809baa230e1d23499e9d8e64c16442705fa096806590f5bc4b37";
    const FIXTURE_M2D_CIPHER0: &str =
        "233f25b1c411311a8ea4dd9f9034e63a44f9fb4808b83c89ee9de79cca7b2398c27a50e891de";
    const FIXTURE_PLAINTEXT_D2M: &[u8] = b"hello from smind e2ee fixture";
    const FIXTURE_PLAINTEXT_M2D: &[u8] = b"hello back from mobile";
    const FIXTURE_DAEMON_HELLO_FRAME_HEX: &str =
        "000000230101017b4e909bbe7ffe44c465a220037d608ee35897d31ef972f07f74892cb0f73f13";

    fn hex_decode(s: &str) -> Vec<u8> {
        (0..s.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap())
            .collect()
    }

    fn hex_encode(b: &[u8]) -> String {
        b.iter().map(|x| format!("{x:02x}")).collect()
    }

    /// The single most important test in this module: reproduces
    /// internal/relay/e2ee/fixture_test.go's TestFixtureVectorsForTypeScriptPort
    /// byte-for-byte, cross-checked against the same hardcoded literals
    /// mobile/src/relay/__tests__/e2ee.test.ts asserts.
    #[test]
    fn cross_language_fixture_vectors() {
        let daemon_kp = KeyPair::from_seed(fixture_seed(0x11));
        let mobile_kp = KeyPair::from_seed(fixture_seed(0x22));

        assert_eq!(hex_encode(&daemon_kp.public()), FIXTURE_DAEMON_PUB_HEX);
        assert_eq!(hex_encode(&mobile_kp.public()), FIXTURE_MOBILE_PUB_HEX);

        let daemon_pub = daemon_kp.public();
        let mobile_pub = mobile_kp.public();

        let mut daemon_session = Session::new(
            &daemon_kp,
            &mobile_pub,
            Role::Daemon,
            &daemon_pub,
            &mobile_pub,
        );
        let mut mobile_session = Session::new(
            &mobile_kp,
            &daemon_pub,
            Role::Mobile,
            &daemon_pub,
            &mobile_pub,
        );

        let (counter, ciphertext) = daemon_session.seal(FIXTURE_PLAINTEXT_D2M).unwrap();
        assert_eq!(counter, 0);
        assert_eq!(hex_encode(&ciphertext), FIXTURE_D2M_CIPHER0);

        let plaintext = mobile_session.open(0, &ciphertext).unwrap();
        assert_eq!(plaintext, FIXTURE_PLAINTEXT_D2M);

        let (reply_counter, reply_ciphertext) = mobile_session.seal(FIXTURE_PLAINTEXT_M2D).unwrap();
        assert_eq!(reply_counter, 0);
        assert_eq!(hex_encode(&reply_ciphertext), FIXTURE_M2D_CIPHER0);

        daemon_session.open(0, &reply_ciphertext).unwrap();
    }

    #[test]
    fn tampered_ciphertext_fails_to_open() {
        let daemon_kp = KeyPair::from_seed(fixture_seed(0x11));
        let mobile_kp = KeyPair::from_seed(fixture_seed(0x22));
        let daemon_pub = daemon_kp.public();
        let mobile_pub = mobile_kp.public();
        let mut daemon_session = Session::new(
            &daemon_kp,
            &mobile_pub,
            Role::Daemon,
            &daemon_pub,
            &mobile_pub,
        );
        let mut mobile_session = Session::new(
            &mobile_kp,
            &daemon_pub,
            Role::Mobile,
            &daemon_pub,
            &mobile_pub,
        );

        let (_, mut ciphertext) = daemon_session.seal(FIXTURE_PLAINTEXT_D2M).unwrap();
        *ciphertext.last_mut().unwrap() ^= 0xff;
        assert_eq!(
            mobile_session.open(0, &ciphertext),
            Err(SessionError::Decrypt)
        );
    }

    #[test]
    fn repeated_counter_is_rejected_as_replay() {
        let daemon_kp = KeyPair::from_seed(fixture_seed(0x11));
        let mobile_kp = KeyPair::from_seed(fixture_seed(0x22));
        let daemon_pub = daemon_kp.public();
        let mobile_pub = mobile_kp.public();
        let mut daemon_session = Session::new(
            &daemon_kp,
            &mobile_pub,
            Role::Daemon,
            &daemon_pub,
            &mobile_pub,
        );
        let mut mobile_session = Session::new(
            &mobile_kp,
            &daemon_pub,
            Role::Mobile,
            &daemon_pub,
            &mobile_pub,
        );

        let (_, ciphertext) = daemon_session.seal(FIXTURE_PLAINTEXT_D2M).unwrap();
        mobile_session.open(0, &ciphertext).unwrap();
        assert_eq!(
            mobile_session.open(0, &ciphertext),
            Err(SessionError::Replay { got: 0, want: 1 })
        );
    }

    #[test]
    fn hello_frame_matches_go_fixture() {
        let daemon_kp = KeyPair::from_seed(fixture_seed(0x11));
        let payload = hello_payload(Role::Daemon, &daemon_kp.public());
        let frame = encode_frame(FRAME_HELLO, &payload);
        assert_eq!(hex_encode(&frame), FIXTURE_DAEMON_HELLO_FRAME_HEX);
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
        let frame = hex_decode("0000000000");
        assert_eq!(decode_frame(&frame), Err(FrameError::ZeroLength));
    }

    #[test]
    fn frame_decode_rejects_truncated() {
        let frame = hex_decode("000000230101");
        assert_eq!(decode_frame(&frame), Err(FrameError::Truncated));
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

    #[test]
    fn parse_hello_rejects_same_role() {
        let kp = KeyPair::from_seed(fixture_seed(0x33));
        let payload = hello_payload(Role::Daemon, &kp.public());
        assert_eq!(
            parse_hello(&payload, Role::Daemon),
            Err(HelloError::SameRole(Role::Daemon))
        );
    }

    #[test]
    fn parse_hello_accepts_opposite_role() {
        let kp = KeyPair::from_seed(fixture_seed(0x33));
        let payload = hello_payload(Role::Daemon, &kp.public());
        let (role, pk) = parse_hello(&payload, Role::Mobile).unwrap();
        assert_eq!(role, Role::Daemon);
        assert_eq!(pk, kp.public());
    }

    #[test]
    fn parse_hello_rejects_wrong_version() {
        let kp = KeyPair::from_seed(fixture_seed(0x33));
        let mut payload = hello_payload(Role::Daemon, &kp.public());
        payload[0] = 9;
        assert_eq!(
            parse_hello(&payload, Role::Mobile),
            Err(HelloError::WrongVersion(9))
        );
    }
}
