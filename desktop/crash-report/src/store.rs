//! Port of `internal/crash` Record + store/rotate: the bounded, redacted,
//! local-only crash report store shared with the daemon
//! (docs/plans/active/crash-capture-desktop.md).

use std::collections::HashMap;
use std::fs;
use std::io;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Mutex, MutexGuard};
use std::time::{Duration, SystemTime};

use serde_json::{json, Value};

use crate::redact::redact;
use crate::report;

/// The source of every report this crate writes; the daemon's reader keys on
/// it to know which half of the app produced the file.
pub const SOURCE: &str = "desktop-shell";

const MSG_MAX_BYTES: usize = 300;
/// The raw backtrace is cut to this before redaction runs (it runs on the panicking thread, and a
/// very deep stack must not make three regex passes take seconds); the report is cut to 64 KiB later.
const MAX_RAW_STACK_BYTES: usize = 256 * 1024;
const PANIC_TYPE_MAX_BYTES: usize = 200;
const STACK_TRUNCATED: &str = "\n...[stack truncated]";
const CRASH_PREFIX: &str = "crash-";
const CRASH_SUFFIX: &str = ".json";
const TMP_PREFIX: &str = ".tmp-crash-";
const SITE_MAX_CHARS: usize = 40;
const TMP_MAX_AGE: Duration = Duration::from_secs(60);

/// Bounds of the crashes directory, as in `internal/crash`.
#[derive(Debug, Clone)]
pub struct Limits {
    pub max_files: usize,
    pub max_file_bytes: usize,
    pub max_dir_bytes: u64,
    pub max_per_minute: usize,
    pub dedupe_window: Duration,
}

impl Default for Limits {
    fn default() -> Self {
        Limits {
            max_files: 20,
            max_file_bytes: 64 * 1024,
            max_dir_bytes: 2 * 1024 * 1024,
            max_per_minute: 10,
            dedupe_window: Duration::from_secs(5 * 60),
        }
    }
}

pub struct Config {
    pub dir: PathBuf,
    pub version: String,
    pub home: Option<PathBuf>,
    pub smind_home: Option<PathBuf>,
    pub now: fn() -> SystemTime,
    pub limits: Limits,
}

impl Config {
    pub fn new(dir: impl Into<PathBuf>, version: impl Into<String>) -> Self {
        Config {
            dir: dir.into(),
            version: version.into(),
            home: report::home_from_env().map(PathBuf::from),
            smind_home: None,
            now: SystemTime::now,
            limits: Limits::default(),
        }
    }
}

#[derive(Default)]
struct State {
    write_times: Vec<SystemTime>,
    last_seen: HashMap<String, SystemTime>,
    suppressed: usize,
    tmp_seq: u64,
}

/// Writes bounded, redacted crash reports; [`Recorder::record`] never panics
/// and never blocks.
pub struct Recorder {
    cfg: Config,
    state: Mutex<State>,
    io: Mutex<()>,
}

impl Recorder {
    /// Does no I/O: the directory is created by the first report.
    pub fn new(cfg: Config) -> Self {
        Recorder {
            cfg,
            state: Mutex::new(State::default()),
            io: Mutex::new(()),
        }
    }

    /// Writes one report and returns its path, or `None` when nothing was
    /// written (dropped by the rate limit, the dedupe window or because
    /// another report is being written right now; or an error). It never
    /// panics: a failure to record must not become a second panic. A failed
    /// write counts like a successful one for the rate limit and the dedupe
    /// window, so a panic loop on a full or unwritable disk retries the whole
    /// path at most `max_per_minute` times a minute, not on every panic.
    pub fn record(
        &self,
        site: &str,
        panic_type: &str,
        message: Option<&str>,
        stack: &str,
    ) -> Option<PathBuf> {
        let r = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            self.record_inner(site, panic_type, message, stack)
        }));
        r.unwrap_or(None)
    }

    fn record_inner(
        &self,
        site: &str,
        panic_type: &str,
        message: Option<&str>,
        stack: &str,
    ) -> Option<PathBuf> {
        let now = (self.cfg.now)();
        let now_n = report::unix_nanos(now);
        let window = self.cfg.limits.dedupe_window.as_nanos() as i128;
        let key = format!("{site}\0{}", report::dedupe_frame(stack));

        let report;
        {
            let mut st = self.lock_state();
            st.write_times
                .retain(|&t| now_n - report::unix_nanos(t) < 60_000_000_000);
            if st.write_times.len() >= self.cfg.limits.max_per_minute {
                st.suppressed += 1;
                return None;
            }
            if let Some(&t) = st.last_seen.get(&key) {
                if now_n - report::unix_nanos(t) < window {
                    st.suppressed += 1;
                    return None;
                }
            }
            // Reserve the slot before the write: failures count too.
            st.write_times.push(now);
            st.last_seen.insert(key, now);
            st.last_seen
                .retain(|_, &mut t| now_n - report::unix_nanos(t) < window);
            report = self.build_report(site, panic_type, message, stack, now, st.suppressed);
        }

        // One writer at a time; a second concurrent report is dropped and
        // counted, never queued behind a slow disk.
        let _io = match self.io.try_lock() {
            Ok(g) => g,
            Err(_) => {
                self.lock_state().suppressed += 1;
                return None;
            }
        };

        let data = match marshal_bounded(&report, self.cfg.limits.max_file_bytes) {
            Some(d) => d,
            None => {
                log_err();
                return None;
            }
        };
        match self.store(data.as_bytes(), now, site) {
            Ok(path) => {
                self.lock_state().suppressed = 0;
                Some(path)
            }
            Err(_) => {
                log_err();
                None
            }
        }
    }

    fn build_report(
        &self,
        site: &str,
        panic_type: &str,
        message: Option<&str>,
        stack: &str,
        now: SystemTime,
        suppressed: usize,
    ) -> Value {
        let home = self.cfg.home.as_deref();
        let smind_home = self.cfg.smind_home.as_deref();
        // A message is kept only when it is a fully static or number-only runtime
        // message: any other panic message may carry prompt text, file contents or
        // secrets, so it is recorded by type only.
        let (message, withheld) = match message.filter(|m| report::message_is_runtime(m)) {
            Some(msg) => (truncate(&redact(msg, home, smind_home), MSG_MAX_BYTES), false),
            None => (String::new(), true),
        };
        json!({
            "schema": 1,
            "source": SOURCE,
            "version": self.cfg.version,
            "os": report::go_os(),
            "arch": report::go_arch(),
            "time": report::rfc3339_nano(now),
            "site": site,
            "panicType": truncate(&redact(panic_type, home, smind_home), PANIC_TYPE_MAX_BYTES),
            "message": message,
            "messageWithheld": withheld,
            "stack": redact(truncate(stack, MAX_RAW_STACK_BYTES).as_str(), home, smind_home),
            "suppressedSinceLast": suppressed,
        })
    }

    fn lock_state(&self) -> MutexGuard<'_, State> {
        self.state.lock().unwrap_or_else(|e| e.into_inner())
    }

    /// Creates the directory, writes data to a temp file in it and renames
    /// the file into place, then rotates.
    fn store(&self, data: &[u8], now: SystemTime, site: &str) -> io::Result<PathBuf> {
        let dir = &self.cfg.dir;
        // Look before touching: create_dir_all and set_permissions both follow
        // a link, so the link check must come first or a planted link would
        // get its target chmod'ed (and written into).
        let md = match fs::symlink_metadata(dir) {
            Ok(md) => md,
            Err(e) if e.kind() == io::ErrorKind::NotFound => {
                let mut b = fs::DirBuilder::new();
                b.recursive(true);
                #[cfg(unix)]
                {
                    use std::os::unix::fs::DirBuilderExt;
                    b.mode(0o700);
                }
                b.create(dir)?;
                fs::symlink_metadata(dir)?
            }
            Err(e) => return Err(e),
        };
        let ft = md.file_type();
        #[cfg(unix)]
        {
            if ft.is_symlink() {
                return Err(io::Error::other("crash directory is a link"));
            }
        }
        #[cfg(windows)]
        {
            use std::os::windows::fs::MetadataExt;
            if md.file_attributes() & 0x400 != 0 {
                // FILE_ATTRIBUTE_REPARSE_POINT: a junction or other link.
                return Err(io::Error::other("crash directory is a reparse point"));
            }
        }
        if !ft.is_dir() {
            return Err(io::Error::other("crash directory is not a directory"));
        }
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(dir, fs::Permissions::from_mode(0o700))?;
        }

        // Two different panics at the same site in the same millisecond must
        // not overwrite each other: take the first free name.
        let sanitized = sanitize_site(site);
        let mut final_path = dir.join(crash_name(now, &sanitized, 0));
        for n in 1..100 {
            match fs::symlink_metadata(&final_path) {
                Err(e) if e.kind() == io::ErrorKind::NotFound => break,
                _ => final_path = dir.join(crash_name(now, &sanitized, n)),
            }
        }

        if fs::symlink_metadata(&final_path).is_ok() {
            // 100 reports in one millisecond: never overwrite one.
            return Err(io::Error::other("no free report name"));
        }
        let seq = {
            let mut st = self.lock_state();
            st.tmp_seq += 1;
            st.tmp_seq
        };
        let tmp = dir.join(format!("{TMP_PREFIX}{}-{seq}", std::process::id()));
        {
            use std::io::Write;
            let mut opts = fs::OpenOptions::new();
            opts.write(true).create_new(true);
            #[cfg(unix)]
            {
                use std::os::unix::fs::OpenOptionsExt;
                opts.mode(0o600);
            }
            let mut f = opts.open(&tmp)?;
            if let Err(e) = f.write_all(data) {
                drop(f);
                let _ = fs::remove_file(&tmp);
                return Err(e);
            }
        }
        if let Err(e) = fs::rename(&tmp, &final_path) {
            let _ = fs::remove_file(&tmp);
            return Err(e);
        }
        self.rotate(now);
        Ok(final_path)
    }

    /// Keeps the newest `max_files` reports, caps the directory at
    /// `max_dir_bytes` (oldest dropped first) and removes leftover temp files
    /// older than `TMP_MAX_AGE`: a fresh one may belong to another writer.
    fn rotate(&self, now: SystemTime) {
        let now_n = report::unix_nanos(now);
        let entries = match fs::read_dir(&self.cfg.dir) {
            Ok(entries) => entries,
            Err(_) => return,
        };
        let mut names: Vec<(String, u64)> = Vec::new();
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().into_owned();
            // DirEntry::metadata does not follow symlinks, like Go's ReadDir.
            let Ok(md) = entry.metadata() else {
                continue;
            };
            if md.is_dir() {
                continue;
            }
            if name.starts_with(TMP_PREFIX) {
                if let Ok(mtime) = md.modified() {
                    if now_n - report::unix_nanos(mtime) > TMP_MAX_AGE.as_nanos() as i128 {
                        let _ = fs::remove_file(self.cfg.dir.join(&name));
                    }
                }
                continue;
            }
            if name.starts_with(CRASH_PREFIX) && name.ends_with(CRASH_SUFFIX) {
                names.push((name, md.len()));
            }
        }
        names.sort(); // the name timestamp orders the files
        if names.len() > self.cfg.limits.max_files {
            let excess = names.len() - self.cfg.limits.max_files;
            for (name, _) in names.drain(..excess) {
                let _ = fs::remove_file(self.cfg.dir.join(name));
            }
        }
        let mut total: u64 = names.iter().map(|(_, size)| size).sum();
        for (name, size) in &names {
            if total <= self.cfg.limits.max_dir_bytes {
                break;
            }
            total -= size;
            let _ = fs::remove_file(self.cfg.dir.join(name));
        }
    }
}

/// Swallows every error after logging one line the first time per process;
/// the line is fixed text, never the error (which may carry paths) or any
/// panic text.
fn log_err() {
    static LOGGED: AtomicBool = AtomicBool::new(false);
    if !LOGGED.swap(true, Ordering::SeqCst) {
        // `eprintln!` PANICS when stderr fails (a closed pipe), and a panic inside a panic hook
        // aborts the process: write and ignore the result instead.
        let _ = io::Write::write_all(
            &mut io::stderr(),
            b"smind-crash-report: could not record a crash report\n",
        );
    }
}

/// Serialises `rep` and, while it exceeds `limit` bytes, cuts the stack
/// (keeping its head) and appends the truncation marker until it fits.
fn marshal_bounded(rep: &Value, limit: usize) -> Option<String> {
    let mut data = serde_json::to_string(rep).ok()?;
    if data.len() <= limit {
        return Some(data);
    }
    let base = rep.get("stack")?.as_str()?.to_string();
    let mut keep = base.len();
    loop {
        keep = keep.saturating_sub(data.len() - limit + STACK_TRUNCATED.len());
        while keep > 0 && !base.is_char_boundary(keep) {
            keep -= 1;
        }
        let mut cut = rep.as_object()?.clone();
        cut.insert("stack".to_string(), json!(format!("{}{}", base.get(..keep)?, STACK_TRUNCATED)));
        data = serde_json::to_string(&cut).ok()?;
        if data.len() <= limit || keep == 0 {
            return Some(data);
        }
    }
}

/// Cuts `s` to at most `limit` bytes without splitting a character.
fn truncate(s: &str, limit: usize) -> String {
    if s.len() <= limit {
        return s.to_string();
    }
    let mut end = limit;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    s[..end].to_string()
}

fn crash_name(now: SystemTime, site: &str, n: usize) -> String {
    let suffix = if n > 0 { format!("-{n}") } else { String::new() };
    format!(
        "{CRASH_PREFIX}{}-{SOURCE}-{site}{suffix}{CRASH_SUFFIX}",
        report::name_stamp(now)
    )
}

fn sanitize_site(site: &str) -> String {
    let mut out = String::new();
    for c in site.to_lowercase().chars() {
        if out.chars().count() >= SITE_MAX_CHARS {
            break;
        }
        match c {
            'a'..='z' | '0'..='9' | '.' | '_' | '-' => out.push(c),
            _ => out.push('-'),
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::Cell;
    use std::collections::BTreeSet;
    use std::path::Path;
    use std::time::UNIX_EPOCH;

    // A thread-local clock behind Config.now: each test thread gets its own,
    // and it starts at the real now so file mtimes line up with it.
    mod clock {
        use super::*;
        thread_local! {
            static NANOS: Cell<Option<u64>> = const { Cell::new(None) };
        }
        pub fn now() -> SystemTime {
            let n = NANOS.with(|c| {
                c.get().unwrap_or_else(|| {
                    let base = SystemTime::now()
                        .duration_since(UNIX_EPOCH)
                        .expect("clock after epoch")
                        .as_nanos() as u64;
                    c.set(Some(base));
                    base
                })
            });
            UNIX_EPOCH + Duration::from_nanos(n)
        }
        pub fn advance(d: Duration) {
            now();
            NANOS.with(|c| c.set(Some(c.get().unwrap() + d.as_nanos() as u64)));
        }
    }

    fn test_cfg(dir: impl Into<PathBuf>) -> Config {
        let mut cfg = Config::new(dir, "0.20.0");
        cfg.now = clock::now;
        cfg.home = None; // never the real home in a test
        cfg.smind_home = None;
        cfg
    }

    fn stack(frame: usize) -> String {
        format!("   0: std::panicking::rust_panic_with_hook\n   1: my_app::panel{frame}::render\n")
    }

    fn crash_files(dir: &Path) -> Vec<String> {
        let mut names: Vec<String> = fs::read_dir(dir)
            .expect("read crashes dir")
            .map(|e| e.expect("entry").file_name().to_string_lossy().into_owned())
            .filter(|n| n.starts_with(CRASH_PREFIX) && n.ends_with(CRASH_SUFFIX))
            .collect();
        names.sort();
        names
    }

    fn read_json(path: &Path) -> Value {
        serde_json::from_str(&fs::read_to_string(path).expect("report readable"))
            .expect("report parses")
    }

    fn json_type(v: &Value) -> &'static str {
        match v {
            Value::Null => "null",
            Value::Bool(_) => "bool",
            Value::Number(_) => "number",
            Value::String(_) => "string",
            Value::Array(_) => "array",
            Value::Object(_) => "object",
        }
    }

    #[test]
    fn report_fields_and_schema() {
        let tmp = tempfile::tempdir().unwrap();
        let r = Recorder::new(test_cfg(tmp.path()));
        let path = r
            .record("desktop.thread", "&str", None, "   0: my_app::render\n")
            .expect("report written");

        let got = read_json(&path);
        let golden: Value =
            serde_json::from_str(include_str!("../../../internal/crash/testdata/desktop_shell_report.json"))
                .expect("golden parses");
        let got_keys: BTreeSet<&String> = got.as_object().unwrap().keys().collect();
        let want_keys: BTreeSet<&String> = golden.as_object().unwrap().keys().collect();
        assert_eq!(got_keys, want_keys, "key sets must match the daemon's");
        for key in &want_keys {
            assert_eq!(
                json_type(&got[*key]),
                json_type(&golden[*key]),
                "key {key} must have the same JSON type"
            );
        }
        assert_eq!(got["schema"], json!(1));
        assert_eq!(got["source"], json!(SOURCE));
        assert_eq!(got["version"], json!("0.20.0"));
        assert_eq!(got["site"], json!("desktop.thread"));
        assert_eq!(got["panicType"], json!("&str"));
        assert_eq!(got["suppressedSinceLast"], json!(0));
    }

    #[test]
    fn withholds_non_runtime_message() {
        let tmp = tempfile::tempdir().unwrap();
        let r = Recorder::new(test_cfg(tmp.path()));
        let path = r
            .record("desktop.main", "String", Some("boom"), &stack(1))
            .expect("report written");
        let got = read_json(&path);
        assert_eq!(got["message"], json!(""));
        assert_eq!(got["messageWithheld"], json!(true));

        // An unwrap error's Debug carries whatever the error holds.
        let path = r
            .record(
                "desktop.main",
                "String",
                Some("called `Result::unwrap()` on an `Err` value: Custom { .. }"),
                &stack(2),
            )
            .expect("report written");
        let got = read_json(&path);
        assert_eq!(got["message"], json!(""));
        assert_eq!(got["messageWithheld"], json!(true));
    }

    #[test]
    fn keeps_runtime_message() {
        let tmp = tempfile::tempdir().unwrap();
        let r = Recorder::new(test_cfg(tmp.path()));
        let path = r
            .record(
                "desktop.thread",
                "usize",
                Some("index out of bounds: the len is 3 but the index is 7"),
                &stack(1),
            )
            .expect("report written");
        let got = read_json(&path);
        assert_eq!(
            got["message"],
            json!("index out of bounds: the len is 3 but the index is 7")
        );
        assert_eq!(got["messageWithheld"], json!(false));
    }

    #[test]
    fn caps_report_size() {
        let tmp = tempfile::tempdir().unwrap();
        let mut cfg = test_cfg(tmp.path());
        cfg.limits.max_file_bytes = 400;
        let r = Recorder::new(cfg);
        let big = "x".repeat(8 * 1024);
        let path = r
            .record("desktop.thread", "&str", None, &big)
            .expect("report written");
        assert!(fs::metadata(&path).expect("stat").len() <= 400);
        let got = read_json(&path);
        let stack = got["stack"].as_str().unwrap();
        assert!(stack.ends_with(STACK_TRUNCATED), "marker missing: {stack:?}");
        assert!(big.starts_with(stack.strip_suffix(STACK_TRUNCATED).unwrap()));
        assert_eq!(got["schema"], json!(1));
    }

    #[test]
    fn keeps_newest_20() {
        let tmp = tempfile::tempdir().unwrap();
        let mut cfg = test_cfg(tmp.path());
        cfg.limits.max_per_minute = 100;
        let r = Recorder::new(cfg);
        let mut paths = Vec::new();
        for i in 0..25 {
            paths.push(r.record("desktop.thread", "&str", None, &stack(i)).expect("report"));
            clock::advance(Duration::from_secs(1));
        }
        assert_eq!(crash_files(tmp.path()).len(), 20);
        for path in &paths[..5] {
            assert!(!path.exists(), "oldest report {path:?} must be gone");
        }
        for path in &paths[5..] {
            assert!(path.exists(), "newest report {path:?} must stay");
        }
    }

    #[test]
    fn caps_dir_size() {
        let tmp = tempfile::tempdir().unwrap();
        let mut cfg = test_cfg(tmp.path());
        cfg.limits.max_per_minute = 100;
        cfg.limits.max_files = 100;
        cfg.limits.max_dir_bytes = 3 * 1024;
        let r = Recorder::new(cfg);
        let big = format!("{}\n", "y".repeat(1024));
        for i in 0..8 {
            r.record("desktop.thread", "&str", None, &format!("{big}   1: my_app::f{i}\n"))
                .expect("report");
            clock::advance(Duration::from_secs(1));
        }
        let files = crash_files(tmp.path());
        assert!(!files.is_empty());
        assert!(files.len() < 8, "the dir cap must drop the oldest");
        let total: u64 = files
            .iter()
            .map(|n| fs::metadata(tmp.path().join(n)).expect("stat").len())
            .sum();
        assert!(total <= 3 * 1024, "dir holds {total} bytes");
    }

    #[test]
    fn rate_limit() {
        let tmp = tempfile::tempdir().unwrap();
        let mut cfg = test_cfg(tmp.path());
        cfg.limits.max_per_minute = 3;
        let r = Recorder::new(cfg);
        for i in 0..3 {
            r.record("desktop.thread", "&str", None, &stack(i)).expect("within the budget");
        }
        assert!(r.record("desktop.thread", "&str", None, &stack(99)).is_none());
        assert!(r.record("desktop.thread", "&str", None, &stack(98)).is_none());
        assert_eq!(crash_files(tmp.path()).len(), 3);

        // A minute later the budget is back.
        clock::advance(Duration::from_secs(61));
        let path = r.record("desktop.thread", "&str", None, &stack(97)).expect("budget back");
        let got = read_json(&path);
        assert_eq!(got["suppressedSinceLast"], json!(2));
        assert_eq!(crash_files(tmp.path()).len(), 4);
    }

    #[test]
    fn dedupes_identical_panics() {
        let tmp = tempfile::tempdir().unwrap();
        let r = Recorder::new(test_cfg(tmp.path()));
        assert!(r.record("desktop.thread", "&str", None, &stack(1)).is_some());
        clock::advance(Duration::from_secs(1));
        assert!(r.record("desktop.thread", "&str", None, &stack(1)).is_none());
        clock::advance(Duration::from_secs(1));
        // The same frame elsewhere is a different panic.
        let path = r.record("desktop.main", "&str", None, &stack(1)).expect("other site");
        let got = read_json(&path);
        assert_eq!(got["suppressedSinceLast"], json!(1));
        assert_eq!(crash_files(tmp.path()).len(), 2);
    }

    #[test]
    fn failed_write_counts_toward_rate_limit() {
        let tmp = tempfile::tempdir().unwrap();
        let plain = tmp.path().join("plain");
        fs::write(&plain, b"not a directory").unwrap();
        let mut cfg = test_cfg(&plain);
        cfg.limits.max_per_minute = 3;
        let r = Recorder::new(cfg);
        for i in 0..10 {
            assert!(r.record("desktop.thread", "&str", None, &stack(i)).is_none());
        }
        let st = r.lock_state();
        assert_eq!(st.write_times.len(), 3, "exactly 3 attempts may be made");
        assert_eq!(st.suppressed, 7);
        drop(st);
        assert!(fs::metadata(&plain).expect("still the file").is_file());
        assert!(crash_files(tmp.path()).is_empty());
    }

    #[test]
    fn second_concurrent_report_dropped() {
        let tmp = tempfile::tempdir().unwrap();
        let r = Recorder::new(test_cfg(tmp.path()));
        let _io = r.io.lock().unwrap_or_else(|e| e.into_inner());
        let before = r.lock_state().suppressed;
        assert!(r.record("desktop.thread", "&str", None, &stack(1)).is_none());
        assert_eq!(r.lock_state().suppressed, before + 1);
        drop(_io);
        assert!(r.record("desktop.thread", "&str", None, &stack(2)).is_some());
    }

    #[cfg(unix)]
    #[test]
    fn perms_0700_0600() {
        use std::os::unix::fs::PermissionsExt;
        let tmp = tempfile::tempdir().unwrap();
        let dir = tmp.path().join("crashes");
        fs::create_dir(&dir).unwrap();
        fs::set_permissions(&dir, fs::Permissions::from_mode(0o755)).unwrap();
        let r = Recorder::new(test_cfg(&dir));
        let path = r
            .record("desktop.thread", "&str", None, &stack(1))
            .expect("report written");
        let dir_mode = fs::metadata(&dir).unwrap().permissions().mode() & 0o777;
        assert_eq!(dir_mode, 0o700);
        let file_mode = fs::metadata(&path).unwrap().permissions().mode() & 0o777;
        assert_eq!(file_mode, 0o600);
    }

    #[cfg(unix)]
    #[test]
    fn refuses_link_dir() {
        use std::os::unix::fs::{symlink, PermissionsExt};
        let tmp = tempfile::tempdir().unwrap();
        let target = tmp.path().join("target");
        fs::create_dir(&target).unwrap();
        fs::set_permissions(&target, fs::Permissions::from_mode(0o755)).unwrap();
        let link = tmp.path().join("link");
        if symlink(&target, &link).is_err() {
            return; // cannot create symlinks here; nothing to check
        }
        let r = Recorder::new(test_cfg(&link));
        assert!(r.record("desktop.thread", "&str", None, &stack(1)).is_none());
        assert!(fs::symlink_metadata(&link).unwrap().file_type().is_symlink());
        let target_mode = fs::metadata(&target).unwrap().permissions().mode() & 0o777;
        assert_eq!(target_mode, 0o755, "the link target must keep its mode");
        assert!(
            fs::read_dir(&target).unwrap().next().is_none(),
            "the link target must receive nothing"
        );
    }

    #[test]
    fn write_failure_is_silent() {
        let tmp = tempfile::tempdir().unwrap();
        let plain = tmp.path().join("plain");
        fs::write(&plain, b"not a directory").unwrap();
        let r = Recorder::new(test_cfg(&plain));
        assert!(r.record("desktop.thread", "&str", None, &stack(1)).is_none());
        // A second failure is equally quiet, and the recorder keeps working.
        assert!(r.record("desktop.thread", "&str", None, &stack(2)).is_none());
        assert!(crash_files(tmp.path()).is_empty());
        assert_eq!(fs::read_to_string(&plain).unwrap(), "not a directory");
    }

    #[test]
    fn same_millisecond_no_overwrite() {
        let tmp = tempfile::tempdir().unwrap();
        let r = Recorder::new(test_cfg(tmp.path()));
        let first = r
            .record("desktop.thread", "&str", None, &stack(1))
            .expect("first report");
        let second = r
            .record("desktop.thread", "&str", None, &stack(2))
            .expect("second report");
        assert_ne!(first, second, "the same millisecond must not overwrite");
        assert!(first.exists() && second.exists());
        let files = crash_files(tmp.path());
        assert_eq!(files.len(), 2);
        assert!(files.iter().any(|n| n.ends_with("-desktop-shell-desktop.thread.json")));
        assert!(files.iter().any(|n| n.ends_with("-desktop-shell-desktop.thread-1.json")));
        assert_ne!(
            read_json(&first)["stack"],
            read_json(&second)["stack"],
            "both files must survive with their own content"
        );
    }

    #[test]
    fn tmp_file_of_other_writer_kept() {
        let tmp = tempfile::tempdir().unwrap();
        let dir = tmp.path().join("crashes");
        fs::create_dir(&dir).unwrap();
        let fresh = dir.join(".tmp-crash-9999-1");
        fs::write(&fresh, b"in flight").unwrap();
        let r = Recorder::new(test_cfg(&dir));
        assert!(r.record("desktop.thread", "&str", None, &stack(1)).is_some());
        assert!(fresh.exists(), "a fresh temp file of another writer is kept");

        let stale = dir.join(".tmp-crash-9999-2");
        fs::write(&stale, b"leftover").unwrap();
        let f = fs::OpenOptions::new().write(true).open(&stale).unwrap();
        f.set_modified(SystemTime::now() - Duration::from_secs(120)).unwrap();
        drop(f);
        clock::advance(Duration::from_secs(1));
        assert!(r.record("desktop.thread", "&str", None, &stack(2)).is_some());
        assert!(!stale.exists(), "an old temp file is a leftover and removed");
        assert!(fresh.exists());
    }
}
