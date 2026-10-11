//! quick-wins AC4: the tray's "pending approvals" indicator -- a
//! tooltip, a first menu item (disabled and reading "No approvals
//! waiting" when there are none; otherwise enabled, reading the count,
//! and clickable to open the most recent waiting task), and, where the
//! OS supports it, a taskbar badge count. The count itself
//! (`smind_daemon_client::Attention`) is updated from `ClientEvent`s in
//! `lib.rs`; this module only renders it.
//!
//! On macOS there is no tray (desktop-macos-app M3): `Tray` keeps the
//! count and drives the Dock badge only; see `lifecycle`.

use std::sync::{Arc, Mutex};

use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIcon, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Wry};
use url::Url;

use smind_daemon_client::{attention, Attention, WorkspaceCache};

use crate::lifecycle::{self, Badge, WindowBadge};
use crate::notify::{self, ClickContext};
use crate::show_main;

const ID_ATTENTION: &str = "attention";
const ID_OPEN: &str = "open";
const ID_QUIT: &str = "quit";

/// What a tray icon click does. The menu is not in this enum: it is shown
/// by the OS on right-click (`show_menu_on_left_click(false)`).
#[derive(Debug, PartialEq, Eq)]
enum ClickAction {
    ShowWindow,
    Ignore,
}

/// A left-button release opens the window (like the Open item); every other
/// click event (press, right/middle button, enter/move/leave) is ignored.
fn click_action(button: MouseButton, state: MouseButtonState) -> ClickAction {
    match (button, state) {
        (MouseButton::Left, MouseButtonState::Up) => ClickAction::ShowWindow,
        _ => ClickAction::Ignore,
    }
}

/// The tray menu's item ids, in order (separator excluded).
const TRAY_MENU_IDS: [&str; 3] = [ID_ATTENTION, ID_OPEN, ID_QUIT];

pub struct Tray {
    state: Arc<Mutex<Attention>>,
    /// The tray icon and its first menu item -- `None` where there is no
    /// tray (macOS).
    ui: Option<TrayUi>,
    badge: Badge<WindowBadge>,
}

struct TrayUi {
    icon: TrayIcon<Wry>,
    item: MenuItem<Wry>,
}

pub fn build(app: &AppHandle, cache: WorkspaceCache, proxy_url: Url) -> tauri::Result<Arc<Tray>> {
    let state = Arc::new(Mutex::new(Attention::new()));
    let ui = if lifecycle::tray_enabled(std::env::consts::OS) {
        Some(build_ui(app, state.clone(), cache, proxy_url)?)
    } else {
        None
    };
    Ok(Arc::new(Tray { state, ui, badge: Badge::new(WindowBadge(app.clone())) }))
}

fn build_ui(app: &AppHandle, click_state: Arc<Mutex<Attention>>, cache: WorkspaceCache, proxy_url: Url) -> tauri::Result<TrayUi> {
    let item = MenuItem::with_id(app, ID_ATTENTION, attention::label(0), false, None::<&str>)?;
    let separator = PredefinedMenuItem::separator(app)?;
    let open = MenuItem::with_id(app, ID_OPEN, "Open", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, ID_QUIT, "Quit", true, None::<&str>)?;
    let tray_menu = Menu::with_items(app, &[&item, &separator, &open, &quit])?;

    let icon = TrayIconBuilder::with_id("main-tray")
        .icon(app.default_window_icon().unwrap().clone())
        .menu(&tray_menu)
        .tooltip(attention::label(0))
        .show_menu_on_left_click(false)
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click { button, button_state, .. } = event {
                if click_action(button, button_state) == ClickAction::ShowWindow {
                    show_main(tray.app_handle());
                }
            }
        })
        .on_menu_event(move |app, event| match event.id().as_ref() {
            ID_OPEN => show_main(app),
            ID_QUIT => app.exit(0),
            ID_ATTENTION => {
                let most_recent = click_state.lock().unwrap().most_recent_task();
                if let Some(task_id) = most_recent {
                    let ctx = ClickContext {
                        app: app.clone(),
                        cache: cache.clone(),
                        proxy_url: proxy_url.clone(),
                    };
                    notify::navigate_to_task(&ctx, task_id);
                }
            }
            _ => {}
        })
        .build(app)?;

    Ok(TrayUi { icon, item })
}

impl Tray {
    pub fn on_permission_pending(&self, task_id: i64, request_id: &str) {
        let count = {
            let mut state = self.state.lock().unwrap();
            state.on_pending(task_id, request_id);
            state.count()
        };
        self.refresh(count);
    }

    pub fn on_run_running(&self, task_id: i64) {
        let count = {
            let mut state = self.state.lock().unwrap();
            state.on_run_running(task_id);
            state.count()
        };
        self.refresh(count);
    }

    pub fn on_run_ended(&self, task_id: i64) {
        let count = {
            let mut state = self.state.lock().unwrap();
            state.on_run_ended(task_id);
            state.count()
        };
        self.refresh(count);
    }

    pub fn on_permission_resolved(&self, request_id: &str) {
        let count = {
            let mut state = self.state.lock().unwrap();
            state.on_permission_resolved(request_id);
            state.count()
        };
        self.refresh(count);
    }

    pub fn on_reconnected(&self) {
        let count = {
            let mut state = self.state.lock().unwrap();
            state.reset();
            state.count()
        };
        self.refresh(count);
    }

    /// Re-applies the shown badge (see `Badge::reapply`).
    pub fn reapply_badge(&self) {
        self.badge.reapply();
    }

    fn refresh(&self, count: usize) {
        if let Some(ui) = &self.ui {
            let label = attention::label(count);
            let _ = ui.item.set_text(&label);
            let _ = ui.item.set_enabled(count > 0);
            let _ = ui.icon.set_tooltip(Some(&label));
        }
        // macOS: Dock badge; Linux: launcher badge where the desktop
        // supports it; Windows: a rendered taskbar overlay icon
        // (`overlay_icon`).
        self.badge.update(count);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tray_left_click_shows_the_window_not_the_menu() {
        assert_eq!(click_action(MouseButton::Left, MouseButtonState::Up), ClickAction::ShowWindow);
        assert_eq!(click_action(MouseButton::Left, MouseButtonState::Down), ClickAction::Ignore);
        assert_eq!(click_action(MouseButton::Right, MouseButtonState::Up), ClickAction::Ignore);
        assert_eq!(click_action(MouseButton::Middle, MouseButtonState::Up), ClickAction::Ignore);
    }

    #[test]
    fn tray_menu_still_has_open_and_quit() {
        assert!(TRAY_MENU_IDS.contains(&ID_OPEN));
        assert!(TRAY_MENU_IDS.contains(&ID_QUIT));
    }
}
