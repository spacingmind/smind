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

mod app_settings;
mod assets;
mod client_watch;
mod commands;
mod daemon_manager;
mod deeplink;
mod editors;
mod lifecycle;
mod menu;
mod notify;
mod open_path;
#[cfg_attr(not(target_os = "windows"), allow(dead_code))]
mod overlay_icon;
mod probe;
mod proxy_port;
mod ssh_tunnel;
mod state;
mod tray;
// `pub` (hidden) so the updater e2e integration test (tests/updater_e2e.rs)
// can drive the same cores the commands call.
#[doc(hidden)]
pub mod updater;
mod window_chrome;
#[cfg(test)]
mod capability_tests;
#[cfg(test)]
mod real_daemon_test;
#[cfg(all(test, unix))]
mod daemon_survival_test;
mod zoom_store;

use state::DesktopState;

const MAIN_WINDOW: &str = "main";
const TOGGLE_SHORTCUT: &str = "CommandOrControl+Shift+S";
const CONNECTIONS_FILE: &str = "connections.json";

/// Verbosity of the app log (stdout and `<log dir>/sMind.log`). The plugin's
/// default is TRACE, which in a release build wrote every WebSocket frame --
/// and the daemon token in the `/ws?token=` request line -- to a file that
/// rotates within minutes, burying the lines Help > Open Log Folder is for.
/// Debug builds keep DEBUG for our own crates only.
fn app_log_level(debug_build: bool) -> log::LevelFilter {
    if debug_build {
        log::LevelFilter::Debug
    } else {
        log::LevelFilter::Info
    }
}

/// Dependencies that log request lines and frames below WARN even at DEBUG.
const QUIET_TARGETS: [&str; 5] = ["tungstenite", "tokio_tungstenite", "hyper_util", "reqwest", "tao"];

fn log_plugin<R: tauri::Runtime>() -> tauri::plugin::TauriPlugin<R> {
    let mut builder = tauri_plugin_log::Builder::new().level(app_log_level(cfg!(debug_assertions)));
    for target in QUIET_TARGETS {
        builder = builder.level_for(target, log::LevelFilter::Warn);
    }
    builder.build()
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let builder = tauri::Builder::default();
    // ADR-0023: updater plugin on macOS/Windows only; Linux (deb) has no
    // updater. Its JS-facing permissions are never granted.
    #[cfg(any(target_os = "macos", target_os = "windows"))]
    let builder = builder.plugin(updater::updater_plugin());
    builder
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
        .plugin(log_plugin())
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
            commands::connections_preview_relay,
            probe::connections_probe,
            ssh_tunnel::connections_add_ssh,
            app_settings::app_settings_get,
            app_settings::app_settings_set,
            commands::connections_relay_status,
            commands::connections_unsupported,
            commands::connections_relay_set_password,
            commands::connections_remove,
            commands::connections_select,
            commands::connections_get_current,
            commands::open_external,
            commands::app_quit,
            menu::app_menu_action,
            editors::editors_list,
            editors::open_in_editor,
            open_path::open_path_default,
            daemon_manager::daemon_status,
            daemon_manager::daemon_install,
            daemon_manager::daemon_update,
            daemon_manager::daemon_restart,
            daemon_manager::take_over_daemon,
            daemon_manager::connection_version,
            window_chrome::window_ready,
            window_chrome::window_set_theme,
            updater::app_update_check,
            updater::app_update_install,
        ])
        .setup(|app| {
            // Resolve the user's login-shell PATH in the background so the
            // managed daemon can find `claude`/`npx` (a GUI app's own PATH is
            // /usr/bin:/bin:...); the first daemon start reuses the result.
            #[cfg(unix)]
            dclient::daemon_manager::login_path::prewarm();

            // AC4: the built-in `local` entry always tracks
            // SMIND_DAEMON_URL/the default, re-derived from the current
            // env on every launch (see Registry::load) rather than
            // trusted from a possibly-stale saved file.
            let local_base_url = dclient::config::daemon_url().unwrap_or_else(|e| {
                eprintln!("smind desktop: {e}; using default");
                dclient::config::daemon_url_from("").unwrap()
            });

            let connections_path = app.path().app_data_dir()?.join(CONNECTIONS_FILE);
            // ADR-0031 D7: evaluated once, before the registry load can
            // create connections.json and before auto_start runs.
            let app_settings_path = app_settings::settings_path(&connections_path);
            let app_settings = app_settings::load_or_upgrade(
                &app_settings_path,
                connections_path.exists(),
                daemon_manager::managed_record_exists(app.handle()),
            );
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
            //
            // The port is the previous launch's when it is still free: the
            // webview keys localStorage by origin, so a port that changes on
            // every launch would reset every UI preference on every launch.
            let proxy_port_path = proxy_port::path_in(&app.path().app_data_dir()?);
            let (port, server_fut) = tauri::async_runtime::block_on(
                dclient::proxy::serve_preferring(proxy_state.clone(), proxy_port::load(&proxy_port_path)),
            )?;
            proxy_port::save(&proxy_port_path, port);
            tauri::async_runtime::spawn(server_fut);

            let proxy_url: Url = format!("http://127.0.0.1:{port}")
                .parse()
                .expect("smind desktop: proxy URL is well-formed");
            let initial_url: Url = format!("http://127.0.0.1:{port}/?k={secret}")
                .parse()
                .expect("smind desktop: initial URL is well-formed");

            // AC3: navigation is restricted to the proxy's own origin --
            // any other URL (an http(s) link inside the bundled UI, e.g.
            // "sMind on GitHub") opens in the OS browser instead of
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
            .title("sMind")
            .inner_size(1280.0, 800.0)
            // Prompt attachments (ADR-0038): the composer takes dropped files
            // through the web `drop` event. Tauri's own drag-drop handler
            // always claims file drops (wry's handler returns true, so
            // WKWebView's performDragOperation never reaches the page), and
            // nothing in the app listens for Tauri's DragDropEvent, so turn it
            // off and let the webview deliver HTML5 drops.
            .disable_drag_drop_handler()
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
                    let current = proxy.registry.lock().unwrap().current().clone();
                    // An ssh connection is reached through its tunnel's local end.
                    let base_url = proxy.upstream_base(&current).unwrap_or(current.base_url);
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

                // macOS registers `smind://` through the bundle's Info.plist
                // (tauri.conf.json `deep-link`); the runtime call is only for
                // Windows/Linux and always answers "unsupported platform" here.
                #[cfg(not(target_os = "macos"))]
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
            let badge_tray = tray.clone();
            win.on_window_event(move |event| match event {
                tauri::WindowEvent::CloseRequested { api, .. } => {
                    api.prevent_close();
                    window_chrome::dismissed(&close_app);
                    let _ = close_win.hide();
                }
                // Windows drops the taskbar overlay when the hidden window's
                // button is re-created; put it back when the window returns.
                tauri::WindowEvent::Focused(true) => badge_tray.reapply_badge(),
                _ => {}
            });

            // AC5: register the toggle shortcut. Another app (or another
            // sMind) may already own it -- Windows and Linux refuse the second
            // registration -- and that must not stop the app from starting:
            // the tray and the window still work without it.
            note_unavailable(
                "global shortcut",
                TOGGLE_SHORTCUT,
                app.global_shortcut().register(TOGGLE_SHORTCUT),
            );

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

            app.manage(updater::UpdateState::default());
            let ssh_tunnels = ssh_tunnel::SshTunnels::default();
            ssh_tunnels.bind_proxy(proxy_state.clone());

            app.manage(DesktopState {
                proxy: proxy_state,
                connections_path,
                app_settings: Mutex::new(app_settings),
                app_settings_path,
                proxy_url,
                ssh_tunnels,
                cache,
                tray,
                client_task: Mutex::new(client_task),
                relay_task: Mutex::new(relay_task),
            });

            // ADR-0031 D7 (b2): a saved ssh connection that is selected gets
            // its tunnel brought up best-effort (the proxy's 502 explains a
            // failure; selecting it again retries), then the watcher follows.
            if initial_conn.kind == dclient::proxy::ConnectionKind::Ssh {
                let handle = app.handle().clone();
                tauri::async_runtime::spawn(async move {
                    let state = handle.state::<DesktopState>();
                    match ssh_tunnel::ensure_tunnel(&state.proxy, &state.ssh_tunnels, &initial_conn).await {
                        Ok(()) => client_watch::restart(&handle, &state, &initial_conn),
                        Err(e) => log::warn!("ssh tunnel for {:?} did not start: {e}", initial_conn.label),
                    }
                });
            }

            // desktop-macos-app M2.3/M2.4: install + start the bundled
            // daemon when none is reachable, or update an older managed
            // one; macOS only (Windows keeps its explicit Install button).
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

/// note_unavailable logs an optional integration that could not be set up and
/// says whether it is now active. Startup never fails because of one.
fn note_unavailable<E: std::fmt::Display>(what: &str, name: &str, result: Result<(), E>) -> bool {
    match result {
        Ok(()) => true,
        Err(e) => {
            log::warn!("smind desktop: {what} {name} is unavailable: {e}");
            false
        }
    }
}

#[cfg(test)]
mod optional_integration_tests {
    use super::note_unavailable;

    #[test]
    fn a_failed_shortcut_registration_is_reported_not_propagated() {
        assert!(note_unavailable("global shortcut", "Ctrl+Shift+S", Ok::<(), String>(())));
        assert!(!note_unavailable("global shortcut", "Ctrl+Shift+S", Err("already registered")));
    }
}

#[cfg(test)]
mod file_drop_tests {
    /// The main window must not install Tauri's drag-drop handler: it swallows
    /// OS file drops before the web `drop` event, which the composer's attach
    /// drop zone depends on (ADR-0038). Tauri exposes no getter for the
    /// attribute, so this guards the builder call itself.
    #[test]
    fn main_window_leaves_html5_file_drops_to_the_webview() {
        let src = include_str!("lib.rs");
        let builder = src.split("WebviewWindowBuilder::new(").nth(1).expect("main window builder");
        let chain = builder.split(".build()?").next().expect("builder chain");
        assert!(
            chain.contains(".disable_drag_drop_handler()"),
            "the main window builder must call disable_drag_drop_handler()"
        );
    }
}

#[cfg(test)]
mod log_level_tests {
    use super::{app_log_level, QUIET_TARGETS};

    /// A release build must never log at DEBUG or below: TRACE put the
    /// daemon token (`GET /ws?token=...`) and every WebSocket frame in the log file.
    #[test]
    fn release_builds_log_at_info() {
        assert_eq!(app_log_level(false), log::LevelFilter::Info);
        assert_eq!(app_log_level(true), log::LevelFilter::Debug);
    }

    #[test]
    fn the_request_line_loggers_are_silenced() {
        for target in ["tungstenite", "hyper_util", "reqwest"] {
            assert!(QUIET_TARGETS.contains(&target), "{target} must be capped at WARN");
        }
    }
}
