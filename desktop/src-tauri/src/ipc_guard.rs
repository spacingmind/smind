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
pub const PROXY_TEMPLATE: &str = include_str!("../capability-templates/proxy.json");
/// The task-window capability template: the smaller allowlist of a task window.
pub const TASK_TEMPLATE: &str = include_str!("../capability-templates/task-window.json");
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

/// verdict is the whole command-level rule as a pure function: the command name, the managed proxy origin (if any) and the
/// calling webview's current URL (or the failure to read it). Anything but an exempt command from the proxy origin refuses.
fn verdict<E>(
    command: &str,
    proxy: Option<ProxyOrigin>,
    url: Result<Url, E>,
) -> Result<(), String> {
    if EXEMPT.iter().any(|(name, _)| *name == command) {
        return Ok(());
    }
    // Fail closed: no managed origin, an unreadable URL or a different origin all refuse.
    let proxy = proxy.ok_or_else(|| REFUSED.to_string())?;
    let url = url.map_err(|_| REFUSED.to_string())?;
    if proxy.matches(&url) {
        Ok(())
    } else {
        Err(REFUSED.to_string())
    }
}

/// check reads the invocation's webview at call time and applies [`verdict`].
fn check<R: Runtime>(invoke: &Invoke<R>) -> Result<(), String> {
    let webview = invoke.message.webview_ref();
    let proxy = webview
        .try_state::<ProxyOrigin>()
        .map(|state| *state.inner());
    verdict(invoke.message.command(), proxy, webview.url())
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
            // the command name only: the URL can carry the desktop key
            log::warn!("ipc refused: {}", invoke.message.command());
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
/// known and before the first window is built. A capability that cannot be added (an `Err`) is logged and that window
/// class gets no IPC: nothing sensitive becomes reachable. An unknown permission in a template makes `add_capability`
/// panic while it holds the authority lock, which poisons it; that is not caught here on purpose (a caught panic would
/// leave every later IPC call panicking). The templates are pinned against the real ACL by the tests below instead.
///
/// Exactly once per app: `add_capability` only ever ADDS (it never replaces), so a second call with another port would
/// leave the first origin valid in the ACL. A second call is a programming error and panics (fails loud at launch).
pub fn install<R: Runtime, M: Manager<R>>(app: &M, port: u16) {
    let proxy = ProxyOrigin::new(port);
    assert!(
        app.manage(proxy),
        "ipc_guard::install called twice: capabilities accumulate, the first origin would stay valid"
    );
    for json in proxy_capabilities(&proxy) {
        if let Err(err) = app.add_capability(json) {
            log::error!("could not add the proxy-origin capability, the window gets no IPC: {err}");
        }
    }
}

// The origin table and the reason format are shared with the integration test (tests/ipc_guard.rs).
#[cfg(test)]
#[path = "../tests/common/mod.rs"]
mod common;

#[cfg(test)]
mod tests {
    use super::*;

    use super::common::{reason_ok, table};

    const PORT: u16 = 47123;

    fn proxy() -> ProxyOrigin {
        ProxyOrigin::new(PORT)
    }

    fn url(s: &str) -> Url {
        s.parse().unwrap()
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
            let _parsed: tauri::utils::acl::capability::CapabilityFile =
                json.parse().expect("parses as a capability");
        }
    }

    #[test]
    fn verdict_fails_closed_on_every_missing_piece() {
        let ok: Result<Url, ()> = Ok(url("http://127.0.0.1:47123/"));
        assert_eq!(verdict("open_external", Some(proxy()), ok.clone()), Ok(()));
        assert_eq!(
            verdict("open_external", None, ok),
            Err(REFUSED.to_string()),
            "no managed origin"
        );
        let unreadable: Result<Url, ()> = Err(());
        assert_eq!(
            verdict("open_external", Some(proxy()), unreadable),
            Err(REFUSED.to_string()),
            "unreadable URL"
        );
        let other: Result<Url, ()> = Ok(url("http://127.0.0.1:47124/"));
        assert_eq!(
            verdict("open_external", Some(proxy()), other),
            Err(REFUSED.to_string()),
            "other port"
        );
    }

    #[test]
    fn reason_format_rejects_placeholders() {
        for bad in [
            "",
            "   ",
            "TODO",
            "todo: later",
            "tbd",
            "n/a",
            "because",
            "needed for x",
        ] {
            assert!(!reason_ok(bad), "{bad:?} must not pass as a reason");
        }
        assert!(reason_ok(
            "reads no window state and is called before any webview exists"
        ));
    }
}
