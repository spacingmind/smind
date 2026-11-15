//! Task-window pool, registry and URL builder (desktop-multi-window Part 1).
//!
//! Rust owns every window name: `window_open_task` takes an integer task id,
//! maps it onto the fixed label pool `task-1..=task-8`, and builds each task
//! window's URL itself from the proxy origin and the per-launch secret. No
//! page string ever reaches a label, a capability glob or a URL.
//!
//! One window per task: opening a task that already has a window focuses it.
//! Task windows use the standard chrome, deny `window.open`, keep the
//! navigation guard, and are DESTROYED on close (the hide-to-tray handler is
//! `main`'s alone); closing frees the pool slot. See
//! `docs/plans/active/desktop-multi-window.md` and the ADR-0013 amendment.

use tauri::webview::{NewWindowResponse, WebviewWindowBuilder};
use tauri::{AppHandle, Manager, State, Window, WindowEvent};

use crate::state::DesktopState;

pub const TASK_LABEL_PREFIX: &str = "task-";

pub const MAX_TASK_WINDOWS: usize = 8;

/// The label of pool slot `index`: `task-1` for `0`..`task-8` for `7`.
/// Callers only pass indexes below `MAX_TASK_WINDOWS`, so this never panics.
pub fn pool_label(index: usize) -> String {
    format!("task-{}", index + 1)
}

/// The inverse of [`pool_label`]: the slot index of an exact pool label, or
/// `None` for anything else (`main`, `task-0`, `task-9`, `task-01`, any
/// padding or trailing characters).
pub fn label_index(label: &str) -> Option<usize> {
    let digits = label.strip_prefix(TASK_LABEL_PREFIX)?;
    let well_formed = !digits.is_empty()
        && digits.bytes().all(|b| b.is_ascii_digit())
        && !digits.starts_with('0');
    if !well_formed {
        return None;
    }
    match digits.parse::<usize>() {
        Ok(n) if (1..=MAX_TASK_WINDOWS).contains(&n) => Some(n - 1),
        _ => None,
    }
}

/// Ids come from the web as JavaScript numbers, so the range is the safe
/// integers `1..=2^53-1` and nothing else.
pub fn validate_task_id(id: i64) -> Result<i64, String> {
    const MAX_SAFE_ID: i64 = 9_007_199_254_740_991; // 2^53 - 1
    if (1..=MAX_SAFE_ID).contains(&id) {
        Ok(id)
    } else {
        Err("task id out of range".to_string())
    }
}

/// The URL a task window loads: the proxy origin, the per-launch secret as
/// the only query pair, and the task route as the fragment. Nothing a page
/// supplies is concatenated in.
pub fn task_url(proxy_origin: &url::Url, secret: &str, task_id: i64) -> url::Url {
    let mut url = proxy_origin.clone();
    url.set_path("/");
    url.set_query(None);
    url.query_pairs_mut().append_pair("k", secret).finish();
    url.set_fragment(Some(&format!("/task/{task_id}")));
    url
}

#[derive(Debug, PartialEq, Eq)]
pub enum Acquired {
    Existing(String),
    New(String),
}

/// Slot registry for the fixed task-window pool: only Rust moves slots, a
/// task id always maps to the same label until its window is closed.
#[derive(Default)]
pub struct TaskWindows {
    slots: std::sync::Mutex<[Option<i64>; MAX_TASK_WINDOWS]>,
}

impl TaskWindows {
    /// Find or allocate the window for `task_id`: the existing label if the
    /// task already has a window, the lowest free label otherwise.
    pub fn acquire(&self, task_id: i64) -> Result<Acquired, String> {
        validate_task_id(task_id)?;
        let mut slots = self.slots.lock().unwrap_or_else(|e| e.into_inner());
        if let Some(i) = slots.iter().position(|slot| *slot == Some(task_id)) {
            return Ok(Acquired::Existing(pool_label(i)));
        }
        match slots.iter().position(|slot| slot.is_none()) {
            Some(i) => {
                slots[i] = Some(task_id);
                Ok(Acquired::New(pool_label(i)))
            }
            None => Err(format!(
                "too many task windows open ({MAX_TASK_WINDOWS}); close one first"
            )),
        }
    }

    /// Free the slot of a closed window. Any other label does nothing.
    pub fn release(&self, label: &str) {
        if let Some(i) = label_index(label) {
            self.slots.lock().unwrap_or_else(|e| e.into_inner())[i] = None;
        }
    }

    /// The label of the window currently holding `task_id`, if any.
    #[allow(dead_code)] // read by tests and by later per-task routing
    pub fn label_for(&self, task_id: i64) -> Option<String> {
        let slots = self.slots.lock().unwrap_or_else(|e| e.into_inner());
        slots
            .iter()
            .position(|slot| *slot == Some(task_id))
            .map(pool_label)
    }
}

/// Brings an existing task window forward. It touches only that window.
fn focus_window(win: &tauri::WebviewWindow) {
    let _ = win.unminimize();
    let _ = win.show();
    let _ = win.set_focus();
}

fn create_task_window(
    app: &AppHandle,
    desktop: &DesktopState,
    label: &str,
    task_id: i64,
) -> Result<(), String> {
    let url = task_url(&desktop.proxy_url, &desktop.proxy.secret, task_id);
    let nav_origin = desktop.proxy_url.origin();
    let builder = WebviewWindowBuilder::new(app, label, tauri::WebviewUrl::External(url))
        .title("sMind")
        .inner_size(1100.0, 740.0)
        // Same as main: OS file drops reach the page as HTML5 drops (ADR-0038).
        .disable_drag_drop_handler()
        // Same navigation guard as main: only the loopback origin navigates; links open in the OS browser.
        .on_navigation(move |url| {
            if url.origin() == nav_origin {
                true
            } else {
                let _ = tauri_plugin_opener::open_url(url.as_str(), None::<&str>);
                false
            }
        })
        // No unmanaged windows: `window.open` and target=_blank are refused.
        .on_new_window(|_url, _features| NewWindowResponse::Deny);
    let win = crate::window_chrome::configure(builder, crate::window_chrome::load_surface(app))
        .build()
        .map_err(|e| e.to_string())?;
    let _ = win.set_zoom(crate::zoom_store::load(app));
    // Task windows are destroyed on close (no CloseRequested handler here); the slot is freed when they are gone.
    let release_app = app.clone();
    let release_label = label.to_string();
    win.on_window_event(move |event| {
        if let WindowEvent::Destroyed = event {
            if let Some(registry) = release_app.try_state::<TaskWindows>() {
                registry.release(&release_label);
            }
        }
    });
    crate::window_chrome::arm_task_fallback(app, label.to_string());
    Ok(())
}

/// Opens (or focuses) the window for `task_id`. The page supplies one integer: Rust picks the label from the pool and
/// builds the URL. Returns the window's label.
#[tauri::command]
pub fn window_open_task(
    app: AppHandle,
    _window: Window,
    registry: State<'_, TaskWindows>,
    desktop: State<'_, DesktopState>,
    task_id: i64,
) -> Result<String, String> {
    let (label, is_new) = match registry.acquire(task_id)? {
        Acquired::Existing(label) => (label, false),
        Acquired::New(label) => (label, true),
    };
    if !is_new {
        if let Some(win) = app.get_webview_window(&label) {
            focus_window(&win);
            return Ok(label);
        }
        // The slot claims a window the OS no longer has: rebuild it in the same slot.
    }
    if let Err(e) = create_task_window(&app, &desktop, &label, task_id) {
        registry.release(&label);
        return Err(e);
    }
    Ok(label)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn new(label: &str) -> Acquired {
        Acquired::New(label.to_string())
    }

    fn existing(label: &str) -> Acquired {
        Acquired::Existing(label.to_string())
    }

    #[test]
    fn pool_labels_are_fixed_and_never_main() {
        for i in 0..MAX_TASK_WINDOWS {
            let label = pool_label(i);
            assert_eq!(label, format!("task-{}", i + 1));
            assert_ne!(label, "main");
            assert_eq!(label_index(&label), Some(i));
        }
    }

    #[test]
    fn label_index_rejects_everything_else() {
        for bad in [
            "main", "task-0", "task-9", "task-01", "task-1 ", " task-1", "task-", "TASK-1",
            "task-1x", "",
        ] {
            assert_eq!(label_index(bad), None, "{bad:?} must not be a pool label");
        }
    }

    #[test]
    fn task_id_validation_table() {
        for bad in [0i64, -1, i64::MIN, 1 << 53] {
            assert_eq!(
                validate_task_id(bad),
                Err("task id out of range".to_string())
            );
        }
        for good in [1i64, 42, (1 << 53) - 1] {
            assert_eq!(validate_task_id(good), Ok(good));
        }
    }

    #[test]
    fn task_url_is_built_from_the_proxy_origin_and_an_integer() {
        let origin: url::Url = "http://127.0.0.1:5000".parse().unwrap();
        let url = task_url(&origin, "sec", 7);
        assert_eq!(url.as_str(), "http://127.0.0.1:5000/?k=sec#/task/7");

        // The secret is the only thing from the caller that reaches the URL,
        // as an encoded query value: an unsafe secret changes the query and
        // nothing else.
        let hostile = "&#/ x";
        let url = task_url(&origin, hostile, 7);
        assert_eq!(url.query(), Some("k=%26%23%2F+x"));
        assert_eq!(
            url.query_pairs()
                .map(|(k, v)| (k.into_owned(), v.into_owned()))
                .collect::<Vec<_>>(),
            vec![("k".to_string(), hostile.to_string())]
        );
        assert_eq!(url.fragment(), Some("/task/7"));
        assert_eq!(url.host_str(), Some("127.0.0.1"));
    }

    #[test]
    fn opening_the_same_task_again_reuses_its_window() {
        let pool = TaskWindows::default();
        assert_eq!(pool.acquire(5), Ok(new("task-1")));
        assert_eq!(pool.acquire(5), Ok(existing("task-1")));
        assert_eq!(pool.label_for(5), Some("task-1".to_string()));
    }

    #[test]
    fn lowest_free_slot_is_used_first() {
        let pool = TaskWindows::default();
        assert_eq!(pool.acquire(1), Ok(new("task-1")));
        assert_eq!(pool.acquire(2), Ok(new("task-2")));
        assert_eq!(pool.acquire(3), Ok(new("task-3")));
        pool.release("task-2");
        assert_eq!(pool.acquire(9), Ok(new("task-2")));
    }

    #[test]
    fn pool_refuses_beyond_the_cap_with_a_clear_error() {
        let pool = TaskWindows::default();
        for id in 1..=(MAX_TASK_WINDOWS as i64) {
            assert_eq!(pool.acquire(id), Ok(new(&pool_label(id as usize - 1))));
        }
        let err = pool.acquire(9).unwrap_err();
        assert!(err.contains("too many task windows open (8)"), "{err}");
        assert_eq!(pool.acquire(3), Ok(existing("task-3")));
    }

    #[test]
    fn closing_a_window_releases_its_slot() {
        let pool = TaskWindows::default();
        for id in 1..=(MAX_TASK_WINDOWS as i64) {
            pool.acquire(id).unwrap();
        }
        pool.release("task-4");
        assert_eq!(pool.acquire(100), Ok(new("task-4")));
        // Non-pool labels release nothing.
        pool.release("main");
        pool.release("task-99");
        assert_eq!(pool.label_for(100), Some("task-4".to_string()));
        for id in 1..=(MAX_TASK_WINDOWS as i64) {
            if id != 4 {
                assert_eq!(
                    pool.label_for(id),
                    Some(format!("task-{id}")),
                    "task {id} must keep its window"
                );
            }
        }
    }

    #[test]
    fn invalid_ids_never_take_a_slot() {
        let pool = TaskWindows::default();
        assert!(pool.acquire(0).is_err());
        assert!(pool.acquire(-5).is_err());
        assert_eq!(pool.acquire(1), Ok(new("task-1")));
    }

    fn body<'a>(src: &'a str, start: &str) -> &'a str {
        let rest = src
            .split(start)
            .nth(1)
            .unwrap_or_else(|| panic!("{start} exists"));
        rest.split("\n}\n").next().unwrap()
    }

    #[test]
    fn task_window_builder_denies_new_windows_and_guards_navigation() {
        let src = include_str!("windows.rs");
        let create = body(src, "fn create_task_window(");
        assert!(create.contains(".on_new_window(|_url, _features| NewWindowResponse::Deny)"));
        assert!(create.contains(".on_navigation("));
        assert!(create.contains("url.origin() == nav_origin"));
        assert!(create.contains(".disable_drag_drop_handler()"));
        assert!(create.contains("window_chrome::configure("));
    }

    #[test]
    fn the_destroyed_handler_releases_the_slot() {
        let create = body(include_str!("windows.rs"), "fn create_task_window(");
        assert!(create.contains("WindowEvent::Destroyed"));
        assert!(create.contains("registry.release(&release_label)"));
        assert!(
            !create.contains("prevent_close"),
            "task windows destroy on close"
        );
    }

    #[test]
    fn focus_only_the_requested_task_window() {
        let src = include_str!("windows.rs");
        let command = body(src, "pub fn window_open_task(");
        assert!(
            !command.contains("MAIN_WINDOW"),
            "the command never touches main"
        );
        assert!(command.contains("get_webview_window(&label)"));
        assert!(
            command.contains("focus_window(&win)"),
            "an existing task window is brought forward"
        );
        let before_create = command.split("create_task_window(").next().unwrap();
        assert!(
            before_create.contains("focus_window(&win)"),
            "focus happens instead of creating a second window"
        );
        let focus = body(src, "fn focus_window(");
        assert!(focus.contains("win.set_focus()"));
    }

    #[test]
    fn the_command_takes_an_integer_and_nothing_else_from_the_page() {
        let src = include_str!("windows.rs");
        let sig = src
            .split("pub fn window_open_task(")
            .nth(1)
            .unwrap()
            .split(") -> Result")
            .next()
            .unwrap();
        assert!(sig.contains("task_id: i64"));
        for forbidden in ["url:", "label:", "String", "path", "title"] {
            assert!(
                !sig.contains(forbidden),
                "window_open_task must not take {forbidden}"
            );
        }
    }

    #[test]
    fn main_window_still_hides_on_close_and_task_windows_do_not() {
        let lib = include_str!("lib.rs");
        assert!(
            lib.contains("api.prevent_close();"),
            "main keeps hide-to-tray"
        );
        assert!(!include_str!("windows.rs")
            .split("#[cfg(test)]")
            .next()
            .unwrap()
            .contains("prevent_close"));
    }

    #[test]
    fn a_task_window_never_writes_the_main_layout() {
        let layout = include_str!("window_layout.rs");
        let save = body(layout, "pub fn save_main(");
        assert!(save.contains("get_webview_window(MAIN_WINDOW)"));
        assert!(
            save.matches("MAIN_WINDOW").count() >= 2,
            "both the lookup and the stored label are main"
        );
        assert!(!save.contains("label()"));
    }
}
