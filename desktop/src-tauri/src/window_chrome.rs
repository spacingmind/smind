//! Main-window chrome (desktop-native-feel D1) and the no-white-flash
//! launch sequence (D2.1).
//!
//! * macOS: overlay title bar, hidden title, traffic lights placed inside
//!   the UI's own 48px header row, sidebar vibrancy behind a transparent
//!   webview.
//! * Windows/Linux: undecorated; the UI draws its own caption buttons
//!   (`DesktopWindowControls`). Opaque -- no Mica/Acrylic yet (see the
//!   plan's Validation: untestable from the macOS dev box).
//! * Every platform: the window is created hidden with the theme's
//!   surface colour as its background, and shown once the bundled UI
//!   reports it has painted (`window_ready`) or after
//!   [`READY_FALLBACK`], whichever comes first, so a broken UI still
//!   surfaces a window.
//!
//! The window-control permissions the UI uses (minimize, toggle-maximize,
//! close, start-dragging, is-maximized, is-fullscreen) are Tauri core
//! `core:window:allow-*` grants in `capabilities/proxy.json`; `close`
//! raises `CloseRequested`, so the drawn close button reuses the
//! hide-to-tray handler in `lib.rs` rather than quitting.

use std::path::PathBuf;
use std::sync::Mutex;
use std::time::Duration;

use tauri::utils::config::Color;
use tauri::webview::WebviewWindowBuilder;
use tauri::{AppHandle, Manager, Runtime, State, Theme};

use crate::MAIN_WINDOW;

/// The smallest the main window can be (logical pixels). The width stays above the web UI's mobile breakpoint
/// (768 px, `hooks/use-mobile.ts`): below it the sidebar becomes a sheet and the header loses the drawn window
/// chrome, which is not a layout a desktop window should fall into. The height keeps the composer, the header
/// and a few rows of the sidebar on screen.
pub const MIN_WINDOW: (f64, f64) = (800.0, 520.0);

/// `Some(size)` when a logical size is below [`MIN_WINDOW`], the size to put the window back to. The OS enforces
/// the minimum while the user drags, but a size saved by a build that had no minimum is restored by the
/// window-state plugin and may be smaller; the shell applies this on every resize so it is corrected at once.
pub fn clamp_to_minimum(width: f64, height: f64) -> Option<(f64, f64)> {
    if width >= MIN_WINDOW.0 && height >= MIN_WINDOW.1 {
        return None;
    }
    Some((width.max(MIN_WINDOW.0), height.max(MIN_WINDOW.1)))
}

/// How long a window may stay hidden waiting for the UI's "painted"
/// signal before it is shown anyway.
pub const READY_FALLBACK: Duration = Duration::from_secs(3);

/// Traffic-light cluster origin on macOS, in logical px from the
/// window's top-left. tao grows the title-bar container by `y` and leaves
/// the buttons at its bottom, so the button centre lands ~2px above `y`:
/// y = 26 centres them on the 48px (`h-12`) header row. Measured on macOS
/// (see the plan's Validation).
#[cfg(target_os = "macos")]
const TRAFFIC_LIGHT_POSITION: (f64, f64) = (16.0, 26.0);

const THEME_FILE: &str = "window-theme.txt";

/// The two surface colours the window can be painted with before the UI
/// has loaded. They mirror `--background` in `web/packages/ui/src/index.css`
/// (`surface_colors_match_index_css` keeps them honest).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Surface {
    Light,
    Dark,
}

impl Surface {
    pub const LIGHT_RGB: (u8, u8, u8) = (0xf8, 0xf8, 0xf8);
    pub const DARK_RGB: (u8, u8, u8) = (0x16, 0x16, 0x16);

    pub fn parse(raw: &str) -> Option<Self> {
        match raw.trim() {
            "light" => Some(Self::Light),
            "dark" => Some(Self::Dark),
            _ => None,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Light => "light",
            Self::Dark => "dark",
        }
    }

    pub fn rgb(self) -> (u8, u8, u8) {
        match self {
            Self::Light => Self::LIGHT_RGB,
            Self::Dark => Self::DARK_RGB,
        }
    }

    fn color(self) -> Color {
        let (r, g, b) = self.rgb();
        Color(r, g, b, 255)
    }
}

/// What a signal means for the hidden window.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Signal {
    /// The bundled UI reported its first paint.
    Ready,
    /// [`READY_FALLBACK`] elapsed.
    Timeout,
    /// The user closed (hid) the window before it was ever shown.
    Dismissed,
}

/// The show-on-ready state machine, kept free of any window handle so it
/// is unit-testable: the first `Ready`/`Timeout` wins and says "show
/// now"; everything after, and anything after `Dismissed`, says nothing.
#[derive(Default)]
pub struct ShowGate {
    settled: Mutex<bool>,
}

impl ShowGate {
    /// Returns true exactly once: when the window should be shown now.
    pub fn signal(&self, signal: Signal) -> bool {
        let mut settled = self.settled.lock().unwrap();
        if *settled {
            return false;
        }
        *settled = true;
        signal != Signal::Dismissed
    }
}

fn theme_path(app: &AppHandle<impl Runtime>) -> Option<PathBuf> {
    app.path()
        .app_config_dir()
        .ok()
        .map(|dir| dir.join(THEME_FILE))
}

/// The surface the last session ended on. A first launch has no record;
/// dark is the guess (the window is hidden until the UI paints, so the
/// guess is only ever seen if the UI never does).
pub fn load_surface(app: &AppHandle<impl Runtime>) -> Surface {
    theme_path(app)
        .and_then(|path| std::fs::read_to_string(path).ok())
        .and_then(|raw| Surface::parse(&raw))
        .unwrap_or(Surface::Dark)
}

fn save_surface(app: &AppHandle<impl Runtime>, surface: Surface) {
    let Some(path) = theme_path(app) else { return };
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    let _ = std::fs::write(path, surface.as_str());
}

/// Applies the per-platform window chrome to the main window's builder.
pub fn configure<'a, R: Runtime, M: Manager<R>>(
    builder: WebviewWindowBuilder<'a, R, M>,
    surface: Surface,
) -> WebviewWindowBuilder<'a, R, M> {
    // D2.1: hidden with a matching background until the UI has painted.
    let builder = builder
        .visible(false)
        .background_color(surface.color())
        .min_inner_size(MIN_WINDOW.0, MIN_WINDOW.1);

    #[cfg(target_os = "macos")]
    let builder = {
        use tauri::window::{Effect, EffectState, EffectsBuilder};
        use tauri::{LogicalPosition, TitleBarStyle};
        builder
            .title_bar_style(TitleBarStyle::Overlay)
            .hidden_title(true)
            .traffic_light_position(LogicalPosition::new(
                TRAFFIC_LIGHT_POSITION.0,
                TRAFFIC_LIGHT_POSITION.1,
            ))
            // Vibrancy needs a transparent window and webview; the UI
            // paints every surface except the sidebar opaquely.
            .transparent(true)
            .effects(
                EffectsBuilder::new()
                    .effect(Effect::Sidebar)
                    .state(EffectState::Active)
                    .build(),
            )
    };

    #[cfg(not(target_os = "macos"))]
    let builder = builder.decorations(false);

    builder
}

/// Shows the main window. Used by the ready signal and its fallback.
fn reveal<R: Runtime>(app: &AppHandle<R>) {
    if let Some(win) = app.get_webview_window(MAIN_WINDOW) {
        let _ = win.show();
        let _ = win.set_focus();
    }
}

/// Arms the fallback: if the UI never reports a paint, show the window
/// anyway after [`READY_FALLBACK`].
pub fn arm_fallback<R: Runtime>(app: &AppHandle<R>) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(READY_FALLBACK).await;
        if app.state::<ShowGate>().signal(Signal::Timeout) {
            reveal(&app);
        }
    });
}

/// The user closed the window to the tray before it ever appeared: do
/// not let a late ready signal pop it back up.
pub fn dismissed<R: Runtime>(app: &AppHandle<R>) {
    app.state::<ShowGate>().signal(Signal::Dismissed);
}

/// The bundled UI has painted its first frame. `main` goes through the launch gate; a task window (created hidden
/// like `main`) is simply shown. Either way it acts on the CALLING window, never on another one.
#[tauri::command]
pub fn window_ready(app: AppHandle, window: tauri::Window, gate: State<'_, ShowGate>) {
    if window.label() != MAIN_WINDOW {
        if let Some(win) = app.get_webview_window(window.label()) {
            // Only the first paint shows it: a reload's repeated signal must not grab focus again.
            if !win.is_visible().unwrap_or(false) {
                let _ = win.show();
                let _ = win.set_focus();
            }
        }
        return;
    }
    if gate.signal(Signal::Ready) {
        reveal(&app);
    }
}

/// Fallback for a task window whose UI never reports a paint: show it anyway after [`READY_FALLBACK`].
pub fn arm_task_fallback<R: Runtime>(app: &AppHandle<R>, label: String) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(READY_FALLBACK).await;
        if let Some(win) = app.get_webview_window(&label) {
            if !win.is_visible().unwrap_or(true) {
                let _ = win.show();
            }
        }
    });
}

/// The window an app-menu action should act on: the focused one, else `main`.
pub fn focused_or_main<R: Runtime>(app: &AppHandle<R>) -> Option<tauri::WebviewWindow<R>> {
    app.webview_windows()
        .into_values()
        .find(|w| w.is_focused().unwrap_or(false))
        .or_else(|| app.get_webview_window(MAIN_WINDOW))
}

/// Mirrors the UI's theme into the window: the native theme (macOS
/// vibrancy and traffic-light appearance, WebView2/GTK chrome) follows
/// the preference, and the resolved surface is remembered as the next
/// launch's pre-paint background.
#[tauri::command]
pub fn window_set_theme(
    app: AppHandle,
    window: tauri::Window,
    preference: String,
    resolved: String,
) -> Result<(), String> {
    let surface = Surface::parse(&resolved)
        .ok_or_else(|| format!("window_set_theme: unknown resolved theme {resolved:?}"))?;
    let theme = match preference.as_str() {
        "light" => Some(Theme::Light),
        "dark" => Some(Theme::Dark),
        "system" => None,
        other => return Err(format!("window_set_theme: unknown preference {other:?}")),
    };
    save_surface(&app, surface);
    // The calling window (main or a task window), never another one.
    if let Some(win) = app.get_webview_window(window.label()) {
        win.set_theme(theme).map_err(|e| e.to_string())?;
        win.set_background_color(Some(surface.color()))
            .map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn window_shown_after_ready_or_timeout() {
        // Ready first: show, and the later timeout is a no-op.
        let gate = ShowGate::default();
        assert!(gate.signal(Signal::Ready));
        assert!(!gate.signal(Signal::Timeout));
        assert!(!gate.signal(Signal::Ready));

        // No ready signal: the timeout shows it, and a late ready doesn't
        // show it a second time.
        let gate = ShowGate::default();
        assert!(gate.signal(Signal::Timeout));
        assert!(!gate.signal(Signal::Ready));

        // Closed to the tray before it ever appeared: neither signal
        // brings it back.
        let gate = ShowGate::default();
        assert!(!gate.signal(Signal::Dismissed));
        assert!(!gate.signal(Signal::Ready));
        assert!(!gate.signal(Signal::Timeout));
    }

    #[test]
    fn fallback_is_at_most_three_seconds() {
        assert!(READY_FALLBACK <= Duration::from_secs(3));
    }

    #[test]
    fn surface_round_trips_and_rejects_garbage() {
        for s in [Surface::Light, Surface::Dark] {
            assert_eq!(Surface::parse(s.as_str()), Some(s));
        }
        assert_eq!(Surface::parse(" dark\n"), Some(Surface::Dark));
        assert_eq!(Surface::parse("system"), None);
        assert_eq!(Surface::parse(""), None);
    }

    #[test]
    fn surface_colors_match_index_css() {
        let css = include_str!("../../../web/packages/ui/src/index.css");
        let hex = |(r, g, b): (u8, u8, u8)| format!("--background: #{r:02x}{g:02x}{b:02x};");
        assert!(
            css.contains(&hex(Surface::LIGHT_RGB)),
            "light --background in index.css no longer matches Surface::LIGHT_RGB"
        );
        assert!(
            css.contains(&hex(Surface::DARK_RGB)),
            "dark --background in index.css no longer matches Surface::DARK_RGB"
        );
    }
}

#[cfg(test)]
mod min_size_tests {
    use super::*;

    #[test]
    fn the_minimum_width_keeps_the_desktop_layout() {
        // hooks/use-mobile.ts flips to the sheet sidebar below 768 CSS px.
        assert!(MIN_WINDOW.0 >= 768.0);
        assert!(MIN_WINDOW.1 >= 480.0);
    }

    #[test]
    fn clamp_to_minimum_table() {
        let (w, h) = MIN_WINDOW;
        type Case = (f64, f64, Option<(f64, f64)>);
        let cases: &[Case] = &[
            (1280.0, 800.0, None),
            (w, h, None),
            (w - 1.0, 800.0, Some((w, 800.0))),
            (1280.0, h - 1.0, Some((1280.0, h))),
            (300.0, 200.0, Some((w, h))),
            (0.0, 0.0, Some((w, h))),
        ];
        for (cw, ch, want) in cases {
            assert_eq!(clamp_to_minimum(*cw, *ch), *want, "{cw}x{ch}");
        }
    }

    #[test]
    fn the_main_window_builder_sets_the_minimum() {
        let src = include_str!("window_chrome.rs");
        let configure = src
            .split("pub fn configure<")
            .nth(1)
            .expect("configure exists");
        let body = configure.split("\n}\n").next().expect("configure body");
        assert!(
            body.contains(".min_inner_size(MIN_WINDOW.0, MIN_WINDOW.1)"),
            "configure must set the window minimum"
        );
    }

    #[test]
    fn the_shell_applies_the_clamp_on_resize() {
        let src = include_str!("lib.rs");
        let handler = src
            .split("tauri::WindowEvent::Resized(")
            .nth(1)
            .expect("the main window handles Resized");
        assert!(
            handler
                .split("_ => {}")
                .next()
                .unwrap()
                .contains("window_chrome::clamp_to_minimum("),
            "the Resized handler must call clamp_to_minimum"
        );
    }
}

#[cfg(test)]
mod multi_window_tests {
    fn body<'a>(src: &'a str, start: &str) -> &'a str {
        let rest = src
            .split(start)
            .nth(1)
            .unwrap_or_else(|| panic!("{start} exists"));
        rest.split("\n}\n").next().unwrap()
    }

    #[test]
    fn window_commands_use_the_calling_window() {
        let src = include_str!("window_chrome.rs");
        let ready = body(src, "pub fn window_ready(");
        assert!(
            ready.contains("window.label()"),
            "window_ready must read the caller's label"
        );
        let theme = body(src, "pub fn window_set_theme(");
        assert!(
            theme.contains("window.label()"),
            "window_set_theme must act on the calling window"
        );
        assert!(
            !theme.contains("get_webview_window(MAIN_WINDOW)"),
            "window_set_theme must not target main"
        );
    }

    #[test]
    fn app_menu_actions_target_the_focused_window_and_zoom_every_window() {
        let menu = include_str!("menu.rs");
        let native = body(menu, "fn handle_native(");
        assert!(native.contains("focused_or_main(app)"));
        assert!(native.contains("main.hide()"), "Hide stays on main");
        let zoom = body(menu, "fn apply_zoom(");
        assert!(
            zoom.contains("webview_windows()"),
            "zoom applies to every open window"
        );
    }

    #[test]
    fn app_wide_menu_events_are_never_broadcast() {
        let menu = include_str!("menu.rs");
        let handler = body(menu, "pub fn handle_event(");
        assert!(handler.contains("emit_to(target.as_str(), \"menu-action\""));
        assert!(
            !handler.contains("app.emit(\"menu-action\""),
            "a broadcast would run the action in every window"
        );
        assert!(!handler.contains("app.emit(\"app-update-check-requested\""));
    }
}
