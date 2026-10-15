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

use crate::relay::crypto::transcript_hash;
use crate::relay::pairing_store::{restrict_dir, set_restrictive_permissions, write_private};

pub const HELLO_METHOD: &str = "relay.device_hello";
pub const HELLO_VERSION: u32 = 1;

const HELLO_DOMAIN: &[u8] = b"smind-device-hello";
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

    /// Signs "smind-device-hello" ‖ transcript_hash ‖ daemon_pub.
    pub fn hello(&self, label: &str, daemon_pub: &[u8; 32], mobile_pub: &[u8; 32]) -> Hello {
        Hello {
            v: HELLO_VERSION,
            device_pub: self.public().to_vec(),
            label: label.to_string(),
            signature: self
                .key
                .sign(&hello_signed_bytes(daemon_pub, mobile_pub))
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

pub fn hello_signed_bytes(daemon_pub: &[u8; 32], mobile_pub: &[u8; 32]) -> Vec<u8> {
    let mut out = Vec::with_capacity(HELLO_DOMAIN.len() + 64);
    out.extend_from_slice(HELLO_DOMAIN);
    out.extend_from_slice(&transcript_hash(daemon_pub, mobile_pub));
    out.extend_from_slice(daemon_pub);
    out
}

pub fn hello_request(request_id: &str, hello: &Hello) -> String {
    serde_json::json!({ "id": request_id, "method": HELLO_METHOD, "params": hello }).to_string()
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
        return Some(HelloOutcome::Refused { code, message });
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
        let key = DeviceKey::from_seed(arr(&s("seed_hex")));
        let (daemon, mobile) = (arr(&s("daemon_pub_hex")), arr(&s("mobile_pub_hex")));
        assert_eq!(to_hex(&key.public()), s("device_pub_hex"));
        assert_eq!(key.device_id(), s("device_id"));
        assert_eq!(
            to_hex(&hello_signed_bytes(&daemon, &mobile)),
            s("signed_bytes_hex")
        );

        let hello = key.hello(&s("label"), &daemon, &mobile);
        assert_eq!(to_hex(&hello.signature), s("signature_hex"));
        // Byte for byte against the Go JSON (same field order and encoding).
        let ours: Value = serde_json::to_value(&hello).unwrap();
        assert_eq!(ours, v["hello"]);
        let back: Hello = serde_json::from_value(v["hello"].clone()).unwrap();
        assert_eq!(back, hello);

        let vk = VerifyingKey::from_bytes(&key.public()).unwrap();
        let sig = Signature::from_slice(&hello.signature).unwrap();
        vk.verify_strict(&hello_signed_bytes(&daemon, &mobile), &sig)
            .unwrap();

        // Negative: signed over a different mobile_pub.
        let neg = &v["negative"];
        let neg_hello: Hello = serde_json::from_value(neg["hello"].clone()).unwrap();
        let neg_sig = Signature::from_slice(&neg_hello.signature).unwrap();
        assert_eq!(
            to_hex(&hello_signed_bytes(
                &daemon,
                &arr(neg["mobile_pub_hex"].as_str().unwrap())
            )),
            neg["signed_bytes_hex"].as_str().unwrap()
        );
        assert!(vk
            .verify_strict(&hello_signed_bytes(&daemon, &mobile), &neg_sig)
            .is_err());
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
        let hello = key.hello("laptop", &[1u8; 32], &[2u8; 32]);
        let v: Value = serde_json::from_str(&hello_request("r1", &hello)).unwrap();
        assert_eq!(v["id"], "r1");
        assert_eq!(v["method"], "relay.device_hello");
        assert_eq!(v["params"]["v"], 1);
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
                    message: format!("m {code}")
                })
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
}
