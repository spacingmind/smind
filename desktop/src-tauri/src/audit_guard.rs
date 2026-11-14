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

/// split_segments_for is THE path normalisation: path text becomes a list of folded segments.
/// Both separators split on Windows, only `/` elsewhere (a backslash is an ordinary file name
/// character there); a verbatim `\\?\` or device `\\.\` prefix is removed (`UNC` becomes `//`);
/// empty and `.` segments are dropped; `..` removes the previous segment and never climbs above the
/// root; segments are lower-cased (the real home may sit on a case-insensitive volume, and folding
/// only ever refuses more). `windows` is a parameter so every rule is table-tested on every OS.
fn split_segments_for(text: &str, windows: bool) -> Vec<String> {
    let mut t = if windows {
        text.replace('\\', "/")
    } else {
        text.to_string()
    };
    if let Some(rest) = t.strip_prefix("//?/").or_else(|| t.strip_prefix("//./")) {
        t = match rest.strip_prefix("UNC/") {
            Some(unc) => format!("//{unc}"),
            None => rest.to_string(),
        };
    }
    let mut out: Vec<String> = Vec::new();
    for seg in t.split('/') {
        match seg {
            "" | "." => {}
            ".." => {
                out.pop();
            }
            other => out.push(other.to_lowercase()),
        }
    }
    out
}

fn split_segments(text: &str) -> Vec<String> {
    split_segments_for(text, cfg!(windows))
}

fn absolute(p: &Path) -> Option<PathBuf> {
    if p.is_absolute() {
        Some(p.to_path_buf())
    } else {
        Some(std::env::current_dir().ok()?.join(p))
    }
}

/// resolve_physical walks the components the way the operating system would: each existing
/// component is resolved through symlinks at once (so `link/..` is the parent of the link's target,
/// and `/var` and `/private/var` are one place), a component that does not exist is kept as text, and
/// `..` removes the previous component of the path resolved so far. `None` (unsure: a dangling link
/// or a component that cannot be resolved) makes the caller refuse.
fn resolve_physical(p: &Path) -> Option<PathBuf> {
    let mut cur = PathBuf::new();
    for c in absolute(p)?.components() {
        match c {
            Component::Prefix(_) | Component::RootDir => cur.push(c.as_os_str()),
            Component::CurDir => {}
            Component::ParentDir => {
                cur.pop();
            }
            Component::Normal(n) => {
                cur.push(n);
                if std::fs::symlink_metadata(&cur).is_ok() {
                    cur = cur.canonicalize().ok()?;
                }
            }
        }
    }
    Some(cur)
}

/// resolve_lexical removes `.` and `..` from the text first (how a program that cleans the path
/// before using it reads it) and only then resolves symlinks on what is left.
fn resolve_lexical(p: &Path) -> Option<PathBuf> {
    let mut clean = PathBuf::new();
    for c in absolute(p)?.components() {
        match c {
            Component::Prefix(_) | Component::RootDir => clean.push(c.as_os_str()),
            Component::CurDir => {}
            Component::ParentDir => {
                clean.pop();
            }
            Component::Normal(n) => clean.push(n),
        }
    }
    resolve_physical(&clean)
}

/// variants are the two readings of a path that must both stay clear of the real home: the
/// operating system's (`resolve_physical`) and a path cleaner's (`resolve_lexical`), each as folded
/// segments. They differ only when a symlink is followed by `..`.
fn variants(p: &Path) -> Option<Vec<Vec<String>>> {
    let physical = split_segments(resolve_physical(p)?.to_str()?);
    let lexical = split_segments(resolve_lexical(p)?.to_str()?);
    Some(vec![physical, lexical])
}

fn starts_with_segments(path: &[String], prefix: &[String]) -> bool {
    path.len() >= prefix.len() && path[..prefix.len()] == *prefix
}

fn check_home(
    get: &dyn Fn(&str) -> Option<String>,
    real_home: Option<&Path>,
) -> Result<(), String> {
    let raw = nonempty(get("SMIND_HOME")).ok_or_else(|| {
        "SMIND_HOME is not set: the audit app must run against a scratch daemon home, never the real one".to_string()
    })?;
    let cannot = |what: &str| {
        format!("cannot compare {what} with the real smind home, so the audit app refuses to start")
    };
    let homes = variants(Path::new(&raw)).ok_or_else(|| cannot(&format!("SMIND_HOME {raw:?}")))?;
    // Fail closed: without the account's real home there is nothing safe to compare against.
    let real_home = real_home.ok_or_else(|| {
        "cannot determine the account's real home directory, so the audit app refuses to start"
            .to_string()
    })?;
    let mut reals: Vec<Vec<String>> =
        variants(&real_home.join(".smind")).ok_or_else(|| cannot("the real home"))?;
    if let Some(h) = nonempty(get("HOME")) {
        reals
            .extend(variants(&Path::new(&h).join(".smind")).ok_or_else(|| cannot("$HOME/.smind"))?);
    }
    for home in &homes {
        for real in &reals {
            // ~/.smind/dogfood is the scratch place for audits, not the daemon home.
            let mut dogfood = real.clone();
            dogfood.push("dogfood".to_string());
            if starts_with_segments(home, &dogfood) {
                continue;
            }
            if starts_with_segments(home, real) {
                return Err(format!(
                    "SMIND_HOME {raw:?} is, or is under, the real smind home: use a scratch directory such as ~/.smind/dogfood/<name>/home"
                ));
            }
        }
    }
    Ok(())
}

fn check_url(get: &dyn Fn(&str) -> Option<String>) -> Result<(), String> {
    let raw = nonempty(get("SMIND_DAEMON_URL")).ok_or_else(|| {
        "SMIND_DAEMON_URL is not set: without it the app would use the default real daemon on port 4648".to_string()
    })?;
    let url = Url::parse(raw.trim())
        .map_err(|e| format!("SMIND_DAEMON_URL {raw:?} is not a URL: {e}"))?;
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
        return Err(format!(
            "SMIND_DAEMON_URL {raw:?} must point at this machine (127.0.0.1, ::1 or localhost)"
        ));
    }
    let port = url
        .port()
        .ok_or_else(|| format!("SMIND_DAEMON_URL {raw:?} needs an explicit scratch port"))?;
    if REAL_DAEMON_PORTS.contains(&port) {
        return Err(format!(
            "SMIND_DAEMON_URL {raw:?} uses port {port}, which belongs to the real daemon"
        ));
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
        Some(std::path::PathBuf::from(std::ffi::OsStr::from_bytes(
            dir.to_bytes(),
        )))
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
        let m: HashMap<String, String> = pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        move |k| m.get(k).cloned()
    }

    /// Fixture: a real temp directory standing in for the account's home, so every path is built
    /// with platform joins (`C:\...` on Windows, `/...` elsewhere) and every comparison is real.
    struct Fx {
        dir: tempfile::TempDir,
    }

    impl Fx {
        fn new() -> Fx {
            Fx {
                dir: tempfile::tempdir().unwrap(),
            }
        }
        fn real_home(&self) -> PathBuf {
            let h = self.dir.path().join("someone");
            std::fs::create_dir_all(h.join(".smind")).unwrap();
            h
        }
        fn smind(&self) -> PathBuf {
            self.real_home().join(".smind")
        }
        /// env with SMIND_HOME = home and a loopback scratch URL; HOME is an unrelated scratch dir.
        fn env(&self, home: &Path) -> impl Fn(&str) -> Option<String> {
            let scratch = self.dir.path().join("scratch-home");
            let m: HashMap<String, String> = [
                ("SMIND_HOME", home.to_string_lossy().into_owned()),
                ("SMIND_DAEMON_URL", "http://127.0.0.1:4719".to_string()),
                ("HOME", scratch.to_string_lossy().into_owned()),
            ]
            .into_iter()
            .map(|(k, v)| (k.to_string(), v))
            .collect();
            move |k| m.get(k).cloned()
        }
    }

    #[test]
    fn home_must_be_set() {
        let fx = Fx::new();
        let real = fx.real_home();
        assert!(check(
            &env(&[("SMIND_DAEMON_URL", "http://127.0.0.1:4719")]),
            Some(&real)
        )
        .unwrap_err()
        .contains("SMIND_HOME"));
        assert!(check(
            &env(&[
                ("SMIND_HOME", ""),
                ("SMIND_DAEMON_URL", "http://127.0.0.1:4719")
            ]),
            Some(&real)
        )
        .unwrap_err()
        .contains("SMIND_HOME"));
        assert!(check(
            &env(&[
                ("SMIND_HOME", "  "),
                ("SMIND_DAEMON_URL", "http://127.0.0.1:4719")
            ]),
            Some(&real)
        )
        .is_err());
    }

    #[test]
    fn home_rejects_the_real_smind_home() {
        let fx = Fx::new();
        let e = fx.env(&fx.smind());
        assert!(check(&e, Some(&fx.real_home()))
            .unwrap_err()
            .contains("real smind home"));
    }

    #[test]
    fn home_rejects_under_the_real_smind_home() {
        let fx = Fx::new();
        let smind = fx.smind();
        let cases = [
            smind.join("worktrees").join("x"),
            // a lexical `..` that comes back into the real home
            smind.join("..").join(".smind").join("z"),
            smind.join("a").join("..").join("b"),
            // a sibling that only shares a name prefix with dogfood is not the scratch place
            smind.join("dogfoodx"),
            // `.` segments
            smind.join(".").join("w"),
        ];
        for p in cases {
            assert!(
                check(&fx.env(&p), Some(&fx.real_home())).is_err(),
                "{p:?} must be refused"
            );
        }
    }

    #[test]
    fn home_rejects_the_real_home_in_any_separator_and_case_spelling() {
        let fx = Fx::new();
        let smind = fx.smind();
        let text = smind.to_string_lossy().into_owned();
        let mut spellings = vec![
            text.to_uppercase(),
            format!("{text}/z"),
            format!("{text}//z"),
        ];
        if cfg!(windows) {
            spellings.push(text.replace('\\', "/"));
            spellings.push(format!("{text}\\z"));
            spellings.push(format!("\\\\?\\{text}"));
        }
        for s in spellings {
            // folding is unconditional (the real home may be on a case-insensitive volume), so every
            // spelling of the real home is refused on every platform
            assert!(
                check(&fx.env(Path::new(&s)), Some(&fx.real_home())).is_err(),
                "{s:?} must be refused"
            );
        }
    }

    #[test]
    fn home_allows_dogfood_under_the_real_home() {
        let fx = Fx::new();
        let p = fx
            .smind()
            .join("dogfood")
            .join("desktop-audit")
            .join("home");
        assert!(check(&fx.env(&p), Some(&fx.real_home())).is_ok());
    }

    #[test]
    fn home_rejects_a_dogfood_escape() {
        let fx = Fx::new();
        let p = fx.smind().join("dogfood").join("..").join("worktrees");
        assert!(
            check(&fx.env(&p), Some(&fx.real_home())).is_err(),
            "dogfood/../worktrees is the real home"
        );
    }

    #[test]
    fn home_allows_a_scratch_dir_elsewhere() {
        let fx = Fx::new();
        let real = fx.real_home();
        assert!(check(&fx.env(&fx.dir.path().join("audit-home")), Some(&real)).is_ok());
        assert!(check(&fx.env(&real.join("scratch").join("home")), Some(&real)).is_ok());
    }

    #[test]
    fn home_fails_closed_when_the_real_home_is_unknown() {
        let fx = Fx::new();
        let e = fx.env(&fx.dir.path().join("audit-home"));
        assert!(check(&e, None).unwrap_err().contains("real home"));
    }

    #[test]
    fn home_under_a_file_is_not_mistaken_for_the_real_home() {
        let fx = Fx::new();
        // a scratch path whose existing ancestor is a file: it cannot exist, the walk-up stops at the file,
        // and the comparison must still work (no panic, no false refusal of an unrelated scratch path)
        let file = fx.dir.path().join("a-file");
        std::fs::write(&file, b"x").unwrap();
        let p = file.join("child");
        let r = check(&fx.env(&p), Some(&fx.real_home()));
        assert!(
            r.is_ok(),
            "a scratch path under a file is not the real home: {r:?}"
        );
    }

    #[cfg(unix)]
    #[test]
    fn home_rejects_a_symlink_onto_the_real_home() {
        let fx = Fx::new();
        let link = fx.dir.path().join("link");
        std::os::unix::fs::symlink(fx.smind(), &link).unwrap();
        assert!(check(&fx.env(&link), Some(&fx.real_home())).is_err());
        // a path under the link that does not exist yet is resolved through it too
        assert!(check(&fx.env(&link.join("new-dir")), Some(&fx.real_home())).is_err());
    }

    #[test]
    fn home_rejects_the_env_home_dot_smind() {
        // $HOME/.smind is refused too, even when the user-database lookup names a different real home.
        let fx = Fx::new();
        let other_real = fx.dir.path().join("another-account");
        let env_home = fx.dir.path().join("envhome");
        let m: HashMap<String, String> = [
            (
                "SMIND_HOME",
                env_home.join(".smind").to_string_lossy().into_owned(),
            ),
            ("SMIND_DAEMON_URL", "http://127.0.0.1:4719".to_string()),
            ("HOME", env_home.to_string_lossy().into_owned()),
        ]
        .into_iter()
        .map(|(k, v)| (k.to_string(), v))
        .collect();
        let e = move |k: &str| m.get(k).cloned();
        assert!(check(&e, Some(&other_real)).is_err());
    }

    /// One table for the normalisation, run on every OS: (text, windows rules?, expected segments).
    #[test]
    fn split_segments_table() {
        let t = |text: &str, windows: bool, want: &[&str]| {
            assert_eq!(
                split_segments_for(text, windows),
                want,
                "{text:?} (windows rules: {windows})"
            );
        };
        for windows in [false, true] {
            t("", windows, &[]);
            t("/", windows, &[]);
            t(".", windows, &[]);
            t("..", windows, &[]);
            t("/a/b/c", windows, &["a", "b", "c"]);
            t("/a/b/../c/./d", windows, &["a", "c", "d"]);
            t("/a//b///c/", windows, &["a", "b", "c"]);
            t("/A/B", windows, &["a", "b"]);
            // leading and repeated `..`, and `..` escaping the root, never climb above it
            t("../a", windows, &["a"]);
            t("/../../a", windows, &["a"]);
            t("/a/../../../b", windows, &["b"]);
            t("/a/b/../../..", windows, &[]);
            t("/a/./././b", windows, &["a", "b"]);
            // a `..` that returns into the same directory
            t("/h/.smind/../.smind/z", windows, &["h", ".smind", "z"]);
            t(
                "/h/.smind/dogfood/../worktrees",
                windows,
                &["h", ".smind", "worktrees"],
            );
            // verbatim and device prefixes come off, UNC keeps server and share as segments
            t("//?/C:/Users/x", windows, &["c:", "users", "x"]);
            t("//./C:/x", windows, &["c:", "x"]);
            t("//?/UNC/srv/share/x", windows, &["srv", "share", "x"]);
        }
        // Windows rules: both separators split
        t(
            "C:\\Users\\x\\.smind\\..\\.smind\\z",
            true,
            &["c:", "users", "x", ".smind", "z"],
        );
        t("C:/Users\\x/y", true, &["c:", "users", "x", "y"]);
        t("\\\\?\\C:\\Users\\x", true, &["c:", "users", "x"]);
        t("\\\\?\\UNC\\srv\\share\\x", true, &["srv", "share", "x"]);
        t("C:\\a\\..\\..\\b", true, &["b"]);
        // other rules: a backslash is an ordinary file name character
        t("/a\\b/c", false, &["a\\b", "c"]);
        t("/a\\..\\b", false, &["a\\..\\b"]);
    }

    #[cfg(unix)]
    #[test]
    fn home_rejects_the_real_home_reached_through_a_symlinked_parent() {
        // macOS keeps temp dirs under /var -> /private/var: one place with two spellings. The real
        // home given by one spelling and SMIND_HOME by the other must still be recognised.
        let fx = Fx::new();
        let alias = fx.dir.path().join("alias");
        std::os::unix::fs::symlink(fx.real_home(), &alias).unwrap();
        let real_via_alias = alias; // real home spelled through a symlink
        let smind_direct = fx.smind();
        assert!(check(&fx.env(&smind_direct), Some(&real_via_alias)).is_err());
        assert!(check(
            &fx.env(&smind_direct.join("a").join("..").join("b")),
            Some(&real_via_alias)
        )
        .is_err());
        let smind_via_alias = real_via_alias.join(".smind");
        assert!(check(&fx.env(&smind_via_alias), Some(&fx.real_home())).is_err());
        assert!(check(
            &fx.env(&smind_via_alias.join("dogfood").join("..").join("w")),
            Some(&fx.real_home())
        )
        .is_err());
        // dogfood stays allowed through either spelling
        assert!(check(
            &fx.env(&smind_via_alias.join("dogfood").join("x")),
            Some(&fx.real_home())
        )
        .is_ok());
    }

    #[cfg(unix)]
    #[test]
    fn home_rejects_a_link_followed_by_dotdot_in_either_reading() {
        // scratch/link -> <real>/.smind/sub: the OS reads scratch/link/../x as <real>/.smind/x, a path
        // cleaner reads it as scratch/x. Both readings are checked, so the first refuses it. And the
        // reverse: scratch/link2 -> <real>/.smind/dogfood/d reads physically as dogfood, lexically as scratch.
        let fx = Fx::new();
        let sub = fx.smind().join("sub");
        std::fs::create_dir_all(&sub).unwrap();
        let scratch = fx.dir.path().join("scratch");
        std::fs::create_dir_all(&scratch).unwrap();
        let link = scratch.join("link");
        std::os::unix::fs::symlink(&sub, &link).unwrap();
        let p = link.join("..").join("x");
        assert!(check(&fx.env(&p), Some(&fx.real_home())).is_err(), "{p:?}");
        // the other way round: a link INSIDE the real home that points outside it, followed by `..`:
        // the OS reads <elsewhere>/y, a path cleaner reads <real>/.smind/y; the cleaner's reading refuses it
        let outside = fx.dir.path().join("outside").join("deep");
        std::fs::create_dir_all(&outside).unwrap();
        let inner = fx.smind().join("inner");
        std::os::unix::fs::symlink(&outside, &inner).unwrap();
        let q = inner.join("..").join("y");
        assert!(check(&fx.env(&q), Some(&fx.real_home())).is_err(), "{q:?}");
        // a link whose target is elsewhere, followed by `..`, is just scratch
        let other = fx.dir.path().join("elsewhere").join("deep");
        std::fs::create_dir_all(&other).unwrap();
        let link2 = scratch.join("link2");
        std::os::unix::fs::symlink(&other, &link2).unwrap();
        assert!(check(&fx.env(&link2.join("..").join("x")), Some(&fx.real_home())).is_ok());
    }

    #[cfg(unix)]
    #[test]
    fn home_fails_closed_on_a_dangling_link() {
        let fx = Fx::new();
        let dangling = fx.dir.path().join("dangling");
        std::os::unix::fs::symlink(fx.dir.path().join("does-not-exist"), &dangling).unwrap();
        let r = check(&fx.env(&dangling.join("home")), Some(&fx.real_home()));
        assert!(
            r.unwrap_err().contains("cannot compare"),
            "an unresolvable path must be refused"
        );
    }

    #[test]
    fn url_must_be_set() {
        let real = Path::new("/Users/someone");
        let base = [("SMIND_HOME", "/tmp/audit-home")];
        assert!(check(&env(&base), Some(real))
            .unwrap_err()
            .contains("SMIND_DAEMON_URL"));
        let with_empty = [("SMIND_HOME", "/tmp/audit-home"), ("SMIND_DAEMON_URL", "")];
        assert!(check(&env(&with_empty), Some(real))
            .unwrap_err()
            .contains("SMIND_DAEMON_URL"));
    }

    fn url_result(url: &str) -> Result<(), String> {
        let e = env(&[("SMIND_HOME", "/tmp/audit-home"), ("SMIND_DAEMON_URL", url)]);
        check(&e, Some(Path::new("/Users/someone")))
    }

    #[test]
    fn url_rejects_garbage_and_other_schemes() {
        for u in [
            "not a url",
            "ftp://127.0.0.1:4719",
            "ws://127.0.0.1:4719",
            "127.0.0.1:4719",
            "http://",
        ] {
            assert!(url_result(u).is_err(), "{u} must be refused");
        }
    }

    #[test]
    fn url_needs_an_explicit_port() {
        assert!(url_result("http://127.0.0.1")
            .unwrap_err()
            .contains("explicit"));
        assert!(url_result("http://127.0.0.1:80")
            .unwrap_err()
            .contains("explicit"));
        assert!(url_result("https://localhost").is_err());
    }

    #[test]
    fn url_rejects_4648_and_4870() {
        for u in [
            "http://127.0.0.1:4648",
            "http://localhost:4648/",
            "http://[::1]:4648",
            "http://127.0.0.1:4870",
        ] {
            assert!(
                url_result(u).unwrap_err().contains("real daemon"),
                "{u} must be refused"
            );
        }
    }

    #[test]
    fn url_rejects_a_remote_host() {
        for u in [
            "http://example.com:4719",
            "http://192.168.1.5:4719",
            &format!("http://{}:4719", ["0"; 4].join(".")),
            "http://127.0.0.1.evil.test:4719",
        ] {
            assert!(
                url_result(u).unwrap_err().contains("this machine"),
                "{u} must be refused"
            );
        }
    }

    #[test]
    fn url_allows_a_loopback_scratch_port() {
        for u in [
            "http://127.0.0.1:4719",
            "http://localhost:4719",
            "http://[::1]:4719",
            "https://127.0.0.1:4719/path",
        ] {
            assert!(url_result(u).is_ok(), "{u} must be allowed");
        }
    }

    #[test]
    fn run_enforces_the_guard_before_building_the_app() {
        let lib = include_str!("lib.rs");
        let run = lib.find("pub fn run()").expect("run() exists");
        let body = &lib[run..];
        let guard = body
            .find("audit_guard::enforce_or_exit()")
            .expect("run() calls the audit guard");
        let builder = body
            .find("tauri::Builder::default()")
            .expect("run() builds the app");
        assert!(
            guard < builder,
            "the guard must run before the Tauri builder is created"
        );
        let before = &body[..guard];
        assert!(
            before
                .trim_end()
                .ends_with("#[cfg(feature = \"audit-guard\")]"),
            "the call must be behind the feature"
        );
    }

    /// The `[features]` table of Cargo.toml, as (name, value) lines.
    fn feature_lines() -> Vec<(String, String)> {
        let cargo = include_str!("../Cargo.toml");
        let table = cargo
            .split("\n[features]\n")
            .nth(1)
            .expect("Cargo.toml has a [features] table");
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
        let default = features
            .iter()
            .find(|(k, _)| k == "default")
            .expect("Cargo.toml sets default features");
        assert_eq!(
            default.1, "[]",
            "default features must stay empty, got {}",
            default.1
        );
        let audit = features
            .iter()
            .find(|(k, _)| k == "audit-guard")
            .expect("the audit-guard feature exists");
        assert!(
            audit.1.contains("dep:libc"),
            "audit-guard enables the optional libc dependency: {}",
            audit.1
        );
        // no other feature pulls it in either
        for (name, value) in &features {
            if name != "audit-guard" {
                assert!(
                    !value.contains("audit-guard"),
                    "feature {name} must not enable audit-guard: {value}"
                );
            }
        }
    }
}
