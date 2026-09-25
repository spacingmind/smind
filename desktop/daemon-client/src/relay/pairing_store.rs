//! Persists the long-lived pairing material a `relay` connection needs to
//! reconnect without rescanning/repasting the pairing URL: the daemon's
//! public key (pinned per ADR-0007(g)), the relay's native gRPC address
//! and pinned TLS fingerprint, the workspace id, and the admission
//! secret (ADR-0011).
//!
//! What is deliberately NOT persisted here: this device's own X25519
//! keypair. It is generated fresh on every connect, matching
//! `mobile/src/relay/e2ee.ts`'s own choice and explicitly allowed by
//! ADR-0007(e)'s 2026-09-17 amendment, which names "process restart with
//! nothing persisted" as exactly the case that falls back to a fresh
//! handshake rather than resuming a session. Persisting the ephemeral
//! session key *and* both AEAD counters would be required to safely
//! resume across a process restart (reusing a key at a previously-used
//! counter is a nonce-reuse violation) — nothing here requires that.
//!
//! One file per relay connection, `0600` on unix. On Windows there is no
//! POSIX mode bit to set; the file relies on the app data directory's
//! own per-user ACL, the same posture Go's `bridge/config.go` and
//! `e2ee/keypair.go` already accept for equally sensitive material
//! (`os.WriteFile(path, data, 0o600)` is a no-op permission bit on
//! Windows there too).

use std::io;
use std::path::{Path, PathBuf};

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use serde::{Deserialize, Serialize};

use crate::relay::pairing::Offer;

const PAIRING_FILE_VERSION: u32 = 1;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RelayPairing {
    pub daemon_id: String,
    pub daemon_public_key: [u8; 32],
    /// The relay's native gRPC address (host:port) — derived from the
    /// offer's grpc-web address, not stored verbatim from it. See
    /// `crate::relay::client::derive_native_grpc_address`.
    pub relay_native_addr: String,
    pub relay_fingerprint: String,
    pub workspace_id: String,
    pub admission_secret: Vec<u8>,
}

#[derive(Serialize, Deserialize)]
struct PersistedPairing {
    v: u32,
    daemon_id: String,
    #[serde(rename = "daemonPublicKeyB64")]
    daemon_public_key_b64: String,
    #[serde(rename = "relayNativeAddr")]
    relay_native_addr: String,
    #[serde(rename = "relayFingerprint")]
    relay_fingerprint: String,
    #[serde(rename = "workspaceId")]
    workspace_id: String,
    #[serde(rename = "admissionSecretB64")]
    admission_secret_b64: String,
}

impl RelayPairing {
    pub fn from_offer(offer: &Offer, relay_native_addr: String) -> Self {
        Self {
            daemon_id: offer.daemon_id.clone(),
            daemon_public_key: offer.public_key,
            relay_native_addr,
            relay_fingerprint: offer.relay_fingerprint.clone(),
            workspace_id: offer.workspace_id.clone(),
            admission_secret: offer.secret.clone(),
        }
    }

    fn to_persisted(&self) -> PersistedPairing {
        PersistedPairing {
            v: PAIRING_FILE_VERSION,
            daemon_id: self.daemon_id.clone(),
            daemon_public_key_b64: URL_SAFE_NO_PAD.encode(self.daemon_public_key),
            relay_native_addr: self.relay_native_addr.clone(),
            relay_fingerprint: self.relay_fingerprint.clone(),
            workspace_id: self.workspace_id.clone(),
            admission_secret_b64: URL_SAFE_NO_PAD.encode(&self.admission_secret),
        }
    }

    fn from_persisted(p: PersistedPairing) -> io::Result<Self> {
        let pk = URL_SAFE_NO_PAD
            .decode(&p.daemon_public_key_b64)
            .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        let daemon_public_key: [u8; 32] = pk.try_into().map_err(|_| {
            io::Error::new(
                io::ErrorKind::InvalidData,
                "daemon public key is not 32 bytes",
            )
        })?;
        let admission_secret = URL_SAFE_NO_PAD
            .decode(&p.admission_secret_b64)
            .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        Ok(RelayPairing {
            daemon_id: p.daemon_id,
            daemon_public_key,
            relay_native_addr: p.relay_native_addr,
            relay_fingerprint: p.relay_fingerprint,
            workspace_id: p.workspace_id,
            admission_secret,
        })
    }
}

fn pairing_dir(app_data_dir: &Path) -> PathBuf {
    app_data_dir.join("relay")
}

fn pairing_path(app_data_dir: &Path, connection_id: &str) -> PathBuf {
    pairing_dir(app_data_dir).join(format!("{connection_id}.json"))
}

/// save persists `pairing` for `connection_id` under `app_data_dir`,
/// `0600` on unix.
pub fn save(app_data_dir: &Path, connection_id: &str, pairing: &RelayPairing) -> io::Result<()> {
    let dir = pairing_dir(app_data_dir);
    std::fs::create_dir_all(&dir)?;
    let data = serde_json::to_string(&pairing.to_persisted())
        .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
    restrict_dir(&dir)?;
    let path = pairing_path(app_data_dir, connection_id);
    write_private(&path, data.as_bytes())?;
    // Also covers a pre-existing file created before this was 0600-at-birth.
    set_restrictive_permissions(&path)?;
    Ok(())
}

/// write_private creates (or truncates) `path` with mode 0600 at creation
/// time on unix, so the key material is never briefly readable by other
/// users between write and chmod.
#[cfg(unix)]
fn write_private(path: &Path, data: &[u8]) -> io::Result<()> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    let mut f = std::fs::OpenOptions::new().write(true).create(true).truncate(true).mode(0o600).open(path)?;
    f.write_all(data)?;
    f.sync_all()
}

#[cfg(not(unix))]
fn write_private(path: &Path, data: &[u8]) -> io::Result<()> {
    std::fs::write(path, data)
}

#[cfg(unix)]
fn restrict_dir(dir: &Path) -> io::Result<()> {
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(dir, std::fs::Permissions::from_mode(0o700))
}

#[cfg(not(unix))]
fn restrict_dir(_dir: &Path) -> io::Result<()> {
    Ok(())
}

#[cfg(unix)]
fn set_restrictive_permissions(path: &Path) -> io::Result<()> {
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600))
}

#[cfg(not(unix))]
fn set_restrictive_permissions(_path: &Path) -> io::Result<()> {
    // No POSIX mode bits on Windows: the file relies on the app data
    // directory's own per-user ACL, same posture as the Go daemon's
    // equally sensitive `relay-connect.json`/`relay-keypair.json`.
    Ok(())
}

/// load reads the pairing for `connection_id`, returning `Ok(None)` if
/// no file exists (a `relay` connection that was never fully paired, or
/// whose pairing was already removed) rather than erroring.
pub fn load(app_data_dir: &Path, connection_id: &str) -> io::Result<Option<RelayPairing>> {
    let path = pairing_path(app_data_dir, connection_id);
    match std::fs::read_to_string(&path) {
        Ok(data) => {
            let persisted: PersistedPairing = serde_json::from_str(&data)
                .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
            Ok(Some(RelayPairing::from_persisted(persisted)?))
        }
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e),
    }
}

/// delete removes the pairing file for `connection_id` (unpair/remove).
/// Missing is not an error — deleting an already-gone pairing is a no-op.
pub fn delete(app_data_dir: &Path, connection_id: &str) -> io::Result<()> {
    match std::fs::remove_file(pairing_path(app_data_dir, connection_id)) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(e),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample() -> RelayPairing {
        RelayPairing {
            daemon_id: "daemon-1".into(),
            daemon_public_key: [0x11; 32],
            relay_native_addr: "relay.example.test:7400".into(),
            relay_fingerprint: "deadbeef".into(),
            workspace_id: "ws-1".into(),
            admission_secret: vec![0xab; 32],
        }
    }

    #[test]
    fn round_trips() {
        let dir = tempfile::tempdir().unwrap();
        let pairing = sample();
        save(dir.path(), "conn-1", &pairing).unwrap();
        let got = load(dir.path(), "conn-1").unwrap().unwrap();
        assert_eq!(got, pairing);
    }

    #[test]
    fn missing_file_is_none_not_error() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(load(dir.path(), "nope").unwrap(), None);
    }

    #[test]
    fn malformed_file_errors_without_panicking() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(dir.path().join("relay")).unwrap();
        std::fs::write(dir.path().join("relay").join("conn-1.json"), b"not json").unwrap();
        assert!(load(dir.path(), "conn-1").is_err());
    }

    #[test]
    fn delete_removes_file_and_is_idempotent() {
        let dir = tempfile::tempdir().unwrap();
        let pairing = sample();
        save(dir.path(), "conn-1", &pairing).unwrap();
        delete(dir.path(), "conn-1").unwrap();
        assert_eq!(load(dir.path(), "conn-1").unwrap(), None);
        // Deleting again (already gone) must not error.
        delete(dir.path(), "conn-1").unwrap();
    }

    #[test]
    fn different_connections_do_not_collide() {
        let dir = tempfile::tempdir().unwrap();
        let mut a = sample();
        a.workspace_id = "ws-a".into();
        let mut b = sample();
        b.workspace_id = "ws-b".into();
        save(dir.path(), "conn-a", &a).unwrap();
        save(dir.path(), "conn-b", &b).unwrap();
        assert_eq!(
            load(dir.path(), "conn-a").unwrap().unwrap().workspace_id,
            "ws-a"
        );
        assert_eq!(
            load(dir.path(), "conn-b").unwrap().unwrap().workspace_id,
            "ws-b"
        );
    }

    #[cfg(unix)]
    #[test]
    fn file_has_restrictive_permissions_on_unix() {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        save(dir.path(), "conn-1", &sample()).unwrap();
        let path = pairing_path(dir.path(), "conn-1");
        let mode = std::fs::metadata(&path).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
    }
}
