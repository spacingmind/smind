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
//! table (all `Mod+letter`/`Mod+[`/`Mod+]`/`Escape`/`Shift+?`) -- except
//! the D3 items, which intentionally *share* the binding table's combos
//! (Ctrl/Cmd+T, +W, +K, +F, +,, +B): choosing one emits a `menu-action`
//! event and the renderer's dedupe keeps the keystroke from firing twice.
//!
//! The D3 items carry their accelerator on macOS only. On Windows and
//! Linux, CmdOrCtrl resolves to Ctrl, and Ctrl+W/K/B/F/T are readline
//! keys inside the terminal pane (delete-word, kill-line, back/forward-
//! char, transpose) -- a native accelerator would steal them from the
//! shell before the binding table's terminal-scope rules can protect
//! them. The items still exist there, acceleratorless, and emit
//! `menu-action` when clicked.

use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu, SubmenuBuilder};
use tauri::{AppHandle, Emitter, Manager, Wry};

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
// D3: renderer actions. The event payload is the action id from
// `web/packages/ui/src/keyboard/actions.ts`, dispatched there through the
// same registry a keystroke uses.
const ID_NEW_TAB: &str = "file-new-tab";
const ID_CLOSE_TAB: &str = "file-close-tab";
const ID_PALETTE: &str = "view-palette";
const ID_FIND: &str = "view-find";
const ID_SETTINGS: &str = "file-settings";
const ID_TOGGLE_SIDEBAR: &str = "view-toggle-sidebar";
const ID_APP_ABOUT: &str = "app-about";
const ID_APP_SETTINGS: &str = "app-settings";
const ID_APP_QUIT: &str = "app-quit";

/// The accelerator for a D3 item: the combo it shares with the binding
/// table on macOS, nothing on Windows/Linux (see the module comment).
fn renderer_accel(combo: &str) -> Option<&str> {
    if cfg!(target_os = "macos") {
        Some(combo)
    } else {
        None
    }
}

pub fn build(app: &AppHandle) -> tauri::Result<Menu<Wry>> {
    // macOS convention (D3.4): the first menu is the app-name menu with
    // About, Settings…, Hide, Hide Others and Quit. PredefinedMenuItem's
    // Linux-unsupported list doesn't cover hide/hide_others, and this
    // submenu is macOS-only anyway. About and Quit stay custom items
    // (they reuse the Help/File handlers); Quit is a fresh id because
    // menu item ids must be unique.
    let mut app_menu: Option<Submenu<Wry>> = None;
    if cfg!(target_os = "macos") {
        let app_submenu = SubmenuBuilder::new(app, "smind")
            .item(&MenuItem::with_id(
                app,
                ID_APP_ABOUT,
                "About smind",
                true,
                None::<&str>,
            )?)
            .separator()
            .item(&MenuItem::with_id(
                app,
                ID_APP_SETTINGS,
                "Settings…",
                true,
                renderer_accel("CmdOrCtrl+,"),
            )?)
            .separator()
            .item(&PredefinedMenuItem::hide(app, Some("Hide smind"))?)
            .item(&PredefinedMenuItem::hide_others(app, Some("Hide Others"))?)
            .separator()
            .item(&MenuItem::with_id(
                app,
                ID_APP_QUIT,
                "Quit smind",
                true,
                Some("CmdOrCtrl+Q"),
            )?)
            .build()?;
        app_menu = Some(app_submenu);
    }

    let mut file_builder = SubmenuBuilder::new(app, "File")
        .item(&MenuItem::with_id(
            app,
            ID_NEW_TAB,
            "New Tab",
            true,
            renderer_accel("CmdOrCtrl+T"),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_CLOSE_TAB,
            "Close Tab",
            true,
            renderer_accel("CmdOrCtrl+W"),
        )?);
    if !cfg!(target_os = "macos") {
        // Settings… lives in the app menu on macOS; the File menu hosts it
        // where there isn't one.
        file_builder = file_builder.separator().item(&MenuItem::with_id(
            app,
            ID_SETTINGS,
            "Settings…",
            true,
            renderer_accel("CmdOrCtrl+,"),
        )?);
    }
    let file = file_builder
        .separator()
        .item(&MenuItem::with_id(
            app,
            ID_RELOAD,
            "Reload",
            true,
            Some("CmdOrCtrl+R"),
        )?)
        .separator()
        .item(&MenuItem::with_id(
            app,
            ID_QUIT,
            "Quit",
            true,
            Some("CmdOrCtrl+Q"),
        )?)
        .build()?;

    let edit = SubmenuBuilder::new(app, "Edit")
        .undo()
        .redo()
        .separator()
        .cut()
        .copy()
        .paste()
        .select_all()
        .build()?;

    let mut view_builder = SubmenuBuilder::new(app, "View")
        .item(&MenuItem::with_id(
            app,
            ID_PALETTE,
            "Command Palette",
            true,
            renderer_accel("CmdOrCtrl+K"),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_FIND,
            "Find",
            true,
            renderer_accel("CmdOrCtrl+F"),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_TOGGLE_SIDEBAR,
            "Toggle Sidebar",
            true,
            renderer_accel("CmdOrCtrl+B"),
        )?)
        .separator()
        .item(&MenuItem::with_id(
            app,
            ID_ZOOM_IN,
            "Zoom In",
            true,
            Some("CmdOrCtrl+Plus"),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_ZOOM_OUT,
            "Zoom Out",
            true,
            Some("CmdOrCtrl+-"),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_ZOOM_RESET,
            "Reset Zoom",
            true,
            Some("CmdOrCtrl+0"),
        )?)
        .separator()
        .item(&MenuItem::with_id(
            app,
            ID_FULLSCREEN,
            "Toggle Fullscreen",
            true,
            Some("F11"),
        )?);
    if cfg!(debug_assertions) {
        view_builder = view_builder.separator().item(&MenuItem::with_id(
            app,
            ID_DEVTOOLS,
            "Toggle DevTools",
            true,
            Some("F12"),
        )?);
    }
    let view = view_builder.build()?;

    let window = SubmenuBuilder::new(app, "Window")
        .item(&MenuItem::with_id(
            app,
            ID_MINIMIZE,
            "Minimize",
            true,
            Some("CmdOrCtrl+M"),
        )?)
        // No tray on macOS (desktop-macos-app M3): the window just hides
        // and the Dock icon brings it back.
        .item(&MenuItem::with_id(
            app,
            ID_HIDE,
            if cfg!(target_os = "macos") {
                "Hide"
            } else {
                "Hide to Tray"
            },
            true,
            None::<&str>,
        )?)
        .build()?;

    let help = SubmenuBuilder::new(app, "Help")
        .item(&MenuItem::with_id(
            app,
            ID_ABOUT,
            "About smind",
            true,
            None::<&str>,
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_LOG_FOLDER,
            "Open Log Folder",
            true,
            None::<&str>,
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_GITHUB,
            "smind on GitHub",
            true,
            None::<&str>,
        )?)
        .build()?;

    let menu = Menu::new(app)?;
    if let Some(app_menu) = &app_menu {
        menu.append(app_menu)?;
    }
    for submenu in [&file, &edit, &view, &window, &help] {
        menu.append(submenu)?;
    }
    Ok(menu)
}

/// handle_event dispatches one menu item click by id. Called from
/// `App::on_menu_event`.
pub fn handle_event(app: &AppHandle, id: &str) {
    // Renderer actions (D3.3): one event whose payload is the action id,
    // dispatched by the UI through the same registry a keystroke uses.
    if let Some(action) = menu_action_for(id) {
        let _ = app.emit("menu-action", action);
        return;
    }

    let Some(win) = app.get_webview_window(MAIN_WINDOW) else {
        return;
    };
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
        ID_APP_ABOUT => show_about(app),
        ID_APP_QUIT => app.exit(0),
        ID_ABOUT => show_about(app),
        ID_LOG_FOLDER => open_log_folder(app),
        ID_GITHUB => {
            use tauri_plugin_opener::OpenerExt;
            let _ = app.opener().open_url(GITHUB_URL, None::<&str>);
        }
        _ => {}
    }
}

/// The action id a menu item id maps to, for items the renderer performs.
/// The strings must match `web/packages/ui/src/keyboard/actions.ts`'s
/// `ActionId` union exactly; the test below pins them.
fn menu_action_for(id: &str) -> Option<&'static str> {
    match id {
        ID_NEW_TAB => Some("tab.new"),
        ID_CLOSE_TAB => Some("tab.close"),
        ID_PALETTE => Some("palette.open"),
        ID_FIND => Some("pane.find"),
        ID_SETTINGS | ID_APP_SETTINGS => Some("settings.open"),
        ID_TOGGLE_SIDEBAR => Some("sidebar.toggle"),
        _ => None,
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn menu_actions_match_web_action_ids() {
        assert_eq!(menu_action_for(ID_NEW_TAB), Some("tab.new"));
        assert_eq!(menu_action_for(ID_CLOSE_TAB), Some("tab.close"));
        assert_eq!(menu_action_for(ID_PALETTE), Some("palette.open"));
        assert_eq!(menu_action_for(ID_FIND), Some("pane.find"));
        assert_eq!(menu_action_for(ID_SETTINGS), Some("settings.open"));
        assert_eq!(menu_action_for(ID_APP_SETTINGS), Some("settings.open"));
        assert_eq!(menu_action_for(ID_TOGGLE_SIDEBAR), Some("sidebar.toggle"));
        // Native-only items keep their Rust behaviour, not a renderer action.
        assert_eq!(menu_action_for(ID_APP_ABOUT), None);
        assert_eq!(menu_action_for(ID_APP_QUIT), None);
        assert_eq!(menu_action_for(ID_QUIT), None);
        assert_eq!(menu_action_for(ID_RELOAD), None);
    }
}
