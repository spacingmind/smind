//! desktop-native-feel D4: "Reveal in Finder/Explorer" and "Open in
//! editor" for local-connection paths. The webview only ever sends an
//! editor **id** from the fixed allowlist below and a path -- never an
//! executable or arguments -- and every rule (local-connection gate,
//! allowlist membership, absolute+exists validation, argv construction)
//! lives in pure functions over injectable probe traits so they are
//! unit-tested without spawning anything. The only OS calls happen in the
//! real probe impls and the final `spawn`, which uses
//! `windows_process::no_window` so nothing flashes a console on Windows.

use std::io;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::mpsc;
use std::time::Duration;

use serde::Serialize;
use smind_daemon_client::proxy::connections::ConnectionKind;
use smind_daemon_client::windows_process;

use crate::state::DesktopState;

pub const FILE_MANAGER_ID: &str = "file-manager";

/// An editor (or the platform file manager) the UI can offer. `kind`
/// drives the menu label convention: "Reveal in <file manager>" vs
/// "Open in <editor>".
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct EditorInfo {
    pub id: String,
    pub label: String,
    pub kind: &'static str,
}

/// The filesystem/PATH surface detection and validation need, so the
/// pure logic can run against a fake in tests.
pub trait EditorProbe {
    fn path_exists(&self, path: &str) -> bool;
    fn is_dir(&self, path: &str) -> bool;
    fn canonicalize(&self, path: &str) -> Result<String, String>;
    fn home_dir(&self) -> PathBuf;
    /// `%LOCALAPPDATA%` on Windows; `None` elsewhere/in tests.
    fn local_appdata(&self) -> Option<PathBuf>;
    /// The machine-wide install roots on Windows (`%ProgramFiles%`,
    /// `%ProgramFiles(x86)%`, ...), where all-users installers put the
    /// editors; empty elsewhere/in tests.
    fn program_files_dirs(&self) -> Vec<PathBuf> {
        Vec::new()
    }
    /// Resolves `name` through PATH to a full executable path, `None`
    /// when not on PATH. Callers only ever ask for an `.exe` on Windows
    /// (never a `.cmd`/`.bat`, which would open a console and execute
    /// shell script, not the editor binary).
    fn path_lookup(&self, name: &str) -> Option<String>;
}

pub struct RealProbe;

impl EditorProbe for RealProbe {
    fn path_exists(&self, path: &str) -> bool {
        Path::new(path).exists()
    }
    fn is_dir(&self, path: &str) -> bool {
        Path::new(path).is_dir()
    }
    fn canonicalize(&self, path: &str) -> Result<String, String> {
        std::fs::canonicalize(path)
            .map(|p| strip_verbatim(&p.display().to_string()))
            .map_err(|e| format!("could not resolve {path}: {e}"))
    }
    fn home_dir(&self) -> PathBuf {
        home_dir()
    }
    fn local_appdata(&self) -> Option<PathBuf> {
        std::env::var("LOCALAPPDATA").ok().map(PathBuf::from)
    }
    fn program_files_dirs(&self) -> Vec<PathBuf> {
        let mut dirs: Vec<PathBuf> = Vec::new();
        for var in ["ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"] {
            if let Some(d) = std::env::var_os(var).map(PathBuf::from) {
                if !dirs.contains(&d) {
                    dirs.push(d);
                }
            }
        }
        dirs
    }
    fn path_lookup(&self, name: &str) -> Option<String> {
        let path_var = std::env::var_os("PATH")?;
        for dir in std::env::split_paths(&path_var) {
            let cand = dir.join(name);
            if cand.is_file() {
                return Some(cand.display().to_string());
            }
        }
        None
    }
}

/// Strips the `\\?\` verbatim prefix `std::fs::canonicalize` returns on
/// Windows: Explorer and editor binaries handle those poorly. A UNC
/// verbatim prefix (`\\?\UNC\server\share`) becomes the plain
/// `\\server\share` form. Non-verbatim paths pass through unchanged.
fn strip_verbatim(path: &str) -> String {
    if let Some(rest) = path.strip_prefix(r"\\?\UNC\") {
        format!("\\\\{rest}")
    } else if let Some(rest) = path.strip_prefix(r"\\?\") {
        rest.to_string()
    } else {
        path.to_string()
    }
}

fn home_dir() -> PathBuf {
    std::env::var_os("HOME")
        .or_else(|| std::env::var_os("USERPROFILE"))
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."))
}

/// macOS app-bundle names for each allowlisted editor id:
/// (id, bundle name, label).
const MACOS_BUNDLES: &[(&str, &str, &str)] = &[
    ("vscode", "Visual Studio Code", "VS Code"),
    ("cursor", "Cursor", "Cursor"),
    ("zed", "Zed", "Zed"),
];

/// Windows install locations -- the per-user `%LOCALAPPDATA%\Programs`
/// and the machine-wide `%ProgramFiles%` / `%ProgramFiles(x86)%` roots --
/// plus the PATH fallback exe: (id, subpath, exe, label). Subpaths use
/// Windows separators and are only ever combined through
/// `win_install_candidates`, so the built string is the same no matter
/// which OS the code runs (and is tested) on.
const WINDOWS_LOCATIONS: &[(&str, &str, &str, &str)] = &[
    ("vscode", r"Microsoft VS Code\Code.exe", "code.exe", "VS Code"),
    ("cursor", r"cursor\Cursor.exe", "cursor.exe", "Cursor"),
    ("zed", r"Zed\Zed.exe", "zed.exe", "Zed"),
];

/// Linux PATH command names: (id, command, label).
const LINUX_NAMES: &[(&str, &str, &str)] =
    &[("vscode", "code", "VS Code"), ("cursor", "cursor", "Cursor"), ("zed", "zed", "Zed")];

/// Builds `<localappdata>\Programs\<subpath>` as a plain string with
/// Windows separators (never `Path::join`, whose separator follows the
/// *host* OS -- a macOS dev machine running the tests must see the exact
/// string a Windows host would).
fn win_program_path(local_appdata: &Path, subpath: &str) -> String {
    win_join(local_appdata, &format!("Programs\\{subpath}"))
}

/// `<root>\<subpath>` with Windows separators regardless of host OS.
fn win_join(root: &Path, subpath: &str) -> String {
    format!("{}\\{}", root.display().to_string().trim_end_matches(['\\', '/']), subpath)
}

/// Every place an editor's exe may live on Windows, in preference order:
/// per-user install first, then the machine-wide roots.
fn win_install_candidates(probe: &dyn EditorProbe, subpath: &str) -> Vec<String> {
    let mut out: Vec<String> =
        probe.local_appdata().map(|la| win_program_path(&la, subpath)).into_iter().collect();
    out.extend(probe.program_files_dirs().iter().map(|pf| win_join(pf, subpath)));
    out
}

/// The first install candidate that exists on disk.
fn win_installed_path(probe: &dyn EditorProbe, subpath: &str) -> Option<String> {
    win_install_candidates(probe, subpath).into_iter().find(|p| probe.path_exists(p))
}

fn file_manager_label(os: &str) -> &'static str {
    match os {
        "macos" => "Finder",
        "windows" => "File Explorer",
        _ => "File Manager",
    }
}

/// detect_editors returns the fixed allowlist's members that are actually
/// installed: `file-manager` always (every supported OS has one), plus
/// only the editors the probe can see.
pub fn detect_editors(os: &str, probe: &dyn EditorProbe) -> Vec<EditorInfo> {
    let mut out = vec![EditorInfo {
        id: FILE_MANAGER_ID.to_string(),
        label: file_manager_label(os).to_string(),
        kind: "fileManager",
    }];
    let editor = |found: bool, id: &str, label: &str, out: &mut Vec<EditorInfo>| {
        if found {
            out.push(EditorInfo { id: id.to_string(), label: label.to_string(), kind: "editor" });
        }
    };
    match os {
        "macos" => {
            for (id, bundle, label) in MACOS_BUNDLES {
                let found = probe.path_exists(&format!("/Applications/{bundle}.app"))
                    || probe.path_exists(
                        &probe.home_dir().join(format!("Applications/{bundle}.app")).display().to_string(),
                    );
                editor(found, id, label, &mut out);
            }
        }
        "windows" => {
            for (id, subpath, exe, label) in WINDOWS_LOCATIONS {
                let found = win_installed_path(probe, subpath).is_some() || probe.path_lookup(exe).is_some();
                editor(found, id, label, &mut out);
            }
        }
        _ => {
            for (id, name, label) in LINUX_NAMES {
                editor(probe.path_lookup(name).is_some(), id, label, &mut out);
            }
        }
    }
    out
}

/// The resolved launch for an editor id: the program to exec and the
/// fixed arguments that precede the path (macOS `open -a <bundle>`).
struct Launch {
    program: String,
    prefix_args: Vec<String>,
}

fn resolve_editor(os: &str, id: &str, probe: &dyn EditorProbe) -> Option<Launch> {
    match os {
        "macos" => {
            let (_, bundle, _) = MACOS_BUNDLES.iter().find(|(e, _, _)| *e == id)?;
            let app = if probe.path_exists(&format!("/Applications/{bundle}.app")) {
                format!("/Applications/{bundle}.app")
            } else {
                probe.home_dir().join(format!("Applications/{bundle}.app")).display().to_string()
            };
            Some(Launch { program: "open".into(), prefix_args: vec!["-a".into(), app] })
        }
        "windows" => {
            let (_, subpath, exe, _) = WINDOWS_LOCATIONS.iter().find(|(e, _, _, _)| *e == id)?;
            let program = win_installed_path(probe, subpath).or_else(|| probe.path_lookup(exe))?;
            Some(Launch { program, prefix_args: vec![] })
        }
        _ => {
            let (_, name, _) = LINUX_NAMES.iter().find(|(e, _, _)| *e == id)?;
            Some(Launch { program: probe.path_lookup(name)?, prefix_args: vec![] })
        }
    }
}

/// build_argv composes the full spawn argv for `id` and `path`. The path
/// is always exactly one argv element, never interpolated into another
/// argument and never passed through a shell. `file-manager` only ever
/// REVEALS -- it never opens (let alone executes) the target itself.
pub fn build_argv(os: &str, id: &str, path: &str, probe: &dyn EditorProbe) -> Result<Vec<String>, String> {
    if id == FILE_MANAGER_ID {
        return Ok(match os {
            // `open -R` reveals in Finder without opening the file.
            "macos" => vec!["open".into(), "-R".into(), path.into()],
            "windows" => {
                if probe.is_dir(path) {
                    vec!["explorer.exe".into(), path.into()]
                } else {
                    vec!["explorer.exe".into(), format!("/select,{path}")]
                }
            }
            // xdg-open on the directory itself: for a file, its parent --
            // never the file (which would launch its handler).
            _ => {
                let dir = if probe.is_dir(path) {
                    path.to_string()
                } else {
                    Path::new(path).parent().map(|p| p.display().to_string()).unwrap_or_else(|| path.to_string())
                };
                vec!["xdg-open".into(), dir]
            }
        });
    }
    let launch =
        resolve_editor(os, id, probe).ok_or_else(|| format!("editor {id:?} is not installed on this machine"))?;
    let mut argv = vec![launch.program];
    argv.extend(launch.prefix_args);
    argv.push(path.to_string());
    Ok(argv)
}

/// The full decision pipeline behind `open_in_editor`, pure over the
/// probe: returns the argv to spawn, or a human-readable error (the UI
/// shows it in a toast). Order: local connection, allowlist, path
/// validation (absolute, exists), argv.
pub fn open_in_editor_argv(
    kind: ConnectionKind,
    os: &str,
    editor_id: &str,
    raw_path: &str,
    probe: &dyn EditorProbe,
) -> Result<Vec<String>, String> {
    if kind != ConnectionKind::Local {
        return Err("paths on this connection aren't on this machine -- switch to the Local connection first".to_string());
    }
    let detected = detect_editors(os, probe);
    if !detected.iter().any(|e| e.id == editor_id) {
        // Distinguish an id the app never knew from an allowlisted editor
        // that simply isn't installed here: both are rejected either way.
        let allowlisted = matches!(editor_id, "vscode" | "cursor" | "zed");
        let known: Vec<&str> = detected.iter().map(|e| e.id.as_str()).collect();
        return Err(if allowlisted {
            format!("{editor_id} is not installed on this machine (available: {})", known.join(", "))
        } else {
            format!("unknown editor {editor_id:?} (available: {})", known.join(", "))
        });
    }
    if raw_path.contains('\0') {
        return Err("the path contains a NUL byte".to_string());
    }
    // The Windows reveal path reaches Explorer through a single
    // `"/select,\"<path>\""` raw argument (see spawn_detached); a
    // embedded quote cannot be escaped there, so refuse it outright.
    if os == "windows" && editor_id == FILE_MANAGER_ID && raw_path.contains('"') {
        return Err("the path contains a quote character, which Explorer cannot select".to_string());
    }
    // Daemon paths are native to the host OS; they are used as given.
    if !Path::new(raw_path).is_absolute() {
        return Err(format!("{raw_path:?} is not an absolute path"));
    }
    if !probe.path_exists(raw_path) {
        return Err(format!("{raw_path} does not exist on this machine"));
    }
    let path = probe.canonicalize(raw_path)?;
    build_argv(os, editor_id, &path, probe)
}

/// The one argv shape that needs Windows raw-argument quoting: Explorer
/// ignores a conventionally-quoted `r"/select,C:\Program Files\x"`
/// (the quotes must sit *inside* the argument, around the path only).
/// Returns the raw argument to pass via `raw_arg`, `None` for every
/// other argv.
fn explorer_select_raw_arg(argv: &[String]) -> Option<String> {
    if argv.len() == 2 && argv[0] == "explorer.exe" && argv[1].starts_with("/select,") && !argv[1].ends_with(",") {
        let path = argv[1].strip_prefix("/select,")?;
        return Some(format!("/select,\"{path}\""));
    }
    None
}

/// Spawns the built argv with no shell, detached, and (on Windows)
/// without a console window. The child is reaped on a helper thread: `open`/`xdg-open` exit
/// quickly, and an un-waited child would linger as a zombie on Unix.
fn spawn_detached(argv: &[String]) -> Result<(), String> {
    let (program, args) = argv.split_first().ok_or("empty argv")?;
    let mut cmd = Command::new(program);
    if explorer_select_raw_arg(argv).is_some() {
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            cmd.raw_arg(explorer_select_raw_arg(argv).expect("checked above"));
            // `args` still holds the logical /select element; skip it.
            cmd.args(&args[1..]);
        }
        #[cfg(not(windows))]
        {
            // Tests on other hosts pass the logical argv straight through.
            cmd.args(args);
        }
    } else {
        cmd.args(args);
    }
    windows_process::no_window(&mut cmd);
    // `open` and `xdg-open` exit within moments and say why they failed
    // (an app that isn't there, no handler), so wait and surface it. Editor
    // binaries stay fire-and-forget, and `explorer.exe` exits 1 even on
    // success, so neither is waited on.
    if matches!(argv[0].as_str(), "open" | "xdg-open") {
        return run_and_report(cmd, &argv[0], OPENER_WAIT);
    }
    let mut child = cmd.spawn().map_err(|e: io::Error| format!("couldn't launch {}: {e}", argv[0]))?;
    std::thread::spawn(move || {
        let _ = child.wait();
    });
    Ok(())
}

/// How long to wait for `open`/`xdg-open` before treating them as
/// launched. `xdg-open` can stay alive as long as the file manager it
/// started does.
const OPENER_WAIT: Duration = Duration::from_secs(5);

/// Runs `cmd` (an `open`/`xdg-open` launcher) and surfaces its failure.
/// Never blocks past `wait` and never on pipe EOF: stdout is discarded,
/// and stderr is drained on a helper thread, because a launched file
/// manager can inherit the pipe and keep it open long after the launcher
/// exited (what made `Command::output()` hang). A launcher still running
/// at the deadline counts as launched; it is reaped when it exits.
fn run_and_report(mut cmd: Command, name: &str, wait: Duration) -> Result<(), String> {
    cmd.stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::piped());
    let mut child = cmd.spawn().map_err(|e: io::Error| format!("couldn't launch {name}: {e}"))?;
    let (err_tx, err_rx) = mpsc::channel::<Vec<u8>>();
    if let Some(mut stderr) = child.stderr.take() {
        std::thread::spawn(move || {
            let mut buf = Vec::new();
            let _ = io::Read::read_to_end(&mut stderr, &mut buf);
            let _ = err_tx.send(buf);
        });
    }
    let (status_tx, status_rx) = mpsc::channel();
    std::thread::spawn(move || {
        let _ = status_tx.send(child.wait());
    });
    match status_rx.recv_timeout(wait) {
        Ok(Ok(status)) if status.success() => Ok(()),
        Ok(Ok(status)) => {
            let stderr = err_rx.recv_timeout(Duration::from_millis(500)).unwrap_or_default();
            let detail = String::from_utf8_lossy(&stderr).trim().to_string();
            Err(if detail.is_empty() { format!("{name} failed ({status})") } else { format!("{name} failed: {detail}") })
        }
        Ok(Err(e)) => Err(format!("couldn't wait for {name}: {e}")),
        // Still running at the deadline: launched, not failed.
        Err(_) => Ok(()),
    }
}

/// Async so the PATH/filesystem probing runs off the main thread (a slow
/// network drive on PATH must not freeze the UI).
#[tauri::command]
pub async fn editors_list() -> Result<Vec<EditorInfo>, String> {
    tauri::async_runtime::spawn_blocking(|| detect_editors(std::env::consts::OS, &RealProbe))
        .await
        .map_err(|e| format!("the editor detection task failed: {e}"))
}

/// Async (like every daemon_manager command) so it runs on the runtime,
/// not the main thread: the PATH/filesystem probing can take a second
/// and would freeze the UI otherwise.
#[tauri::command]
pub async fn open_in_editor(
    state: tauri::State<'_, DesktopState>,
    editor_id: String,
    path: String,
) -> Result<(), String> {
    // `State<'_, _>` can't cross into the blocking task (its lifetime is
    // the command call's own), so the shared handle it needs is cloned
    // out as an Arc and the logic runs on the `_cached` variant.
    let registry = state.proxy.registry.clone();
    let argv = tauri::async_runtime::spawn_blocking(move || {
        let kind = registry.lock().unwrap().current().kind;
        open_in_editor_argv(kind, std::env::consts::OS, &editor_id, &path, &RealProbe)
    })
    .await
    .map_err(|e| format!("the open-in-editor task failed: {e}"))??;
    tauri::async_runtime::spawn_blocking(move || spawn_detached(&argv))
        .await
        .map_err(|e| format!("the launch task failed: {e}"))?
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;

    /// A fake filesystem: a set of existing paths, a home dir, an
    /// optional %LOCALAPPDATA%, and a PATH resolved to full paths.
    #[derive(Default)]
    struct FakeProbe {
        existing: Vec<String>,
        dirs: Vec<String>,
        home: PathBuf,
        local_appdata: Option<PathBuf>,
        path: Vec<(String, String)>,
        canonicalized: RefCell<Vec<String>>,
    }

    impl FakeProbe {
        fn with(mut self, p: &str) -> Self {
            self.existing.push(p.to_string());
            self
        }
        fn dir(mut self, p: &str) -> Self {
            self.dirs.push(p.to_string());
            self.existing.push(p.to_string());
            self
        }
        fn on_path(mut self, name: &str, full: &str) -> Self {
            self.path.push((name.to_string(), full.to_string()));
            self.existing.push(full.to_string());
            self
        }
    }

    impl EditorProbe for FakeProbe {
        fn path_exists(&self, path: &str) -> bool {
            self.existing.iter().any(|p| p == path)
        }
        fn is_dir(&self, path: &str) -> bool {
            self.dirs.iter().any(|p| p == path)
        }
        fn canonicalize(&self, path: &str) -> Result<String, String> {
            self.canonicalized.borrow_mut().push(path.to_string());
            Ok(path.to_string())
        }
        fn home_dir(&self) -> PathBuf {
            self.home.clone()
        }
        fn local_appdata(&self) -> Option<PathBuf> {
            self.local_appdata.clone()
        }
        fn path_lookup(&self, name: &str) -> Option<String> {
            self.path.iter().find(|(n, _)| n == name).map(|(_, f)| f.clone())
        }
    }

    const MACOS_HOME: &str = "/Users/test";
    const WIN_LOCAL: &str = r"C:\Users\test\AppData\Local";

    fn macos_probe() -> FakeProbe {
        let mut p = FakeProbe { home: PathBuf::from(MACOS_HOME), ..FakeProbe::default() };
        p = p.with("/Applications/Visual Studio Code.app");
        p.with("/Applications/Cursor.app")
    }

    fn win_probe() -> FakeProbe {
        FakeProbe {
            local_appdata: Some(PathBuf::from(WIN_LOCAL)),
            ..FakeProbe::default()
        }
        .with(r"C:\Users\test\AppData\Local\Programs\Microsoft VS Code\Code.exe")
        .on_path("cursor.exe", r"C:\Users\test\AppData\Local\Programs\cursor\Cursor.exe")
    }

    #[test]
    fn windows_editors_are_found_under_program_files_too() {
        struct SystemWide(FakeProbe);
        impl EditorProbe for SystemWide {
            fn path_exists(&self, p: &str) -> bool {
                self.0.path_exists(p)
            }
            fn is_dir(&self, p: &str) -> bool {
                self.0.is_dir(p)
            }
            fn canonicalize(&self, p: &str) -> Result<String, String> {
                self.0.canonicalize(p)
            }
            fn home_dir(&self) -> PathBuf {
                self.0.home_dir()
            }
            fn local_appdata(&self) -> Option<PathBuf> {
                self.0.local_appdata()
            }
            fn program_files_dirs(&self) -> Vec<PathBuf> {
                vec![PathBuf::from(r"C:\Program Files"), PathBuf::from(r"C:\Program Files (x86)")]
            }
            fn path_lookup(&self, n: &str) -> Option<String> {
                self.0.path_lookup(n)
            }
        }
        let probe = SystemWide(
            FakeProbe { local_appdata: Some(PathBuf::from(WIN_LOCAL)), ..FakeProbe::default() }
                .with(r"C:\Program Files\Microsoft VS Code\Code.exe")
                .with(r"C:\Program Files (x86)\Zed\Zed.exe"),
        );
        let ids: Vec<String> = detect_editors("windows", &probe).iter().map(|e| e.id.clone()).collect();
        assert_eq!(ids, vec!["file-manager", "vscode", "zed"]);
        let argv = build_argv("windows", "vscode", r"C:\work", &probe).unwrap();
        assert_eq!(argv[0], r"C:\Program Files\Microsoft VS Code\Code.exe");
        let argv = build_argv("windows", "zed", r"C:\work", &probe).unwrap();
        assert_eq!(argv[0], r"C:\Program Files (x86)\Zed\Zed.exe");
        // A per-user install still wins over a machine-wide one.
        let both = SystemWide(
            FakeProbe { local_appdata: Some(PathBuf::from(WIN_LOCAL)), ..FakeProbe::default() }
                .with(r"C:\Users\test\AppData\Local\Programs\Microsoft VS Code\Code.exe")
                .with(r"C:\Program Files\Microsoft VS Code\Code.exe"),
        );
        let argv = build_argv("windows", "vscode", r"C:\work", &both).unwrap();
        assert_eq!(argv[0], r"C:\Users\test\AppData\Local\Programs\Microsoft VS Code\Code.exe");
    }

    #[cfg(unix)]
    #[test]
    fn opener_returns_even_when_a_grandchild_keeps_stderr_open() {
        let started = std::time::Instant::now();
        let mut cmd = Command::new("sh");
        // The launcher exits 0 at once; a background child holds the pipe.
        cmd.args(["-c", "sleep 5 & exit 0"]);
        assert!(run_and_report(cmd, "sh", Duration::from_secs(3)).is_ok());
        assert!(started.elapsed() < Duration::from_secs(3), "blocked on the inherited pipe");
    }

    #[cfg(unix)]
    #[test]
    fn opener_failure_carries_stderr_and_a_slow_opener_is_not_an_error() {
        let mut cmd = Command::new("sh");
        cmd.args(["-c", "echo no handler >&2; exit 3"]);
        let err = run_and_report(cmd, "xdg-open", Duration::from_secs(3)).unwrap_err();
        assert_eq!(err, "xdg-open failed: no handler");

        let mut cmd = Command::new("sh");
        cmd.args(["-c", "sleep 2"]);
        assert!(run_and_report(cmd, "xdg-open", Duration::from_millis(100)).is_ok());
    }

    fn linux_probe() -> FakeProbe {
        FakeProbe::default().on_path("code", "/usr/bin/code").on_path("zed", "/usr/bin/zed")
    }

    #[test]
    fn editors_list_only_allowlisted() {
        let macos = detect_editors("macos", &macos_probe());
        let ids: Vec<&str> = macos.iter().map(|e| e.id.as_str()).collect();
        assert_eq!(ids, vec!["file-manager", "vscode", "cursor"], "only the allowlist, only what's detected");
        assert_eq!(macos[0].label, "Finder");
        assert_eq!(macos[0].kind, "fileManager");
        assert_eq!(macos.iter().filter(|e| e.kind == "editor").count(), 2);

        // A user-level bundle (~/Applications) counts too; Zed absent.
        // The expected string is built the same way the production code
        // builds it (Path::join), so the test holds on any host OS.
        let user_app = Path::new(MACOS_HOME).join("Applications/Zed.app").display().to_string();
        let user_only =
            FakeProbe { home: PathBuf::from(MACOS_HOME), ..FakeProbe::default() }.with(&user_app);
        let user_ids: Vec<String> = detect_editors("macos", &user_only).iter().map(|e| e.id.clone()).collect();
        assert_eq!(user_ids, vec!["file-manager", "zed"]);
        // And the launch names that user-level bundle, not /Applications.
        let argv = build_argv("macos", "zed", "/tmp/x", &user_only).unwrap();
        assert_eq!(
            argv,
            vec!["open".to_string(), "-a".to_string(), user_app, "/tmp/x".to_string()]
        );

        let win = detect_editors("windows", &win_probe());
        let ids: Vec<&str> = win.iter().map(|e| e.id.as_str()).collect();
        assert_eq!(ids, vec!["file-manager", "vscode", "cursor"]);
        assert_eq!(win[0].label, "File Explorer");

        // An empty filesystem still yields exactly the file manager.
        let none = detect_editors("linux", &FakeProbe::default());
        assert_eq!(none.len(), 1);
        assert_eq!(none[0].id, "file-manager");
        assert_eq!(none[0].label, "File Manager");

        let linux = detect_editors("linux", &linux_probe());
        let ids: Vec<&str> = linux.iter().map(|e| e.id.as_str()).collect();
        assert_eq!(ids, vec!["file-manager", "vscode", "zed"]);
    }

    #[test]
    fn open_in_editor_rejects_unknown_id() {
        let probe = macos_probe();
        let err =
            open_in_editor_argv(ConnectionKind::Local, "macos", "sublime", "/tmp/x", &probe).unwrap_err();
        assert!(err.contains("unknown editor"), "{err}");
        // A known id that isn't installed on this machine is equally rejected.
        let err =
            open_in_editor_argv(ConnectionKind::Local, "macos", "zed", "/tmp/x", &probe).unwrap_err();
        assert!(err.contains("zed is not installed"), "{err}");
    }

    #[test]
    fn open_in_editor_rejects_relative_or_missing_path() {
        // Fixtures must be absolute on the host OS running the test:
        // "/no/such/dir" is not absolute on Windows.
        let (os, probe, missing) =
            if cfg!(windows) { ("windows", win_probe(), r"C:\no\such\dir") } else { ("macos", macos_probe(), "/no/such/dir") };
        let err = open_in_editor_argv(ConnectionKind::Local, os, "vscode", "relative/path", &probe)
            .unwrap_err();
        assert!(err.contains("not an absolute path"), "{err}");

        // A genuinely missing path.
        let err = open_in_editor_argv(ConnectionKind::Local, os, "vscode", missing, &probe).unwrap_err();
        assert!(err.contains("does not exist"), "{err}");

        // Same rules for the file manager.
        let err = open_in_editor_argv(ConnectionKind::Local, os, FILE_MANAGER_ID, "readme.md", &probe)
            .unwrap_err();
        assert!(err.contains("not an absolute path"), "{err}");
    }

    #[test]
    fn open_in_editor_rejects_non_local_connection() {
        let probe = macos_probe();
        for kind in [ConnectionKind::Url, ConnectionKind::Relay] {
            let err =
                open_in_editor_argv(kind, "macos", "vscode", "/tmp/x", &probe).unwrap_err();
            assert!(err.contains("aren't on this machine"), "url/relay must be rejected: {err}");
            // Rejected before any id/path work: even a garbage editor id
            // reports the connection problem, not the editor problem.
            let err =
                open_in_editor_argv(kind, "macos", "garbage", "not/absolute", &probe).unwrap_err();
            assert!(err.contains("aren't on this machine"), "{err}");
        }
    }

    #[test]
    fn windows_paths_pass_through_untranslated() {
        // Native Windows paths reach reveal/open argv exactly as given,
        // even with spaces and non-ASCII, as one argv element.
        let spaced = r"C:\Users\test\My Répo\projekt";
        let probe = win_probe().dir(spaced);
        let argv =
            open_in_editor_argv(ConnectionKind::Local, "windows", FILE_MANAGER_ID, spaced, &probe)
                .unwrap();
        assert_eq!(argv, vec!["explorer.exe".to_string(), spaced.to_string()]);

        let argv =
            open_in_editor_argv(ConnectionKind::Local, "windows", "vscode", spaced, &probe).unwrap();
        assert_eq!(
            argv,
            vec![
                r"C:\Users\test\AppData\Local\Programs\Microsoft VS Code\Code.exe".to_string(),
                spaced.to_string(),
            ]
        );

        // A file is selected, not opened, with the path still one element.
        let file = r"C:\Users\test\My Répo\noté.md";
        let probe = probe.with(file);
        let argv =
            open_in_editor_argv(ConnectionKind::Local, "windows", FILE_MANAGER_ID, file, &probe)
                .unwrap();
        assert_eq!(argv, vec!["explorer.exe".to_string(), format!("/select,{file}")]);
    }

    #[test]
    fn argv_keeps_the_path_as_one_element_per_os_and_editor() {
        let p = "/tmp/repo";
        // macOS: open -a <bundle> <path> -- the path is a single element
        // after the fixed prefix, never concatenated into another arg.
        let argv = build_argv("macos", "vscode", p, &macos_probe()).unwrap();
        assert_eq!(
            argv,
            vec![
                "open".to_string(),
                "-a".to_string(),
                "/Applications/Visual Studio Code.app".to_string(),
                p.to_string()
            ]
        );
        let argv = build_argv("macos", "cursor", p, &macos_probe()).unwrap();
        assert_eq!(argv.last().unwrap(), p);
        assert_eq!(argv.len(), 4);

        // Windows: the real .exe (never a .cmd/.bat), path as one element.
        let wp = r"C:\repo";
        let argv = build_argv("windows", "vscode", wp, &win_probe()).unwrap();
        assert_eq!(argv[0], r"C:\Users\test\AppData\Local\Programs\Microsoft VS Code\Code.exe");
        assert_eq!(argv.last().unwrap(), wp);
        // Cursor came from the PATH lookup -- still an .exe.
        let argv = build_argv("windows", "cursor", wp, &win_probe()).unwrap();
        assert!(argv[0].ends_with(".exe"), "never a .cmd/.bat: {}", argv[0]);
        assert_eq!(argv.last().unwrap(), wp);

        // Linux: the command itself, path as one element.
        let argv = build_argv("linux", "vscode", p, &linux_probe()).unwrap();
        assert_eq!(argv, vec!["/usr/bin/code".to_string(), p.to_string()]);

        // A path that looks like an option is still just a path element:
        // it is never placed where a program option would be parsed from.
        let tricky = "/tmp/-rf";
        let argv = build_argv("linux", "zed", tricky, &linux_probe()).unwrap();
        assert_eq!(argv, vec!["/usr/bin/zed".to_string(), tricky.to_string()]);
    }

    #[test]
    fn strip_verbatim_normalizes_windows_canonicalize_output() {
        assert_eq!(strip_verbatim(r"C:\repo"), r"C:\repo");
        assert_eq!(strip_verbatim(r"\\?\C:\Users\test\repo"), r"C:\Users\test\repo");
        assert_eq!(strip_verbatim(r"\\?\UNC\server\share"), r"\\server\share");
        // No prefix confusion: a path that merely starts with backslashes.
        assert_eq!(strip_verbatim(r"\\server\share"), r"\\server\share");
    }

    #[test]
    fn explorer_select_raw_arg_quotes_the_path_inside_the_argument() {
        // A spaced path must become one raw argument with inner quotes.
        let argv = argv_from(&["explorer.exe", r"/select,C:\Program Files\x"]);
        assert_eq!(explorer_select_raw_arg(&argv).as_deref(), Some("/select,\"C:\\Program Files\\x\""));
        // Any other argv shape is spawned conventionally.
        assert_eq!(explorer_select_raw_arg(&argv_from(&["explorer.exe", r"C:\dir"])), None);
        assert_eq!(explorer_select_raw_arg(&argv_from(&["code", r"/select,C:\x"])), None);
        // A quote in the path can never reach this stage (rejected in
        // open_in_editor_argv).
        assert!(open_in_editor_argv(
            ConnectionKind::Local,
            "windows",
            FILE_MANAGER_ID,
            "C:\\a\"b",
            &FakeProbe::default().dir("C:\\a\"b"),
        )
        .is_err());
    }

    fn argv_from(parts: &[&str]) -> Vec<String> {
        parts.iter().map(|p| p.to_string()).collect()
    }

    #[test]
    fn file_manager_only_reveals_never_opens_the_target() {
        // macOS: `open -R` reveals; plain `open <path>` (which would open
        // the file) must never be built.
        let argv = build_argv("macos", FILE_MANAGER_ID, "/tmp/repo/evil.sh", &macos_probe()).unwrap();
        assert_eq!(argv, vec!["open".to_string(), "-R".to_string(), "/tmp/repo/evil.sh".to_string()]);

        // Windows file: explorer /select,<path> selects it; the file
        // itself is never explorer's argument (that would execute it).
        let win = FakeProbe {
            local_appdata: Some(PathBuf::from(WIN_LOCAL)),
            ..FakeProbe::default()
        }
        .with(r"C:\repo\evil.exe");
        let argv = build_argv("windows", FILE_MANAGER_ID, r"C:\repo\evil.exe", &win).unwrap();
        assert_eq!(argv, vec!["explorer.exe".to_string(), "/select,C:\\repo\\evil.exe".to_string()]);
        // Windows dir: the dir itself is opened (a dir cannot execute).
        let win_dir = win.dir(r"C:\repo");
        let argv = build_argv("windows", FILE_MANAGER_ID, r"C:\repo", &win_dir).unwrap();
        assert_eq!(argv, vec!["explorer.exe".to_string(), r"C:\repo".to_string()]);

        // Linux file: xdg-open gets the PARENT dir, never the file.
        let linux = FakeProbe::default().dir("/tmp/repo");
        let argv = build_argv("linux", FILE_MANAGER_ID, "/tmp/repo/evil.sh", &linux).unwrap();
        assert_eq!(argv, vec!["xdg-open".to_string(), "/tmp/repo".to_string()]);
        // Linux dir: the dir itself.
        let argv = build_argv("linux", FILE_MANAGER_ID, "/tmp/repo", &linux).unwrap();
        assert_eq!(argv, vec!["xdg-open".to_string(), "/tmp/repo".to_string()]);
    }
}
