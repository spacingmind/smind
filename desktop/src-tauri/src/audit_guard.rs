//! The audit build's launch guard (docs/plans/active/desktop-audit-app-guard.md).
//!
//! `scripts/desktop-audit-build.sh` builds the app with the `audit-guard` cargo
//! feature. Such an app must never reach the real daemon: it starts only when
//! `SMIND_HOME` names a scratch daemon home and `SMIND_DAEMON_URL` names a loopback
//! daemon on a port that is not the real daemon's, and otherwise exits with a
//! message before it touches any daemon, file or window. A release build does not
//! set the feature, so none of this runs there.
//!
//! The rule is a pure function over an environment lookup and the account's real
//! home directory, so it is tested without a window; `enforce_or_exit` is the one
//! call at the top of `run()`.

use std::path::{Component, Path, PathBuf};

use url::{Host, Url};

/// The ports of the real daemon and its alternate default: an audit app must not use them.
const REAL_DAEMON_PORTS: [u16; 2] = [4648, 4870];

fn nonempty(v: Option<String>) -> Option<String> {
    v.filter(|s| !s.trim().is_empty())
}

/// canon_loose resolves symlinks for the deepest existing ancestor of `p` and appends the
/// rest, so a path that does not exist yet is still compared by its real location.
fn canon_loose(p: &Path) -> PathBuf {
    let abs = if p.is_absolute() {
        p.to_path_buf()
    } else {
        std::env::current_dir().map(|d| d.join(p)).unwrap_or_else(|_| p.to_path_buf())
    };
    let mut existing = abs.clone();
    let mut rest: Vec<std::ffi::OsString> = Vec::new();
    while !existing.exists() {
        match (existing.file_name(), existing.parent()) {
            (Some(name), Some(parent)) => {
                rest.push(name.to_os_string());
                existing = parent.to_path_buf();
            }
            _ => break,
        }
    }
    let mut out = existing.canonicalize().unwrap_or(existing);
    for part in rest.into_iter().rev() {
        out.push(part);
    }
    // `..` components that survive in the non-existing tail would defeat starts_with.
    let mut clean = PathBuf::new();
    for c in out.components() {
        match c {
            Component::ParentDir => {
                clean.pop();
            }
            Component::CurDir => {}
            other => clean.push(other.as_os_str()),
        }
    }
    clean
}

fn check_home(get: &dyn Fn(&str) -> Option<String>, real_home: Option<&Path>) -> Result<(), String> {
    let raw = nonempty(get("SMIND_HOME")).ok_or_else(|| {
        "SMIND_HOME is not set: the audit app must run against a scratch daemon home, never the real one".to_string()
    })?;
    let home = canon_loose(Path::new(&raw));
    let mut real_homes: Vec<PathBuf> = Vec::new();
    if let Some(h) = real_home {
        real_homes.push(canon_loose(&h.join(".smind")));
    }
    if let Some(h) = nonempty(get("HOME")) {
        real_homes.push(canon_loose(&Path::new(&h).join(".smind")));
    }
    for real in real_homes {
        // ~/.smind/dogfood is the scratch place for audits, not the daemon home.
        if home.starts_with(real.join("dogfood")) {
            continue;
        }
        if home.starts_with(&real) {
            return Err(format!(
                "SMIND_HOME {raw:?} is, or is under, the real smind home {real:?}: use a scratch directory such as ~/.smind/dogfood/<name>/home"
            ));
        }
    }
    Ok(())
}

fn check_url(get: &dyn Fn(&str) -> Option<String>) -> Result<(), String> {
    let raw = nonempty(get("SMIND_DAEMON_URL")).ok_or_else(|| {
        "SMIND_DAEMON_URL is not set: without it the app would use the default real daemon on port 4648".to_string()
    })?;
    let url = Url::parse(raw.trim()).map_err(|e| format!("SMIND_DAEMON_URL {raw:?} is not a URL: {e}"))?;
    if url.scheme() != "http" && url.scheme() != "https" {
        return Err(format!("SMIND_DAEMON_URL {raw:?} must be an http(s) URL"));
    }
    let loopback = match url.host() {
        Some(Host::Domain(d)) => d.eq_ignore_ascii_case("localhost"),
        Some(Host::Ipv4(ip)) => ip.is_loopback(),
        Some(Host::Ipv6(ip)) => ip.is_loopback(),
        None => false,
    };
    if !loopback {
        return Err(format!("SMIND_DAEMON_URL {raw:?} must point at this machine (127.0.0.1, ::1 or localhost)"));
    }
    let port = url
        .port()
        .ok_or_else(|| format!("SMIND_DAEMON_URL {raw:?} needs an explicit scratch port"))?;
    if REAL_DAEMON_PORTS.contains(&port) {
        return Err(format!("SMIND_DAEMON_URL {raw:?} uses port {port}, which belongs to the real daemon"));
    }
    Ok(())
}

/// check is the whole rule: Ok when the environment names a scratch daemon, Err(reason) otherwise.
#[cfg_attr(not(any(feature = "audit-guard", test)), allow(dead_code))]
pub fn check(get: &dyn Fn(&str) -> Option<String>, real_home: Option<&Path>) -> Result<(), String> {
    check_home(get, real_home)?;
    check_url(get)
}

/// The account's real home directory from the user database. An isolated launch sets `HOME`
/// to a scratch directory, so `$HOME` alone cannot name the real `~/.smind`.
#[cfg(all(unix, feature = "audit-guard"))]
fn real_home_dir() -> Option<PathBuf> {
    use std::os::unix::ffi::OsStrExt;
    // SAFETY: called once at startup before any thread exists; the returned record is read
    // immediately and not kept.
    unsafe {
        let pw = libc::getpwuid(libc::getuid());
        if pw.is_null() || (*pw).pw_dir.is_null() {
            return None;
        }
        let dir = std::ffi::CStr::from_ptr((*pw).pw_dir);
        Some(PathBuf::from(std::ffi::OsStr::from_bytes(dir.to_bytes())))
    }
}

#[cfg(all(not(unix), feature = "audit-guard"))]
fn real_home_dir() -> Option<PathBuf> {
    None
}

/// enforce_or_exit runs the rule against the process environment and exits with status 2 and a
/// message on stderr when it fails. Called first in `run()`, before anything else starts.
#[cfg(feature = "audit-guard")]
pub fn enforce_or_exit() {
    let real_home = real_home_dir();
    if let Err(reason) = check(&|k| std::env::var(k).ok(), real_home.as_deref()) {
        eprintln!(
            "smind audit build: {reason}\n\
             This copy only runs against a scratch daemon. Set SMIND_HOME (a scratch directory, not ~/.smind) and \
             SMIND_DAEMON_URL (http://127.0.0.1:<port>, not 4648); see scripts/desktop-audit-build.sh."
        );
        std::process::exit(2);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    fn env(pairs: &[(&str, &str)]) -> impl Fn(&str) -> Option<String> {
        let m: HashMap<String, String> = pairs.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect();
        move |k| m.get(k).cloned()
    }

    fn ok_env(home: &str) -> Vec<(&str, &str)> {
        vec![("SMIND_HOME", home), ("SMIND_DAEMON_URL", "http://127.0.0.1:4719"), ("HOME", "/nonexistent-scratch-home")]
    }

    #[test]
    fn home_must_be_set() {
        let real = Path::new("/Users/someone");
        assert!(check(&env(&[("SMIND_DAEMON_URL", "http://127.0.0.1:4719")]), Some(real)).unwrap_err().contains("SMIND_HOME"));
        assert!(check(&env(&[("SMIND_HOME", ""), ("SMIND_DAEMON_URL", "http://127.0.0.1:4719")]), Some(real))
            .unwrap_err()
            .contains("SMIND_HOME"));
        assert!(check(&env(&[("SMIND_HOME", "  "), ("SMIND_DAEMON_URL", "http://127.0.0.1:4719")]), Some(real)).is_err());
    }

    #[test]
    fn home_rejects_the_real_smind_home() {
        let real = Path::new("/Users/someone");
        let e = env(&ok_env("/Users/someone/.smind"));
        assert!(check(&e, Some(real)).unwrap_err().contains("real smind home"));
    }

    #[test]
    fn home_rejects_under_the_real_smind_home() {
        let real = Path::new("/Users/someone");
        for p in ["/Users/someone/.smind/worktrees/x", "/Users/someone/.smind/../.smind/z", "/Users/someone/.smind/dogfoodx"] {
            assert!(check(&env(&ok_env(p)), Some(real)).is_err(), "{p} must be refused");
        }
    }

    #[test]
    fn home_allows_dogfood_under_the_real_home() {
        let real = Path::new("/Users/someone");
        assert!(check(&env(&ok_env("/Users/someone/.smind/dogfood/desktop-audit/home")), Some(real)).is_ok());
    }

    #[test]
    fn home_allows_a_scratch_dir_elsewhere() {
        let real = Path::new("/Users/someone");
        assert!(check(&env(&ok_env("/tmp/audit-home")), Some(real)).is_ok());
        assert!(check(&env(&ok_env("/Users/someone/scratch/home")), Some(real)).is_ok());
    }

    #[cfg(unix)]
    #[test]
    fn home_rejects_a_symlink_onto_the_real_home() {
        let dir = tempfile::tempdir().unwrap();
        let real_home = dir.path().join("realhome");
        std::fs::create_dir_all(real_home.join(".smind")).unwrap();
        let link = dir.path().join("link");
        std::os::unix::fs::symlink(real_home.join(".smind"), &link).unwrap();
        let e = env(&ok_env(link.to_str().unwrap()));
        assert!(check(&e, Some(&real_home)).is_err());
        // a path under the link that does not exist yet is resolved through it too
        let under = link.join("new-dir");
        let e = env(&ok_env(under.to_str().unwrap()));
        assert!(check(&e, Some(&real_home)).is_err());
    }

    #[test]
    fn home_rejects_the_env_home_dot_smind() {
        // The user-database lookup failed (None): $HOME/.smind is still refused.
        let e = env(&[("SMIND_HOME", "/Users/x/.smind"), ("SMIND_DAEMON_URL", "http://127.0.0.1:4719"), ("HOME", "/Users/x")]);
        assert!(check(&e, None).is_err());
    }

    #[test]
    fn url_must_be_set() {
        let real = Path::new("/Users/someone");
        let base = [("SMIND_HOME", "/tmp/audit-home")];
        assert!(check(&env(&base), Some(real)).unwrap_err().contains("SMIND_DAEMON_URL"));
        let with_empty = [("SMIND_HOME", "/tmp/audit-home"), ("SMIND_DAEMON_URL", "")];
        assert!(check(&env(&with_empty), Some(real)).unwrap_err().contains("SMIND_DAEMON_URL"));
    }

    fn url_result(url: &str) -> Result<(), String> {
        let e = env(&[("SMIND_HOME", "/tmp/audit-home"), ("SMIND_DAEMON_URL", url)]);
        check(&e, Some(Path::new("/Users/someone")))
    }

    #[test]
    fn url_rejects_garbage_and_other_schemes() {
        for u in ["not a url", "ftp://127.0.0.1:4719", "ws://127.0.0.1:4719", "127.0.0.1:4719", "http://"] {
            assert!(url_result(u).is_err(), "{u} must be refused");
        }
    }

    #[test]
    fn url_needs_an_explicit_port() {
        assert!(url_result("http://127.0.0.1").unwrap_err().contains("explicit"));
        assert!(url_result("http://127.0.0.1:80").unwrap_err().contains("explicit"));
        assert!(url_result("https://localhost").is_err());
    }

    #[test]
    fn url_rejects_4648_and_4870() {
        for u in ["http://127.0.0.1:4648", "http://localhost:4648/", "http://[::1]:4648", "http://127.0.0.1:4870"] {
            assert!(url_result(u).unwrap_err().contains("real daemon"), "{u} must be refused");
        }
    }

    #[test]
    fn url_rejects_a_remote_host() {
        for u in ["http://example.com:4719", "http://192.168.1.5:4719", "http://0.0.0.0:4719", "http://127.0.0.1.evil.test:4719"] {
            assert!(url_result(u).unwrap_err().contains("this machine"), "{u} must be refused");
        }
    }

    #[test]
    fn url_allows_a_loopback_scratch_port() {
        for u in ["http://127.0.0.1:4719", "http://localhost:4719", "http://[::1]:4719", "https://127.0.0.1:4719/path"] {
            assert!(url_result(u).is_ok(), "{u} must be allowed");
        }
    }

    #[test]
    fn run_enforces_the_guard_before_building_the_app() {
        let lib = include_str!("lib.rs");
        let run = lib.find("pub fn run()").expect("run() exists");
        let body = &lib[run..];
        let guard = body.find("audit_guard::enforce_or_exit()").expect("run() calls the audit guard");
        let builder = body.find("tauri::Builder::default()").expect("run() builds the app");
        assert!(guard < builder, "the guard must run before the Tauri builder is created");
        let before = &body[..guard];
        assert!(before.trim_end().ends_with("#[cfg(feature = \"audit-guard\")]"), "the call must be behind the feature");
    }

    /// The `[features]` table of Cargo.toml, as (name, value) lines.
    fn feature_lines() -> Vec<(String, String)> {
        let cargo = include_str!("../Cargo.toml");
        let table = cargo.split("\n[features]\n").nth(1).expect("Cargo.toml has a [features] table");
        table
            .lines()
            .take_while(|l| !l.trim_start().starts_with('['))
            .filter_map(|l| l.split_once('='))
            .map(|(k, v)| (k.trim().to_string(), v.trim().to_string()))
            .collect()
    }

    #[test]
    fn audit_guard_is_not_a_default_feature() {
        let features = feature_lines();
        let default = features.iter().find(|(k, _)| k == "default").expect("Cargo.toml sets default features");
        assert_eq!(default.1, "[]", "default features must stay empty, got {}", default.1);
        let audit = features.iter().find(|(k, _)| k == "audit-guard").expect("the audit-guard feature exists");
        assert!(audit.1.contains("dep:libc"), "audit-guard enables the optional libc dependency: {}", audit.1);
        // no other feature pulls it in either
        for (name, value) in &features {
            if name != "audit-guard" {
                assert!(!value.contains("audit-guard"), "feature {name} must not enable audit-guard: {value}");
            }
        }
    }
}
