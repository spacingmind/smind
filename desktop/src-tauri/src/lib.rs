//! smind desktop shell: ADR-0013 bundled UI + loopback proxy. The main
//! window loads `http://127.0.0.1:<port>/` (a Rust-side proxy serving
//! the bundled `web/packages/ui` build and reverse-proxying `/api/*` +
//! `/ws` to whichever connection is selected), not the daemon's own
//! page (ADR-0012, superseded in part -- see the ADR). Every native
//! feature (tray, notifications, deep links, shortcuts) is still driven
//! from the Rust side, unaffected.

use std::sync::Mutex;

use tauri::webview::WebviewWindowBuilder;
use tauri::Manager;
use tauri_plugin_global_shortcut::GlobalShortcutExt;
use url::Url;

use smind_daemon_client as dclient;
use smind_daemon_client::proxy::{ProxyState, Registry};
use smind_daemon_client::Config;

mod assets;
mod client_watch;
mod commands;
mod deeplink;
mod menu;
mod notify;
mod state;
mod tray;
mod zoom_store;

use state::DesktopState;

const MAIN_WINDOW: &str = "main";
const TOGGLE_SHORTCUT: &str = "CommandOrControl+Shift+S";
const CONNECTIONS_FILE: &str = "connections.json";

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
        .invoke_handler(tauri::generate_handler![
            commands::connections_list,
            commands::connections_add,
            commands::connections_remove,
            commands::connections_select,
            commands::connections_get_current,
            commands::open_external,
        ])
        .setup(|app| {
            // AC4: the built-in `local` entry always tracks
            // SMIND_DAEMON_URL/the default, re-derived from the current
            // env on every launch (see Registry::load) rather than
            // trusted from a possibly-stale saved file.
            let local_base_url = dclient::config::daemon_url().unwrap_or_else(|e| {
                eprintln!("smind desktop: {e}; using default");
                dclient::config::daemon_url_from("").unwrap()
            });

            let connections_path = app.path().app_data_dir()?.join(CONNECTIONS_FILE);
            let registry = Registry::load(&connections_path, &local_base_url);

            // AC3: a fresh secret every launch.
            let secret = dclient::proxy::secret::generate_secret();
            let proxy_state = ProxyState::new(secret.clone(), registry, Box::new(assets::EmbeddedAssets));

            // AC2: bind the loopback proxy and start serving before the
            // window is built, so its initial URL can name the real
            // port. Binding a random TCP port is effectively
            // instantaneous, so blocking setup() briefly here (rather
            // than starting the window on a placeholder and navigating
            // later, the way ADR-0012's fallback page did) keeps this
            // simple and avoids ever showing a page with no `?k=` to
            // exchange.
            let (port, server_fut) = tauri::async_runtime::block_on(dclient::proxy::serve(proxy_state.clone()))?;
            tauri::async_runtime::spawn(server_fut);

            let proxy_url: Url = format!("http://127.0.0.1:{port}").parse().expect("smind desktop: proxy URL is well-formed");
            let initial_url: Url =
                format!("http://127.0.0.1:{port}/?k={secret}").parse().expect("smind desktop: initial URL is well-formed");

            // AC3: navigation is restricted to the proxy's own origin --
            // any other URL (an http(s) link inside the bundled UI, e.g.
            // "smind on GitHub") opens in the OS browser instead of
            // navigating the window away from the app.
            let nav_origin = proxy_url.origin();
            let win = WebviewWindowBuilder::new(app, MAIN_WINDOW, tauri::WebviewUrl::External(initial_url))
                .title("smind")
                .inner_size(1280.0, 800.0)
                .on_navigation(move |url| {
                    if url.origin() == nav_origin {
                        true
                    } else {
                        let _ = tauri_plugin_opener::open_url(url.as_str(), None::<&str>);
                        false
                    }
                })
                .build()?;

            // quick-wins AC2: restore the persisted zoom level.
            let _ = win.set_zoom(zoom_store::load(app.handle()));

            // quick-wins AC2: native app menu (File/Edit/View/Window/Help).
            let win_menu = menu::build(app.handle())?;
            app.set_menu(win_menu)?;
            app.on_menu_event(|app, event| menu::handle_event(app, event.id().0.as_str()));

            // AC3/quick-wins AC4: tray icon with Open / Quit plus a
            // pending-approvals indicator (tooltip, first menu item,
            // taskbar badge). Its "open most recent waiting task" click
            // navigates within the proxy origin (AC7), same as a
            // notification click.
            let cache = dclient::WorkspaceCache::new();
            let tray = tray::build(app.handle(), cache.clone(), proxy_url.clone())?;

            // quick-wins AC6: deep links, both at launch (get_current)
            // and while running (on_open_url, fed by single-instance's
            // deep-link feature for a second launch). Uses whichever
            // connection is currently selected to resolve an unknown
            // task's workspaceId, and navigates within the proxy origin.
            {
                use tauri_plugin_deep_link::DeepLinkExt;

                fn current_cfg(proxy: &ProxyState) -> Config {
                    let base_url = proxy.registry.lock().unwrap().current().base_url.clone();
                    Config { daemon_url: base_url.parse().expect("smind desktop: saved connection URL is well-formed") }
                }

                let handle = app.handle().clone();
                let open_cache = cache.clone();
                let open_proxy_state = proxy_state.clone();
                let open_proxy_url = proxy_url.clone();
                app.deep_link().on_open_url(move |event| {
                    for url in event.urls() {
                        deeplink::handle(&handle, open_cache.clone(), current_cfg(&open_proxy_state), open_proxy_url.clone(), url.as_str());
                    }
                });

                if let Err(e) = app.deep_link().register_all() {
                    eprintln!("smind desktop: deep link registration failed: {e}");
                }

                if let Ok(Some(urls)) = app.deep_link().get_current() {
                    let cfg = current_cfg(&proxy_state);
                    for url in urls {
                        deeplink::handle(app.handle(), cache.clone(), cfg.clone(), proxy_url.clone(), url.as_str());
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

            // AC7: start the daemon-client watcher (tray pending count,
            // OS notifications) against whichever connection is
            // currently selected; connections_select restarts it.
            let initial_daemon_url: Url =
                proxy_state.registry.lock().unwrap().current().base_url.parse().expect("smind desktop: saved connection URL is well-formed");
            let client_task =
                client_watch::spawn(app.handle(), tray.clone(), cache.clone(), proxy_url.clone(), Config { daemon_url: initial_daemon_url });

            app.manage(DesktopState {
                proxy: proxy_state,
                connections_path,
                proxy_url,
                cache,
                tray,
                client_task: Mutex::new(Some(client_task)),
            });

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running smind desktop");
}

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
