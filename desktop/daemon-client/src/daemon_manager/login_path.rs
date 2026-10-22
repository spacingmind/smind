//! The user's login-shell `PATH` for the app-managed daemon (macOS + Linux).
//!
//! A Finder/launchd-launched app inherits `PATH=/usr/bin:/bin:/usr/sbin:/sbin`,
//! so a daemon started from it can't find `claude` (`~/.local/bin`) or
//! `npx`/`node` (`~/.nvm/...`): no agent can run. `smind service install`
//! avoids this by capturing the installing shell's PATH; the app has no
//! shell, so it asks the user's login shell instead.
//!
//! `resolve` runs `$SHELL -ilc '<script>'` where the script prints the
//! *exported* PATH between nonce-bearing markers (through `printenv`, a child
//! process, so it is right for fish as well, whose `$PATH` is a list, not a
//! string), tolerating any rc-file noise around the markers. The shell runs in
//! its own process group with stdin on /dev/null and a hard timeout; on
//! timeout the whole group is killed and reaped. When the shell can't be
//! used we start from the inherited PATH and append the usual install dirs
//! that exist on disk. Only PATH is taken from the login environment (the rest
//! could hold secrets); the result is cached per app launch (`cached`) and
//! prewarmed in the background (`prewarm`) so starting the daemon never waits
//! for the shell.
//!
//! The whole module is unix-only: Windows has its own spawn path
//! (`windows_native`) which is deliberately untouched.

use std::ffi::OsString;
use std::fs;
use std::io::Read;
use std::os::unix::fs::PermissionsExt;
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::mpsc;
use std::sync::OnceLock;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

/// How long the login shell may take (an rc file that runs `nvm`/`pyenv`
/// init can be slow, but a hung one must not hold up the daemon start).
pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(4);

/// Output cap: a shell that spews rc noise forever is treated as a failure.
const MAX_OUTPUT: usize = 1 << 20;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PathSource {
    /// PATH came from the user's login shell.
    LoginShell,
    /// The shell couldn't be used (reason); inherited PATH plus common dirs.
    Fallback(String),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResolvedPath {
    pub path: String,
    pub source: PathSource,
}

impl PathSource {
    pub fn describe(&self) -> String {
        match self {
            PathSource::LoginShell => "PATH from login shell".to_string(),
            PathSource::Fallback(reason) => format!("PATH from fallback dirs ({reason})"),
        }
    }
}

impl ResolvedPath {
    /// The value of `SMIND_PATH_SOURCE` for the daemon (shown by the daemon's
    /// "not found on PATH" diagnostics).
    pub fn source_env(&self) -> String {
        format!("desktop app, {}", self.source.describe())
    }
}

/// Everything `resolve` reads from the environment, injectable for tests.
#[derive(Debug, Clone)]
pub struct Inputs {
    pub shell: Option<OsString>,
    pub inherited_path: String,
    pub home: Option<PathBuf>,
    /// Absolute dirs probed by the fallback (after `~/.local/bin`).
    pub system_dirs: Vec<PathBuf>,
    pub timeout: Duration,
}

impl Inputs {
    pub fn from_env() -> Self {
        Self {
            shell: std::env::var_os("SHELL"),
            inherited_path: std::env::var_os("PATH").map(|p| p.to_string_lossy().into_owned()).unwrap_or_default(),
            home: std::env::var_os("HOME").filter(|h| !h.is_empty()).map(PathBuf::from),
            system_dirs: vec![PathBuf::from("/opt/homebrew/bin"), PathBuf::from("/usr/local/bin")],
            timeout: DEFAULT_TIMEOUT,
        }
    }
}

static CACHE: OnceLock<ResolvedPath> = OnceLock::new();

/// The per-launch resolution. The first caller resolves (bounded by
/// `DEFAULT_TIMEOUT`); concurrent callers wait for that same resolution, so a
/// start racing the prewarm reuses it instead of running a second shell.
pub fn cached() -> &'static ResolvedPath {
    CACHE.get_or_init(|| {
        let resolved = resolve(&Inputs::from_env());
        if let PathSource::Fallback(reason) = &resolved.source {
            eprintln!("smind desktop: login-shell PATH unavailable ({reason}); using inherited PATH plus common dirs");
        }
        resolved
    })
}

/// Starts `cached()` on a background thread so app start isn't delayed.
pub fn prewarm() {
    let _ = std::thread::Builder::new().name("smind-login-path".into()).spawn(|| {
        cached();
    });
}

pub fn resolve(inputs: &Inputs) -> ResolvedPath {
    let fallback = |reason: String| ResolvedPath {
        path: merge_paths(&inputs.inherited_path, &existing_fallback_dirs(inputs.home.as_deref(), &inputs.system_dirs).join(":")),
        source: PathSource::Fallback(reason),
    };
    let Some(shell) = inputs.shell.as_deref().filter(|s| !s.is_empty()) else {
        return fallback("$SHELL is not set".to_string());
    };
    let shell = Path::new(shell);
    if !is_executable_file(shell) {
        return fallback(format!("$SHELL ({}) is not an executable file", shell.display()));
    }
    match login_shell_path(shell, inputs.timeout) {
        Ok(login) => ResolvedPath { path: merge_paths(&login, &inputs.inherited_path), source: PathSource::LoginShell },
        Err(reason) => fallback(reason),
    }
}

/// Entries of `primary` first, then those of `secondary` not already present;
/// duplicates and empty entries are dropped, first occurrence wins.
pub fn merge_paths(primary: &str, secondary: &str) -> String {
    let mut seen: Vec<&str> = Vec::new();
    for entry in primary.split(':').chain(secondary.split(':')) {
        if !entry.is_empty() && !seen.contains(&entry) {
            seen.push(entry);
        }
    }
    seen.join(":")
}

fn is_executable_file(path: &Path) -> bool {
    fs::metadata(path).map(|m| m.is_file() && m.permissions().mode() & 0o111 != 0).unwrap_or(false)
}

/// The fallback dirs that exist: `~/.local/bin`, the system dirs, then the
/// newest nvm node's `bin`.
fn existing_fallback_dirs(home: Option<&Path>, system_dirs: &[PathBuf]) -> Vec<String> {
    let mut candidates: Vec<PathBuf> = Vec::new();
    if let Some(home) = home {
        candidates.push(home.join(".local/bin"));
    }
    candidates.extend(system_dirs.iter().cloned());
    if let Some(home) = home {
        candidates.extend(newest_nvm_bin(&home.join(".nvm/versions/node")));
    }
    candidates.into_iter().filter(|p| p.is_dir()).map(|p| p.to_string_lossy().into_owned()).collect()
}

/// `<versions>/<newest vX.Y.Z>/bin`, comparing the version numerically;
/// names that aren't `vMAJOR[.MINOR[.PATCH]]` are ignored.
fn newest_nvm_bin(versions_dir: &Path) -> Option<PathBuf> {
    let newest = fs::read_dir(versions_dir)
        .ok()?
        .filter_map(|e| e.ok())
        .filter_map(|e| Some((parse_node_version(e.file_name().to_str()?)?, e.path())))
        .max_by_key(|(version, _)| *version)?;
    Some(newest.1.join("bin"))
}

fn parse_node_version(name: &str) -> Option<(u64, u64, u64)> {
    let mut parts = name.strip_prefix('v')?.split('.');
    let major = parts.next()?.parse().ok()?;
    let minor = parts.next().map_or(Some(0), |p| p.parse().ok())?;
    let patch = parts.next().map_or(Some(0), |p| p.parse().ok())?;
    parts.next().is_none().then_some((major, minor, patch))
}

fn nonce() -> String {
    static COUNTER: AtomicU64 = AtomicU64::new(0);
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0);
    format!("{}_{}_{}", std::process::id(), nanos, COUNTER.fetch_add(1, Ordering::Relaxed))
}

fn markers(nonce: &str) -> (String, String) {
    (format!("__SMIND_PATH_{nonce}_START__"), format!("__SMIND_PATH_{nonce}_END__"))
}

/// The script run by the login shell. `printenv` is a child process, so it
/// reports the exported PATH in every shell (bash, zsh, fish).
fn script(start: &str, end: &str) -> String {
    format!("echo {start}; /usr/bin/printenv PATH || printenv PATH; echo {end}")
}

/// The PATH between the markers, ignoring everything before the start marker
/// and after the end marker. `None` if either marker is missing or the PATH
/// is empty.
fn extract_path(output: &str, start: &str, end: &str) -> Option<String> {
    let after_start = &output[output.find(start)? + start.len()..];
    let between = &after_start[..after_start.find(end)?];
    let path = between.lines().map(str::trim).rfind(|l| !l.is_empty())?;
    Some(path.to_string())
}

fn login_shell_path(shell: &Path, timeout: Duration) -> Result<String, String> {
    let nonce = nonce();
    let (start, end) = markers(&nonce);
    let mut child = Command::new(shell)
        .arg("-ilc")
        .arg(script(&start, &end))
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .process_group(0)
        .spawn()
        .map_err(|e| format!("could not run login shell {}: {e}", shell.display()))?;
    let mut stdout = child.stdout.take().expect("smind desktop: stdout was piped");

    // The reader answers as soon as the end marker arrives (so a stray
    // background process holding the pipe can't delay us) or at EOF.
    let (tx, rx) = mpsc::channel::<Option<String>>();
    let (reader_start, reader_end) = (start.clone(), end.clone());
    std::thread::spawn(move || {
        let mut buf: Vec<u8> = Vec::new();
        let mut chunk = [0u8; 4096];
        loop {
            match stdout.read(&mut chunk) {
                Ok(0) | Err(_) => break,
                Ok(n) => {
                    buf.extend_from_slice(&chunk[..n]);
                    let text = String::from_utf8_lossy(&buf);
                    if let Some(path) = extract_path(&text, &reader_start, &reader_end) {
                        let _ = tx.send(Some(path));
                        return;
                    }
                    if buf.len() > MAX_OUTPUT {
                        break;
                    }
                }
            }
        }
        let _ = tx.send(None);
    });

    let outcome = rx.recv_timeout(timeout);
    let exited = child.try_wait().ok().flatten();
    kill_group_and_reap(&mut child);
    match outcome {
        Ok(Some(path)) => Ok(path),
        Ok(None) => Err(match exited {
            Some(status) => format!("login shell exited with {status} without printing PATH"),
            None => "login shell printed no PATH markers".to_string(),
        }),
        Err(_) => Err(format!("login shell timed out after {}s", timeout.as_secs_f32())),
    }
}

/// SIGKILL the shell's whole process group (it is the group leader), then
/// reap the shell so no zombie is left behind.
fn kill_group_and_reap(child: &mut Child) {
    let pgid = child.id();
    let kill = if Path::new("/bin/kill").exists() { "/bin/kill" } else { "kill" };
    let _ = Command::new(kill)
        .args(["-KILL", "--"])
        .arg(format!("-{pgid}"))
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    let _ = child.kill();
    let _ = child.wait();
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Instant;

    const START: &str = "__SMIND_PATH_1_2_3_START__";
    const END: &str = "__SMIND_PATH_1_2_3_END__";

    fn extract(output: &str) -> Option<String> {
        extract_path(output, START, END)
    }

    #[test]
    fn extracts_path_between_markers() {
        assert_eq!(extract(&format!("{START}\n/a:/b\n{END}\n")).as_deref(), Some("/a:/b"));
    }

    #[test]
    fn ignores_noise_before_and_after() {
        let out = format!("Last login: Mon\nmotd line\n\x1b[1;32mhello\x1b[0m\n{START}\n/a:/b\n{END}\ngoodbye\n\x1b[0m");
        assert_eq!(extract(&out).as_deref(), Some("/a:/b"));
    }

    #[test]
    fn noise_glued_to_the_start_marker_is_ignored() {
        assert_eq!(extract(&format!("prompt% {START}\n/a\n{END}\n")).as_deref(), Some("/a"));
    }

    #[test]
    fn multi_line_noise_around_markers() {
        let out = format!("a\nb\nc\n\n{START}\n/x:/y\n{END}\nd\ne\nf\n");
        assert_eq!(extract(&out).as_deref(), Some("/x:/y"));
    }

    #[test]
    fn missing_markers_fail() {
        assert_eq!(extract("/a:/b\n"), None);
        assert_eq!(extract(&format!("{START}\n/a:/b\n")), None);
        assert_eq!(extract(&format!("/a:/b\n{END}\n")), None);
        assert_eq!(extract(""), None);
    }

    #[test]
    fn empty_path_fails() {
        assert_eq!(extract(&format!("{START}\n{END}\n")), None);
        assert_eq!(extract(&format!("{START}\n\n   \n{END}\n")), None);
    }

    #[test]
    fn markers_in_noise_without_the_nonce_do_not_match() {
        let out = "__SMIND_PATH_START__\n/evil\n__SMIND_PATH_END__\n__SMIND_PATH_9_9_9_START__\n/evil2\n__SMIND_PATH_9_9_9_END__\n";
        assert_eq!(extract(out), None);
        let with_real = format!("{out}{START}\n/good\n{END}\n");
        assert_eq!(extract(&with_real).as_deref(), Some("/good"));
    }

    #[test]
    fn nonces_differ_between_calls() {
        assert_ne!(nonce(), nonce());
    }

    #[test]
    fn script_prints_both_markers_around_printenv() {
        let s = script(START, END);
        assert!(s.starts_with(&format!("echo {START};")) && s.ends_with(&format!("echo {END}")));
        assert!(s.contains("printenv PATH"));
    }

    #[test]
    fn merge_puts_login_first_then_new_inherited_dedupes_and_drops_empties() {
        assert_eq!(merge_paths("/l/a::/l/b:/usr/bin", "/usr/bin:/bin::/l/a:/sbin"), "/l/a:/l/b:/usr/bin:/bin:/sbin");
        assert_eq!(merge_paths("", "/bin:/bin"), "/bin");
        assert_eq!(merge_paths("", ""), "");
    }

    #[test]
    fn node_versions_compare_numerically_and_junk_is_ignored() {
        assert_eq!(parse_node_version("v20.11.1"), Some((20, 11, 1)));
        assert_eq!(parse_node_version("v9"), Some((9, 0, 0)));
        assert_eq!(parse_node_version("20.11.1"), None);
        assert_eq!(parse_node_version("v20.x"), None);
        assert_eq!(parse_node_version("vlatest"), None);
        assert_eq!(parse_node_version("v1.2.3.4"), None);
    }

    fn mkdir(p: &Path) {
        fs::create_dir_all(p).unwrap();
    }

    fn inputs(shell: Option<&str>, home: Option<&Path>, system_dirs: Vec<PathBuf>, timeout: Duration) -> Inputs {
        Inputs {
            shell: shell.map(OsString::from),
            inherited_path: "/usr/bin:/bin".to_string(),
            home: home.map(Path::to_path_buf),
            system_dirs,
            timeout,
        }
    }

    #[test]
    fn fallback_appends_existing_dirs_with_newest_nvm_node() {
        let tmp = tempfile::tempdir().unwrap();
        let home = tmp.path().join("home");
        let sys = tmp.path().join("sys/bin");
        let missing = tmp.path().join("sys/missing");
        mkdir(&home.join(".local/bin"));
        mkdir(&sys);
        for v in ["v9.11.2", "v20.11.1", "v20.9.0", "v18.20.4", "latest", "v21.x"] {
            mkdir(&home.join(".nvm/versions/node").join(v).join("bin"));
        }
        let got = resolve(&inputs(None, Some(&home), vec![sys.clone(), missing], Duration::from_secs(1)));
        let expect = format!("/usr/bin:/bin:{}:{}:{}", home.join(".local/bin").display(), sys.display(), home.join(".nvm/versions/node/v20.11.1/bin").display());
        assert_eq!(got.path, expect);
        assert_eq!(got.source, PathSource::Fallback("$SHELL is not set".to_string()));
    }

    #[test]
    fn fallback_with_nothing_on_disk_is_the_inherited_path() {
        let tmp = tempfile::tempdir().unwrap();
        let got = resolve(&inputs(Some(""), Some(tmp.path()), vec![tmp.path().join("nope")], Duration::from_secs(1)));
        assert_eq!(got.path, "/usr/bin:/bin");
        assert!(matches!(got.source, PathSource::Fallback(_)));
        let got = resolve(&inputs(None, None, vec![], Duration::from_secs(1)));
        assert_eq!(got.path, "/usr/bin:/bin");
    }

    #[test]
    fn nonexistent_or_non_executable_shell_falls_back() {
        let tmp = tempfile::tempdir().unwrap();
        let got = resolve(&inputs(Some("/definitely/not/a/shell"), Some(tmp.path()), vec![], Duration::from_secs(1)));
        assert!(matches!(&got.source, PathSource::Fallback(r) if r.contains("not an executable file")), "{got:?}");

        let plain = tmp.path().join("plain");
        fs::write(&plain, "#!/bin/sh\n").unwrap();
        let got = resolve(&inputs(Some(plain.to_str().unwrap()), Some(tmp.path()), vec![], Duration::from_secs(1)));
        assert!(matches!(got.source, PathSource::Fallback(_)));

        // a directory is not a shell either
        let got = resolve(&inputs(Some(tmp.path().to_str().unwrap()), None, vec![], Duration::from_secs(1)));
        assert!(matches!(got.source, PathSource::Fallback(_)));
    }

    fn fake_shell(dir: &Path, body: &str) -> PathBuf {
        let path = dir.join("fakesh");
        fs::write(&path, format!("#!/bin/sh\n{body}\n")).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o755)).unwrap();
        path
    }

    #[test]
    fn runs_the_real_script_end_to_end_through_a_fake_login_shell() {
        let tmp = tempfile::tempdir().unwrap();
        // behaves like `sh -ilc <script>` with noisy rc files and a custom PATH
        let shell = fake_shell(
            tmp.path(),
            "[ \"$1\" = -ilc ] || exit 9\necho 'Welcome! (motd)'\nprintf '\\033[1mrc noise\\033[0m'\nexport PATH=/fake/login/bin:/fake/other:/usr/bin\neval \"$2\"\necho 'trailing noise'",
        );
        let got = resolve(&inputs(Some(shell.to_str().unwrap()), None, vec![], Duration::from_secs(5)));
        assert_eq!(got.source, PathSource::LoginShell, "{got:?}");
        assert_eq!(got.path, "/fake/login/bin:/fake/other:/usr/bin:/bin");
    }

    #[test]
    fn shell_without_markers_or_failing_falls_back() {
        let tmp = tempfile::tempdir().unwrap();
        let shell = fake_shell(tmp.path(), "echo only noise\nexit 3");
        let got = resolve(&inputs(Some(shell.to_str().unwrap()), None, vec![], Duration::from_secs(5)));
        assert!(matches!(&got.source, PathSource::Fallback(r) if r.contains("without printing PATH")), "{got:?}");
        assert_eq!(got.path, "/usr/bin:/bin");
    }

    fn pid_alive(pid: &str) -> bool {
        Command::new("kill").args(["-0", pid]).stdout(Stdio::null()).stderr(Stdio::null()).status().map(|s| s.success()).unwrap_or(false)
    }

    #[test]
    fn hung_shell_times_out_quickly_and_the_whole_group_is_gone() {
        let tmp = tempfile::tempdir().unwrap();
        let pids = tmp.path().join("pids");
        let shell = fake_shell(tmp.path(), &format!("sleep 30 &\necho \"$$ $!\" > '{}'\nwait\n", pids.display()));
        let started = Instant::now();
        let got = resolve(&inputs(Some(shell.to_str().unwrap()), None, vec![], Duration::from_millis(500)));
        assert!(started.elapsed() < Duration::from_secs(2), "took {:?}", started.elapsed());
        assert!(matches!(&got.source, PathSource::Fallback(r) if r.contains("timed out")), "{got:?}");

        let recorded = fs::read_to_string(&pids).unwrap();
        for pid in recorded.split_whitespace() {
            let deadline = Instant::now() + Duration::from_secs(2);
            while pid_alive(pid) && Instant::now() < deadline {
                std::thread::sleep(Duration::from_millis(20));
            }
            assert!(!pid_alive(pid), "pid {pid} still alive after timeout");
        }
    }

    #[test]
    fn a_lingering_background_process_holding_stdout_does_not_delay_the_result() {
        let tmp = tempfile::tempdir().unwrap();
        let shell = fake_shell(tmp.path(), "export PATH=/fake/bin\neval \"$2\"\nsleep 30 &\nexit 0");
        let started = Instant::now();
        let got = resolve(&inputs(Some(shell.to_str().unwrap()), None, vec![], Duration::from_secs(5)));
        assert!(started.elapsed() < Duration::from_secs(3), "took {:?}", started.elapsed());
        assert_eq!(got.source, PathSource::LoginShell, "{got:?}");
        assert_eq!(got.path, "/fake/bin:/usr/bin:/bin");
    }

    #[test]
    fn describes_the_source_for_the_daemon() {
        let login = ResolvedPath { path: "/a".into(), source: PathSource::LoginShell };
        assert_eq!(login.source_env(), "desktop app, PATH from login shell");
        let fb = ResolvedPath { path: "/a".into(), source: PathSource::Fallback("login shell timed out after 4s".into()) };
        assert_eq!(fb.source_env(), "desktop app, PATH from fallback dirs (login shell timed out after 4s)");
    }

    /// Run with `cargo test -- --ignored --nocapture prints_real_login_shell_path`.
    #[test]
    #[ignore]
    fn prints_real_login_shell_path() {
        let inputs = Inputs::from_env();
        let started = Instant::now();
        let resolved = resolve(&inputs);
        eprintln!("shell: {:?}", inputs.shell);
        eprintln!("elapsed: {:?}", started.elapsed());
        eprintln!("source: {:?}", resolved.source);
        eprintln!("SMIND_PATH_SOURCE: {}", resolved.source_env());
        eprintln!("PATH: {}", resolved.path);
        for entry in resolved.path.split(':') {
            eprintln!("  {entry}");
        }
    }
}
