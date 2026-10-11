//! desktop-native-feel D4.5: the loopback-proxy capability is an exact,
//! hand-reviewed allowlist (ADR-0013 section 2) -- no wildcard, no
//! default permission sets, no origin but the loopback proxy. A new
//! command or window grant must be added here on purpose.

use serde_json::Value;

const PROXY_CAPABILITY: &str = include_str!("../capabilities/proxy.json");
const BUILD_RS: &str = include_str!("../build.rs");

#[test]
fn capability_allowlist_exact() {
    let cap: Value = serde_json::from_str(PROXY_CAPABILITY).expect("proxy.json parses");
    let mut got: Vec<&str> = cap["permissions"]
        .as_array()
        .expect("permissions array")
        .iter()
        .map(|p| p.as_str().expect("permission is a string"))
        .collect();
    got.sort_unstable();
    let mut want = vec![
        "allow-connections-list",
        "allow-connections-add",
        "allow-connections-add-relay",
        "allow-connections-remove",
        "allow-connections-select",
        "allow-connections-get-current",
        "allow-open-external",
        "allow-editors-list",
        "allow-open-in-editor",
        "allow-daemon-status",
        "allow-daemon-install",
        "allow-daemon-update",
        "allow-daemon-restart",
        "allow-take-over-daemon",
        "allow-connection-version",
        "allow-window-ready",
        "allow-window-set-theme",
        "core:window:allow-minimize",
        "core:window:allow-toggle-maximize",
        "core:window:allow-internal-toggle-maximize",
        "core:window:allow-close",
        "core:window:allow-start-dragging",
        "core:window:allow-is-maximized",
        "core:window:allow-is-fullscreen",
        "core:event:allow-listen",
        "core:event:allow-unlisten",
    ];
    want.sort_unstable();
    assert_eq!(
        got, want,
        "proxy.json permissions changed: update this test deliberately"
    );

    for p in &got {
        assert!(!p.contains('*'), "wildcard permission {p}");
        assert!(!p.ends_with(":default"), "default permission set {p}");
    }
    assert_eq!(cap["local"], Value::Bool(false));
    let urls: Vec<&str> = cap["remote"]["urls"]
        .as_array()
        .expect("remote.urls")
        .iter()
        .filter_map(Value::as_str)
        .collect();
    assert_eq!(
        urls,
        vec!["http://127.0.0.1:*"],
        "grant is scoped to the loopback proxy only"
    );
}

/// Every app-command grant (`allow-<cmd>`) must name a command build.rs
/// declares, so the allowlist can't drift from the generated permissions.
#[test]
fn capability_app_grants_match_build_rs_commands() {
    let cap: Value = serde_json::from_str(PROXY_CAPABILITY).expect("proxy.json parses");
    for p in cap["permissions"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(Value::as_str)
    {
        let Some(cmd) = p.strip_prefix("allow-") else {
            continue;
        };
        let cmd = cmd.replace('-', "_");
        assert!(
            BUILD_RS.contains(&format!("\"{cmd}\"")),
            "{p} has no `{cmd}` in build.rs"
        );
    }
}
