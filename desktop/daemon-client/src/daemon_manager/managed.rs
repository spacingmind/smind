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
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
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

/// assert_safe_to_install refuses `install`/`update` outright when the
/// port is currently owned by a process this app doesn't manage
/// (`ManagedState::Unmanaged`) -- the bug this guards against: install/
/// update would otherwise overwrite the binary and try to start a new
/// process on an already-occupied port, and a naive "whoever now owns the
/// port must be ours" pid-recovery step (see `verify_fresh_start`) would
/// then adopt the *existing unmanaged* daemon's pid as if the app had
/// just started it -- after which a later Restart/Update kills the
/// user's own daemon. `NotRunning` and `Managed` both proceed; only
/// `take_over` (an explicit, user-confirmed action) may adopt an
/// `Unmanaged` daemon.
pub fn assert_safe_to_install(record: Option<&ManagedRecord>, port_owner: Option<u32>, port: u16) -> Result<(), String> {
    if classify(record, port_owner) == ManagedState::Unmanaged {
        let pid = port_owner.expect("smind desktop: Unmanaged implies a port owner");
        return Err(format!(
            "an unmanaged daemon (pid {pid}) is already running on port {port} -- stop it, or use \"take over management\""
        ));
    }
    Ok(())
}

/// safe_to_kill decides whether it's safe to signal `record.pid` before a
/// fresh start (install/update's own restart, and `restart` itself): the
/// live port owner must still be exactly this pid (closes the same
/// PID-reuse race `restart` already guarded against), *and* the process
/// at that pid must actually be the binary this record names (an
/// independent identity check -- a pid can be reused by an unrelated
/// process, including the user's own unmanaged daemon, even while
/// numerically matching a stale record). `exe_matches` is computed by the
/// caller (`native`/`wsl`'s own OS-specific identity check), so this
/// function needs no OS access to test.
pub fn safe_to_kill(record: &ManagedRecord, port_owner: Option<u32>, exe_matches: bool) -> bool {
    port_owner == Some(record.pid) && exe_matches
}

/// verify_started_identity is the part of "is this really ours" that
/// always applies after (re)starting a process, whether or not a new
/// version was just installed: something must be listening, and the
/// identity check must pass -- the same check that would have caught Bug
/// 1 (if the port is still held by the previous unmanaged occupant
/// because our own process failed to bind, its exe path will not match).
/// Used directly by `restart` (which doesn't install anything, so there
/// is no "expected version" to compare against); `verify_fresh_start`
/// layers the version check on top for install/update.
pub fn verify_started_identity(port_owner: Option<u32>, exe_matches: bool) -> Result<u32, String> {
    let pid = port_owner.ok_or_else(|| "daemon did not start (nothing is listening on the configured port)".to_string())?;
    if !exe_matches {
        return Err("daemon did not start (the process on the configured port is not the binary that was just installed)".to_string());
    }
    Ok(pid)
}

/// verify_fresh_start decides whether a just-started process is really
/// ours before the caller trusts it enough to save a `ManagedRecord`:
/// `verify_started_identity`, plus -- since this is only ever called
/// right after install/update -- the daemon's reported `/healthz`
/// version (if it answered at all) must match what was just installed.
pub fn verify_fresh_start(port_owner: Option<u32>, exe_matches: bool, reported_version: Option<&str>, expected_version: &str) -> Result<u32, String> {
    let pid = verify_started_identity(port_owner, exe_matches)?;
    if let Some(reported) = reported_version {
        if reported != expected_version {
            return Err(format!("daemon did not start (reports version {reported:?}, expected {expected_version:?})"));
        }
    }
    Ok(pid)
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
    fn assert_safe_to_install_refuses_when_unmanaged() {
        // The exact regression this guards against: an unmanaged daemon
        // (e.g. the user's own `./bin/smind serve`) already owns the
        // port -- install/update must refuse outright, before touching
        // anything, rather than adopting it.
        let err = assert_safe_to_install(None, Some(999), 4648).unwrap_err();
        assert!(err.contains("999"), "error should name the unmanaged pid: {err}");
        assert!(err.contains("take over"), "error should point at take over: {err}");

        let err2 = assert_safe_to_install(Some(&record(123)), Some(999), 4648).unwrap_err();
        assert!(err2.contains("999"));
    }

    #[test]
    fn assert_safe_to_install_allows_not_running_or_already_managed() {
        assert!(assert_safe_to_install(None, None, 4648).is_ok());
        assert!(assert_safe_to_install(Some(&record(123)), None, 4648).is_ok());
        assert!(assert_safe_to_install(Some(&record(123)), Some(123), 4648).is_ok());
    }

    #[test]
    fn safe_to_kill_requires_both_port_match_and_exe_match() {
        let r = record(123);
        assert!(safe_to_kill(&r, Some(123), true));
        // Stale record: pid is alive (matches some port owner numerically
        // or not) but exe identity doesn't check out, or the port owner
        // has since changed -- either alone must block the kill.
        assert!(!safe_to_kill(&r, Some(123), false));
        assert!(!safe_to_kill(&r, Some(456), true));
        assert!(!safe_to_kill(&r, None, true));
    }

    #[test]
    fn verify_started_identity_requires_an_owner_and_exe_match() {
        assert!(verify_started_identity(None, true).is_err());
        assert!(verify_started_identity(Some(999), false).is_err());
        assert_eq!(verify_started_identity(Some(999), true), Ok(999));
    }

    #[test]
    fn verify_fresh_start_requires_an_owner_and_exe_match() {
        assert!(verify_fresh_start(None, true, Some("0.7.0"), "0.7.0").is_err());
        // Bug 1's exact shape: something is listening (the old unmanaged
        // daemon, because ours failed to bind), but it isn't our binary.
        let err = verify_fresh_start(Some(999), false, None, "0.7.0").unwrap_err();
        assert!(err.contains("not the binary"), "{err}");
        assert_eq!(verify_fresh_start(Some(999), true, None, "0.7.0"), Ok(999));
    }

    #[test]
    fn verify_fresh_start_rejects_a_version_mismatch() {
        let err = verify_fresh_start(Some(999), true, Some("0.6.0"), "0.7.0").unwrap_err();
        assert!(err.contains("0.6.0") && err.contains("0.7.0"), "{err}");
        assert_eq!(verify_fresh_start(Some(999), true, Some("0.7.0"), "0.7.0"), Ok(999));
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
