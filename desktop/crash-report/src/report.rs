//! Static pieces of a report: the directory the shell's crashes live in, time
//! formatting without a time crate, Go-vocabulary os/arch, the panic-message
//! allow-list and the dedupe frame (docs/plans/active/crash-capture-desktop.md).

use std::path::{Path, PathBuf};
use std::sync::LazyLock;
use std::time::{SystemTime, UNIX_EPOCH};

use regex::Regex;

/// The Tauri app identifier; pinned to `desktop/src-tauri/tauri.conf.json` by a
/// test on each side, because the crashes directory must be the one Tauri
/// reports as `app_data_dir`.
pub const IDENTIFIER: &str = "dev.spacingmind.desktop";

/// The crashes directory for the given platform inputs, mirroring Go's
/// `crash.DesktopDir()`: macOS `~/Library/Application Support/<id>/crashes`,
/// Windows `%APPDATA%/<id>/crashes`, elsewhere
/// `${XDG_DATA_HOME:-~/.local/share}/<id>/crashes`. `None` when the needed
/// input is missing (an empty string counts as missing, as an unset env var
/// does in Go).
pub fn default_dir_from(
    os: &str,
    home: Option<&str>,
    appdata: Option<&str>,
    xdg: Option<&str>,
) -> Option<PathBuf> {
    fn nonempty(s: Option<&str>) -> Option<&str> {
        s.filter(|s| !s.is_empty())
    }
    // A relative APPDATA / XDG_DATA_HOME is ignored (the XDG specification says the same, and the
    // Go side, `desktopDirFrom`, applies the same rule): the shell's working directory is not
    // stable, so a relative base would put reports where nothing looks. Judged by the target OS,
    // not the host's.
    fn absolute(os: &str, p: &str) -> bool {
        if os == "windows" {
            let mut b = p.bytes();
            p.starts_with("\\\\")
                || matches!(
                    (b.next(), b.next(), b.next()),
                    (Some(d), Some(b':'), Some(b'\\' | b'/')) if d.is_ascii_alphabetic()
                )
        } else {
            p.starts_with('/')
        }
    }
    match os {
        "macos" => {
            let home = nonempty(home)?;
            Some(
                Path::new(home)
                    .join("Library")
                    .join("Application Support")
                    .join(IDENTIFIER)
                    .join("crashes"),
            )
        }
        "windows" => {
            let appdata = nonempty(appdata).filter(|p| absolute(os, p))?;
            Some(Path::new(appdata).join(IDENTIFIER).join("crashes"))
        }
        _ => {
            let base = match nonempty(xdg).filter(|p| absolute(os, p)) {
                Some(xdg) => PathBuf::from(xdg),
                None => Path::new(nonempty(home)?).join(".local").join("share"),
            };
            Some(base.join(IDENTIFIER).join("crashes"))
        }
    }
}

/// `default_dir_from` on this machine's OS and env (HOME / APPDATA /
/// XDG_DATA_HOME; USERPROFILE as the Windows home).
pub fn default_dir() -> Option<PathBuf> {
    default_dir_from(
        std::env::consts::OS,
        home_from_env().as_deref(),
        env_nonempty("APPDATA").as_deref(),
        env_nonempty("XDG_DATA_HOME").as_deref(),
    )
}

fn env_nonempty(key: &str) -> Option<String> {
    std::env::var(key).ok().filter(|v| !v.is_empty())
}

/// The home directory from the environment: `HOME`, and on Windows (where only
/// `USERPROFILE` is normally set) that.
pub(crate) fn home_from_env() -> Option<String> {
    home_from(
        cfg!(windows),
        env_nonempty("HOME"),
        env_nonempty("USERPROFILE"),
    )
}

fn home_from(windows: bool, home: Option<String>, userprofile: Option<String>) -> Option<String> {
    home.or(if windows { userprofile } else { None })
}

/// `os` in Go's vocabulary (`darwin`, not `macos`) so one reader handles the
/// daemon's and the shell's reports.
pub fn go_os() -> &'static str {
    match std::env::consts::OS {
        "macos" => "darwin",
        os => os,
    }
}

/// `arch` in Go's vocabulary (`arm64`, not `aarch64`).
pub fn go_arch() -> &'static str {
    match std::env::consts::ARCH {
        "aarch64" => "arm64",
        "x86_64" => "amd64",
        "x86" => "386",
        arch => arch,
    }
}

/// Signed nanoseconds since the Unix epoch (negative before it): the one
/// clock arithmetic all time helpers share.
pub(crate) fn unix_nanos(t: SystemTime) -> i128 {
    match t.duration_since(UNIX_EPOCH) {
        Ok(d) => d.as_nanos() as i128,
        Err(e) => -(e.duration().as_nanos() as i128),
    }
}

fn utc_parts(n: i128) -> (i64, u32, u32, u32, u32, u32) {
    let secs = n.div_euclid(1_000_000_000);
    let secs_of_day = secs.rem_euclid(86_400);
    let (y, m, d) = civil_from_days(secs.div_euclid(86_400) as i64);
    (
        y,
        m,
        d,
        (secs_of_day / 3_600) as u32,
        ((secs_of_day % 3_600) / 60) as u32,
        (secs_of_day % 60) as u32,
    )
}

/// Days since the Unix epoch to a civil date, Howard Hinnant's
/// `civil_from_days` (https://howardhinnant.github.io/date_algorithms.html).
fn civil_from_days(z: i64) -> (i64, u32, u32) {
    let z = z + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64; // [0, 146096]
    let yoe = (doe - doe / 1_460 + doe / 36_524 - doe / 146_096) / 365; // [0, 399]
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100); // [0, 365]
    let mp = (5 * doy + 2) / 153; // [0, 11]
    let d = doy - (153 * mp + 2) / 5 + 1; // [1, 31]
    let m = if mp < 10 { mp + 3 } else { mp - 9 }; // [1, 12]
    (y + i64::from(m <= 2), m as u32, d as u32)
}

/// `t` as Go's `time.RFC3339Nano` in UTC, e.g. `2026-11-15T01:02:03.004005006Z`:
/// the fraction's trailing zeros are trimmed and the fraction is dropped when
/// it is zero.
pub fn rfc3339_nano(t: SystemTime) -> String {
    let n = unix_nanos(t);
    let nanos = n.rem_euclid(1_000_000_000) as u32;
    let (y, mo, d, h, mi, s) = utc_parts(n);
    let mut out = format!("{y:04}-{mo:02}-{d:02}T{h:02}:{mi:02}:{s:02}");
    if nanos > 0 {
        let frac = format!("{nanos:09}");
        out.push('.');
        out.push_str(frac.trim_end_matches('0'));
    }
    out.push('Z');
    out
}

/// The file-name timestamp of a report: UTC `yyyymmddThhmmss.mmm`
/// (milliseconds, zero-padded to three digits).
pub fn name_stamp(t: SystemTime) -> String {
    let n = unix_nanos(t);
    let nanos = n.rem_euclid(1_000_000_000) as u32;
    let (y, mo, d, h, mi, s) = utc_parts(n);
    format!(
        "{y:04}{mo:02}{d:02}T{h:02}{mi:02}{s:02}.{:03}",
        nanos / 1_000_000
    )
}

#[allow(clippy::expect_used)] // fixed patterns, compiled by `warm_up` before the hook exists
static RUNTIME_MESSAGES: LazyLock<Vec<Regex>> = LazyLock::new(|| {
    [
        r"^index out of bounds: the len is \d+ but the index is \d+$",
        r"^range (start|end) index \d+ out of range for slice of length \d+$",
        r"^slice index starts at \d+ but ends at \d+$",
        r"^attempt to (add|subtract|multiply|negate|shift left|shift right) with overflow$",
        r"^attempt to divide by zero$",
        r"^attempt to calculate the remainder with a divisor of zero$",
        r"^called `Option::unwrap\(\)` on a `None` value$",
        r"^already borrowed: BorrowMutError$",
        r"^already mutably borrowed: BorrowError$",
        r"^capacity overflow$",
    ]
    .iter()
    .map(|p| Regex::new(p).expect("static pattern"))
    .collect()
});

/// Compiles the patterns used on the hook path (see `redact::warm_up`).
pub fn warm_up() {
    let _ = message_is_runtime("");
    let _ = dedupe_frame("");
}

/// Whether a panic message is one of the fully static or number-only runtime
/// messages and so may be kept (still redacted). Everything else may carry
/// user text (`unwrap()` on an `Err` prints the error's `Debug`, `expect` and
/// formatted panics print their arguments) and is withheld.
pub fn message_is_runtime(msg: &str) -> bool {
    RUNTIME_MESSAGES.iter().any(|re| re.is_match(msg))
}

const PANIC_MACHINERY: &[&str] = &[
    "smind_crash_report::",
    "std::panicking",
    "core::panicking",
    "rust_begin_unwind",
    "std::backtrace",
    "std::sys",
    "__rust_",
];

#[allow(clippy::expect_used)] // fixed pattern, compiled by `warm_up` before the hook exists
static HASH_SUFFIX: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"::h[0-9a-f]{16}$").expect("static pattern"));

/// The first stack frame outside the panic machinery and this crate: the
/// frame that identifies where the panic happened. Frame lines look like
/// `   3: some::path::function`; the indented `at file:line` lines between
/// them are skipped, and a trailing `::h<16 hex>` hash suffix is cut off so
/// addresses never reach the key. Empty when no such frame exists.
pub fn dedupe_frame(stack: &str) -> String {
    for line in stack.lines() {
        let Some(symbol) = frame_symbol(line) else {
            continue;
        };
        if PANIC_MACHINERY.iter().any(|m| symbol.contains(m)) {
            continue;
        }
        // The `#[cold]` helpers of the standard library that every `unwrap`/`expect`/index panic
        // goes through (`core::option::unwrap_failed`, `core::slice::index::...`): the same
        // frame for unrelated panics, so they cannot be the identity of one. A stripped build
        // has `<unknown>` frames; those are skipped too (such panics dedupe by site only).
        if [
            "core::",
            "alloc::",
            "std::",
            "<core::",
            "<alloc::",
            "<std::",
            "<unknown>",
        ]
        .iter()
        .any(|p| symbol.starts_with(p))
        {
            continue;
        }
        return HASH_SUFFIX.replace(symbol, "").into_owned();
    }
    String::new()
}

fn frame_symbol(line: &str) -> Option<&str> {
    let t = line.trim_start();
    let digits = t.find(|c: char| !c.is_ascii_digit())?;
    if digits == 0 {
        return None;
    }
    let symbol = t.get(digits..)?.strip_prefix(": ")?;
    if symbol.is_empty() {
        None
    } else {
        Some(symbol)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    fn at(secs: i64, nanos: u32) -> SystemTime {
        if secs >= 0 {
            UNIX_EPOCH + Duration::new(secs as u64, nanos)
        } else {
            UNIX_EPOCH - Duration::new(secs.unsigned_abs(), nanos)
        }
    }

    #[test]
    fn default_dir_per_platform() {
        let home = "/Users/u";
        let appdata = r"C:\Users\u\AppData\Roaming";
        let xdg = "/home/u/data";

        assert_eq!(
            default_dir_from("macos", Some(home), Some(appdata), Some(xdg)),
            Some(PathBuf::from(
                "/Users/u/Library/Application Support/dev.spacingmind.desktop/crashes"
            ))
        );
        assert_eq!(
            default_dir_from("macos", None, Some(appdata), Some(xdg)),
            None
        );
        assert_eq!(default_dir_from("macos", Some(""), None, None), None);

        assert_eq!(
            default_dir_from("windows", Some(home), Some(appdata), Some(xdg)),
            Some(PathBuf::from(appdata).join(IDENTIFIER).join("crashes"))
        );
        assert_eq!(
            default_dir_from("windows", Some(home), None, Some(xdg)),
            None
        );
        assert_eq!(
            default_dir_from("windows", Some(home), Some(""), None),
            None
        );

        assert_eq!(
            default_dir_from("linux", Some(home), None, Some(xdg)),
            Some(PathBuf::from(
                "/home/u/data/dev.spacingmind.desktop/crashes"
            ))
        );
        assert_eq!(
            default_dir_from("linux", Some(home), Some(appdata), None),
            Some(PathBuf::from(
                "/Users/u/.local/share/dev.spacingmind.desktop/crashes"
            ))
        );
        assert_eq!(default_dir_from("linux", None, Some(appdata), None), None);

        // default_dir wires the real OS and env vars into default_dir_from.
        let env_home = std::env::var("HOME").ok().filter(|h| !h.is_empty());
        let env_userprofile = std::env::var("USERPROFILE").ok().filter(|h| !h.is_empty());
        let expected_home = if cfg!(windows) {
            env_home.or(env_userprofile)
        } else {
            env_home
        };
        assert_eq!(
            default_dir(),
            default_dir_from(
                std::env::consts::OS,
                expected_home.as_deref(),
                std::env::var("APPDATA")
                    .ok()
                    .filter(|s| !s.is_empty())
                    .as_deref(),
                std::env::var("XDG_DATA_HOME")
                    .ok()
                    .filter(|s| !s.is_empty())
                    .as_deref(),
            )
        );
    }

    #[test]
    fn identifier_matches_tauri_conf() {
        let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../src-tauri/tauri.conf.json");
        let conf: serde_json::Value =
            serde_json::from_str(&std::fs::read_to_string(path).expect("tauri.conf.json readable"))
                .expect("tauri.conf.json parses");
        assert_eq!(conf["identifier"].as_str(), Some(IDENTIFIER));
    }

    #[test]
    fn rfc3339_nano_and_name_stamp() {
        // 1794704523 = 2026-11-15T01:02:03Z, the golden report's time.
        let t = at(1_794_704_523, 4_005_006);
        assert_eq!(rfc3339_nano(t), "2026-11-15T01:02:03.004005006Z");
        assert_eq!(name_stamp(t), "20261115T010203.004");

        // No fraction when the nanoseconds are zero, milliseconds padded.
        let t = at(1_794_704_523, 0);
        assert_eq!(rfc3339_nano(t), "2026-11-15T01:02:03Z");
        assert_eq!(name_stamp(t), "20261115T010203.000");

        // Trailing zeros of the fraction are trimmed (Go's RFC3339Nano).
        let t = at(1_794_704_523, 400_000_000);
        assert_eq!(rfc3339_nano(t), "2026-11-15T01:02:03.4Z");
        let t = at(1_794_704_523, 4_000_000);
        assert_eq!(rfc3339_nano(t), "2026-11-15T01:02:03.004Z");
        let t = at(1_794_704_523, 500_000_000);
        assert_eq!(rfc3339_nano(t), "2026-11-15T01:02:03.5Z");

        // Epoch and a leap-year date in February.
        assert_eq!(rfc3339_nano(UNIX_EPOCH), "1970-01-01T00:00:00Z");
        assert_eq!(name_stamp(UNIX_EPOCH), "19700101T000000.000");
        let t = at(1_740_960_000, 999_999_999); // 2025-03-03T00:00:00Z, past the 2024 leap day
        assert_eq!(rfc3339_nano(t), "2025-03-03T00:00:00.999999999Z");
        assert_eq!(name_stamp(t), "20250303T000000.999");

        // One second before the epoch still formats as a civil date.
        assert_eq!(rfc3339_nano(at(-1, 0)), "1969-12-31T23:59:59Z");
    }

    #[test]
    fn runtime_message_allow_list() {
        let kept = [
            "index out of bounds: the len is 3 but the index is 7",
            "range start index 9 out of range for slice of length 3",
            "range end index 9 out of range for slice of length 3",
            "slice index starts at 5 but ends at 3",
            "attempt to add with overflow",
            "attempt to subtract with overflow",
            "attempt to multiply with overflow",
            "attempt to negate with overflow",
            "attempt to shift left with overflow",
            "attempt to shift right with overflow",
            "attempt to divide by zero",
            "attempt to calculate the remainder with a divisor of zero",
            "called `Option::unwrap()` on a `None` value",
            "already borrowed: BorrowMutError",
            "already mutably borrowed: BorrowError",
            "capacity overflow",
        ];
        for msg in kept {
            assert!(message_is_runtime(msg), "expected kept: {msg:?}");
        }

        let withheld = [
            "called `Result::unwrap()` on an `Err` value: Custom { .. }",
            "explicit panic",
            "boom",
            "",
        ];
        for msg in withheld {
            assert!(!message_is_runtime(msg), "expected withheld: {msg:?}");
        }

        // Anchored: a runtime shape wrapped in other text is not runtime.
        assert!(!message_is_runtime(
            "panic: index out of bounds: the len is 3 but the index is 7"
        ));
        assert!(!message_is_runtime(
            "index out of bounds: the len is 3 but the index is 7!"
        ));
        assert!(!message_is_runtime(
            "attempt to add with overflow in user.rs"
        ));
    }

    #[test]
    fn dedupe_frame_skips_panic_machinery() {
        let stack = "stack backtrace:\n   0: smind_crash_report::hook\n             \
                     at ./src/lib.rs:20:18\n   1: std::panicking::rust_panic_with_hook\n   \
                     2: std::sys::backtrace::__rust_end_short_backtrace\n   3: \
                     core::panicking::panic_fmt\n   4: rust_begin_unwind\n   5: \
                     __rust_end_short_backtrace\n   6: std::backtrace::Backtrace::force_capture\n   \
                     7: my_app::ui::panel::render::h8ab97d0c9d29e6ba\n             \
                     at ./src/ui/panel.rs:42:13\n   8: my_app::ui::panel::update\n";
        assert_eq!(dedupe_frame(stack), "my_app::ui::panel::render");

        // Nothing outside the machinery: no key, no dedupe.
        let machinery_only =
            "   0: smind_crash_report::hook\n   1: std::panicking::rust_panic_with_hook\n";
        assert_eq!(dedupe_frame(machinery_only), "");

        // A hash suffix that is not exactly 16 hex chars is part of the symbol.
        assert_eq!(
            dedupe_frame("   2: my_app::f::h8ab97d0c9d29e6b\n"),
            "my_app::f::h8ab97d0c9d29e6b"
        );
        // No frames at all.
        assert_eq!(dedupe_frame(""), "");
    }

    #[test]
    fn default_dir_ignores_relative_bases() {
        let id = IDENTIFIER;
        let d = |os, home, appdata, xdg| default_dir_from(os, home, appdata, xdg);
        assert_eq!(
            d("linux", Some("/home/a"), None, Some("relative/xdg")),
            Some(PathBuf::from(format!("/home/a/.local/share/{id}/crashes")))
        );
        assert_eq!(
            d("linux", Some("/home/a"), None, Some(".")),
            Some(PathBuf::from(format!("/home/a/.local/share/{id}/crashes")))
        );
        assert_eq!(d("linux", None, None, Some("relative")), None);
        assert_eq!(
            d("linux", Some("/home/a"), None, Some("/data/xdg")),
            Some(PathBuf::from(format!("/data/xdg/{id}/crashes")))
        );
        assert_eq!(
            d("windows", Some(r"C:\Users\a"), Some("AppData"), None),
            None
        );
        assert_eq!(
            d("windows", None, Some(r"C:\Users\a\AppData\Roaming"), None),
            Some(
                Path::new(r"C:\Users\a\AppData\Roaming")
                    .join(id)
                    .join("crashes")
            )
        );
        assert!(d("windows", None, Some(r"\\server\share\AppData"), None).is_some());
        assert_eq!(d("macos", None, None, None), None);
    }

    #[test]
    fn dedupe_frame_skips_std_cold_helpers() {
        // every unwrap/expect/index panic goes through the same std helper frame: it must not
        // be the identity of the panic (two unrelated unwraps are two panics)
        let a = "   0: smind_crash_report::hook\n   1: core::option::unwrap_failed\n   \
                 2: my_app::ui::panel::render\n";
        let b = "   0: smind_crash_report::hook\n   1: core::option::unwrap_failed\n   \
                 2: my_app::tray::update\n";
        assert_eq!(dedupe_frame(a), "my_app::ui::panel::render");
        assert_eq!(dedupe_frame(b), "my_app::tray::update");
        let stripped = "   0: <unknown>\n   1: <unknown>\n";
        assert_eq!(dedupe_frame(stripped), "");
    }

    #[test]
    fn home_falls_back_to_userprofile_on_windows_only() {
        let up = Some(r"C:\Users\x".to_string());
        assert_eq!(home_from(true, None, up.clone()), up);
        assert_eq!(home_from(false, None, up.clone()), None);
        assert_eq!(
            home_from(true, Some("/h".into()), up),
            Some("/h".to_string())
        );
    }
}
