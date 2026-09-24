//! OS notification click -> focus + navigate to the task's route
//! (quick-wins AC3).
//!
//! `tauri-plugin-notification`'s action/click handling is mobile-only
//! (confirmed against its own docs and the upstream plugin's issue
//! tracker: no click callback ships for desktop Windows/Linux), so each
//! platform's own notification crate is used directly instead:
//! `tauri-winrt-notification::Toast::on_activated` on Windows,
//! `notify_rust`'s `wait_for_action` on Linux. Neither is invoked by the
//! remote (daemon-origin) webview, so neither needs a capability grant.

use tauri::AppHandle;
use url::Url;

use smind_daemon_client::cache::WorkspaceCache;
use smind_daemon_client::route;

use crate::MAIN_WINDOW;

/// Everything a click handler needs, cloned into whatever thread the OS
/// invokes it from.
#[derive(Clone)]
pub struct ClickContext {
    pub app: AppHandle,
    pub cache: WorkspaceCache,
    pub daemon_url: Url,
}

/// show displays an OS notification for `task_id`, wiring up a click
/// handler on the platforms that support one.
pub fn show(ctx: ClickContext, title: String, body: String, task_id: i64) {
    #[cfg(target_os = "windows")]
    windows::show(ctx, title, body, task_id);
    #[cfg(target_os = "linux")]
    linux::show(ctx, title, body, task_id);
    #[cfg(not(any(target_os = "windows", target_os = "linux")))]
    fallback::show(ctx, title, body);
}

/// on_click is the platform-independent click reaction: focus the main
/// window and, if the task's workspace id has been resolved by then,
/// navigate to its route -- if not, AC3 says to still focus the window.
/// Dispatched via `run_on_main_thread` since the OS notification
/// callback fires on its own thread, not the app's main thread.
fn on_click(ctx: &ClickContext, task_id: i64) {
    let app = ctx.app.clone();
    let cache = ctx.cache.clone();
    let daemon_url = ctx.daemon_url.clone();
    let _ = ctx.app.run_on_main_thread(move || {
        use tauri::Manager;
        let Some(win) = app.get_webview_window(MAIN_WINDOW) else {
            return;
        };
        let _ = win.show();
        let _ = win.set_focus();
        if let Some(workspace_id) = cache.get(task_id) {
            let url = route::task_route_url(&daemon_url, workspace_id, task_id, route::DEFAULT_KIND);
            if let Ok(url) = url.parse() {
                let _ = win.navigate(url);
            }
        }
    });
}

#[cfg(target_os = "windows")]
mod windows {
    use std::sync::Mutex;

    use tauri_winrt_notification::Toast;

    use super::{on_click, ClickContext};

    /// `Toast::show()`'s activation handler must outlive the call or
    /// WinRT drops it early with no unregister path, so shown toasts are
    /// simply kept alive for the process's lifetime -- acceptable for a
    /// desktop app's session-scoped notification volume.
    static KEEPALIVE: Mutex<Vec<Toast>> = Mutex::new(Vec::new());

    pub fn show(ctx: ClickContext, title: String, body: String, task_id: i64) {
        let toast = Toast::new(Toast::POWERSHELL_APP_ID)
            .title(&title)
            .text1(&body)
            .on_activated(move |_action| {
                on_click(&ctx, task_id);
                Ok(())
            });
        if toast.show().is_ok() {
            KEEPALIVE.lock().unwrap().push(toast);
        }
    }
}

#[cfg(target_os = "linux")]
mod linux {
    use notify_rust::Notification;

    use super::{on_click, ClickContext};

    /// notify-rust's own reserved action key for "closed without an
    /// action" -- not a click.
    const CLOSED: &str = "__closed";

    pub fn show(ctx: ClickContext, title: String, body: String, task_id: i64) {
        let shown = Notification::new()
            .summary(&title)
            .body(&body)
            .action("default", "Open")
            .show();
        let Ok(handle) = shown else { return };
        // wait_for_action blocks, so it needs its own thread; it also
        // fires for the freedesktop "clicked the notification body"
        // case wherever the notification server supports it (support
        // varies by desktop environment -- AC3's "as far as the
        // platform supports it").
        std::thread::spawn(move || {
            handle.wait_for_action(|action| {
                if action != CLOSED {
                    on_click(&ctx, task_id);
                }
            });
        });
    }
}

#[cfg(not(any(target_os = "windows", target_os = "linux")))]
mod fallback {
    use tauri_plugin_notification::NotificationExt;

    use super::ClickContext;

    pub fn show(ctx: ClickContext, title: String, body: String) {
        let _ = ctx.app.notification().builder().title(title).body(body).show();
    }
}
