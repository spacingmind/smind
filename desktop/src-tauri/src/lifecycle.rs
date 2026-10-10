//! desktop-macos-app M3: the macOS app lifecycle -- Dock, not tray.
//!
//! macOS gets no menu-bar tray icon (the Dock is the app's presence): the
//! window's close button only hides it (lib.rs), clicking the Dock icon
//! brings it back (`RunEvent::Reopen`), and the pending-approvals count
//! shows as the Dock badge. Windows and Linux keep the tray exactly as
//! before. The decisions live here as small pure pieces so they're
//! testable without a webview.

use std::sync::Mutex;

use tauri::{AppHandle, Manager, RunEvent};

use crate::MAIN_WINDOW;

/// tray_enabled is the tray-setup gate, a pure function of the target OS
/// (`std::env::consts::OS`): every platform but macOS builds the tray.
pub fn tray_enabled(target_os: &str) -> bool {
    target_os != "macos"
}

/// BadgeSetter is the one OS call the attention badge needs: show `count`,
/// or clear the badge on `None`.
pub trait BadgeSetter {
    /// Returns whether the OS accepted it: a failed set is retried on the
    /// next update/`reapply` instead of being remembered as shown.
    fn set(&self, count: Option<i64>) -> bool;
}

/// Badge drives a `BadgeSetter` from the attention count, touching the OS
/// only when the shown value actually changes: it sets the count while it
/// is above zero and clears once at zero (never clearing a badge that was
/// never shown).
pub struct Badge<S: BadgeSetter> {
    setter: S,
    shown: Mutex<usize>,
}

impl<S: BadgeSetter> Badge<S> {
    pub fn new(setter: S) -> Self {
        Self { setter, shown: Mutex::new(0) }
    }

    pub fn update(&self, count: usize) {
        let mut shown = self.shown.lock().unwrap();
        if *shown == count {
            return;
        }
        if self.setter.set(if count > 0 { Some(count as i64) } else { None }) {
            *shown = count;
        }
    }

    /// Sets the currently shown count again. Windows drops a taskbar
    /// overlay when the window's taskbar button is re-created (hidden to
    /// the tray, then shown), so the app calls this when the window gains
    /// focus. A no-op while nothing is shown.
    pub fn reapply(&self) {
        let shown = *self.shown.lock().unwrap();
        if shown > 0 {
            self.setter.set(Some(shown as i64));
        }
    }
}

/// WindowBadge sets the badge through the main window -- on macOS that is
/// the Dock tile (the label stays up while the window is hidden); on Linux
/// the launcher badge where the desktop supports it. Windows has no
/// numeric badge API, so it gets a rendered overlay icon on the taskbar
/// button instead (D4.4), cleared at zero.
pub struct WindowBadge(pub AppHandle);

impl BadgeSetter for WindowBadge {
    fn set(&self, count: Option<i64>) -> bool {
        let Some(win) = self.0.get_webview_window(MAIN_WINDOW) else {
            log::info!("badge set to {count:?}: no main window");
            return false;
        };
        #[cfg(target_os = "windows")]
        let res = {
            let icon = count.map(|n| {
                let (rgba, w, h) = crate::overlay_icon::overlay_icon_rgba(n.max(0) as usize);
                tauri::image::Image::new_owned(rgba, w, h)
            });
            win.set_overlay_icon(icon)
        };
        #[cfg(not(target_os = "windows"))]
        let res = win.set_badge_count(count);
        log::info!("badge set to {count:?}: {res:?}");
        res.is_ok()
    }
}

/// handle_run_event is the app-level event hook (`App::run`'s callback).
/// Only macOS emits `Reopen`: the Dock icon was clicked (or the app was
/// re-launched from Finder) while running -- show the main window again,
/// even when it was hidden by the close button or minimized.
pub fn handle_run_event(app: &AppHandle, event: RunEvent) {
    #[cfg(target_os = "macos")]
    if let RunEvent::Reopen { has_visible_windows, .. } = event {
        log::info!("dock reopen (has_visible_windows={has_visible_windows}): showing the main window");
        if let Some(win) = app.get_webview_window(MAIN_WINDOW) {
            let _ = win.unminimize();
            let _ = win.show();
            let _ = win.set_focus();
        }
    }
    #[cfg(not(target_os = "macos"))]
    let _ = (app, event);
}

#[cfg(test)]
mod tests {
    use std::cell::RefCell;

    use super::*;

    #[derive(Default)]
    struct Recorder(RefCell<Vec<Option<i64>>>);

    impl BadgeSetter for &Recorder {
        fn set(&self, count: Option<i64>) -> bool {
            self.0.borrow_mut().push(count);
            true
        }
    }

    /// Rejects the first `n` sets, like a window with no taskbar button yet.
    struct Flaky(RefCell<Vec<Option<i64>>>, std::cell::Cell<usize>);

    impl BadgeSetter for &Flaky {
        fn set(&self, count: Option<i64>) -> bool {
            self.0.borrow_mut().push(count);
            if self.1.get() > 0 {
                self.1.set(self.1.get() - 1);
                return false;
            }
            true
        }
    }

    #[test]
    fn badge_retries_after_a_failed_set_and_reapplies() {
        let flaky = Flaky(RefCell::default(), std::cell::Cell::new(1));
        let badge = Badge::new(&flaky);
        badge.update(2); // rejected: not recorded as shown
        badge.update(2); // retried, accepted
        badge.update(2); // unchanged: no OS call
        badge.reapply(); // window re-shown: set again
        badge.update(0);
        badge.reapply(); // nothing shown: no call
        assert_eq!(*flaky.0.borrow(), vec![Some(2), Some(2), Some(2), None]);
    }

    #[test]
    fn badge_follows_attention_count_macos() {
        let rec = Recorder::default();
        let badge = Badge::new(&rec);

        badge.update(0); // nothing shown yet: no OS call
        badge.update(3);
        badge.update(3); // unchanged: no OS call
        badge.update(0);
        badge.update(0);

        assert_eq!(*rec.0.borrow(), vec![Some(3), None]);
    }

    #[test]
    fn tray_not_built_on_macos() {
        assert!(!tray_enabled("macos"));
        assert!(tray_enabled("windows"));
        assert!(tray_enabled("linux"));
    }
}
