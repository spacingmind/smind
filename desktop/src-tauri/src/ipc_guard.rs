//! The shell's IPC boundary (docs/plans/active/desktop-ipc-origin-guard.md, ADR-0013): only the loopback proxy's own
//! origin may call the shell's commands, checked twice because Tauri shows each layer a different URL.
//!
//! * **Frame level, by the ACL.** The proxy and task-window capabilities are templates (`capability-templates/`) whose
//!   `remote.urls` names the proxy origin exactly. `setup` adds them with `add_capability` once the proxy port is known
//!   (the proxy is bound once per process, so the origin is fixed for its life). Tauri matches the URL of the *calling
//!   frame* against that pattern, so a subframe on another loopback port never reaches a command.
//! * **Command level, by one guard on the handler.** A command is not given the calling frame's URL, only the webview's
//!   top-level URL. `guarded` wraps the whole `generate_handler!` list and refuses, before any command runs, a call from a
//!   webview whose current URL is not the proxy origin (scheme, host, port, no userinfo). Every command is guarded by
//!   default; a command can only be exempted by name in [`EXEMPT`], with a reason, which a drift test forces.
//!
//! `ProxyOrigin` is read from managed state at call time, and a missing state refuses (fails closed).

use std::net::Ipv4Addr;

use tauri::ipc::Invoke;
use tauri::{Manager, Runtime};
use url::{Host, Url};

/// The proxy capability template: the main window's allowlist.
const PROXY_TEMPLATE: &str = include_str!("../capability-templates/proxy.json");
/// The task-window capability template: the smaller allowlist of a task window.
const TASK_TEMPLATE: &str = include_str!("../capability-templates/task-window.json");
/// What the templates carry where the origin goes.
pub const ORIGIN_PLACEHOLDER: &str = "__PROXY_ORIGIN__";

/// Commands the guard lets through from any origin, each with the reason. Empty: every command is sensitive (it launches
/// something, changes a window or the connection, or reads state the page must not hand to another origin), and a rule
/// that exempts a read gains nothing because the ACL already gives the same protection. Adding a name needs a reason.
pub const EXEMPT: &[(&str, &str)] = &[];

/// The loopback proxy's origin: `http://127.0.0.1:<port>`, fixed for the process.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ProxyOrigin {
    port: u16,
}

impl ProxyOrigin {
    pub fn new(port: u16) -> Self {
        Self { port }
    }

    /// The origin as the capability pattern and the logs spell it.
    pub fn origin(&self) -> String {
        format!("http://127.0.0.1:{}", self.port)
    }

    /// Whether `url` is on the proxy origin: scheme `http`, host exactly `127.0.0.1`, the proxy's port, no userinfo. The
    /// path, query and fragment do not matter. Anything else (another port, `localhost`, `127.0.0.2`, `[::1]`, `https`,
    /// `tauri:`, a URL without a host) is refused.
    pub fn matches(&self, url: &Url) -> bool {
        url.scheme() == "http"
            && url.host() == Some(Host::Ipv4(Ipv4Addr::new(127, 0, 0, 1)))
            && url.port_or_known_default() == Some(self.port)
            && url.username().is_empty()
            && url.password().is_none()
    }
}

/// The error a refused call returns; it names no URL so a page learns nothing from it.
pub const REFUSED: &str =
    "command refused: the calling window is not showing the sMind proxy origin";

/// check is the whole command-level rule for one invocation.
fn check<R: Runtime>(invoke: &Invoke<R>) -> Result<(), String> {
    let command = invoke.message.command();
    if EXEMPT.iter().any(|(name, _)| *name == command) {
        return Ok(());
    }
    let webview = invoke.message.webview_ref();
    // Fail closed: no managed origin, an unreadable URL or a different origin all refuse.
    let proxy = webview
        .try_state::<ProxyOrigin>()
        .ok_or_else(|| REFUSED.to_string())?;
    let url = webview.url().map_err(|_| REFUSED.to_string())?;
    if proxy.matches(&url) {
        Ok(())
    } else {
        Err(REFUSED.to_string())
    }
}

/// guarded wraps an invoke handler (the `generate_handler!` list) so every command passes [`check`] first. A refused call
/// is answered with [`REFUSED`] and never reaches the handler.
pub fn guarded<R, F>(inner: F) -> impl Fn(Invoke<R>) -> bool + Send + Sync + 'static
where
    R: Runtime,
    F: Fn(Invoke<R>) -> bool + Send + Sync + 'static,
{
    move |invoke: Invoke<R>| {
        if let Err(reason) = check(&invoke) {
            invoke.resolver.reject(reason);
            return true;
        }
        inner(invoke)
    }
}

/// capability_json is a template with the proxy origin written in.
pub fn capability_json(template: &str, proxy: &ProxyOrigin) -> String {
    template.replace(ORIGIN_PLACEHOLDER, &proxy.origin())
}

/// The two capabilities (main window, task windows) with the proxy origin written in.
pub fn proxy_capabilities(proxy: &ProxyOrigin) -> [String; 2] {
    [
        capability_json(PROXY_TEMPLATE, proxy),
        capability_json(TASK_TEMPLATE, proxy),
    ]
}

/// install manages the proxy origin and adds the exact-origin capabilities. It runs in `setup`, once the proxy port is
/// known and before the first window is built. If a capability cannot be added the failure is logged and the app starts
/// with no IPC for that window class: nothing sensitive becomes reachable (fails closed).
pub fn install<R: Runtime, M: Manager<R>>(app: &M, port: u16) {
    let proxy = ProxyOrigin::new(port);
    app.manage(proxy);
    for json in proxy_capabilities(&proxy) {
        // `add_capability` resolves the permissions against the compiled ACL and panics (rather than errors) on an unknown
        // one: catch it, so a bad template costs the window its IPC instead of crashing the app at launch.
        let added =
            std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| app.add_capability(json)));
        match added {
            Ok(Ok(())) => {}
            Ok(Err(err)) => log::error!("smind desktop: could not add the proxy-origin capability, the window gets no IPC: {err}"),
            Err(_) => log::error!("smind desktop: the proxy-origin capability did not resolve, the window gets no IPC"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const PORT: u16 = 47123;

    fn proxy() -> ProxyOrigin {
        ProxyOrigin::new(PORT)
    }

    fn url(s: &str) -> Url {
        s.parse().unwrap()
    }

    /// (url, accepted?): the one table for the origin rule, also used against the capability pattern below.
    fn table() -> Vec<(String, bool)> {
        let ok = |s: &str| (s.to_string(), true);
        let no = |s: &str| (s.to_string(), false);
        vec![
            ok("http://127.0.0.1:47123"),
            ok("http://127.0.0.1:47123/"),
            ok("http://127.0.0.1:47123/?k=secret#/workspace/1/task/2"),
            ok("http://127.0.0.1:47123/a/b/c.html?x=1"),
            // another port
            no("http://127.0.0.1:47124/"),
            no("http://127.0.0.1:3000/"),
            no("http://127.0.0.1:4648/"),
            no("http://127.0.0.1/"),
            // another host
            no("http://localhost:47123/"),
            no("http://127.0.0.2:47123/"),
            no("http://[::1]:47123/"),
            no("http://0.0.0.0:47123/"),
            no("http://127.0.0.1.evil.test:47123/"),
            no("http://evil.test:47123/"),
            // another scheme
            no("https://127.0.0.1:47123/"),
            no("tauri://localhost/"),
            no("http://tauri.localhost/"),
            no("file:///tmp/x.html"),
            no("ws://127.0.0.1:47123/"),
            // userinfo
            no("http://user@127.0.0.1:47123/"),
            no("http://user:pass@127.0.0.1:47123/"),
            no("http://:pass@127.0.0.1:47123/"),
            // no host, opaque
            no("about:blank"),
            no("data:text/html,hi"),
            no("javascript:alert(1)"),
            no("blob:http://127.0.0.1:47123/abc"),
        ]
    }

    #[test]
    fn origin_table() {
        for (u, want) in table() {
            assert_eq!(proxy().matches(&url(&u)), want, "{u}");
        }
        assert_eq!(proxy().origin(), "http://127.0.0.1:47123");
    }

    #[test]
    fn a_different_proxy_port_changes_the_answer() {
        let other = ProxyOrigin::new(47124);
        assert!(other.matches(&url("http://127.0.0.1:47124/")));
        assert!(!other.matches(&url("http://127.0.0.1:47123/")));
    }

    // ---- the capability templates: the ACL's frame-level check ----

    use std::str::FromStr;
    use tauri::utils::acl::RemoteUrlPattern;

    fn pattern_of(template: &str) -> RemoteUrlPattern {
        let json: serde_json::Value = serde_json::from_str(&capability_json(template, &proxy()))
            .expect("capability JSON parses");
        let urls = json["remote"]["urls"].as_array().expect("remote.urls");
        assert_eq!(urls.len(), 1, "exactly one remote URL pattern");
        RemoteUrlPattern::from_str(urls[0].as_str().unwrap()).expect("the URL pattern parses")
    }

    /// The pattern Tauri's ACL matches against the calling frame's URL, from the real templates with the origin written in:
    /// the proxy origin matches, and no other port, host, scheme or opaque URL does. (Userinfo is handled in the next test.)
    #[test]
    fn capability_template_url_pattern_is_exact() {
        for (name, template) in [("proxy", PROXY_TEMPLATE), ("task-window", TASK_TEMPLATE)] {
            let pattern = pattern_of(template);
            for (u, want) in table() {
                if u.contains('@') {
                    continue;
                }
                assert_eq!(pattern.test(&url(&u)), want, "{name} template vs {u}");
            }
        }
    }

    /// A frame at `http://user@127.0.0.1:<port>/` is the proxy's own origin (userinfo is not part of an origin), so the
    /// ACL pattern needs no stricter rule; what the pattern does with it is pinned here so a change is deliberate. The
    /// command-level guard refuses it either way (`origin_table`).
    #[test]
    fn capability_template_url_pattern_treats_userinfo_as_same_origin() {
        let pattern = pattern_of(PROXY_TEMPLATE);
        assert!(pattern.test(&url("http://user@127.0.0.1:47123/")));
        assert!(!pattern.test(&url("http://user@127.0.0.1:47124/")));
    }

    #[test]
    fn capability_json_parses_as_capabilities_with_the_allowlists_intact() {
        for template in [PROXY_TEMPLATE, TASK_TEMPLATE] {
            let json = capability_json(template, &proxy());
            assert!(
                !json.contains(ORIGIN_PLACEHOLDER),
                "the placeholder is replaced"
            );
            assert!(!json.contains("127.0.0.1:*"), "no wildcard port");
            let parsed: tauri::utils::acl::capability::CapabilityFile =
                json.parse().expect("parses as a capability");
            let _ = parsed;
        }
        assert_eq!(proxy_capabilities(&proxy()).len(), 2);
    }

    // ---- every command passes through the one guard ----

    use tauri::test::{mock_builder, mock_context, noop_assets, MockRuntime, INVOKE_KEY};
    use tauri::webview::InvokeRequest;
    use tauri::{WebviewUrl, WebviewWindowBuilder};

    /// The command names in `generate_handler![...]`, read from lib.rs so a new command is in the table without an edit here.
    fn handler_commands() -> Vec<String> {
        let lib = include_str!("lib.rs");
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
                !reason.trim().is_empty(),
                "exempt command {name} needs a reason"
            );
        }
        // the guarded set is everything else: with EXEMPT empty, that is the whole handler
        let guarded_count = names
            .iter()
            .filter(|n| !EXEMPT.iter().any(|(e, _)| e == n))
            .count();
        assert_eq!(guarded_count + EXEMPT.len(), names.len());
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
            .build(crate::app_context())
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
        let lib = include_str!("lib.rs");
        assert!(
            lib.contains(".invoke_handler(ipc_guard::guarded(tauri::generate_handler!["),
            "run() must wrap generate_handler! in ipc_guard::guarded"
        );
        assert_eq!(
            lib.matches("generate_handler![").count(),
            1,
            "one handler list, so no command can bypass the guard"
        );
    }

    #[test]
    fn run_installs_the_origin_before_the_first_window() {
        let lib = include_str!("lib.rs");
        let run = lib.find("pub fn run()").expect("run() exists");
        let body = &lib[run..];
        let install = body
            .find("ipc_guard::install(app, port)")
            .expect("setup installs the proxy origin and capabilities");
        let window = body
            .find("WebviewWindowBuilder::new(")
            .expect("setup builds the main window");
        assert!(
            install < window,
            "the origin and the exact-origin capabilities must exist before any window"
        );
    }

    #[test]
    fn exempt_list_needs_a_reason() {
        for (name, reason) in EXEMPT {
            assert!(
                !reason.trim().is_empty(),
                "exempt command {name} needs a reason"
            );
        }
    }
}
