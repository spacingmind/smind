//! quick-wins AC2: the native app menu (File/Edit/View/Window/Help).
//!
//! Custom `MenuItem`s (not `PredefinedMenuItem`) drive Quit, Minimize and
//! Fullscreen: `tauri::menu::PredefinedMenuItem`'s own docs list
//! `quit`/`close_window`/`minimize`/`fullscreen` as "Linux: Unsupported", so a
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
//! Accelerator table (`accelerator`, pinned by
//! `menu_accelerators_follow_the_platform_table`): macOS carries every
//! item's accelerator. On Windows/Linux only the items that are honoured
//! there keep one -- Reload, Zoom In/Out/Reset, Toggle Fullscreen, Quit
//! (the renderer owns Ctrl+Q there, see `keyboard/shortcuts.ts`'s
//! `app-quit`) and the debug-only DevTools F12. Minimize's Cmd+M is a
//! macOS convention, so it has none elsewhere. The window is undecorated
//! on Windows/Linux, so the menu bar itself is not drawn; the same
//! actions live in the header's app menu (`app-menu.tsx`), which reaches
//! the native-only ones through `app_menu_action`.
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
// desktop-auto-update U3: Check for Updates… (app menu on macOS, Help on
// Windows, absent on Linux).
const ID_APP_CHECK_UPDATES: &str = "app-check-updates";
const ID_HELP_CHECK_UPDATES: &str = "help-check-updates";
// ADR-0031 D7: Help > Run Setup Again… reopens first-run (every platform).
const ID_RUN_SETUP: &str = "help-run-setup";

/// The event the bundled UI listens to (`platform.ts` `onRunSetupRequested`).
const RUN_SETUP_EVENT: &str = "run-setup-requested";

/// desktop-auto-update U3: where the Check for Updates… item lives.
#[derive(Debug, PartialEq, Eq, Clone, Copy)]
pub enum UpdateMenuLocation {
    AppMenu,
    HelpMenu,
    None,
}

pub fn update_menu_location(os: &str) -> UpdateMenuLocation {
    match os {
        "macos" => UpdateMenuLocation::AppMenu,
        "windows" => UpdateMenuLocation::HelpMenu,
        _ => UpdateMenuLocation::None,
    }
}

/// Every item that can carry an accelerator, with its macOS combo and
/// whether the combo is also honoured on Windows/Linux.
const ACCELERATORS: &[(&str, &str, bool)] = &[
    (ID_APP_SETTINGS, "CmdOrCtrl+,", false),
    (ID_APP_QUIT, "CmdOrCtrl+Q", false),
    (ID_NEW_TAB, "CmdOrCtrl+T", false),
    (ID_CLOSE_TAB, "CmdOrCtrl+W", false),
    (ID_SETTINGS, "CmdOrCtrl+,", false),
    (ID_RELOAD, "CmdOrCtrl+R", true),
    (ID_QUIT, "CmdOrCtrl+Q", true),
    (ID_PALETTE, "CmdOrCtrl+K", false),
    (ID_FIND, "CmdOrCtrl+F", false),
    (ID_TOGGLE_SIDEBAR, "CmdOrCtrl+B", false),
    (ID_ZOOM_IN, "CmdOrCtrl+Plus", true),
    (ID_ZOOM_OUT, "CmdOrCtrl+-", true),
    (ID_ZOOM_RESET, "CmdOrCtrl+0", true),
    (ID_FULLSCREEN, "F11", true),
    (ID_DEVTOOLS, "F12", true),
    (ID_MINIMIZE, "CmdOrCtrl+M", false),
];

/// The accelerator `item` shows on `os` (`std::env::consts::OS` spelling).
fn accelerator(item: &str, os: &str) -> Option<&'static str> {
    ACCELERATORS
        .iter()
        .find(|(id, _, _)| *id == item)
        .and_then(|(_, combo, everywhere)| (os == "macos" || *everywhere).then_some(*combo))
}

fn accel(item: &str) -> Option<&'static str> {
    accelerator(item, std::env::consts::OS)
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
        let app_submenu = SubmenuBuilder::new(app, "sMind")
            .item(&MenuItem::with_id(
                app,
                ID_APP_ABOUT,
                "About sMind",
                true,
                None::<&str>,
            )?)
            .separator()
            .item(&MenuItem::with_id(
                app,
                ID_APP_CHECK_UPDATES,
                "Check for Updates…",
                true,
                None::<&str>,
            )?)
            .separator()
            .item(&MenuItem::with_id(
                app,
                ID_APP_SETTINGS,
                "Settings…",
                true,
                accel(ID_APP_SETTINGS),
            )?)
            .separator()
            .item(&PredefinedMenuItem::hide(app, Some("Hide sMind"))?)
            .item(&PredefinedMenuItem::hide_others(app, Some("Hide Others"))?)
            .separator()
            .item(&MenuItem::with_id(
                app,
                ID_APP_QUIT,
                "Quit sMind",
                true,
                accel(ID_APP_QUIT),
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
            accel(ID_NEW_TAB),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_CLOSE_TAB,
            "Close Tab",
            true,
            accel(ID_CLOSE_TAB),
        )?);
    if !cfg!(target_os = "macos") {
        // Settings… lives in the app menu on macOS; the File menu hosts it
        // where there isn't one.
        file_builder = file_builder.separator().item(&MenuItem::with_id(
            app,
            ID_SETTINGS,
            "Settings…",
            true,
            accel(ID_SETTINGS),
        )?);
    }
    let file = file_builder
        .separator()
        .item(&MenuItem::with_id(
            app,
            ID_RELOAD,
            "Reload",
            true,
            accel(ID_RELOAD),
        )?)
        .separator()
        .item(&MenuItem::with_id(
            app,
            ID_QUIT,
            "Quit",
            true,
            accel(ID_QUIT),
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
            accel(ID_PALETTE),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_FIND,
            "Find",
            true,
            accel(ID_FIND),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_TOGGLE_SIDEBAR,
            "Toggle Sidebar",
            true,
            accel(ID_TOGGLE_SIDEBAR),
        )?)
        .separator()
        .item(&MenuItem::with_id(
            app,
            ID_ZOOM_IN,
            "Zoom In",
            true,
            accel(ID_ZOOM_IN),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_ZOOM_OUT,
            "Zoom Out",
            true,
            accel(ID_ZOOM_OUT),
        )?)
        .item(&MenuItem::with_id(
            app,
            ID_ZOOM_RESET,
            "Reset Zoom",
            true,
            accel(ID_ZOOM_RESET),
        )?)
        .separator()
        .item(&MenuItem::with_id(
            app,
            ID_FULLSCREEN,
            "Toggle Fullscreen",
            true,
            accel(ID_FULLSCREEN),
        )?);
    if cfg!(debug_assertions) {
        view_builder = view_builder.separator().item(&MenuItem::with_id(
            app,
            ID_DEVTOOLS,
            "Toggle DevTools",
            true,
            accel(ID_DEVTOOLS),
        )?);
    }
    let view = view_builder.build()?;

    let window = SubmenuBuilder::new(app, "Window")
        .item(&MenuItem::with_id(
            app,
            ID_MINIMIZE,
            "Minimize",
            true,
            accel(ID_MINIMIZE),
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

    let mut help_builder = SubmenuBuilder::new(app, "Help")
        .item(&MenuItem::with_id(
            app,
            ID_ABOUT,
            "About sMind",
            true,
            None::<&str>,
        )?);
    if update_menu_location(std::env::consts::OS) == UpdateMenuLocation::HelpMenu {
        help_builder = help_builder.item(&MenuItem::with_id(
            app,
            ID_HELP_CHECK_UPDATES,
            "Check for Updates…",
            true,
            None::<&str>,
        )?);
    }
    let help = help_builder
        .item(&MenuItem::with_id(
            app,
            ID_RUN_SETUP,
            "Run Setup Again…",
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
            "sMind on GitHub",
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

    // desktop-auto-update U3: the check request also works while the
    // main window is hidden (macOS hidden-window state), so it must run
    // before the get_webview_window early return.
    if matches!(id, ID_APP_CHECK_UPDATES | ID_HELP_CHECK_UPDATES) {
        crate::show_main(app);
        let _ = app.emit("app-update-check-requested", ());
        return;
    }

    // ADR-0031 D7: reopen first-run; the UI keeps saved connections as they are.
    if id == ID_RUN_SETUP {
        crate::show_main(app);
        let _ = app.emit_to(MAIN_WINDOW, RUN_SETUP_EVENT, ());
        return;
    }

    handle_native(app, id);
}

/// The native-only items (everything `menu_action_for` does not claim).
fn handle_native(app: &AppHandle, id: &str) {
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
        // The webview devtools API only exists in debug builds: the tauri
        // `devtools` feature is deliberately not enabled (W4), so a release
        // webview has no Inspect entries either.
        #[cfg(debug_assertions)]
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

/// The ids `app_menu_action` accepts, and the menu item each stands for.
/// Renderer actions are not here: the header app menu dispatches those
/// through the keyboard registry, never over IPC.
const NATIVE_MENU_ACTIONS: &[(&str, &str)] = &[
    ("reload", ID_RELOAD),
    ("zoom-in", ID_ZOOM_IN),
    ("zoom-out", ID_ZOOM_OUT),
    ("zoom-reset", ID_ZOOM_RESET),
    ("fullscreen", ID_FULLSCREEN),
    ("minimize", ID_MINIMIZE),
    ("hide", ID_HIDE),
    ("about", ID_ABOUT),
    ("log-folder", ID_LOG_FOLDER),
    ("github", ID_GITHUB),
    // desktop-auto-update U3: Windows' Check for Updates… (Help menu there),
    // reached from the header app menu; `app_menu_action` routes it through
    // `handle_event`, whose update branch shows the window and emits
    // `app-update-check-requested`.
    ("check-updates", ID_HELP_CHECK_UPDATES),
    ("quit", ID_QUIT),
];

fn native_item_for(action: &str) -> Option<&'static str> {
    NATIVE_MENU_ACTIONS
        .iter()
        .find(|(name, _)| *name == action)
        .map(|(_, item)| *item)
}

/// The header app menu's (Windows/Linux) native items: runs the same logic
/// as clicking the menu item. One command with an allowlist rather than one
/// command per item; an unknown id is rejected.
#[tauri::command]
pub fn app_menu_action(app: AppHandle, id: String) -> Result<(), String> {
    let item = native_item_for(&id).ok_or_else(|| format!("app_menu_action: unknown id {id:?}"))?;
    handle_event(&app, item);
    Ok(())
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
        .message(format!("sMind desktop\nVersion {version}"))
        .title("About sMind")
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
    fn tauri_dependency_does_not_enable_devtools() {
        let manifest = include_str!("../Cargo.toml");
        let line = manifest
            .lines()
            .find(|l| l.trim_start().starts_with("tauri = "))
            .expect("Cargo.toml has a `tauri = ` dependency line");
        assert!(
            !line.contains("devtools"),
            "the tauri `devtools` feature would put Inspect in the release webview: {line}"
        );
    }

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

    #[test]
    fn run_setup_again_is_a_distinct_native_item_and_event() {
        assert_eq!(ID_RUN_SETUP, "help-run-setup");
        // Not a renderer action and not on the header-menu allowlist: the
        // native Help item emits the event itself.
        assert_eq!(menu_action_for(ID_RUN_SETUP), None);
        assert_eq!(native_item_for("run-setup"), None);
        assert_eq!(RUN_SETUP_EVENT, "run-setup-requested");
        let ids = [ID_ABOUT, ID_LOG_FOLDER, ID_GITHUB, ID_HELP_CHECK_UPDATES, ID_APP_CHECK_UPDATES];
        assert!(!ids.contains(&ID_RUN_SETUP));
    }

    #[test]
    fn update_menu_location() {
        assert_eq!(
            super::update_menu_location("macos"),
            super::UpdateMenuLocation::AppMenu
        );
        assert_eq!(
            super::update_menu_location("windows"),
            super::UpdateMenuLocation::HelpMenu
        );
        assert_eq!(
            super::update_menu_location("linux"),
            super::UpdateMenuLocation::None
        );
    }

    #[test]
    fn menu_accelerators_follow_the_platform_table() {
        // macOS: every item that can carry one does.
        for (id, combo, _) in ACCELERATORS {
            assert_eq!(accelerator(id, "macos"), Some(*combo), "{id} on macos");
        }
        // Windows/Linux: renderer-action items (D3 rule) and the macOS-only
        // conventions carry none...
        for os in ["windows", "linux"] {
            for id in [
                ID_NEW_TAB,
                ID_CLOSE_TAB,
                ID_PALETTE,
                ID_FIND,
                ID_SETTINGS,
                ID_TOGGLE_SIDEBAR,
                ID_MINIMIZE,
                ID_APP_QUIT,
                ID_APP_SETTINGS,
            ] {
                assert_eq!(accelerator(id, os), None, "{id} on {os}");
            }
            // ...and only the honoured-everywhere set keeps one.
            let mut kept: Vec<&str> = ACCELERATORS
                .iter()
                .filter(|(id, _, _)| accelerator(id, os).is_some())
                .map(|(id, _, _)| *id)
                .collect();
            kept.sort_unstable();
            let mut want = vec![
                ID_RELOAD,
                ID_QUIT,
                ID_ZOOM_IN,
                ID_ZOOM_OUT,
                ID_ZOOM_RESET,
                ID_FULLSCREEN,
                ID_DEVTOOLS,
            ];
            want.sort_unstable();
            assert_eq!(kept, want, "{os}");
        }
        // No renderer action is ever honoured natively off macOS.
        for (id, _, everywhere) in ACCELERATORS {
            if menu_action_for(id).is_some() {
                assert!(!everywhere, "{id}");
            }
        }
    }

    #[test]
    fn app_menu_action_allowlist_is_native_only_and_rejects_unknown_ids() {
        for (name, item) in NATIVE_MENU_ACTIONS {
            assert_eq!(native_item_for(name), Some(*item));
            assert_eq!(menu_action_for(item), None, "{name} must not be a renderer action");
        }
        assert_eq!(native_item_for("tab.new"), None);
        assert_eq!(native_item_for("file-reload"), None);
        assert_eq!(native_item_for(""), None);
        assert_eq!(native_item_for("devtools"), None);
    }
}
