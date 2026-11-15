//! desktop-native-feel D4.5: the loopback-proxy capability is an exact,
//! hand-reviewed allowlist (ADR-0013 section 2) -- no wildcard, no
//! default permission sets, no origin but the loopback proxy. A new
//! command or window grant must be added here on purpose.

use serde_json::Value;

const PROXY_CAPABILITY: &str = include_str!("../capabilities/proxy.json");
const TASK_CAPABILITY: &str = include_str!("../capabilities/task-window.json");
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
        "allow-connections-preview-relay",
        "allow-connections-relay-set-password",
        "allow-connections-probe",
        "allow-connections-add-ssh",
        "allow-app-settings-get",
        "allow-app-settings-set",
        "allow-connections-relay-status",
        "allow-connections-unsupported",
        "allow-connections-remove",
        "allow-connections-select",
        "allow-connections-get-current",
        "allow-open-external",
        "allow-editors-list",
        "allow-open-in-editor",
        "allow-open-path-default",
        "allow-daemon-status",
        "allow-daemon-install",
        "allow-daemon-update",
        "allow-daemon-restart",
        "allow-take-over-daemon",
        "allow-connection-version",
        "allow-window-ready",
        "allow-window-set-theme",
        "allow-app-update-check",
        "allow-app-update-install",
        "allow-app-quit",
        "allow-app-menu-action",
        "allow-context-menu-show",
        "allow-window-open-task",
        "allow-notify-test",
        "allow-attention-focus-report",
        "core:window:allow-minimize",
        "core:window:allow-toggle-maximize",
        "core:window:allow-internal-toggle-maximize",
        "core:window:allow-close",
        "core:window:allow-start-dragging",
        "core:window:allow-is-maximized",
        "core:window:allow-is-fullscreen",
        "core:window:allow-theme",
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

/// `app_quit` is reachable from the bundled UI (renderer Ctrl+Q on
/// Windows/Linux) and is registered with the invoke handler.
#[test]
fn app_quit_is_allowlisted_and_registered() {
    let cap: Value = serde_json::from_str(PROXY_CAPABILITY).expect("proxy.json parses");
    let perms: Vec<&str> = cap["permissions"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(Value::as_str)
        .collect();
    assert!(perms.contains(&"allow-app-quit"));
    assert!(perms.contains(&"allow-app-menu-action"));
    assert!(BUILD_RS.contains("\"app_menu_action\""));
    assert!(include_str!("lib.rs").contains("menu::app_menu_action"));
    assert!(BUILD_RS.contains("\"app_quit\""));
    assert!(include_str!("lib.rs").contains("commands::app_quit"));
}

/// `connections_preview_relay` (ADR-0031 P3) is allowlisted, declared in
/// build.rs and registered with the invoke handler.
#[test]
fn proxy_capability_lists_preview_relay() {
    let cap: Value = serde_json::from_str(PROXY_CAPABILITY).expect("proxy.json parses");
    let perms: Vec<&str> = cap["permissions"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(Value::as_str)
        .collect();
    assert!(perms.contains(&"allow-connections-preview-relay"));
    assert!(BUILD_RS.contains("\"connections_preview_relay\""));
    assert!(include_str!("lib.rs").contains("commands::connections_preview_relay"));
}

/// `connections_relay_set_password` (ADR-0031 P13c) is allowlisted, declared in
/// build.rs and registered; it takes the daemon password in and returns nothing.
#[test]
fn relay_set_password_is_allowlisted_and_registered() {
    let cap: Value = serde_json::from_str(PROXY_CAPABILITY).expect("proxy.json parses");
    let perms: Vec<&str> = cap["permissions"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(Value::as_str)
        .collect();
    assert!(perms.contains(&"allow-connections-relay-set-password"));
    assert!(BUILD_RS.contains("\"connections_relay_set_password\""));
    assert!(include_str!("lib.rs").contains("commands::connections_relay_set_password"));
}

/// `connections_relay_status` (ADR-0031 P13a) is allowlisted, declared in
/// build.rs and registered with the invoke handler; it returns the connection
/// state, never key material.
#[test]
fn relay_status_is_allowlisted_and_registered() {
    let cap: Value = serde_json::from_str(PROXY_CAPABILITY).expect("proxy.json parses");
    let perms: Vec<&str> = cap["permissions"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(Value::as_str)
        .collect();
    assert!(perms.contains(&"allow-connections-relay-status"));
    assert!(BUILD_RS.contains("\"connections_relay_status\""));
    assert!(include_str!("lib.rs").contains("commands::connections_relay_status"));
}

fn perms_of(cap: &str) -> Vec<String> {
    let cap: Value = serde_json::from_str(cap).expect("capability parses");
    cap["permissions"]
        .as_array()
        .expect("permissions array")
        .iter()
        .map(|p| p.as_str().expect("permission is a string").to_string())
        .collect()
}

/// ADR-0013 amendment (2026-11-15): the main window's capability stays scoped to `main` alone.
#[test]
fn capability_proxy_stays_main_only() {
    let cap: Value = serde_json::from_str(PROXY_CAPABILITY).expect("proxy.json parses");
    assert_eq!(cap["windows"], serde_json::json!(["main"]));
    let default: Value = serde_json::from_str(include_str!("../capabilities/default.json"))
        .expect("default.json parses");
    assert_eq!(default["windows"], serde_json::json!(["main"]));
}

/// The task-window capability is scoped to exactly the `task-*` pool pattern (the only glob in any capability),
/// to the loopback proxy origin, never local.
#[test]
fn capability_task_window_scope_is_exactly_the_task_glob() {
    let cap: Value = serde_json::from_str(TASK_CAPABILITY).expect("task-window.json parses");
    assert_eq!(cap["windows"], serde_json::json!(["task-*"]));
    assert_eq!(cap["local"], Value::Bool(false));
    assert_eq!(
        cap["remote"]["urls"],
        serde_json::json!(["http://127.0.0.1:*"])
    );
    // Evaluate the pattern from the file against the labels: every pool label matches, `main` and look-alikes do not.
    let pattern = cap["windows"][0].as_str().unwrap();
    let prefix = pattern.strip_suffix('*').expect("a trailing-star glob");
    assert!(!prefix.contains('*') && prefix == crate::windows::TASK_LABEL_PREFIX);
    for i in 0..crate::windows::MAX_TASK_WINDOWS {
        assert!(crate::windows::pool_label(i).starts_with(prefix));
    }
    for other in ["main", "tauri-1", "xtask-1", "Task-1"] {
        assert!(
            !other.starts_with(prefix),
            "{other} must not match the task-window glob"
        );
    }
}

/// The task-window permission list is exact and hand-reviewed, like the main window's.
#[test]
fn capability_task_window_allowlist_exact() {
    let mut got = perms_of(TASK_CAPABILITY);
    got.sort();
    let mut want: Vec<String> = [
        "allow-open-external",
        "allow-editors-list",
        "allow-open-in-editor",
        "allow-open-path-default",
        "allow-connections-get-current",
        "allow-window-ready",
        "allow-window-set-theme",
        "allow-window-open-task",
        "allow-context-menu-show",
        "core:window:allow-minimize",
        "core:window:allow-toggle-maximize",
        "core:window:allow-internal-toggle-maximize",
        "core:window:allow-close",
        "core:window:allow-start-dragging",
        "core:window:allow-is-maximized",
        "core:window:allow-is-fullscreen",
        "core:window:allow-theme",
        "core:event:allow-listen",
        "core:event:allow-unlisten",
    ]
    .iter()
    .map(|s| s.to_string())
    .collect();
    want.sort();
    assert_eq!(
        got, want,
        "task-window.json permissions changed: update this test deliberately"
    );
    for p in &got {
        assert!(!p.contains('*'), "wildcard permission {p}");
        assert!(!p.ends_with(":default"), "default permission set {p}");
    }
}

/// No command that changes connections or settings, manages the daemon or the app, or quits may be in the task list.
#[test]
fn capability_task_window_has_no_main_only_command() {
    let got = perms_of(TASK_CAPABILITY);
    for forbidden in [
        "allow-connections-list",
        "allow-connections-add",
        "allow-connections-add-relay",
        "allow-connections-preview-relay",
        "allow-connections-probe",
        "allow-connections-add-ssh",
        "allow-connections-relay-status",
        "allow-connections-unsupported",
        "allow-connections-relay-set-password",
        "allow-connections-remove",
        "allow-connections-select",
        "allow-app-settings-get",
        "allow-app-settings-set",
        "allow-daemon-status",
        "allow-daemon-install",
        "allow-daemon-update",
        "allow-daemon-restart",
        "allow-take-over-daemon",
        "allow-connection-version",
        "allow-app-update-check",
        "allow-app-update-install",
        "allow-app-quit",
        "allow-app-menu-action",
        "allow-notify-test",
    ] {
        assert!(
            !got.iter().any(|p| p == forbidden),
            "{forbidden} must stay main-only"
        );
    }
}

/// Every app-command grant in the task capability names a command build.rs declares.
#[test]
fn capability_task_window_grants_match_build_rs_commands() {
    for p in perms_of(TASK_CAPABILITY) {
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

/// `window_open_task` is declared in build.rs and registered with the invoke handler.
#[test]
fn window_open_task_is_declared_and_registered() {
    assert!(BUILD_RS.contains("\"window_open_task\""));
    assert!(include_str!("lib.rs").contains("windows::window_open_task"));
}
