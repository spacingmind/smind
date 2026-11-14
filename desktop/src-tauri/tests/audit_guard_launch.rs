//! The built audit app (cargo feature `audit-guard`) exits with status 2 and a message, before it
//! creates a window or any file, when it is not pointed at a scratch daemon. Only the refusals are
//! launched here: a passing environment would start the app. Compiled only with the feature:
//!
//!   cargo test --features audit-guard --test audit_guard_launch
#![cfg(feature = "audit-guard")]

use std::process::Command;

const BIN: &str = env!("CARGO_BIN_EXE_smind-desktop");

/// launch runs the app with an empty environment plus `vars` and a scratch HOME, returning
/// (exit code, stderr, whether the scratch HOME is still empty).
fn launch(vars: &[(&str, &str)]) -> (Option<i32>, String, bool) {
    let home = tempfile::tempdir().unwrap();
    let out = Command::new(BIN)
        .env_clear()
        .env("HOME", home.path())
        .env("PATH", "/usr/bin:/bin")
        .envs(vars.iter().copied())
        .output()
        .expect("the app binary starts");
    let empty = std::fs::read_dir(home.path()).unwrap().next().is_none();
    (out.status.code(), String::from_utf8_lossy(&out.stderr).into_owned(), empty)
}

fn refused(vars: &[(&str, &str)], names: &str) {
    let (code, stderr, empty) = launch(vars);
    assert_eq!(code, Some(2), "{vars:?} must exit 2, stderr: {stderr}");
    assert!(stderr.contains("smind audit build:"), "{vars:?}: no audit message: {stderr}");
    assert!(stderr.contains(names), "{vars:?}: message must name {names}: {stderr}");
    assert!(empty, "{vars:?}: the app created files before refusing");
}

#[test]
fn refuses_with_no_environment() {
    refused(&[], "SMIND_HOME");
}

#[test]
fn refuses_without_a_scratch_home() {
    refused(&[("SMIND_DAEMON_URL", "http://127.0.0.1:4719")], "SMIND_HOME");
}

#[test]
fn refuses_the_real_smind_home_even_with_a_scratch_home_env() {
    // HOME is a scratch directory (launch sets it), so only the user database can name the real one;
    // the test process's own HOME is the real home on a developer machine and CI runner alike.
    let real = std::env::var("HOME").expect("HOME is set for the test process");
    let smind = format!("{real}/.smind");
    refused(&[("SMIND_HOME", smind.as_str()), ("SMIND_DAEMON_URL", "http://127.0.0.1:4719")], "SMIND_HOME");
}

#[test]
fn refuses_without_a_daemon_url() {
    refused(&[("SMIND_HOME", "/tmp/smind-audit-guard-test-home")], "SMIND_DAEMON_URL");
}

#[test]
fn refuses_the_real_daemon_port() {
    refused(&[("SMIND_HOME", "/tmp/smind-audit-guard-test-home"), ("SMIND_DAEMON_URL", "http://127.0.0.1:4648")], "SMIND_DAEMON_URL");
}
