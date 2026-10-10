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
use tauri::tray::{TrayIcon, TrayIconBuilder};
use tauri::{AppHandle, Wry};
use url::Url;

use smind_daemon_client::{attention, Attention, WorkspaceCache};

use crate::lifecycle::{self, Badge, WindowBadge};
use crate::notify::{self, ClickContext};
use crate::show_main;

const ID_ATTENTION: &str = "attention";
const ID_OPEN: &str = "open";
const ID_QUIT: &str = "quit";

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
        .show_menu_on_left_click(true)
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

    pub fn on_reconnected(&self) {
        let count = {
            let mut state = self.state.lock().unwrap();
            state.reset();
            state.count()
        };
        self.refresh(count);
    }

    fn refresh(&self, count: usize) {
        if let Some(ui) = &self.ui {
            let label = attention::label(count);
            let _ = ui.item.set_text(&label);
            let _ = ui.item.set_enabled(count > 0);
            let _ = ui.icon.set_tooltip(Some(&label));
        }
        // Windows has no cross-platform badge API (only a per-window
        // overlay icon image, which this quick pass doesn't have an
        // asset for); macOS shows the Dock badge, Linux a launcher badge
        // where the desktop environment supports it.
        self.badge.update(count);
    }
}
