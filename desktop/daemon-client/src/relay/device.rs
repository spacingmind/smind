//! Device identity for the multi-device relay (ADR-0031 D9, P13a).
//!
//! A device is a long-lived Ed25519 key. Its id is derived from the public
//! key, and it proves possession with a hello signed over the one E2EE
//! handshake it is sent on. Byte-for-byte compatible with
//! `internal/relay/devices/identity.go` (see the shared test vector).
//!
//! The key lives in `relay/<sha256hex(connection id)>.devkey`, one per relay
//! connection, created at first pairing and deleted with the connection. No
//! function here returns the seed, and `DeviceKey` is neither `Clone` nor
//! `Serialize` and never prints key material.

use std::io;
use std::path::{Path, PathBuf};

use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use ed25519_dalek::{Signer, SigningKey};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::relay::pairing_store::{restrict_dir, set_restrictive_permissions, write_private};

pub const HELLO_METHOD: &str = "relay.device_hello";
pub const HELLO_VERSION: u32 = 2;
/// The event the daemon sends right after the E2EE handshake on a device
/// route; its `challenge` is part of what the hello signs (ADR-0031 D9).
pub const CHALLENGE_EVENT: &str = "relay.device_challenge";
pub const CHALLENGE_LEN: usize = 32;

const HELLO_DOMAIN: &[u8] = b"smind-device-hello-v2";
const DEVICE_ID_LEN: usize = 26;
const MAX_LABEL_BYTES: usize = 64;
const KEY_FILE_VERSION: u32 = 1;

/// Lowercase unpadded RFC 4648 base32 of SHA-256(public), first 26 chars.
pub fn device_id(public: &[u8; 32]) -> String {
    const ALPHABET: &[u8; 32] = b"abcdefghijklmnopqrstuvwxyz234567";
    let sum = Sha256::digest(public);
    let mut out = String::with_capacity(DEVICE_ID_LEN);
    let (mut acc, mut bits) = (0u32, 0u32);
    for b in sum.iter() {
        if out.len() == DEVICE_ID_LEN {
            break;
        }
        acc = (acc << 8) | u32::from(*b);
        bits += 8;
        while bits >= 5 && out.len() < DEVICE_ID_LEN {
            bits -= 5;
            out.push(ALPHABET[((acc >> bits) & 31) as usize] as char);
        }
        acc &= (1 << bits) - 1;
    }
    out
}

pub struct DeviceKey {
    key: SigningKey,
}

impl std::fmt::Debug for DeviceKey {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("DeviceKey(<redacted>)")
    }
}

impl DeviceKey {
    pub fn generate() -> Self {
        let mut seed = [0u8; 32];
        getrandom::fill(&mut seed).expect("smind desktop: system RNG unavailable");
        let k = Self::from_seed(seed);
        seed.fill(0);
        k
    }

    pub fn from_seed(seed: [u8; 32]) -> Self {
        Self {
            key: SigningKey::from_bytes(&seed),
        }
    }

    pub fn public(&self) -> [u8; 32] {
        self.key.verifying_key().to_bytes()
    }

    pub fn device_id(&self) -> String {
        device_id(&self.public())
    }

    /// Signs the v2 bytes (see [`hello_signed_bytes`]): bound to the E2EE
    /// session (`th`, taken from the established channel), the relay route's
    /// `session_id` and the daemon's challenge.
    pub fn hello(
        &self,
        label: &str,
        daemon_pub: &[u8; 32],
        th: &[u8; 32],
        session_id: &str,
        challenge: &[u8; CHALLENGE_LEN],
    ) -> Hello {
        Hello {
            v: HELLO_VERSION,
            device_pub: self.public().to_vec(),
            label: label.to_string(),
            signature: self
                .key
                .sign(&hello_signed_bytes(daemon_pub, th, session_id, challenge))
                .to_bytes()
                .to_vec(),
        }
    }

    fn seed_bytes(&self) -> [u8; 32] {
        self.key.to_bytes()
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Hello {
    pub v: u32,
    #[serde(with = "std_b64")]
    pub device_pub: Vec<u8>,
    pub label: String,
    #[serde(with = "std_b64")]
    pub signature: Vec<u8>,
}

/// Standard base64 with padding, matching Go's `[]byte` JSON encoding.
mod std_b64 {
    use super::{Engine, STANDARD};
    use serde::{Deserialize, Deserializer, Serializer};

    pub fn serialize<S: Serializer>(b: &[u8], s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&STANDARD.encode(b))
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(d: D) -> Result<Vec<u8>, D::Error> {
        let s = String::deserialize(d)?;
        STANDARD.decode(s).map_err(serde::de::Error::custom)
    }
}

/// "smind-device-hello-v2" ‖ th ‖ daemon_pub ‖
/// uint16BE(len(session_id)) ‖ session_id ‖ challenge — the Go
/// `devices.HelloSignedBytes`, byte for byte. `th` is the established
/// channel's transcript hash (ADR-0035 §2, `Channel::transcript_hash`),
/// never recomputed here.
pub fn hello_signed_bytes(
    daemon_pub: &[u8; 32],
    th: &[u8; 32],
    session_id: &str,
    challenge: &[u8; CHALLENGE_LEN],
) -> Vec<u8> {
    let sid = session_id.as_bytes();
    let sid_len = u16::try_from(sid.len()).expect("session id longer than 65535 bytes");
    let mut out = Vec::with_capacity(HELLO_DOMAIN.len() + 64 + 2 + sid.len() + CHALLENGE_LEN);
    out.extend_from_slice(HELLO_DOMAIN);
    out.extend_from_slice(th);
    out.extend_from_slice(daemon_pub);
    out.extend_from_slice(&sid_len.to_be_bytes());
    out.extend_from_slice(sid);
    out.extend_from_slice(challenge);
    out
}

/// `Some(challenge)` when `text` is the daemon's `relay.device_challenge`
/// event with a well-formed 32-byte challenge; `None` for any other message.
/// A challenge of another version or length is `Some(Err(..))`.
pub fn parse_challenge(text: &str) -> Option<Result<[u8; CHALLENGE_LEN], String>> {
    let v: serde_json::Value = serde_json::from_str(text).ok()?;
    if v.get("event")?.as_str()? != CHALLENGE_EVENT {
        return None;
    }
    let params = v.get("params")?;
    let version = params.get("v").and_then(|x| x.as_u64()).unwrap_or(0);
    if version != u64::from(HELLO_VERSION) {
        return Some(Err(format!("daemon challenge version {version}")));
    }
    let raw = params
        .get("challenge")
        .and_then(|c| c.as_str())
        .and_then(|c| STANDARD.decode(c).ok());
    Some(
        raw.and_then(|b| <[u8; CHALLENGE_LEN]>::try_from(b).ok())
            .ok_or_else(|| "daemon challenge is malformed".to_string()),
    )
}

pub fn hello_request(request_id: &str, hello: &Hello) -> String {
    hello_request_with_password(request_id, hello, None)
}

/// The hello request carrying the daemon password (ADR-0031 P13c) when there
/// is one. The password rides in the request's params only: inside the E2EE
/// channel, outside the signature, and never in the `Hello` struct (whose Debug
/// output would otherwise be one careless `{:?}` away from a log).
pub fn hello_request_with_password(request_id: &str, hello: &Hello, password: Option<&str>) -> String {
    let mut params = serde_json::to_value(hello).expect("hello serializes");
    if let (Some(p), Some(obj)) = (password.filter(|p| !p.is_empty()), params.as_object_mut()) {
        obj.insert("password".into(), serde_json::Value::String(p.to_string()));
    }
    serde_json::json!({ "id": request_id, "method": HELLO_METHOD, "params": params }).to_string()
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HelloOutcome {
    Approved {
        device_id: String,
        state: String,
    },
    Refused {
        code: String,
        message: String,
        /// `error.retry_after` (seconds) on a rate-limited refusal.
        retry_after: Option<u64>,
    },
    /// An older daemon that does not know `relay.device_hello`.
    MethodNotFound,
}

/// `None` means the text is not the response to `request_id` (an event, or
/// another request's reply).
pub fn parse_hello_response(request_id: &str, text: &str) -> Option<HelloOutcome> {
    let v: serde_json::Value = serde_json::from_str(text).ok()?;
    if v.get("id")?.as_str()? != request_id {
        return None;
    }
    if let Some(err) = v.get("error").filter(|e| !e.is_null()) {
        let message = err
            .get("message")
            .and_then(|m| m.as_str())
            .unwrap_or("")
            .to_string();
        let code = err
            .get("code")
            .and_then(|c| c.as_str())
            .unwrap_or("")
            .to_string();
        if code.is_empty() && message.starts_with("method not found") {
            return Some(HelloOutcome::MethodNotFound);
        }
        let retry_after = err.get("retry_after").and_then(|r| r.as_u64());
        return Some(HelloOutcome::Refused { code, message, retry_after });
    }
    let result = v.get("result")?;
    Some(HelloOutcome::Approved {
        device_id: result.get("device_id")?.as_str()?.to_string(),
        state: result
            .get("state")
            .and_then(|s| s.as_str())
            .unwrap_or("")
            .to_string(),
    })
}

/// Same rule as Go `devices.ValidateLabel`.
pub fn validate_label(label: &str) -> Result<(), String> {
    if label.len() > MAX_LABEL_BYTES {
        return Err(format!("label is longer than {MAX_LABEL_BYTES} bytes"));
    }
    // `&str` is always valid UTF-8, so only the character classes remain.
    if label.chars().any(is_forbidden_char) {
        return Err("label contains a control or invisible character".into());
    }
    Ok(())
}

/// Unicode categories Cc, Cf, Zl, Zp, Co (explicit ranges; no table crate).
fn is_forbidden_char(c: char) -> bool {
    let u = c as u32;
    if c.is_control() {
        return true;
    }
    matches!(u,
        0x00AD | 0x0600..=0x0605 | 0x061C | 0x06DD | 0x070F | 0x0890..=0x0891 | 0x08E2 | 0x180E
        | 0x200B..=0x200F | 0x2028 | 0x2029 | 0x202A..=0x202E | 0x2060..=0x2064 | 0x2066..=0x206F
        | 0xFEFF | 0xFFF9..=0xFFFB | 0x110BD | 0x110CD | 0x13430..=0x1343F | 0x1BCA0..=0x1BCA3
        | 0x1D173..=0x1D17A | 0xE0001 | 0xE0020..=0xE007F
        // Co: private use
        | 0xE000..=0xF8FF | 0xF0000..=0xFFFFD | 0x100000..=0x10FFFD)
}

// ---- key store ----

#[derive(Serialize, Deserialize)]
struct KeyFile {
    v: u32,
    seed_b64: String,
}

fn key_path(app_data_dir: &Path, connection_id: &str) -> PathBuf {
    let name: String = Sha256::digest(connection_id.as_bytes())
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect();
    app_data_dir.join("relay").join(format!("{name}.devkey"))
}

fn invalid(msg: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, msg.to_string())
}

fn parse_key_file(data: &str) -> io::Result<DeviceKey> {
    let f: KeyFile =
        serde_json::from_str(data).map_err(|_| invalid("device key file is corrupt"))?;
    if f.v != KEY_FILE_VERSION {
        return Err(invalid("device key file has an unsupported version"));
    }
    let seed = STANDARD
        .decode(&f.seed_b64)
        .map_err(|_| invalid("device key file is corrupt"))?;
    let seed: [u8; 32] = seed
        .try_into()
        .map_err(|_| invalid("device key seed is not 32 bytes"))?;
    Ok(DeviceKey::from_seed(seed))
}

/// Loads the connection's device key, generating and persisting one on first
/// use. A present but unreadable/corrupt file is an error, never regenerated:
/// silently minting a new identity would orphan the approved device.
///
/// Creation is atomic and race-free: the key is written in full to a private
/// temp file and then hard-linked into place, which fails if the file already
/// exists. Two concurrent first uses (app start racing a pairing, two quick
/// selects) therefore agree on ONE key — the loser reads the winner's — and a
/// reader never sees a half-written file. (A zero-length file is a crash
/// residue of a pre-atomic version: no identity was ever used with it, so it is
/// replaced.)
pub fn load_or_create_key(app_data_dir: &Path, connection_id: &str) -> io::Result<DeviceKey> {
    let path = key_path(app_data_dir, connection_id);
    if let Some(key) = read_key(&path)? {
        return Ok(key);
    }
    let dir = app_data_dir.join("relay");
    std::fs::create_dir_all(&dir)?;
    restrict_dir(&dir)?;
    let key = DeviceKey::generate();
    let data = serde_json::to_string(&KeyFile {
        v: KEY_FILE_VERSION,
        seed_b64: STANDARD.encode(key.seed_bytes()),
    })
    .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
    let mut nonce = [0u8; 8];
    getrandom::fill(&mut nonce).map_err(|e| io::Error::other(e.to_string()))?;
    let tmp = dir.join(format!(
        ".devkey-{}-{:016x}.tmp",
        std::process::id(),
        u64::from_be_bytes(nonce)
    ));
    write_private(&tmp, data.as_bytes())?;
    set_restrictive_permissions(&tmp)?;
    let linked = std::fs::hard_link(&tmp, &path);
    let _ = std::fs::remove_file(&tmp);
    match linked {
        Ok(()) => Ok(key),
        Err(e) if e.kind() == io::ErrorKind::AlreadyExists => match read_key(&path)? {
            Some(winner) => Ok(winner),
            None => {
                // The existing file is a zero-length crash residue: replace it.
                std::fs::remove_file(&path)?;
                load_or_create_key(app_data_dir, connection_id)
            }
        },
        Err(e) => Err(e),
    }
}

/// read_key reads the key file: `Ok(None)` when it is missing or empty.
fn read_key(path: &Path) -> io::Result<Option<DeviceKey>> {
    match std::fs::read_to_string(path) {
        Ok(data) if data.trim().is_empty() => Ok(None),
        Ok(data) => parse_key_file(&data).map(Some),
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e),
    }
}

/// Removes the connection's device key; missing is Ok.
pub fn delete_key(app_data_dir: &Path, connection_id: &str) -> io::Result<()> {
    match std::fs::remove_file(key_path(app_data_dir, connection_id)) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(e),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::{Signature, VerifyingKey};

    #[test]
    fn concurrent_first_use_agrees_on_one_key() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().to_path_buf();
        let keys: Vec<[u8; 32]> = std::thread::scope(|scope| {
            let handles: Vec<_> = (0..8)
                .map(|_| {
                    let path = path.clone();
                    scope.spawn(move || load_or_create_key(&path, "relay:c@h:1").unwrap().public())
                })
                .collect();
            handles.into_iter().map(|h| h.join().unwrap()).collect()
        });
        assert!(keys.iter().all(|k| *k == keys[0]), "concurrent first uses minted different keys");
        assert_eq!(load_or_create_key(&path, "relay:c@h:1").unwrap().public(), keys[0]);
        // No temp file is left behind.
        let leftovers: Vec<_> = std::fs::read_dir(path.join("relay"))
            .unwrap()
            .filter_map(|e| e.ok())
            .filter(|e| e.file_name().to_string_lossy().ends_with(".tmp"))
            .collect();
        assert!(leftovers.is_empty(), "{leftovers:?}");
    }

    #[test]
    fn a_zero_length_key_file_is_replaced_but_garbage_is_an_error() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(dir.path().join("relay")).unwrap();
        let path = key_path(dir.path(), "relay:c@h:2");
        std::fs::write(&path, b"").unwrap();
        let key = load_or_create_key(dir.path(), "relay:c@h:2").unwrap();
        assert_eq!(load_or_create_key(dir.path(), "relay:c@h:2").unwrap().public(), key.public());
        std::fs::write(&path, b"not json").unwrap();
        assert!(load_or_create_key(dir.path(), "relay:c@h:2").is_err(), "garbage must stay an error");
    }
    use serde_json::Value;

    const VECTOR: &str = include_str!(concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../../internal/relay/devices/testdata/device_hello_vector.json"
    ));

    fn hex(s: &str) -> Vec<u8> {
        (0..s.len() / 2)
            .map(|i| u8::from_str_radix(&s[2 * i..2 * i + 2], 16).unwrap())
            .collect()
    }
    fn to_hex(b: &[u8]) -> String {
        b.iter().map(|x| format!("{x:02x}")).collect()
    }
    fn arr(s: &str) -> [u8; 32] {
        hex(s).try_into().unwrap()
    }

    #[test]
    fn device_id_matches_public_key_hash() {
        let v: Value = serde_json::from_str(VECTOR).unwrap();
        let public = arr(v["device_pub_hex"].as_str().unwrap());
        let id = device_id(&public);
        assert_eq!(id, v["device_id"].as_str().unwrap());
        assert_eq!(id.len(), 26);
        assert!(id.chars().all(|c| matches!(c, 'a'..='z' | '2'..='7')));
        assert_ne!(id, device_id(&[0u8; 32]));
    }

    #[test]
    fn device_hello_signature_verifies_against_go_vector() {
        let v: Value = serde_json::from_str(VECTOR).unwrap();
        let s = |k: &str| v[k].as_str().unwrap().to_string();
        assert_eq!(v["version"], 2);
        let key = DeviceKey::from_seed(arr(&s("seed_hex")));
        let (daemon, daemon_eph, mobile) = (
            arr(&s("daemon_pub_hex")),
            arr(&s("daemon_eph_pub_hex")),
            arr(&s("mobile_pub_hex")),
        );
        // `th` is what the channel exposes; the vector's value is the E2EE v2 transcript.
        let th = crate::relay::crypto::transcript_hash(&daemon, &daemon_eph, &mobile);
        assert_eq!(to_hex(&th), s("transcript_hash_hex"));
        let (session_id, challenge) = (s("session_id"), arr(&s("challenge_hex")));
        assert_eq!(to_hex(&key.public()), s("device_pub_hex"));
        assert_eq!(key.device_id(), s("device_id"));

        // The challenge the daemon sends parses to the vector's challenge.
        let msg = v["challenge_message"].to_string();
        assert_eq!(parse_challenge(&msg), Some(Ok(challenge)));

        assert_eq!(
            to_hex(&hello_signed_bytes(&daemon, &th, &session_id, &challenge)),
            s("signed_bytes_hex")
        );
        let hello = key.hello(&s("label"), &daemon, &th, &session_id, &challenge);
        assert_eq!(to_hex(&hello.signature), s("signature_hex"));
        // Byte for byte against the Go JSON (same field order and encoding).
        let ours: Value = serde_json::to_value(&hello).unwrap();
        assert_eq!(ours, v["hello"]);
        let back: Hello = serde_json::from_value(v["hello"].clone()).unwrap();
        assert_eq!(back, hello);

        let vk = VerifyingKey::from_bytes(&key.public()).unwrap();
        let sig = Signature::from_slice(&hello.signature).unwrap();
        vk.verify_strict(
            &hello_signed_bytes(&daemon, &th, &session_id, &challenge),
            &sig,
        )
        .unwrap();

        // Negatives: each signature covers bytes that differ from the real
        // ones in exactly one input; none may verify against the real bytes.
        let real = hello_signed_bytes(&daemon, &th, &session_id, &challenge);
        let negatives = v["negative"].as_array().unwrap();
        let names: Vec<_> = negatives.iter().map(|n| n["changed"].as_str().unwrap()).collect();
        assert_eq!(
            names,
            ["daemon_pub", "mobile_pub", "daemon_eph_pub", "session_id", "challenge"]
        );
        for neg in negatives {
            assert_eq!(neg["error"], "bad_signature");
        }
        for neg in negatives {
            let name = neg["name"].as_str().unwrap();
            let their_bytes = hex(neg["signed_bytes_hex"].as_str().unwrap());
            assert_ne!(their_bytes, real, "{name}");
            let neg_hello: Hello = serde_json::from_value(neg["hello"].clone()).unwrap();
            let neg_sig = Signature::from_slice(&neg_hello.signature).unwrap();
            assert!(vk.verify_strict(&real, &neg_sig).is_err(), "{name} must not verify");
            assert!(
                vk.verify_strict(&their_bytes, &neg_sig).is_ok(),
                "{name} signature is over its own bytes"
            );
        }
    }

    #[test]
    fn parse_challenge_variants() {
        let ok = |c: &str| {
            format!(r#"{{"event":"relay.device_challenge","params":{{"v":2,"challenge":"{c}"}}}}"#)
        };
        let good = STANDARD.encode([9u8; 32]);
        assert_eq!(parse_challenge(&ok(&good)), Some(Ok([9u8; 32])));
        assert!(matches!(parse_challenge(&ok(&STANDARD.encode([9u8; 31]))), Some(Err(_))));
        assert!(matches!(parse_challenge(&ok("!!")), Some(Err(_))));
        assert!(matches!(
            parse_challenge(r#"{"event":"relay.device_challenge","params":{"v":3,"challenge":""}}"#),
            Some(Err(_))
        ));
        assert_eq!(parse_challenge(r#"{"event":"other","params":{}}"#), None);
        assert_eq!(parse_challenge(r#"{"id":"r1","result":{}}"#), None);
        assert_eq!(parse_challenge("not json"), None);
    }

    #[cfg(unix)]
    #[test]
    fn device_key_generated_once_mode_0600() {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        let id = "relay:ws-1@relay.example.test:7400";
        let a = load_or_create_key(dir.path(), id).unwrap();
        let b = load_or_create_key(dir.path(), id).unwrap();
        assert_eq!(a.public(), b.public());
        let other = load_or_create_key(dir.path(), "relay:other@h:1").unwrap();
        assert_ne!(a.public(), other.public());
        let path = key_path(dir.path(), id);
        assert_eq!(
            std::fs::metadata(&path).unwrap().permissions().mode() & 0o777,
            0o600
        );
        assert_eq!(
            std::fs::metadata(dir.path().join("relay"))
                .unwrap()
                .permissions()
                .mode()
                & 0o777,
            0o700
        );
        let name = path.file_name().unwrap().to_string_lossy().into_owned();
        assert!(
            name.strip_suffix(".devkey")
                .unwrap()
                .chars()
                .all(|c| c.is_ascii_hexdigit()),
            "{name}"
        );
    }

    #[test]
    fn device_key_deleted_when_connection_removed() {
        let dir = tempfile::tempdir().unwrap();
        let id = "relay:ws-1@h:1";
        load_or_create_key(dir.path(), id).unwrap();
        assert!(key_path(dir.path(), id).is_file());
        delete_key(dir.path(), id).unwrap();
        assert!(!key_path(dir.path(), id).exists());
        delete_key(dir.path(), id).unwrap();

        load_or_create_key(dir.path(), id).unwrap();
        crate::relay::pairing_store::delete(dir.path(), id).unwrap();
        assert!(!key_path(dir.path(), id).exists());
    }

    #[test]
    fn device_key_corrupt_file_is_an_error() {
        let dir = tempfile::tempdir().unwrap();
        let id = "relay:ws-1@h:1";
        let path = key_path(dir.path(), id);
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        // (A zero-length file is a crash residue, not corruption: see
        // `a_zero_length_key_file_is_replaced_but_garbage_is_an_error`.)
        for bad in [
            "{\"v\":1,\"seed_b6",
            "{\"v\":1,\"seed_b64\":\"AAAA\"}",
            "{\"v\":2,\"seed_b64\":\"\"}",
            "garbage",
        ] {
            std::fs::write(&path, bad).unwrap();
            assert!(load_or_create_key(dir.path(), id).is_err(), "{bad:?}");
            assert_eq!(
                std::fs::read_to_string(&path).unwrap(),
                bad,
                "must not be overwritten"
            );
        }
    }

    #[test]
    fn device_key_never_exposed() {
        let seed = [0x42u8; 32];
        let key = DeviceKey::from_seed(seed);
        let dbg = format!("{key:?}");
        assert_eq!(dbg, "DeviceKey(<redacted>)");
        assert!(!dbg.contains(&to_hex(&seed)) && !dbg.contains(&to_hex(&key.public())));
        assert!(!dbg.contains(&STANDARD.encode(seed)) && !dbg.contains("66, 66"));
    }

    #[test]
    fn hello_request_shape() {
        let key = DeviceKey::from_seed([7u8; 32]);
        let hello = key.hello("laptop", &[1u8; 32], &[2u8; 32], "sess", &[3u8; CHALLENGE_LEN]);
        let v: Value = serde_json::from_str(&hello_request("r1", &hello)).unwrap();
        assert_eq!(v["id"], "r1");
        assert_eq!(v["method"], "relay.device_hello");
        assert_eq!(v["params"]["v"], 2);
        assert_eq!(v["params"]["label"], "laptop");
        assert_eq!(v["params"]["device_pub"], STANDARD.encode(key.public()));
        assert_eq!(v["params"]["signature"], STANDARD.encode(&hello.signature));
        assert_eq!(v.as_object().unwrap().len(), 3);
    }

    #[test]
    fn parse_hello_response_variants() {
        let p = |t: &str| parse_hello_response("r1", t);
        assert_eq!(
            p(r#"{"id":"r1","result":{"v":1,"device_id":"abc","state":"approved"}}"#),
            Some(HelloOutcome::Approved {
                device_id: "abc".into(),
                state: "approved".into()
            })
        );
        for code in ["awaiting_approval", "revoked", "bad_hello"] {
            assert_eq!(
                p(&format!(
                    r#"{{"id":"r1","error":{{"message":"m {code}","code":"{code}"}}}}"#
                )),
                Some(HelloOutcome::Refused {
                    code: code.into(),
                    message: format!("m {code}"),
                    retry_after: None,
                })
            );
        }
        // P13c: a rate-limited refusal says when to come back.
        assert_eq!(
            p(r#"{"id":"r1","error":{"message":"wait","code":"too_many_attempts","retry_after":8}}"#),
            Some(HelloOutcome::Refused {
                code: "too_many_attempts".into(),
                message: "wait".into(),
                retry_after: Some(8),
            })
        );
        for code in ["password_required", "incorrect_password"] {
            let parsed = p(&format!(r#"{{"id":"r1","error":{{"message":"m","code":"{code}"}}}}"#));
            assert!(
                matches!(parsed, Some(HelloOutcome::Refused { code: ref c, retry_after: None, .. }) if c == code),
                "{parsed:?}"
            );
        }
        assert_eq!(
            p(r#"{"id":"r1","error":{"message":"method not found: relay.device_hello"}}"#),
            Some(HelloOutcome::MethodNotFound)
        );
        assert_eq!(p(r#"{"id":"other","result":{"device_id":"abc"}}"#), None);
        assert_eq!(p(r#"{"event":"x"}"#), None);
        assert_eq!(p("not json"), None);
    }

    #[test]
    fn validate_label_rules() {
        assert!(validate_label("").is_ok());
        assert!(validate_label("Pixel 9 (test vector) – ünï 📱").is_ok());
        assert!(validate_label(&"a".repeat(64)).is_ok());
        assert!(validate_label(&"a".repeat(65)).is_err());
        assert!(validate_label(&"é".repeat(33)).is_err(), "66 bytes");
        for bad in [
            "a\nb",
            "a\0b",
            "a\u{7f}b",
            "a\u{85}b",
            "a\u{202e}b",
            "a\u{200b}b",
            "a\u{feff}b",
            "a\u{ad}b",
            "a\u{2028}b",
            "a\u{2029}b",
            "a\u{e000}b",
            "a\u{f8ff}b",
            "a\u{f0000}b",
            "a\u{e0001}b",
        ] {
            assert!(validate_label(bad).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn the_password_rides_in_params_and_never_in_the_signed_hello() {
        let key = DeviceKey::generate();
        let challenge = [7u8; CHALLENGE_LEN];
        let hello = key.hello("laptop", &[1u8; 32], &[2u8; 32], "sess", &challenge);
        let plain: serde_json::Value = serde_json::from_str(&hello_request("r1", &hello)).unwrap();
        assert!(plain["params"].get("password").is_none(), "no password unless one is given");
        let with: serde_json::Value =
            serde_json::from_str(&hello_request_with_password("r1", &hello, Some("pw-correct-horse"))).unwrap();
        assert_eq!(with["params"]["password"], "pw-correct-horse");
        // Everything else, including the signature, is byte-identical: the signed bytes do not change.
        let mut stripped = with.clone();
        stripped["params"].as_object_mut().unwrap().remove("password");
        assert_eq!(stripped, plain);
        // An empty password is no password.
        let empty: serde_json::Value =
            serde_json::from_str(&hello_request_with_password("r1", &hello, Some(""))).unwrap();
        assert!(empty["params"].get("password").is_none());
        // And the Hello struct itself cannot print a password: it has no such field.
        assert!(!format!("{hello:?}").contains("pw-correct-horse"));
    }
}
