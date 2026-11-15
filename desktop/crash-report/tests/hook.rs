//! The panic hook, end to end. The hook is process-global, so each scenario runs in the
//! `crash-child` helper process (src/bin/crash_child.rs), on a real main thread.

use std::path::{Path, PathBuf};
use std::process::{Command, Output};

fn reports(dir: &Path) -> Vec<PathBuf> {
    let mut v: Vec<PathBuf> = std::fs::read_dir(dir)
        .map(|rd| {
            rd.flatten()
                .map(|e| e.path())
                .filter(|p| p.file_name().is_some_and(|n| n.to_string_lossy().starts_with("crash-")))
                .collect()
        })
        .unwrap_or_default();
    v.sort();
    v
}

fn run_child(mode: &str, dir: &Path, extra: Option<&Path>) -> Output {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_crash-child"));
    cmd.arg(mode).arg(dir).env("RUST_BACKTRACE", "0");
    if let Some(e) = extra {
        cmd.arg(e);
    }
    cmd.output().unwrap()
}

fn parse(path: &Path) -> serde_json::Value {
    serde_json::from_slice(&std::fs::read(path).unwrap()).unwrap()
}

// crash-rs-main-thread-panic-produces-report
#[test]
fn main_thread_panic_produces_report() {
    let d = tempfile::tempdir().unwrap();
    let dir = d.path().join("crashes");
    let out = run_child("main-panic", &dir, None);
    assert!(!out.status.success(), "a main-thread panic still ends the child");
    let files = reports(&dir);
    assert_eq!(files.len(), 1, "stderr: {}", String::from_utf8_lossy(&out.stderr));
    let rep = parse(&files[0]);
    assert_eq!(rep["source"], "desktop-shell");
    assert_eq!(rep["site"], "desktop.main");
    assert_eq!(rep["messageWithheld"], true);
    assert!(!std::fs::read_to_string(&files[0]).unwrap().contains("SECRET-MAIN-TEXT"));
    // the previous (default) hook still printed the panic
    assert!(String::from_utf8_lossy(&out.stderr).contains("SECRET-MAIN-TEXT"));
}

// crash-rs-thread-panic-produces-report
#[test]
fn thread_panic_produces_report() {
    let d = tempfile::tempdir().unwrap();
    let dir = d.path().join("crashes");
    let out = run_child("thread-panic", &dir, None);
    assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
    let files = reports(&dir);
    assert_eq!(files.len(), 1);
    let rep = parse(&files[0]);
    assert_eq!(rep["site"], "desktop.thread");
    assert_eq!(rep["panicType"], "&str");
    assert!(!std::fs::read_to_string(&files[0]).unwrap().contains("SECRET-THREAD-TEXT"));
}

// crash-rs-runtime-panic-message-kept: an index-out-of-bounds message is on the allow-list.
#[test]
fn runtime_panic_message_is_kept() {
    let d = tempfile::tempdir().unwrap();
    let dir = d.path().join("crashes");
    let out = run_child("runtime-panic", &dir, None);
    assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
    let files = reports(&dir);
    assert_eq!(files.len(), 1);
    let rep = parse(&files[0]);
    assert_eq!(rep["messageWithheld"], false);
    assert!(rep["message"].as_str().unwrap().contains("index out of bounds"));
}

// crash-rs-previous-hook-still-runs
#[test]
fn previous_hook_still_runs() {
    let d = tempfile::tempdir().unwrap();
    let dir = d.path().join("crashes");
    let marker = d.path().join("marker");
    let out = run_child("previous-hook", &dir, Some(&marker));
    assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
    assert!(marker.exists(), "the hook installed before ours was not called");
    assert_eq!(reports(&dir).len(), 1);
}

// crash-rs-hook-write-failure-is-contained: a recorder that cannot write must not hurt the
// process or the panic that is already being handled.
#[test]
fn hook_write_failure_is_contained() {
    let d = tempfile::tempdir().unwrap();
    let file = d.path().join("regular-file");
    std::fs::write(&file, "x").unwrap();
    let out = run_child("unwritable", &d.path().join("unused"), Some(&file));
    assert!(out.status.success(), "{:?}\n{}", out.status, String::from_utf8_lossy(&out.stderr));
    assert!(String::from_utf8_lossy(&out.stdout).contains("CHILD-SURVIVED"));
    assert!(String::from_utf8_lossy(&out.stderr).contains("outer"), "the default hook still printed");
}

// crash-rs-hook-survives-closed-stderr: `eprintln!` panics on a stderr that returns an error
// (a closed pipe), and a panic inside a panic hook aborts the process.
#[test]
#[cfg(unix)]
fn hook_survives_closed_stderr() {
    use std::process::Stdio;
    let d = tempfile::tempdir().unwrap();
    let file = d.path().join("regular-file");
    std::fs::write(&file, "x").unwrap();
    let mut child = Command::new(env!("CARGO_BIN_EXE_crash-child"))
        .arg("unwritable-slow")
        .arg(d.path().join("unused"))
        .arg(&file)
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    drop(child.stderr.take()); // the reader goes away: writes to the child's stderr now fail
    let out = child.wait_with_output().unwrap();
    assert!(out.status.success(), "the hook aborted on a closed stderr: {:?}", out.status);
    assert!(String::from_utf8_lossy(&out.stdout).contains("CHILD-SURVIVED"));
}

// crash-rs-install-does-no-io
#[test]
fn install_does_no_io() {
    let d = tempfile::tempdir().unwrap();
    let dir = d.path().join("crashes");
    let out = run_child("no-io", &dir, None);
    assert!(out.status.success());
    assert!(String::from_utf8_lossy(&out.stdout).contains("CHILD-INSTALLED exists=false"));
    assert!(!dir.exists(), "install created the directory");
}

// crash-rs-install-default-without-a-directory-says-so: no derivable directory is an error,
// never a silent no-op (install_default prints the line).
#[test]
fn install_default_without_a_directory_is_an_error() {
    assert!(smind_crash_report::install_default_with(None, "x").is_err());
}
