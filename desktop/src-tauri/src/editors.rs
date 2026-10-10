//! desktop-native-feel D4: "Reveal in Finder/Explorer" and "Open in
//! editor" for local-connection paths. The webview only ever sends an
//! editor **id** from the fixed allowlist below and a path -- never an
//! executable or arguments -- and every rule (local-connection gate,
//! allowlist membership, absolute+exists validation, WSL2 translation,
//! argv construction) lives in pure functions over injectable
//! probe/runner traits so they are unit-tested without spawning
//! anything. The only OS calls happen in the real probe/runner impls
//! and the final `spawn`, which uses `windows_process::no_window` (the
//! same discipline `wsl::run` uses) so nothing flashes a console on
//! Windows.

use std::io;
use std::path::{Path, PathBuf};
use std::process::Command;

use serde::Serialize;
use smind_daemon_client::daemon_manager::wsl;
use smind_daemon_client::proxy::connections::ConnectionKind;
use smind_daemon_client::windows_process;

use crate::daemon_manager::{detect_platform_cached, resolve_distro_cached, Platform};
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

/// Windows install locations under `%LOCALAPPDATA%\Programs`, plus the
/// PATH fallback exe: (id, subpath, exe, label). Subpaths use Windows
/// separators and are only ever combined through `win_program_path`, so
/// the built string is the same no matter which OS the code runs (and
/// is tested) on.
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
    format!("{}\\Programs\\{}", local_appdata.display().to_string().trim_end_matches('\\'), subpath)
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
                let found = probe
                    .local_appdata()
                    .map(|la| probe.path_exists(&win_program_path(&la, subpath)))
                    .unwrap_or(false)
                    || probe.path_lookup(exe).is_some();
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
            let program = probe
                .local_appdata()
                .map(|la| win_program_path(&la, subpath))
                .filter(|p| probe.path_exists(p))
                .or_else(|| probe.path_lookup(exe))?;
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

/// The WSL2 boundary: a `wsl.exe wslpath -w` call, injectable for tests.
pub trait WslPathRunner {
    fn wslpath(&self, distro: &str, linux_path: &str) -> Result<String, String>;
}

pub struct WslExeRunner;

impl WslPathRunner for WslExeRunner {
    fn wslpath(&self, distro: &str, linux_path: &str) -> Result<String, String> {
        let argv = vec![
            wsl::WSL_EXE.to_string(),
            "-d".into(),
            distro.into(),
            "--".into(),
            "wslpath".into(),
            "-w".into(),
            linux_path.into(),
        ];
        let out = wsl::run(&argv).map_err(|e| format!("translating {linux_path} to a Windows path failed: {e}"))?;
        if !out.status.success() {
            return Err(format!(
                "translating {linux_path} to a Windows path failed: {}",
                decode_wsl_output(&out.stderr).trim()
            ));
        }
        let win = decode_wsl_output(&out.stdout).trim().to_string();
        if win.is_empty() {
            return Err(format!("translating {linux_path} to a Windows path returned nothing"));
        }
        Ok(win)
    }
}

/// wsl.exe writes UTF-16LE output (with or without a BOM) on some
/// locales and plain UTF-8 on others; accept both.
fn decode_wsl_output(raw: &[u8]) -> String {
    let (chunks, _) = raw.as_chunks::<2>();
    let looks_utf16 = (raw.len() >= 2 && raw[0] == 0xFF && raw[1] == 0xFE)
        || chunks.iter().take(8).any(|c| c[1] == 0x00 && c[0] != 0);
    if looks_utf16 && raw.len().is_multiple_of(2) {
        let units: Vec<u16> = chunks.iter().map(|c| u16::from_le_bytes([c[0], c[1]])).collect();
        return String::from_utf16_lossy(&units);
    }
    String::from_utf8_lossy(raw).into_owned()
}

/// The full decision pipeline behind `open_in_editor`, pure over the
/// probe/runner: returns the argv to spawn, or a human-readable error
/// (the UI shows it in a toast). Order: local connection, allowlist,
/// path validation (absolute, exists), WSL2 translation, argv.
#[allow(clippy::too_many_arguments)]
pub fn open_in_editor_argv(
    kind: ConnectionKind,
    platform: Platform,
    os: &str,
    editor_id: &str,
    raw_path: &str,
    probe: &dyn EditorProbe,
    wsl_runner: &dyn WslPathRunner,
    distro: Option<&str>,
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
    let path = if platform == Platform::Wsl2 {
        if !raw_path.starts_with('/') {
            return Err(format!("{raw_path:?} is not an absolute Linux path (WSL2 paths start with /)"));
        }
        let distro = distro.ok_or_else(|| "no default WSL distro found".to_string())?;
        let translated = wsl_runner.wslpath(distro, raw_path)?;
        if !probe.path_exists(&translated) {
            return Err(format!("{raw_path} (Windows path {translated}) does not exist on this machine"));
        }
        translated
    } else {
        if !Path::new(raw_path).is_absolute() {
            return Err(format!("{raw_path:?} is not an absolute path"));
        }
        if !probe.path_exists(raw_path) {
            return Err(format!("{raw_path} does not exist on this machine"));
        }
        probe.canonicalize(raw_path)?
    };
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
/// without a console window -- the same discipline `wsl::run` uses.
/// The child is reaped on a helper thread: `open`/`xdg-open` exit
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
    let mut child = cmd.spawn().map_err(|e: io::Error| format!("couldn't launch {}: {e}", argv[0]))?;
    std::thread::spawn(move || {
        let _ = child.wait();
    });
    Ok(())
}

#[tauri::command]
pub fn editors_list() -> Vec<EditorInfo> {
    detect_editors(std::env::consts::OS, &RealProbe)
}

/// Async (like every daemon_manager command) so it runs on the runtime,
/// not the main thread: the WSL2 path spawns `wsl.exe`, which can take
/// a second and would freeze the UI otherwise.
#[tauri::command]
pub async fn open_in_editor(
    state: tauri::State<'_, DesktopState>,
    editor_id: String,
    path: String,
) -> Result<(), String> {
    // `State<'_, _>` can't cross into the blocking task (its lifetime is
    // the command call's own), so the shared handles it needs are cloned
    // out as Arcs and the logic runs on the `_cached` variants.
    let registry = state.proxy.registry.clone();
    let platform_cache = state.daemon_manager_platform.clone();
    let distro_cache = state.daemon_manager_distro.clone();
    let argv = tauri::async_runtime::spawn_blocking(move || {
        let kind = registry.lock().unwrap().current().kind;
        let platform = detect_platform_cached(&platform_cache);
        let distro = if platform == Platform::Wsl2 { Some(resolve_distro_cached(&distro_cache)?) } else { None };
        open_in_editor_argv(
            kind,
            platform,
            std::env::consts::OS,
            &editor_id,
            &path,
            &RealProbe,
            &WslExeRunner,
            distro.as_deref(),
        )
    })
    .await
    .map_err(|e| format!("the open-in-editor task failed: {e}"))??;
    spawn_detached(&argv)
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

    struct FakeWsl {
        map: Vec<(String, Result<String, String>)>,
    }

    impl WslPathRunner for FakeWsl {
        fn wslpath(&self, _distro: &str, linux_path: &str) -> Result<String, String> {
            self.map
                .iter()
                .find(|(p, _)| p == linux_path)
                .map(|(_, r)| r.clone())
                .unwrap_or_else(|| Err(format!("no mapping for {linux_path}")))
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

    fn linux_probe() -> FakeProbe {
        FakeProbe::default().on_path("code", "/usr/bin/code").on_path("zed", "/usr/bin/zed")
    }

    fn no_wsl() -> FakeWsl {
        FakeWsl { map: vec![] }
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
        let mut user_only = FakeProbe {
            home: PathBuf::from(MACOS_HOME),
            ..FakeProbe::default()
        };
        user_only = user_only.with("/Users/test/Applications/Zed.app");
        let user_ids: Vec<String> = detect_editors("macos", &user_only).iter().map(|e| e.id.clone()).collect();
        assert_eq!(user_ids, vec!["file-manager", "zed"]);
        // And the launch names that user-level bundle, not /Applications.
        let argv = build_argv("macos", "zed", "/tmp/x", &user_only).unwrap();
        assert_eq!(
            argv,
            vec!["open".to_string(), "-a".to_string(), "/Users/test/Applications/Zed.app".to_string(), "/tmp/x".to_string()]
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
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Macos,
            "macos",
            "sublime",
            "/tmp/x",
            &probe,
            &no_wsl(),
            None,
        )
        .unwrap_err();
        assert!(err.contains("unknown editor"), "{err}");
        // A known id that isn't installed on this machine is equally rejected.
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Macos,
            "macos",
            "zed",
            "/tmp/x",
            &probe,
            &no_wsl(),
            None,
        )
        .unwrap_err();
        assert!(err.contains("zed is not installed"), "{err}");
    }

    #[test]
    fn open_in_editor_rejects_relative_or_missing_path() {
        let probe = macos_probe();
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Macos,
            "macos",
            "vscode",
            "relative/path",
            &probe,
            &no_wsl(),
            None,
        )
        .unwrap_err();
        assert!(err.contains("not an absolute path"), "{err}");

        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Macos,
            "macos",
            "vscode",
            "/no/such/dir",
            &probe,
            &no_wsl(),
            None,
        )
        .unwrap_err();
        assert!(err.contains("does not exist"), "{err}");

        // Same rules for the file manager.
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Macos,
            "macos",
            FILE_MANAGER_ID,
            "readme.md",
            &probe,
            &no_wsl(),
            None,
        )
        .unwrap_err();
        assert!(err.contains("not an absolute path"), "{err}");
    }

    #[test]
    fn open_in_editor_rejects_non_local_connection() {
        let probe = macos_probe();
        for kind in [ConnectionKind::Url, ConnectionKind::Relay] {
            let err = open_in_editor_argv(
                kind,
                Platform::Macos,
                "macos",
                "vscode",
                "/tmp/x",
                &probe,
                &no_wsl(),
                None,
            )
            .unwrap_err();
            assert!(err.contains("aren't on this machine"), "url/relay must be rejected: {err}");
            // Rejected before any id/path work: even a garbage editor id
            // reports the connection problem, not the editor problem.
            let err = open_in_editor_argv(
                kind,
                Platform::Macos,
                "macos",
                "garbage",
                "not/absolute",
                &probe,
                &no_wsl(),
                None,
            )
            .unwrap_err();
            assert!(err.contains("aren't on this machine"), "{err}");
        }
    }

    #[test]
    fn wslpath_translation_ok() {
        let probe = FakeProbe::default().dir(r"C:\Users\test\repo");
        let wsl = FakeWsl {
            map: vec![("/home/test/repo".to_string(), Ok(r"C:\Users\test\repo".to_string()))],
        };
        let argv = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Wsl2,
            "windows",
            FILE_MANAGER_ID,
            "/home/test/repo",
            &probe,
            &wsl,
            Some("Ubuntu"),
        )
        .unwrap();
        // The translated path (a dir) is opened in Explorer as one element.
        assert_eq!(argv, vec!["explorer.exe".to_string(), r"C:\Users\test\repo".to_string()]);

        // A non-/ Linux path is refused before any translation runs.
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Wsl2,
            "windows",
            FILE_MANAGER_ID,
            "home/test",
            &probe,
            &wsl,
            Some("Ubuntu"),
        )
        .unwrap_err();
        assert!(err.contains("absolute Linux path"), "{err}");
        // A NUL byte never reaches wsl.exe.
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Wsl2,
            "windows",
            FILE_MANAGER_ID,
            "/a\0b",
            &probe,
            &wsl,
            Some("Ubuntu"),
        )
        .unwrap_err();
        assert!(err.contains("NUL"), "{err}");
        // The translated path must exist on the Windows side.
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Wsl2,
            "windows",
            FILE_MANAGER_ID,
            "/gone",
            &probe,
            &FakeWsl { map: vec![("/gone".to_string(), Ok(r"C:\gone".to_string()))] },
            Some("Ubuntu"),
        )
        .unwrap_err();
        assert!(err.contains("does not exist"), "{err}");
    }

    #[test]
    fn wslpath_translation_failure_is_error() {
        let probe = FakeProbe::default();
        let wsl = FakeWsl {
            map: vec![("/home/test/gone".to_string(), Err("wslpath: No such file or directory".to_string()))],
        };
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Wsl2,
            "windows",
            FILE_MANAGER_ID,
            "/home/test/gone",
            &probe,
            &wsl,
            Some("Ubuntu"),
        )
        .unwrap_err();
        assert!(err.contains("No such file or directory"), "stderr must surface for the toast: {err}");
        // No distro resolved: equally an error, never a silent no-op.
        let err = open_in_editor_argv(
            ConnectionKind::Local,
            Platform::Wsl2,
            "windows",
            FILE_MANAGER_ID,
            "/home/test/repo",
            &probe,
            &wsl,
            None,
        )
        .unwrap_err();
        assert!(err.contains("distro"), "{err}");
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
            Platform::Macos,
            "windows",
            FILE_MANAGER_ID,
            "C:\\a\"b",
            &FakeProbe::default().dir("C:\\a\"b"),
            &no_wsl(),
            None,
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
