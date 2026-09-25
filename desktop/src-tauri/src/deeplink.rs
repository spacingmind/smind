//! quick-wins AC6: `smind://task/<id>` and
//! `smind://workspace/<wsId>/task/<id>` open or focus the app and
//! navigate to that task. Only navigation is possible -- an unknown or
//! malformed link just focuses the window, per the AC's own wording.

use tauri::{AppHandle, Manager};
use url::Url;

use smind_daemon_client::route::DeepLink;
use smind_daemon_client::{client, route, Config, WorkspaceCache};

use crate::notify::{navigate_to_task, ClickContext};
use crate::MAIN_WINDOW;

/// handle processes one `smind://` URL. `cfg` is the currently-selected
/// connection (used only to resolve an unknown task's workspaceId, per
/// the `Task` arm below); `proxy_url` is the fixed loopback proxy
/// origin every navigation happens within (AC7).
pub fn handle(app: &AppHandle, cache: WorkspaceCache, cfg: Config, proxy_url: Url, raw: &str) {
    let Some(link) = route::parse_deep_link(raw) else {
        focus_only(app);
        return;
    };

    focus_only(app);

    match link {
        DeepLink::WorkspaceTask { workspace_id, task_id } => {
            cache.insert(task_id, workspace_id);
            navigate(app, &cache, &proxy_url, task_id);
        }
        DeepLink::Task { task_id } => {
            if cache.get(task_id).is_some() {
                navigate(app, &cache, &proxy_url, task_id);
                return;
            }
            // No workspaceId cached yet (nothing has resolved this task
            // this session) -- ask the daemon directly rather than
            // leaving the link at "focused, but didn't go anywhere".
            let app = app.clone();
            tauri::async_runtime::spawn(async move {
                if let Some(workspace_id) = client::resolve_workspace_id(&cfg, task_id).await {
                    cache.insert(task_id, workspace_id);
                    navigate(&app, &cache, &proxy_url, task_id);
                }
            });
        }
    }
}

fn navigate(app: &AppHandle, cache: &WorkspaceCache, proxy_url: &Url, task_id: i64) {
    let ctx = ClickContext {
        app: app.clone(),
        cache: cache.clone(),
        proxy_url: proxy_url.clone(),
    };
    navigate_to_task(&ctx, task_id);
}

fn focus_only(app: &AppHandle) {
    if let Some(win) = app.get_webview_window(MAIN_WINDOW) {
        let _ = win.show();
        let _ = win.set_focus();
    }
}
