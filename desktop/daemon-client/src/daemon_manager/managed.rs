//! The managed/unmanaged decision (AC3): the app only updates/restarts a
//! daemon it started itself, tracked by a pid recorded in `ManagedRecord`
//! and re-validated against whoever is *actually* bound to the configured
//! port right now (`find_port_owner`, implemented per-platform in
//! `wsl`/`native`). This module is pure -- callers resolve "is a daemon
//! reachable" and "who owns the port" themselves (real OS calls) and pass
//! the answers in, so the decision table itself needs no OS access to test.

use std::fs;
use std::path::Path;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ManagedRecord {
    pub pid: u32,
    pub version: String,
    #[serde(rename = "installedAt")]
    pub installed_at: String,
    #[serde(rename = "exePath")]
    pub exe_path: String,
}

/// ManagedState is the decision-table's output: what the UI should show
/// and which actions are valid.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ManagedState {
    /// Nothing is answering on the configured port (or a recorded managed
    /// process died without anything else taking the port). Offer Install.
    NotRunning,
    /// The port's current owner matches our recorded pid. Offer
    /// Update/Restart.
    Managed,
    /// Something is answering on the port, but it isn't the process we
    /// recorded (or we have no record at all). Never touched; offer
    /// "take over management" only with explicit confirmation.
    Unmanaged,
}

/// classify implements the AC3 decision table. `port_owner` is `None` when
/// nothing is listening on the configured port at all.
pub fn classify(record: Option<&ManagedRecord>, port_owner: Option<u32>) -> ManagedState {
    match port_owner {
        None => ManagedState::NotRunning,
        Some(owner) => match record {
            Some(r) if r.pid == owner => ManagedState::Managed,
            _ => ManagedState::Unmanaged,
        },
    }
}

/// take_over builds a fresh record trusting `port_owner` as the managed
/// pid from now on. It performs no process action of its own -- see the
/// module doc and AC3: invoking this *is* the confirmed action, so the
/// confirmation step lives entirely in the caller (the UI).
pub fn take_over(port_owner: u32, version_unknown_marker: &str, exe_path_unknown_marker: &str, installed_at: &str) -> ManagedRecord {
    ManagedRecord {
        pid: port_owner,
        version: version_unknown_marker.to_string(),
        installed_at: installed_at.to_string(),
        exe_path: exe_path_unknown_marker.to_string(),
    }
}

/// load reads a `ManagedRecord` from `path`, treating a missing or
/// malformed file as "no record" (never fails startup).
pub fn load(path: &Path) -> Option<ManagedRecord> {
    let data = fs::read_to_string(path).ok()?;
    serde_json::from_str(&data).ok()
}

/// save writes `record` to `path` as JSON, creating parent directories as
/// needed.
pub fn save(path: &Path, record: &ManagedRecord) -> std::io::Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    let data = serde_json::to_string_pretty(record).expect("smind desktop: managed record serializes");
    fs::write(path, data)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn record(pid: u32) -> ManagedRecord {
        ManagedRecord { pid, version: "0.7.0".to_string(), installed_at: "2026-09-25T00:00:00Z".to_string(), exe_path: "/tmp/smind".to_string() }
    }

    #[test]
    fn nothing_listening_is_not_running_even_with_a_record() {
        assert_eq!(classify(Some(&record(123)), None), ManagedState::NotRunning);
        assert_eq!(classify(None, None), ManagedState::NotRunning);
    }

    #[test]
    fn port_owner_matches_record_is_managed() {
        assert_eq!(classify(Some(&record(123)), Some(123)), ManagedState::Managed);
    }

    #[test]
    fn port_owner_mismatch_or_no_record_is_unmanaged() {
        assert_eq!(classify(Some(&record(123)), Some(456)), ManagedState::Unmanaged);
        assert_eq!(classify(None, Some(456)), ManagedState::Unmanaged);
    }

    #[test]
    fn take_over_produces_a_record_matching_the_port_owner() {
        let r = take_over(789, "unknown", "unknown", "2026-09-25T00:00:00Z");
        assert_eq!(r.pid, 789);
        assert_eq!(classify(Some(&r), Some(789)), ManagedState::Managed);
    }

    #[test]
    fn persistence_round_trip() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("managed.json");
        let r = record(42);
        save(&path, &r).unwrap();
        let loaded = load(&path).unwrap();
        assert_eq!(loaded, r);
    }

    #[test]
    fn load_missing_or_malformed_is_none() {
        let dir = tempfile::tempdir().unwrap();
        assert!(load(&dir.path().join("nope.json")).is_none());
        let bad = dir.path().join("bad.json");
        fs::write(&bad, "not json").unwrap();
        assert!(load(&bad).is_none());
    }
}
