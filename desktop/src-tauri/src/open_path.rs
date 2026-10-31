//! Office mode (ADR-0049 section 5.1): "Open in app" for a file the agent produced.
//!
//! `open_path_default(root, rel_path)` opens one regular document in the OS default application. The webview
//! sends the task's worktree root and a worktree-relative path; every decision lives in `resolve_openable`, a
//! pure function over an injectable probe, so each refusal is unit-tested without touching the OS. The only OS
//! calls are the real probe's `canonicalize`/`metadata` and the final `tauri-plugin-opener` path call: no shell,
//! no argv string, no `cmd /c start`.
//!
//! In order, a request is refused unless: the attached daemon is the local one (a remote daemon's paths are not
//! on this machine); neither path holds a NUL; on Windows neither names a UNC share or a device (so resolving it
//! can never start SMB authentication); `rel_path` is relative; the canonical (symlink-resolved) target lies
//! inside the canonical root; the target is a regular file (not a directory, `.app` bundle or link); and its
//! lower-cased extension is on the allow-list. Error text is fixed vocabulary: it never carries a path.

use serde::Serialize;
use smind_daemon_client::proxy::connections::ConnectionKind;

use crate::state::DesktopState;

/// The only extensions that may be opened (S15). An allow-list, never a deny-list: launchers (`exe app command
/// bat cmd ps1 sh msi dmg jar lnk url desktop scr`), scripts, and `html`/`svg` (a browser with file access) are
/// refused by default.
pub const ALLOWED_EXTENSIONS: [&str; 19] = [
    "pdf", "docx", "doc", "xlsx", "xls", "pptx", "ppt", "odt", "ods", "odp", "rtf", "csv", "txt", "md", "png", "jpg",
    "jpeg", "gif", "webp",
];

/// The refusals, as the UI shows them. None carries a path.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
pub enum Refusal {
    Remote,
    BadPath,
    Network,
    Outside,
    NotAFile,
    NotAllowed,
    Missing,
    NoApp,
}

impl Refusal {
    pub fn message(self) -> &'static str {
        match self {
            Refusal::Remote => "This file is on another computer, so it can't be opened here",
            Refusal::BadPath => "That path can't be opened",
            Refusal::Network => "Files on a network share can't be opened from here",
            Refusal::Outside => "That file is outside the task's folder",
            Refusal::NotAFile => "Only files can be opened, not folders",
            Refusal::NotAllowed => "That file type can't be opened from here",
            Refusal::Missing => "That file no longer exists",
            Refusal::NoApp => "No application is set to open this file type",
        }
    }
}

/// The filesystem surface the decision needs.
pub trait OpenProbe {
    /// Resolves symlinks (and, on Windows, strips the `\\?\` verbatim prefix); `Err(NotFound)` when missing.
    fn canonicalize(&self, path: &str) -> Result<String, std::io::ErrorKind>;
    /// True for a regular file; false for a directory, a link, a device or anything else.
    fn is_regular_file(&self, path: &str) -> bool;
}

pub struct RealOpenProbe;

impl OpenProbe for RealOpenProbe {
    fn canonicalize(&self, path: &str) -> Result<String, std::io::ErrorKind> {
        std::fs::canonicalize(path)
            .map(|p| strip_verbatim(&p.display().to_string()))
            .map_err(|e| e.kind())
    }
    fn is_regular_file(&self, path: &str) -> bool {
        // symlink_metadata: a link is not a regular file even if its target is (the path is already canonical,
        // so a link here means something raced in after the canonicalize).
        std::fs::symlink_metadata(path).map(|m| m.file_type().is_file()).unwrap_or(false)
    }
}

fn strip_verbatim(path: &str) -> String {
    if let Some(rest) = path.strip_prefix(r"\\?\UNC\") {
        format!("\\\\{rest}")
    } else if let Some(rest) = path.strip_prefix(r"\\?\") {
        rest.to_string()
    } else {
        path.to_string()
    }
}

/// A Windows UNC share or device path (`\\server\share`, `//server/share`, `\\.\pipe`, `\\?\UNC\...`); the local
/// verbatim form `\\?\C:\...` is not one.
fn is_unc_or_device(path: &str) -> bool {
    let p = path.replace('/', "\\");
    if !p.starts_with("\\\\") {
        return false;
    }
    match p.strip_prefix("\\\\?\\") {
        Some(rest) => rest.len() >= 4 && rest[..4].eq_ignore_ascii_case("UNC\\"),
        None => true,
    }
}

/// True when `rel` could name something other than a path below the root: absolute, rooted, drive-qualified.
fn is_not_relative(rel: &str, windows: bool) -> bool {
    if rel.is_empty() || rel.starts_with('/') || rel.starts_with('\\') {
        return true;
    }
    windows && rel.as_bytes().get(1) == Some(&b':')
}

fn components(path: &str, windows: bool) -> Vec<String> {
    let norm = if windows { path.replace('\\', "/") } else { path.to_string() };
    norm.split('/')
        .filter(|c| !c.is_empty())
        .map(|c| if windows { c.to_ascii_lowercase() } else { c.to_string() })
        .collect()
}

/// Whether the canonical `target` lies strictly below the canonical `root`, compared component by component
/// (so `/work/a-evil` is not inside `/work/a`), case-insensitively on Windows.
fn inside(root: &str, target: &str, windows: bool) -> bool {
    let (r, t) = (components(root, windows), components(target, windows));
    !r.is_empty() && t.len() > r.len() && t[..r.len()] == r[..] && !t.iter().any(|c| c == "..")
}

fn extension_allowed(path: &str) -> bool {
    let name = path.rsplit(['/', '\\']).next().unwrap_or("");
    match name.rfind('.') {
        Some(dot) if dot > 0 => {
            let ext = name[dot + 1..].to_ascii_lowercase();
            ALLOWED_EXTENSIONS.contains(&ext.as_str())
        }
        _ => false,
    }
}

/// The whole decision. `os` is `std::env::consts::OS` (a parameter so Windows rows run on any host); on success
/// the canonical path to hand to the opener.
pub fn resolve_openable(
    kind: ConnectionKind,
    os: &str,
    root: &str,
    rel: &str,
    probe: &dyn OpenProbe,
) -> Result<String, Refusal> {
    if kind != ConnectionKind::Local {
        return Err(Refusal::Remote);
    }
    if root.is_empty() || root.contains('\0') || rel.contains('\0') {
        return Err(Refusal::BadPath);
    }
    let windows = os == "windows";
    if windows && (is_unc_or_device(root) || is_unc_or_device(rel)) {
        return Err(Refusal::Network);
    }
    if is_not_relative(rel, windows) {
        return Err(Refusal::BadPath);
    }
    let sep = if windows { '\\' } else { '/' };
    let joined = format!("{}{}{}", root.trim_end_matches(['/', '\\']), sep, rel);

    let canon_root = probe.canonicalize(root).map_err(|_| Refusal::BadPath)?;
    let canon = probe.canonicalize(&joined).map_err(|k| match k {
        std::io::ErrorKind::NotFound => Refusal::Missing,
        _ => Refusal::BadPath,
    })?;
    // Canonicalising can itself land on a share (a junction to one): re-check the result, before any open.
    if windows && (is_unc_or_device(&canon_root) || is_unc_or_device(&canon)) {
        return Err(Refusal::Network);
    }
    if !inside(&canon_root, &canon, windows) {
        return Err(Refusal::Outside);
    }
    if !probe.is_regular_file(&canon) {
        return Err(Refusal::NotAFile);
    }
    if !extension_allowed(&canon) {
        return Err(Refusal::NotAllowed);
    }
    Ok(canon)
}

/// Opens `rel_path` below the task worktree `root` in the OS default application (local daemon only).
#[tauri::command]
pub async fn open_path_default(
    app: tauri::AppHandle,
    state: tauri::State<'_, DesktopState>,
    root: String,
    rel_path: String,
) -> Result<(), String> {
    let registry = state.proxy.registry.clone();
    let canon = tauri::async_runtime::spawn_blocking(move || {
        let kind = registry.lock().unwrap().current().kind;
        resolve_openable(kind, std::env::consts::OS, &root, &rel_path, &RealOpenProbe)
    })
    .await
    .map_err(|_| Refusal::NoApp.message().to_string())?
    .map_err(|r| r.message().to_string())?;

    use tauri_plugin_opener::OpenerExt;
    app.opener()
        .open_path(canon, None::<&str>)
        .map_err(|_| Refusal::NoApp.message().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    /// A fake filesystem: canonical results per input path, and the set of regular files.
    #[derive(Default)]
    struct Fake {
        canon: HashMap<String, String>,
        files: Vec<String>,
    }

    impl Fake {
        /// A path that exists, canonicalises to itself and is a regular file.
        fn file(mut self, p: &str) -> Self {
            self.canon.insert(p.to_string(), p.to_string());
            self.files.push(p.to_string());
            self
        }
        /// A directory (canonical to itself, not a regular file).
        fn dir(mut self, p: &str) -> Self {
            self.canon.insert(p.to_string(), p.to_string());
            self
        }
        /// `from` resolves (through a link or `..`) to `to`.
        fn link(mut self, from: &str, to: &str) -> Self {
            self.canon.insert(from.to_string(), to.to_string());
            self
        }
    }

    impl OpenProbe for Fake {
        fn canonicalize(&self, path: &str) -> Result<String, std::io::ErrorKind> {
            self.canon.get(path).cloned().ok_or(std::io::ErrorKind::NotFound)
        }
        fn is_regular_file(&self, path: &str) -> bool {
            self.files.iter().any(|f| f == path)
        }
    }

    const ROOT: &str = "/home/u/wt";

    fn unix() -> Fake {
        Fake::default().dir(ROOT)
    }

    fn open(fake: &Fake, rel: &str) -> Result<String, Refusal> {
        resolve_openable(ConnectionKind::Local, "linux", ROOT, rel, fake)
    }

    #[test]
    fn opens_an_allow_listed_regular_file_inside_the_root() {
        for ext in ALLOWED_EXTENSIONS {
            let p = format!("{ROOT}/out/report.{ext}");
            let fake = unix().file(&p);
            assert_eq!(open(&fake, &format!("out/report.{ext}")), Ok(p.clone()), "{ext}");
        }
        // case-insensitive extension
        let fake = unix().file(&format!("{ROOT}/Q3.PDF"));
        assert_eq!(open(&fake, "Q3.PDF"), Ok(format!("{ROOT}/Q3.PDF")));
    }

    #[test]
    fn open_path_default_refuses_outside_root() {
        // a ".." that resolves out of the root, an absolute-looking rel, a sibling that merely shares the prefix
        let fake = unix()
            .link(&format!("{ROOT}/../secret.pdf"), "/home/u/secret.pdf")
            .file("/home/u/secret.pdf")
            .link(&format!("{ROOT}/x/../../y.pdf"), "/home/u/y.pdf")
            .file("/home/u/y.pdf")
            .link(&format!("{ROOT}/sib.pdf"), "/home/u/wt-evil/sib.pdf")
            .file("/home/u/wt-evil/sib.pdf");
        assert_eq!(open(&fake, "../secret.pdf"), Err(Refusal::Outside));
        assert_eq!(open(&fake, "x/../../y.pdf"), Err(Refusal::Outside));
        assert_eq!(open(&fake, "sib.pdf"), Err(Refusal::Outside), "a sibling sharing the root's prefix is outside");
        for rel in ["/etc/passwd.txt", "/x.pdf", "/home/u/secret.pdf"] {
            assert_eq!(open(&fake, rel), Err(Refusal::BadPath), "{rel}");
        }
        // the root itself is not a file inside it
        assert_eq!(open(&fake, "."), Err(Refusal::Missing));
    }

    #[test]
    fn open_path_default_symlink_escape() {
        // inside-looking name, canonical target elsewhere
        let fake = unix().link(&format!("{ROOT}/evil.pdf"), "/etc/shadow.pdf").file("/etc/shadow.pdf");
        assert_eq!(open(&fake, "evil.pdf"), Err(Refusal::Outside));
        // a link that stayed inside is fine
        let fake = unix().link(&format!("{ROOT}/alias.pdf"), &format!("{ROOT}/real.pdf")).file(&format!("{ROOT}/real.pdf"));
        assert_eq!(open(&fake, "alias.pdf"), Ok(format!("{ROOT}/real.pdf")));
        // a root that is itself reached through a link compares canonical to canonical
        let fake = Fake::default()
            .link("/tmp/wt-link", "/data/wt")
            .dir("/data/wt")
            .link("/tmp/wt-link/a.pdf", "/data/wt/a.pdf")
            .file("/data/wt/a.pdf");
        assert_eq!(
            resolve_openable(ConnectionKind::Local, "linux", "/tmp/wt-link", "a.pdf", &fake),
            Ok("/data/wt/a.pdf".to_string())
        );
    }

    #[test]
    fn open_path_default_directory_and_app_bundle() {
        let fake = unix().dir(&format!("{ROOT}/sub")).dir(&format!("{ROOT}/Tool.app")).dir(&format!("{ROOT}/folder.pdf"));
        assert_eq!(open(&fake, "sub"), Err(Refusal::NotAFile));
        assert_eq!(open(&fake, "Tool.app"), Err(Refusal::NotAFile));
        assert_eq!(open(&fake, "folder.pdf"), Err(Refusal::NotAFile), "a directory named like an allowed file");
    }

    #[test]
    fn open_path_default_extension_not_allow_listed() {
        let refused = [
            "exe", "app", "command", "bat", "cmd", "ps1", "sh", "msi", "dmg", "jar", "lnk", "url", "desktop", "scr",
            "html", "svg", "htm", "js", "py", "pif", "com", "vbs", "reg", "dll", "so", "docm", "xlsm", "zip", "",
        ];
        for ext in refused {
            let name = if ext.is_empty() { "report".to_string() } else { format!("report.{ext}") };
            let fake = unix().file(&format!("{ROOT}/{name}"));
            assert_eq!(open(&fake, &name), Err(Refusal::NotAllowed), "{name}");
            let upper = format!("{ROOT}/{}", name.to_uppercase());
            let fake = unix().file(&upper);
            assert_eq!(open(&fake, &name.to_uppercase()), Err(Refusal::NotAllowed), "{name} upper");
        }
        // a double extension is judged by its last one, and a bare dotfile has none
        let fake = unix().file(&format!("{ROOT}/a.pdf.exe")).file(&format!("{ROOT}/.pdf")).file(&format!("{ROOT}/a.exe.pdf"));
        assert_eq!(open(&fake, "a.pdf.exe"), Err(Refusal::NotAllowed));
        assert_eq!(open(&fake, ".pdf"), Err(Refusal::NotAllowed));
        assert_eq!(open(&fake, "a.exe.pdf"), Ok(format!("{ROOT}/a.exe.pdf")));
        // the extension is taken from the canonical target, not the name the agent chose
        let fake = unix().link(&format!("{ROOT}/pretty.pdf"), &format!("{ROOT}/run.sh")).file(&format!("{ROOT}/run.sh"));
        assert_eq!(open(&fake, "pretty.pdf"), Err(Refusal::NotAllowed));
    }

    #[test]
    fn open_path_default_unc_on_windows() {
        let win = |root: &str, rel: &str, fake: &Fake| resolve_openable(ConnectionKind::Local, "windows", root, rel, fake);
        let fake = Fake::default();
        // a UNC root, a UNC rel, forward-slash UNC, device and verbatim-UNC spellings: refused before any probe
        for (root, rel) in [
            (r"\\server\share\wt", "a.pdf"),
            ("//server/share/wt", "a.pdf"),
            (r"C:\wt", r"\\server\share\a.pdf"),
            (r"C:\wt", "//server/share/a.pdf"),
            (r"\\?\UNC\server\share\wt", "a.pdf"),
            (r"C:\wt", r"\\?\UNC\server\share\a.pdf"),
            (r"\\.\pipe\x", "a.pdf"),
        ] {
            assert_eq!(win(root, rel, &fake), Err(Refusal::Network), "{root} {rel}");
        }
        // a junction that canonicalises onto a share is refused after resolution
        let fake = Fake::default()
            .dir(r"C:\wt")
            .link(r"C:\wt\j\a.pdf", r"\\server\share\a.pdf")
            .file(r"\\server\share\a.pdf");
        assert_eq!(win(r"C:\wt", r"j\a.pdf", &fake), Err(Refusal::Network));
        // the local verbatim form is not a share, and a drive-relative or rooted rel is refused
        let fake = Fake::default().dir(r"C:\wt").file(r"C:\wt\out\A.PDF");
        assert_eq!(win(r"C:\wt", r"out\A.PDF", &fake), Ok(r"C:\wt\out\A.PDF".to_string()));
        assert!(!is_unc_or_device(r"\\?\C:\wt\a.pdf"));
        assert!(is_unc_or_device(r"\\?\unc\s\x"));
        for rel in [r"C:a.pdf", r"C:\a.pdf", r"\a.pdf", "/a.pdf"] {
            assert_eq!(win(r"C:\wt", rel, &fake), Err(Refusal::BadPath), "{rel}");
        }
        // Windows paths compare case-insensitively and with either separator
        let fake = Fake::default().dir(r"C:\Work\WT").link(r"C:\Work\WT\a.pdf", r"c:\work\wt\A.pdf").file(r"c:\work\wt\A.pdf");
        assert_eq!(win(r"C:\Work\WT", "a.pdf", &fake), Ok(r"c:\work\wt\A.pdf".to_string()));
    }

    #[test]
    fn open_path_default_remote_daemon() {
        let fake = unix().file(&format!("{ROOT}/a.pdf"));
        for kind in [ConnectionKind::Url, ConnectionKind::Relay, ConnectionKind::Ssh] {
            assert_eq!(resolve_openable(kind, "linux", ROOT, "a.pdf", &fake), Err(Refusal::Remote), "{kind:?}");
        }
        assert!(resolve_openable(ConnectionKind::Local, "linux", ROOT, "a.pdf", &fake).is_ok());
    }

    #[test]
    fn open_path_default_bad_input() {
        let fake = unix().file(&format!("{ROOT}/a.pdf"));
        assert_eq!(open(&fake, "a\0.pdf"), Err(Refusal::BadPath));
        assert_eq!(open(&fake, ""), Err(Refusal::BadPath));
        assert_eq!(resolve_openable(ConnectionKind::Local, "linux", "", "a.pdf", &fake), Err(Refusal::BadPath));
        assert_eq!(resolve_openable(ConnectionKind::Local, "linux", "/home/u/a\0b", "a.pdf", &fake), Err(Refusal::BadPath));
        assert_eq!(open(&fake, "gone.pdf"), Err(Refusal::Missing));
        // a root that does not resolve
        assert_eq!(resolve_openable(ConnectionKind::Local, "linux", "/nope", "a.pdf", &fake), Err(Refusal::BadPath));
    }

    #[test]
    fn open_path_default_error_text_has_no_path() {
        let all = [
            Refusal::Remote, Refusal::BadPath, Refusal::Network, Refusal::Outside, Refusal::NotAFile, Refusal::NotAllowed,
            Refusal::Missing, Refusal::NoApp,
        ];
        for r in all {
            let m = r.message();
            assert!(!m.contains('/') && !m.contains('\\') && !m.contains(':'), "{m}");
            assert!(m.len() < 80, "{m}");
        }
        assert_eq!(Refusal::NoApp.message(), "No application is set to open this file type");
    }

    #[test]
    fn containment_helpers() {
        assert!(inside("/a/b", "/a/b/c.pdf", false));
        assert!(!inside("/a/b", "/a/b", false));
        assert!(!inside("/a/b", "/a/bc/c.pdf", false));
        assert!(!inside("/a/b", "/a/b/../c.pdf", false));
        assert!(!inside("", "/a.pdf", false));
        assert!(inside(r"C:\A\B", r"c:/a/b/c.pdf", true));
        assert!(!inside("/A/B", "/a/b/c.pdf", false), "unix paths are case-sensitive");
    }

    /// The real probe on a real directory: a file opens, a `..` escape, a directory and a bad extension do not.
    #[test]
    fn real_filesystem_rows() {
        let tmp = tempfile::tempdir().unwrap();
        let root = tmp.path().join("wt");
        std::fs::create_dir_all(root.join("out")).unwrap();
        std::fs::write(root.join("out").join("a.PDF"), b"%PDF-1.4").unwrap();
        std::fs::write(root.join("run.exe"), b"MZ").unwrap();
        std::fs::create_dir(root.join("dir.pdf")).unwrap();
        std::fs::write(tmp.path().join("secret.pdf"), b"%PDF-1.4").unwrap();
        let root_s = root.to_str().unwrap();
        let go = |rel: &str| resolve_openable(ConnectionKind::Local, std::env::consts::OS, root_s, rel, &RealOpenProbe);
        let sep = std::path::MAIN_SEPARATOR;
        assert!(go(&format!("out{sep}a.PDF")).is_ok());
        assert_eq!(go(&format!("..{sep}secret.pdf")), Err(Refusal::Outside));
        assert_eq!(go("dir.pdf"), Err(Refusal::NotAFile));
        assert_eq!(go("run.exe"), Err(Refusal::NotAllowed));
        assert_eq!(go("missing.pdf"), Err(Refusal::Missing));
    }

    /// A real symlink out of the root (skipped where the OS refuses to create one without privilege).
    #[test]
    fn real_symlink_escape() {
        let tmp = tempfile::tempdir().unwrap();
        let root = tmp.path().join("wt");
        std::fs::create_dir(&root).unwrap();
        let outside = tmp.path().join("outside.pdf");
        std::fs::write(&outside, b"%PDF-1.4").unwrap();
        let link = root.join("evil.pdf");
        #[cfg(unix)]
        let made = std::os::unix::fs::symlink(&outside, &link).is_ok();
        #[cfg(windows)]
        let made = std::os::windows::fs::symlink_file(&outside, &link).is_ok();
        if !made {
            eprintln!("skipped: cannot create a symlink here");
            return;
        }
        let r = resolve_openable(ConnectionKind::Local, std::env::consts::OS, root.to_str().unwrap(), "evil.pdf", &RealOpenProbe);
        assert_eq!(r, Err(Refusal::Outside));
    }
}
