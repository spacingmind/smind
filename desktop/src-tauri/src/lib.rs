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
mod daemon_manager;
mod deeplink;
mod editors;
mod lifecycle;
mod menu;
mod notify;
#[cfg_attr(not(target_os = "windows"), allow(dead_code))]
mod overlay_icon;
mod state;
mod tray;
mod window_chrome;
#[cfg(test)]
mod capability_tests;
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
        // forcing an off-screen position. VISIBLE and DECORATIONS are
        // deliberately not restored: the former would show the window
        // before the UI has painted (D2.1), the latter would hand a
        // previously-saved native frame back to an undecorated window
        // (D1), leaving two sets of caption buttons.
        .plugin(
            tauri_plugin_window_state::Builder::default()
                .with_state_flags(
                    tauri_plugin_window_state::StateFlags::all()
                        - tauri_plugin_window_state::StateFlags::VISIBLE
                        - tauri_plugin_window_state::StateFlags::DECORATIONS,
                )
                .build(),
        )
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
            commands::connections_add_relay,
            commands::connections_remove,
            commands::connections_select,
            commands::connections_get_current,
            commands::open_external,
            editors::editors_list,
            editors::open_in_editor,
            daemon_manager::daemon_status,
            daemon_manager::daemon_install,
            daemon_manager::daemon_update,
            daemon_manager::daemon_restart,
            daemon_manager::take_over_daemon,
            daemon_manager::connection_version,
            window_chrome::window_ready,
            window_chrome::window_set_theme,
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
            let proxy_state =
                ProxyState::new(secret.clone(), registry, Box::new(assets::EmbeddedAssets));

            // AC2: bind the loopback proxy and start serving before the
            // window is built, so its initial URL can name the real
            // port. Binding a random TCP port is effectively
            // instantaneous, so blocking setup() briefly here (rather
            // than starting the window on a placeholder and navigating
            // later, the way ADR-0012's fallback page did) keeps this
            // simple and avoids ever showing a page with no `?k=` to
            // exchange.
            let (port, server_fut) =
                tauri::async_runtime::block_on(dclient::proxy::serve(proxy_state.clone()))?;
            tauri::async_runtime::spawn(server_fut);

            let proxy_url: Url = format!("http://127.0.0.1:{port}")
                .parse()
                .expect("smind desktop: proxy URL is well-formed");
            let initial_url: Url = format!("http://127.0.0.1:{port}/?k={secret}")
                .parse()
                .expect("smind desktop: initial URL is well-formed");

            // AC3: navigation is restricted to the proxy's own origin --
            // any other URL (an http(s) link inside the bundled UI, e.g.
            // "smind on GitHub") opens in the OS browser instead of
            // navigating the window away from the app.
            let nav_origin = proxy_url.origin();
            // D1/D2.1: per-platform chrome, created hidden and shown
            // when the UI reports its first paint (or after the fallback).
            app.manage(window_chrome::ShowGate::default());
            let builder = WebviewWindowBuilder::new(
                app,
                MAIN_WINDOW,
                tauri::WebviewUrl::External(initial_url),
            )
            .title("smind")
            .inner_size(1280.0, 800.0)
            .on_navigation(move |url| {
                if url.origin() == nav_origin {
                    true
                } else {
                    let _ = tauri_plugin_opener::open_url(url.as_str(), None::<&str>);
                    false
                }
            });
            let win = window_chrome::configure(builder, window_chrome::load_surface(app.handle()))
                .build()?;
            window_chrome::arm_fallback(app.handle());

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
                    Config {
                        daemon_url: base_url
                            .parse()
                            .expect("smind desktop: saved connection URL is well-formed"),
                    }
                }

                let handle = app.handle().clone();
                let open_cache = cache.clone();
                let open_proxy_state = proxy_state.clone();
                let open_proxy_url = proxy_url.clone();
                app.deep_link().on_open_url(move |event| {
                    for url in event.urls() {
                        deeplink::handle(
                            &handle,
                            open_cache.clone(),
                            current_cfg(&open_proxy_state),
                            open_proxy_url.clone(),
                            url.as_str(),
                        );
                    }
                });

                if let Err(e) = app.deep_link().register_all() {
                    eprintln!("smind desktop: deep link registration failed: {e}");
                }

                if let Ok(Some(urls)) = app.deep_link().get_current() {
                    let cfg = current_cfg(&proxy_state);
                    for url in urls {
                        deeplink::handle(
                            app.handle(),
                            cache.clone(),
                            cfg.clone(),
                            proxy_url.clone(),
                            url.as_str(),
                        );
                    }
                }
            }

            // AC3: hide-on-close instead of destroy.
            let close_win = win.clone();
            let close_app = app.handle().clone();
            win.on_window_event(move |event| {
                if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    window_chrome::dismissed(&close_app);
                    let _ = close_win.hide();
                }
            });

            // AC5: register the toggle shortcut.
            app.global_shortcut().register(TOGGLE_SHORTCUT)?;

            // AC7: start the daemon-client watcher (tray pending count,
            // OS notifications) -- and, for a relay-kind connection, the
            // relay transport itself -- against whichever connection is
            // currently selected; connections_select restarts both.
            let initial_conn = proxy_state.registry.lock().unwrap().current().clone();
            let (client_task, relay_task) = client_watch::spawn_initial(
                app.handle(),
                &proxy_state,
                tray.clone(),
                cache.clone(),
                proxy_url.clone(),
                &initial_conn,
                &connections_path,
            );

            app.manage(DesktopState {
                proxy: proxy_state,
                connections_path,
                proxy_url,
                cache,
                tray,
                client_task: Mutex::new(client_task),
                relay_task: Mutex::new(relay_task),
                daemon_manager_distro: std::sync::Arc::new(Mutex::new(None)),
                daemon_manager_platform: std::sync::Arc::new(Mutex::new(None)),
            });

            // desktop-macos-app M2.3/M2.4: install + start the bundled
            // daemon when none is reachable, or update an older managed
            // one; macOS only (WSL2 keeps its explicit Install button).
            #[cfg(target_os = "macos")]
            tauri::async_runtime::spawn(daemon_manager::auto_start(app.handle().clone()));

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building smind desktop")
        .run(lifecycle::handle_run_event);
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
