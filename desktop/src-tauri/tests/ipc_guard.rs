//! The IPC origin guard against the real Tauri runtime pieces: the mock runtime, the compiled ACL manifest and the
//! source pins on `run()`. These live in an integration test, not in the lib's unit tests, on purpose: on Windows the
//! mock runtime reaches muda's `TaskDialogIndirect` import, which needs the Common Controls v6 manifest dependency, and
//! `build.rs` only gives integration-test executables that dependency (`rustc-link-arg-tests`). In the lib's unit-test
//! executable it fails to load with STATUS_ENTRYPOINT_NOT_FOUND (0xc0000139) before any test runs.
//! The pure rules (origin table, pattern, verdict, reason format) stay unit tests in `src/ipc_guard.rs`.

mod common;

use common::{reason_ok, table};
use smind_desktop_lib::app_context;
use smind_desktop_lib::ipc_guard::*;
use tauri::ipc::Invoke;
use tauri::Manager;
use url::Url;

const PORT: u16 = 47123;

fn proxy() -> ProxyOrigin {
    ProxyOrigin::new(PORT)
}

fn url(s: &str) -> Url {
    s.parse().unwrap()
}

use tauri::test::{mock_builder, mock_context, noop_assets, MockRuntime, INVOKE_KEY};
use tauri::webview::InvokeRequest;
use tauri::{WebviewUrl, WebviewWindowBuilder};

/// The command names in `generate_handler![...]`, read from lib.rs so a new command is in the table without an edit here.
fn handler_commands() -> Vec<String> {
    let lib = include_str!("../src/lib.rs");
    let start = lib
        .find("generate_handler![")
        .expect("lib.rs builds a handler")
        + "generate_handler![".len();
    let end = start + lib[start..].find("]))").expect("the handler list ends");
    lib[start..end]
        .split(',')
        .map(|item| {
            item.trim()
                .rsplit("::")
                .next()
                .unwrap_or("")
                .trim()
                .to_string()
        })
        .filter(|name| !name.is_empty())
        .collect()
}

/// A mock app whose handler is the guard around a stub that answers "ran"; `manage` decides whether the proxy origin exists.
fn mock_app(manage: bool) -> tauri::App<MockRuntime> {
    let app = mock_builder()
        .invoke_handler(guarded(|invoke: Invoke<MockRuntime>| {
            invoke.resolver.resolve("ran");
            true
        }))
        .build(mock_context(noop_assets()))
        .expect("mock app builds");
    if manage {
        app.manage(proxy());
    }
    app
}

fn call(
    webview: &tauri::WebviewWindow<MockRuntime>,
    cmd: &str,
) -> Result<String, serde_json::Value> {
    let request = InvokeRequest {
        cmd: cmd.into(),
        callback: tauri::ipc::CallbackFn(0),
        error: tauri::ipc::CallbackFn(1),
        url: if cfg!(any(windows, target_os = "android")) {
            "http://tauri.localhost"
        } else {
            "tauri://localhost"
        }
        .parse()
        .unwrap(),
        body: tauri::ipc::InvokeBody::default(),
        headers: Default::default(),
        invoke_key: INVOKE_KEY.to_string(),
    };
    tauri::test::get_ipc_response(webview, request)
        .map(|body| body.deserialize::<String>().unwrap())
}

fn window_at(
    app: &tauri::App<MockRuntime>,
    label: &str,
    at: &str,
) -> tauri::WebviewWindow<MockRuntime> {
    WebviewWindowBuilder::new(app, label, WebviewUrl::External(url(at)))
        .build()
        .expect("mock window")
}

#[test]
fn handler_list_is_read_from_lib_rs() {
    let names = handler_commands();
    assert!(names.len() >= 30, "{names:?}");
    for must in [
        "open_path_default",
        "open_in_editor",
        "open_external",
        "window_open_task",
        "notify_test",
        "app_quit",
    ] {
        assert!(
            names.iter().any(|n| n == must),
            "{must} is in the handler list: {names:?}"
        );
    }
}

#[test]
fn guard_allows_every_command_from_the_proxy_origin() {
    let app = mock_app(true);
    let win = window_at(&app, "main", "http://127.0.0.1:47123/?k=secret");
    for cmd in handler_commands() {
        assert_eq!(
            call(&win, &cmd).as_deref(),
            Ok("ran"),
            "{cmd} from the proxy origin"
        );
    }
}

#[test]
fn guard_refuses_every_command_from_another_origin() {
    let app = mock_app(true);
    for (i, (u, accepted)) in table().into_iter().enumerate() {
        if accepted
            || u.starts_with("about:")
            || u.starts_with("data:")
            || u.starts_with("javascript:")
            || u.starts_with("blob:")
            || u.starts_with("file:")
        {
            continue; // a webview cannot be built at these in the mock; the matcher table covers them
        }
        let win = window_at(&app, &format!("w{i}"), &u);
        for cmd in handler_commands() {
            if EXEMPT.iter().any(|(n, _)| *n == cmd) {
                continue;
            }
            assert_eq!(
                call(&win, &cmd),
                Err(serde_json::json!(REFUSED)),
                "{cmd} from {u}"
            );
        }
    }
}

#[test]
fn guard_fails_closed_without_the_proxy_origin_state() {
    let app = mock_app(false);
    let win = window_at(&app, "main", "http://127.0.0.1:47123/");
    for cmd in handler_commands() {
        assert_eq!(
            call(&win, &cmd),
            Err(serde_json::json!(REFUSED)),
            "{cmd} with no managed origin"
        );
    }
}

#[test]
fn guard_reads_the_origin_at_call_time() {
    // The managed origin is looked up on every call: a webview that was on the proxy origin when the guard was built
    // is refused once it is on another one, and one that was elsewhere is allowed once it navigates onto the proxy.
    let app = mock_app(true);
    let win = window_at(&app, "main", "http://127.0.0.1:47123/");
    assert_eq!(call(&win, "open_external").as_deref(), Ok("ran"));
    win.navigate(url("http://127.0.0.1:47999/"))
        .expect("navigate");
    assert_eq!(call(&win, "open_external"), Err(serde_json::json!(REFUSED)));
    win.navigate(url("http://127.0.0.1:47123/again"))
        .expect("navigate back");
    assert_eq!(call(&win, "open_external").as_deref(), Ok("ran"));
}

// ---- drift: the allowlists, the handler and build.rs agree, and exemptions are deliberate ----

fn permissions_of(template: &str) -> Vec<String> {
    let json: serde_json::Value = serde_json::from_str(template).unwrap();
    json["permissions"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(|p| p.as_str().map(str::to_string))
        .collect()
}

fn app_command_grants(template: &str) -> Vec<String> {
    permissions_of(template)
        .into_iter()
        .filter_map(|p| p.strip_prefix("allow-").map(|c| c.replace('-', "_")))
        .collect()
}

#[test]
fn every_handler_command_has_an_allowlist_entry_and_a_build_rs_entry() {
    let build_rs = include_str!("../build.rs");
    let main_grants = app_command_grants(PROXY_TEMPLATE);
    for cmd in handler_commands() {
        assert!(main_grants.contains(&cmd), "{cmd} is registered but not granted in capability-templates/proxy.json: decide whether it is reachable at all");
        assert!(
            build_rs.contains(&format!("\"{cmd}\"")),
            "{cmd} is not declared in build.rs"
        );
    }
    for grant in main_grants {
        assert!(
            handler_commands().contains(&grant),
            "{grant} is granted but not in the handler list"
        );
    }
}

#[test]
fn exempt_list_must_name_real_commands_and_every_other_command_is_guarded() {
    let names = handler_commands();
    for (name, reason) in EXEMPT {
        assert!(
            names.iter().any(|n| n == name),
            "exempt command {name} is not in the handler list"
        );
        assert!(
            reason_ok(reason),
            "exempt command {name} needs a real reason, got {reason:?}"
        );
    }
}

#[test]
#[should_panic(expected = "called twice")]
fn install_twice_is_refused() {
    let app = acl_app();
    install(&app, PORT + 1);
}

// ---- the real ACL: the compiled manifest, the real templates added the way setup adds them ----

/// An app built from the REAL context (the compiled ACL manifest and the static `capabilities/`), whose handler is a stub
/// that answers "ran": it only runs when Tauri's ACL lets the call through, so the answer is the ACL's verdict. The
/// guard wraps the stub as in `run()`; the request URL is the frame's, the webview's own URL is the proxy origin.
fn acl_app() -> tauri::App<MockRuntime> {
    let app = mock_builder()
        .invoke_handler(guarded(|invoke: Invoke<MockRuntime>| {
            invoke.resolver.resolve("ran");
            true
        }))
        .build(app_context())
        .expect("the real context builds");
    install(&app, PORT);
    app
}

fn frame_call(
    webview: &tauri::WebviewWindow<MockRuntime>,
    cmd: &str,
    frame: &str,
) -> Result<String, serde_json::Value> {
    let request = InvokeRequest {
        cmd: cmd.into(),
        callback: tauri::ipc::CallbackFn(0),
        error: tauri::ipc::CallbackFn(1),
        url: url(frame),
        body: tauri::ipc::InvokeBody::default(),
        headers: Default::default(),
        invoke_key: INVOKE_KEY.to_string(),
    };
    tauri::test::get_ipc_response(webview, request)
        .map(|body| body.deserialize::<String>().unwrap())
}

#[test]
fn install_adds_the_runtime_capabilities_against_the_real_acl_without_panicking() {
    // `install` would log and swallow a failure, so prove the success path: the proxy origin frame reaches an allowlisted command.
    let app = acl_app();
    let win = window_at(&app, "main", "http://127.0.0.1:47123/?k=s");
    assert_eq!(
        frame_call(&win, "open_path_default", "http://127.0.0.1:47123/").as_deref(),
        Ok("ran")
    );
}

#[test]
fn acl_refuses_frames_off_the_proxy_origin_for_every_allowlisted_command() {
    let app = acl_app();
    let main = window_at(&app, "main", "http://127.0.0.1:47123/?k=s");
    let task = window_at(&app, "task-1", "http://127.0.0.1:47123/?k=s");
    let main_cmds = app_command_grants(PROXY_TEMPLATE);
    let task_cmds = app_command_grants(TASK_TEMPLATE);
    assert!(main_cmds.len() >= 30 && !task_cmds.is_empty());
    for (u, accepted) in table() {
        let frame = url(&u);
        // the ACL only sees a frame URL that is a real URL with a host
        if frame.host().is_none() || u.contains('@') {
            continue;
        }
        for cmd in &main_cmds {
            let got = frame_call(&main, cmd, &u);
            assert_eq!(
                got.as_deref() == Ok("ran"),
                accepted,
                "main window, {cmd}, frame {u}: {got:?}"
            );
        }
        for cmd in &task_cmds {
            let got = frame_call(&task, cmd, &u);
            assert_eq!(
                got.as_deref() == Ok("ran"),
                accepted,
                "task window, {cmd}, frame {u}: {got:?}"
            );
        }
    }
}

#[test]
fn acl_keeps_main_only_commands_away_from_a_task_window() {
    let app = acl_app();
    let task = window_at(&app, "task-1", "http://127.0.0.1:47123/?k=s");
    let main_only: Vec<String> = app_command_grants(PROXY_TEMPLATE)
        .into_iter()
        .filter(|c| !app_command_grants(TASK_TEMPLATE).contains(c))
        .collect();
    assert!(main_only.iter().any(|c| c == "app_quit"));
    for cmd in main_only {
        assert!(
            frame_call(&task, &cmd, "http://127.0.0.1:47123/").is_err(),
            "{cmd} must not reach a task window"
        );
    }
}

// ---- run() wires both layers ----

#[test]
fn run_wraps_the_whole_handler_in_the_guard() {
    let lib = include_str!("../src/lib.rs");
    assert!(
        lib.contains(".invoke_handler(ipc_guard::guarded(tauri::generate_handler!["),
        "run() must wrap generate_handler! in ipc_guard::guarded"
    );
    assert_eq!(
        lib.matches("generate_handler![").count(),
        1,
        "one handler list, so no command can bypass the guard"
    );
    assert_eq!(
        lib.matches(".invoke_handler(").count(),
        1,
        "a later invoke_handler would replace the guarded one"
    );
}

#[test]
fn run_installs_the_origin_before_the_first_window() {
    let lib = include_str!("../src/lib.rs");
    let run = lib.find("pub fn run()").expect("run() exists");
    let body = &lib[run..];
    let install = body
        .find("ipc_guard::install(app, port)")
        .expect("setup installs the proxy origin and capabilities");
    let line_start = body[..install].rfind('\n').map_or(0, |n| n + 1);
    assert!(
        !body[line_start..install].contains("//"),
        "the install call must not be commented out"
    );
    let window = body
        .find("WebviewWindowBuilder::new(")
        .expect("setup builds the main window");
    assert!(
        install < window,
        "the origin and the exact-origin capabilities must exist before any window"
    );
}
