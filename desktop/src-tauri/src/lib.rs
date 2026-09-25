//! smind desktop thin-client shell (ADR-0012): the main window just
//! displays the daemon's web UI; every native feature is driven from
//! the Rust side. A second instance only focuses the existing window.

use tauri::Manager;
use tauri::webview::WebviewWindowBuilder;
use tauri_plugin_global_shortcut::GlobalShortcutExt;

use smind_daemon_client as dclient;
use smind_daemon_client::{ClientEvent, Config};

mod deeplink;
mod menu;
mod notify;
mod tray;
mod zoom_store;

const MAIN_WINDOW: &str = "main";
const TOGGLE_SHORTCUT: &str = "CommandOrControl+Shift+S";
const HEALTHZ_POLL: std::time::Duration = std::time::Duration::from_secs(2);

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        // AC2: single instance must be registered before anything that
        // creates windows or tray icons.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(win) = app.get_webview_window(MAIN_WINDOW) {
                let _ = win.show();
                let _ = win.set_focus();
            }
        }))
        .plugin(tauri_plugin_notification::init())
        // AC5: the handler fires for every registered shortcut; there is
        // exactly one.
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, _shortcut, event| {
                    if event.state == tauri_plugin_global_shortcut::ShortcutState::Pressed {
                        toggle_main(app);
                    }
                })
                .build(),
        )
        // quick-wins AC1: size/position/maximized persist across restarts,
        // restored automatically on the main window's `ready` event
        // (fires for a window built at runtime via WebviewWindowBuilder,
        // not only ones declared in tauri.conf.json); falls back to the
        // OS's own placement when the saved monitor is gone rather than
        // forcing an off-screen position.
        .plugin(tauri_plugin_window_state::Builder::default().build())
        // quick-wins AC2 Help > Open Log Folder: writes to the platform
        // log dir (app.path().app_log_dir()) in addition to stdout.
        .plugin(tauri_plugin_log::Builder::new().build())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        // quick-wins AC6: smind://... deep links. A link that arrives
        // while the app is already running is forwarded here through
        // tauri-plugin-single-instance's deep-link feature (registered
        // above), which is why single-instance is set up first.
        .plugin(tauri_plugin_deep_link::init())
        .setup(|app| {
            let cfg = Config {
                daemon_url: dclient::config::daemon_url().unwrap_or_else(|e| {
                    eprintln!("smind desktop: {e}; using default");
                    dclient::config::daemon_url_from("").unwrap()
                }),
            };

            // AC1: start on the local fallback page, then switch to the
            // daemon UI once /healthz answers. The tauri.conf.json window
            // entry is only a fallback for config-driven creation; we
            // create the window here so startup never races the plugin
            // setup order.
            let win = WebviewWindowBuilder::new(
                app,
                MAIN_WINDOW,
                tauri::WebviewUrl::App(FALLBACK_PAGE.into()),
            )
            .title("smind")
            .inner_size(1280.0, 800.0)
            .build()?;

            // quick-wins AC2: restore the persisted zoom level.
            let _ = win.set_zoom(zoom_store::load(app.handle()));

            // quick-wins AC2: native app menu (File/Edit/View/Window/Help).
            let win_menu = menu::build(app.handle())?;
            app.set_menu(win_menu)?;
            app.on_menu_event(|app, event| menu::handle_event(app, event.id().0.as_str()));

            // quick-wins AC5: live retry state on the offline page,
            // pushed via `eval` on every failed /healthz poll (not a
            // re-navigate: the local asset origin differs by platform,
            // `tauri://localhost` vs `http://tauri.localhost`, so this
            // avoids depending on that -- see
            // `smind_daemon_client::offline`).
            let watch_cfg = cfg.clone();
            let watch_win = win.clone();
            let offline_win = win.clone();
            let offline_daemon_url = cfg.daemon_url.to_string();
            tauri::async_runtime::spawn(async move {
                watch_daemon_and_navigate(
                    &watch_cfg,
                    || {
                        let _ = watch_win.navigate(watch_cfg.daemon_url.clone());
                    },
                    move |attempt| {
                        let state = dclient::offline::OfflineState {
                            daemon_url: offline_daemon_url.clone(),
                            attempt,
                            next_retry_secs: HEALTHZ_POLL.as_secs(),
                        };
                        let js = format!(
                            "window.__smindOfflineUpdate && window.__smindOfflineUpdate({:?})",
                            state.to_query_string()
                        );
                        let _ = offline_win.eval(js);
                    },
                )
                .await;
            });

            // AC3/quick-wins AC4: tray icon with Open / Quit plus a
            // pending-approvals indicator (tooltip, first menu item,
            // taskbar badge).
            let cache = dclient::WorkspaceCache::new();
            let tray = tray::build(app.handle(), cache.clone(), cfg.daemon_url.clone())?;

            // quick-wins AC6: deep links, both at launch (get_current)
            // and while running (on_open_url, fed by single-instance's
            // deep-link feature for a second launch).
            {
                use tauri_plugin_deep_link::DeepLinkExt;

                let handle = app.handle().clone();
                let open_cache = cache.clone();
                let open_cfg = cfg.clone();
                app.deep_link().on_open_url(move |event| {
                    for url in event.urls() {
                        deeplink::handle(&handle, open_cache.clone(), open_cfg.clone(), url.as_str());
                    }
                });

                if let Err(e) = app.deep_link().register_all() {
                    eprintln!("smind desktop: deep link registration failed: {e}");
                }

                if let Ok(Some(urls)) = app.deep_link().get_current() {
                    for url in urls {
                        deeplink::handle(app.handle(), cache.clone(), cfg.clone(), url.as_str());
                    }
                }
            }

            // AC3: hide-on-close instead of destroy.
            let close_win = win.clone();
            win.on_window_event(move |event| {
                if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    let _ = close_win.hide();
                }
            });

            // AC5: register the toggle shortcut.
            app.global_shortcut().register(TOGGLE_SHORTCUT)?;

            // AC4/quick-wins AC3+AC4: daemon events -> OS notifications
            // (click -> focus + navigate) and the tray's pending-
            // approvals indicator, both fed by the same event stream.
            let notify_cfg = cfg.clone();
            let notify_cache = cache.clone();
            let handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                dclient::client::run(cfg, cache, move |event| match event {
                    ClientEvent::Notification(n) => {
                        tray.on_permission_pending(n.task_id, &n.request_id);
                        let ctx = notify::ClickContext {
                            app: handle.clone(),
                            cache: notify_cache.clone(),
                            daemon_url: notify_cfg.daemon_url.clone(),
                        };
                        notify::show(ctx, n.title, n.body, n.task_id);
                    }
                    ClientEvent::RunRunning { task_id } => tray.on_run_running(task_id),
                    ClientEvent::Reconnected => tray.on_reconnected(),
                })
                .await;
            });

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running smind desktop");
}

const FALLBACK_PAGE: &str = "offline.html";

fn toggle_main(app: &tauri::AppHandle) {
    if let Some(win) = app.get_webview_window(MAIN_WINDOW) {
        if win.is_visible().unwrap_or(false) {
            let _ = win.hide();
        } else {
            show_main_window(&win);
        }
    }
}

fn show_main(app: &tauri::AppHandle) {
    if let Some(win) = app.get_webview_window(MAIN_WINDOW) {
        show_main_window(&win);
    }
}

fn show_main_window(win: &tauri::WebviewWindow) {
    let _ = win.show();
    let _ = win.set_focus();
}

/// Polls GET /healthz until the daemon answers, then navigates the main
/// window to the daemon UI exactly once. Calls update_offline(attempt)
/// (starting at 0, before the first check) so the fallback page can show
/// live retry state (quick-wins AC5).
async fn watch_daemon_and_navigate(cfg: &Config, navigate: impl Fn(), update_offline: impl Fn(u32)) {
    let mut attempt: u32 = 0;
    update_offline(attempt);
    loop {
        if dclient::client::healthz_ok(cfg).await {
            eprintln!("smind desktop: /healthz ok, switching to daemon UI");
            navigate();
            return;
        }
        attempt += 1;
        update_offline(attempt);
        tokio::time::sleep(HEALTHZ_POLL).await;
    }
}
