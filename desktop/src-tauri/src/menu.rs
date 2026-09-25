//! quick-wins AC2: the native app menu (File/Edit/View/Window/Help).
//!
//! Custom `MenuItem`s (not `PredefinedMenuItem`) drive Quit, Minimize and
//! Fullscreen: `tauri::menu::PredefinedMenuItem`'s own docs list
//! `quit`/`close_window`/`minimize`/`fullscreen` as "Linux: Unsupported",
//! and Linux/WSLg is where this app is actually exercised live -- a
//! custom item calling the window/app API directly behaves the same on
//! every platform this ships for. Edit's Cut/Copy/Paste/Select All have
//! no such caveat, so those stay predefined (needed for macOS clipboard
//! shortcuts to work at all).
//!
//! Accelerators are plain OS conventions (Ctrl+R, Ctrl+Q, Ctrl+Plus/Minus/
//! 0, F11, F12) chosen to not collide with any combo in
//! `web/packages/ui/src/keyboard/shortcuts.ts`'s `SHORTCUT_BINDINGS`
//! table (all `Mod+letter`/`Mod+[`/`Mod+]`/`Escape`/`Shift+?`).

use tauri::menu::{Menu, MenuItem, SubmenuBuilder};
use tauri::{AppHandle, Manager, Wry};

use smind_daemon_client::zoom;

use crate::{zoom_store, MAIN_WINDOW};

const GITHUB_URL: &str = "https://github.com/spacingmind/smind";

const ID_RELOAD: &str = "file-reload";
const ID_QUIT: &str = "file-quit";
const ID_ZOOM_IN: &str = "view-zoom-in";
const ID_ZOOM_OUT: &str = "view-zoom-out";
const ID_ZOOM_RESET: &str = "view-zoom-reset";
const ID_FULLSCREEN: &str = "view-fullscreen";
const ID_DEVTOOLS: &str = "view-devtools";
const ID_MINIMIZE: &str = "window-minimize";
const ID_HIDE: &str = "window-hide";
const ID_ABOUT: &str = "help-about";
const ID_LOG_FOLDER: &str = "help-log-folder";
const ID_GITHUB: &str = "help-github";

pub fn build(app: &AppHandle) -> tauri::Result<Menu<Wry>> {
    let file = SubmenuBuilder::new(app, "File")
        .item(&MenuItem::with_id(app, ID_RELOAD, "Reload", true, Some("CmdOrCtrl+R"))?)
        .separator()
        .item(&MenuItem::with_id(app, ID_QUIT, "Quit", true, Some("CmdOrCtrl+Q"))?)
        .build()?;

    let edit = SubmenuBuilder::new(app, "Edit")
        .cut()
        .copy()
        .paste()
        .select_all()
        .build()?;

    let mut view_builder = SubmenuBuilder::new(app, "View")
        .item(&MenuItem::with_id(app, ID_ZOOM_IN, "Zoom In", true, Some("CmdOrCtrl+Plus"))?)
        .item(&MenuItem::with_id(app, ID_ZOOM_OUT, "Zoom Out", true, Some("CmdOrCtrl+-"))?)
        .item(&MenuItem::with_id(app, ID_ZOOM_RESET, "Reset Zoom", true, Some("CmdOrCtrl+0"))?)
        .separator()
        .item(&MenuItem::with_id(app, ID_FULLSCREEN, "Toggle Fullscreen", true, Some("F11"))?);
    if cfg!(debug_assertions) {
        view_builder = view_builder
            .separator()
            .item(&MenuItem::with_id(app, ID_DEVTOOLS, "Toggle DevTools", true, Some("F12"))?);
    }
    let view = view_builder.build()?;

    let window = SubmenuBuilder::new(app, "Window")
        .item(&MenuItem::with_id(app, ID_MINIMIZE, "Minimize", true, Some("CmdOrCtrl+M"))?)
        .item(&MenuItem::with_id(app, ID_HIDE, "Hide to Tray", true, None::<&str>)?)
        .build()?;

    let help = SubmenuBuilder::new(app, "Help")
        .item(&MenuItem::with_id(app, ID_ABOUT, "About smind", true, None::<&str>)?)
        .item(&MenuItem::with_id(app, ID_LOG_FOLDER, "Open Log Folder", true, None::<&str>)?)
        .item(&MenuItem::with_id(app, ID_GITHUB, "smind on GitHub", true, None::<&str>)?)
        .build()?;

    Menu::with_items(app, &[&file, &edit, &view, &window, &help])
}

/// handle_event dispatches one menu item click by id. Called from
/// `App::on_menu_event`.
pub fn handle_event(app: &AppHandle, id: &str) {
    let Some(win) = app.get_webview_window(MAIN_WINDOW) else { return };
    match id {
        ID_RELOAD => {
            let _ = win.eval("window.location.reload()");
        }
        ID_QUIT => app.exit(0),
        ID_ZOOM_IN => apply_zoom(app, &win, zoom::step_in),
        ID_ZOOM_OUT => apply_zoom(app, &win, zoom::step_out),
        ID_ZOOM_RESET => apply_zoom(app, &win, |_| zoom::DEFAULT),
        ID_FULLSCREEN => {
            let is_fullscreen = win.is_fullscreen().unwrap_or(false);
            let _ = win.set_fullscreen(!is_fullscreen);
        }
        ID_DEVTOOLS => {
            if win.is_devtools_open() {
                win.close_devtools();
            } else {
                win.open_devtools();
            }
        }
        ID_MINIMIZE => {
            let _ = win.minimize();
        }
        ID_HIDE => {
            let _ = win.hide();
        }
        ID_ABOUT => show_about(app),
        ID_LOG_FOLDER => open_log_folder(app),
        ID_GITHUB => {
            use tauri_plugin_opener::OpenerExt;
            let _ = app.opener().open_url(GITHUB_URL, None::<&str>);
        }
        _ => {}
    }
}

fn apply_zoom(app: &AppHandle, win: &tauri::WebviewWindow, step: impl FnOnce(f64) -> f64) {
    let current = zoom_store::load(app);
    let next = step(current);
    let _ = win.set_zoom(next);
    zoom_store::save(app, next);
}

fn show_about(app: &AppHandle) {
    use tauri_plugin_dialog::DialogExt;
    let version = app.package_info().version.to_string();
    app.dialog()
        .message(format!("smind desktop\nVersion {version}"))
        .title("About smind")
        .blocking_show();
}

fn open_log_folder(app: &AppHandle) {
    use tauri_plugin_opener::OpenerExt;
    if let Ok(dir) = app.path().app_log_dir() {
        let _ = std::fs::create_dir_all(&dir);
        let _ = app.opener().open_path(dir.to_string_lossy(), None::<&str>);
    }
}
