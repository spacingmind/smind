//! The shell's crash-capture wiring (docs/plans/active/crash-capture-desktop.md, AC5).

/// crash-shell-installs-first-in-run: `run()` installs the crash hook before anything else
/// (only the audit-build guard, which may exit, may come first).
#[test]
fn crash_shell_installs_first_in_run() {
    let src = include_str!("lib.rs");
    let body = src
        .split("pub fn run() {")
        .nth(1)
        .expect("lib.rs has pub fn run()");
    let first = body
        .lines()
        .map(str::trim)
        .filter(|l| !l.is_empty() && !l.starts_with("//"))
        .find(|l| *l != "#[cfg(feature = \"audit-guard\")]" && *l != "audit_guard::enforce_or_exit();")
        .expect("run() has a first statement");
    assert!(
        first.starts_with("smind_crash_report::install_default("),
        "the first statement of run() must install the crash hook, found {first:?}"
    );
}

/// crash-shell-identifier-matches-tauri-conf: the crash crate derives the directory from the
/// same identifier Tauri uses for `app_data_dir`.
#[test]
fn crash_shell_identifier_matches_tauri_conf() {
    let conf: serde_json::Value =
        serde_json::from_str(include_str!("../tauri.conf.json")).expect("tauri.conf.json parses");
    assert_eq!(
        conf["identifier"].as_str(),
        Some(smind_crash_report::report::IDENTIFIER),
        "the crash directory would not be the app data directory doctor looks in"
    );
}
