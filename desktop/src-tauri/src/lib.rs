//! smind desktop thin-client shell (ADR-0012): the main window just
//! displays the daemon's web UI; every native feature is driven from
//! the Rust side. A second instance only focuses the existing window.

use tauri::Manager;
use tauri::menu::{Menu, MenuItem};
use tauri::tray::TrayIconBuilder;
use tauri::webview::WebviewWindowBuilder;
use tauri_plugin_global_shortcut::GlobalShortcutExt;
use tauri_plugin_notification::NotificationExt;

use smind_daemon_client as dclient;
use smind_daemon_client::Config;

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

            let watch_cfg = cfg.clone();
            let watch_win = win.clone();
            tauri::async_runtime::spawn(async move {
                watch_daemon_and_navigate(&watch_cfg, || {
                    let _ = watch_win.navigate(watch_cfg.daemon_url.clone());
                })
                .await;
            });

            // AC3: tray icon with Open / Quit.
            let open = MenuItem::with_id(app, "open", "Open", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&open, &quit])?;
            TrayIconBuilder::with_id("main-tray")
                .icon(app.default_window_icon().unwrap().clone())
                .menu(&menu)
                .show_menu_on_left_click(true)
                .on_menu_event(|app, event| match event.id().as_ref() {
                    "open" => show_main(app),
                    "quit" => app.exit(0),
                    _ => {}
                })
                .build(app)?;

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

            // AC4: daemon events -> OS notifications.
            let handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                dclient::client::run(cfg, move |n| {
                    let _ = handle
                        .notification()
                        .builder()
                        .title(n.title)
                        .body(n.body)
                        .show();
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
/// window to the daemon UI exactly once.
async fn watch_daemon_and_navigate(cfg: &Config, navigate: impl Fn()) {
    loop {
        if dclient::client::healthz_ok(cfg).await {
            eprintln!("smind desktop: /healthz ok, switching to daemon UI");
            navigate();
            return;
        }
        tokio::time::sleep(HEALTHZ_POLL).await;
    }
}
