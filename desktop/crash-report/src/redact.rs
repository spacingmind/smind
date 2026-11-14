//! Port of internal/crash/redact.go. The golden vectors in
//! internal/crash/testdata/redact_vectors.json are the contract (tests/golden.rs).
//!
//! Go's `\b` is ASCII-only while Rust's is Unicode, so every `\b` in a Go
//! pattern is written `(?-u:\b)` here; everything else is copied verbatim.

use regex::{Captures, NoExpand, Regex};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{LazyLock, Mutex};

static RE_UNIX_USER_DIR: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?i)(?:/Users/|/home/)[\p{L}\p{N}_.'+@-]+(?: [\p{L}\p{N}_.'+@-]+)*|/root(?-u:\b)",
    )
    .unwrap()
});
static RE_WINDOWS_USER: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)[A-Za-z]:[\\/]Users[\\/][\p{L}\p{N}_.'+@-]+(?: [\p{L}\p{N}_.'+@-]+)*")
        .unwrap()
});
static RE_BEARER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)(?-u:\b)(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]+").unwrap());
static RE_SK: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?-u:\b)sk-[A-Za-z0-9_-]{8,}").unwrap());
static RE_GITHUB_TOKEN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?-u:\b)(?:ghp|gho|ghs|ghu|ghr)_[A-Za-z0-9]{16,}").unwrap());
static RE_GITHUB_PAT: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?-u:\b)github_pat_[A-Za-z0-9_]{16,}").unwrap());
static RE_AWS_KEY: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?-u:\b)(?:AKIA|ASIA)[A-Z0-9]{16}(?-u:\b)").unwrap());
static RE_JWT: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?-u:\b)eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*").unwrap()
});
static RE_KEY_VALUE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r#"(?i)(["']?[a-z0-9_-]*(?:token|secret|pass(?:word|wd|phrase)?|apikey|api[_-]key|authorization|cookie|credential|private[_-]?key)[a-z0-9_-]*["']?\s*[=:]\s*)("[^"]*"|'[^']*'|\S+)"#,
    )
    .unwrap()
});
static RE_AUTH_LINE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r#"(?i)(["']?(?:proxy-)?authorization["']?\s*[=:]\s*|(?-u:\b)(?:set-)?cookie["']?\s*[=:]\s*)[^\r\n]*"#,
    )
    .unwrap()
});
static RE_SCHEME_CRED: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?i)((?-u:\b)(?:token|apikey|api-key|digest|negotiate|ntlm)\s+)[A-Za-z0-9._~+/=-]{12,}",
    )
    .unwrap()
});
static RE_URL_USERINFO: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)(?-u:\b)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@").unwrap()
});
static RE_HEX_RUN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?-u:\b)[0-9a-fA-F]{32,}(?-u:\b)").unwrap());
static RE_HIGH_ENTROPY: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[A-Za-z0-9_-]{32,}").unwrap());
static RE_PEM: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)",
    )
    .unwrap()
});
static RE_VENDOR_TOKENS: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?-u:\b)xox[abprs]-[A-Za-z0-9-]{8,}|(?-u:\b)AIza[0-9A-Za-z_-]{30,}|(?-u:\b)glpat-[A-Za-z0-9_-]{16,}|(?-u:\b)npm_[A-Za-z0-9]{30,}|(?-u:\b)ya29\.[A-Za-z0-9._-]{20,}|(?-u:\b)[sr]k_(?:live|test)_[A-Za-z0-9]{12,}",
    )
    .unwrap()
});

static PLAIN_SECRETS: [&LazyLock<Regex>; 9] = [
    &RE_PEM,
    &RE_BEARER,
    &RE_SK,
    &RE_GITHUB_TOKEN,
    &RE_GITHUB_PAT,
    &RE_AWS_KEY,
    &RE_VENDOR_TOKENS,
    &RE_JWT,
    &RE_HEX_RUN,
];

/// Removes paths under the user's home and smind home, other users'
/// directories, and secret shapes from `s`.
pub fn redact(s: &str, home: Option<&Path>, smind_home: Option<&Path>) -> String {
    // Applied until nothing changes (at most 3 passes): one rule's output can
    // expose the input of another, and the stored text must be stable.
    let mut s = s.to_string();
    for _ in 0..3 {
        let next = redact_once(&s, home, smind_home);
        if next == s {
            break;
        }
        s = next;
    }
    s
}

fn redact_once(s: &str, home: Option<&Path>, smind_home: Option<&Path>) -> String {
    let mut s = redact_paths(s, home, smind_home);
    // The Windows shape runs first: its drive-letter prefix would otherwise
    // survive a /Users/... match cut in the middle of the path.
    s = RE_WINDOWS_USER.replace_all(&s, "<user dir>").into_owned();
    s = RE_UNIX_USER_DIR.replace_all(&s, "<user dir>").into_owned();
    s = RE_AUTH_LINE
        .replace_all(&s, "${1}[redacted]")
        .into_owned();
    s = RE_SCHEME_CRED
        .replace_all(&s, "${1}[redacted]")
        .into_owned();
    for re in &PLAIN_SECRETS {
        s = re.replace_all(&s, NoExpand("[redacted]")).into_owned();
    }
    s = RE_KEY_VALUE
        .replace_all(&s, "${1}[redacted]")
        .into_owned();
    s = RE_URL_USERINFO
        .replace_all(&s, "${1}[redacted]@")
        .into_owned();
    s = RE_HIGH_ENTROPY
        .replace_all(&s, |caps: &Captures| redact_high_entropy_run(&caps[0]))
        .into_owned();
    // Defence in depth: a secret rule must not be able to split a path back
    // into visibility.
    redact_paths(&s, home, smind_home)
}

/// Replaces the smind home with `$SMIND_HOME` and the home with `~`, longest
/// path first (the smind home usually lives under the home). Each path is
/// matched as given, in slash form and in backslash form, plus the canonical
/// path when it resolves to something different.
fn redact_paths(s: &str, home: Option<&Path>, smind_home: Option<&Path>) -> String {
    let mut pairs: Vec<(String, &str)> = Vec::with_capacity(2);
    for (path, repl) in [(smind_home, "$SMIND_HOME"), (home, "~")] {
        let Some(path) = path else { continue };
        let path = path.to_string_lossy();
        let path = path.trim_end_matches(['/', '\\']);
        if path.is_empty() || path == "/" {
            continue;
        }
        pairs.push((path.to_string(), repl));
    }
    pairs.sort_by(|a, b| b.0.len().cmp(&a.0.len()));
    let mut s = s.to_string();
    for (path, repl) in &pairs {
        let mut forms = vec![
            path.clone(),
            path.replace('\\', "/"),
            path.replace('/', "\\"),
        ];
        if let Some(real) = resolved_path(Path::new(path)) {
            let real = real.to_string_lossy().into_owned();
            if real != *path {
                forms.push(real.clone());
                forms.push(real.replace('\\', "/"));
            }
        }
        for form in forms {
            s = replace_at_boundary(&s, &form, repl);
        }
    }
    s
}

/// Remembers `fs::canonicalize` per path: redaction runs on every report and
/// the lookup can be slow (network homes, automounts).
fn resolved_path(path: &Path) -> Option<PathBuf> {
    static CACHE: LazyLock<Mutex<HashMap<PathBuf, Option<PathBuf>>>> =
        LazyLock::new(|| Mutex::new(HashMap::new()));
    let mut cache = CACHE.lock().unwrap();
    if let Some(hit) = cache.get(path) {
        return hit.clone();
    }
    let real = std::fs::canonicalize(path).ok();
    cache.insert(path.to_path_buf(), real.clone());
    real
}

/// Replaces `path` in `s` only where it ends at a path boundary: /Users/al
/// must not eat the start of /Users/alice.
fn replace_at_boundary(s: &str, path: &str, repl: &str) -> String {
    if path.is_empty() {
        return s.to_string();
    }
    let mut out = String::with_capacity(s.len());
    let mut rest = s;
    loop {
        let Some(i) = rest.find(path) else {
            out.push_str(rest);
            return out;
        };
        let end = i + path.len();
        if rest.as_bytes().get(end).is_some_and(|&c| is_path_name_byte(c)) {
            out.push_str(&rest[..end]);
            rest = &rest[end..];
            continue;
        }
        out.push_str(&rest[..i]);
        out.push_str(repl);
        rest = &rest[end..];
    }
}

/// Reports whether `c` would continue a path component name.
fn is_path_name_byte(c: u8) -> bool {
    c.is_ascii_alphanumeric()
        || matches!(c, b'_' | b'-' | b'.' | b'%' | b'~')
        || c >= 0x80
}

/// Replaces a 32+ character mixed-case alphanumeric run (at least one digit,
/// one upper-case and one lower-case letter) with `[redacted]`.
fn redact_high_entropy_run(run: &str) -> String {
    let mut digit = false;
    let mut upper = false;
    let mut lower = false;
    for c in run.chars() {
        match c {
            '0'..='9' => digit = true,
            'A'..='Z' => upper = true,
            'a'..='z' => lower = true,
            _ => {}
        }
    }
    if digit && upper && lower {
        "[redacted]".to_string()
    } else {
        run.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::redact;
    use std::path::Path;

    fn r(s: &str, home: &str, smind_home: &str) -> String {
        let home = (!home.is_empty()).then_some(Path::new(home));
        let smind_home = (!smind_home.is_empty()).then_some(Path::new(smind_home));
        redact(s, home, smind_home)
    }

    #[test]
    fn redacts_home_dir() {
        let dir = tempfile::tempdir().unwrap();
        let home = dir.path();
        let smind_home = home.join(".smind");
        let red = |input: &str| {
            redact(
                input,
                Some(home),
                Some(smind_home.as_path()),
            )
        };
        let cases = [
            (
                format!("open {}/notes.txt: no such file", home.display()),
                "open ~/notes.txt: no such file",
            ),
            (
                format!("read {}\\notes.txt", home.display().to_string().replace('/', "\\")),
                "read ~\\notes.txt",
            ),
            (
                format!("{}/src/main.go:12", home.display()),
                "~/src/main.go:12",
            ),
        ];
        for (input, want) in cases {
            let got = red(&input);
            assert_eq!(got, want, "input {input:?}");
            assert!(!got.contains(&home.display().to_string()), "still contains the home: {got:?}");
        }
    }

    #[test]
    fn path_boundary_sibling_user() {
        assert_eq!(
            r("/home/testerson/x.go and /home/tester/y.go and /home/tester2/z.go", "/home/tester", "/home/tester/.smind"),
            "<user dir>/x.go and ~/y.go and <user dir>/z.go"
        );
        assert_eq!(
            r("see /home/tester.", "/home/tester", "/home/tester/.smind"),
            "see <user dir>"
        );
    }

    #[test]
    fn user_dir_with_spaces() {
        assert_eq!(
            r("C:\\Users\\Alice Smith\\go\\pkg\\x.go:12 +0x1", "/home/tester", "/home/tester/.smind"),
            "<user dir>\\go\\pkg\\x.go:12 +0x1"
        );
        assert_eq!(
            r("/home/Jo Ann Marie/code/x.go", "/home/tester", "/home/tester/.smind"),
            "<user dir>/code/x.go"
        );
    }

    #[test]
    fn auth_scheme_credentials() {
        let cases = [
            ("Authorization: Token abc123", "Authorization: [redacted]"),
            (
                "Proxy-Authorization: Digest username=u, response=xyz789",
                "Proxy-Authorization: [redacted]",
            ),
            ("Cookie: sid=abc123; theme=dark", "Cookie: [redacted]"),
            ("retry with token abcdefghijklmnop", "retry with token [redacted]"),
        ];
        for (input, want) in cases {
            assert_eq!(r(input, "/home/tester", "/home/tester/.smind"), want, "input {input:?}");
        }
    }

    #[test]
    fn clean_stack_unchanged() {
        let stack = "goroutine 7 [running]:\ngithub.com/spacingmind/smind/internal/wsapi.(*conn).dispatch.func1()\n\t/usr/local/go/src/runtime/panic.go:787 +0x130\n";
        assert_eq!(r(stack, "/home/tester", "/home/tester/.smind"), stack);
    }

    #[test]
    fn idempotent_on_output() {
        let input = "token=/home/tester/.smind/x y and Authorization: Bearer abcDEF123456.token";
        let once = r(input, "/home/tester", "/home/tester/.smind");
        let twice = r(&once, "/home/tester", "/home/tester/.smind");
        assert_eq!(once, twice);
        assert_eq!(once, "token=[redacted] and Authorization: [redacted]");
    }
}
