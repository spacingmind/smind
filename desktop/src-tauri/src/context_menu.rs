//! DT2 (a): native context menus (docs/plans/active/desktop-native-feel.md).
//!
//! The bundled UI builds a menu MODEL (labels, opaque item ids, enabled flags,
//! separators, one level of submenu), sends it with the pointer position, and
//! gets the chosen item id back as a `context-menu-action` event. Rust pops a
//! native menu (`Menu::popup_at`) from that model and nothing else: the
//! webview can name no path, command or URL through this channel, and a
//! selection only ever returns the id the webview itself supplied.
//!
//! Every item id is namespaced per open (`ctx:<open>:<id>`) so a selection can
//! only reach the menu that is currently armed: a stale one (an earlier menu,
//! a duplicate, one past the timeout) is ignored, and opening a second menu
//! replaces the first. Tauri's menu API does not report a dismissal, so an
//! armed menu simply expires (`ARMED_TTL`) or is replaced.

use std::collections::HashSet;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use tauri::menu::{ContextMenu, Menu, MenuItemBuilder, PredefinedMenuItem, Submenu};
use tauri::{AppHandle, Emitter, LogicalPosition, Manager, State, Window};

use crate::MAIN_WINDOW;

/// The event the bundled UI listens to (`platform.ts` `onContextMenuAction`).
pub const ACTION_EVENT: &str = "context-menu-action";

const ID_PREFIX: &str = "ctx:";
const MAX_ENTRIES: usize = 64;
const MAX_ID_LEN: usize = 64;
const MAX_LABEL_CHARS: usize = 120;
const MAX_COORD: f64 = 100_000.0;
/// How long an opened menu can still deliver a selection.
const ARMED_TTL: Duration = Duration::from_secs(30);

fn yes() -> bool {
    true
}

/// One entry of a menu model. Mirrors `web/packages/ui/src/lib/menu-model.ts`.
#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
#[serde(tag = "kind", rename_all = "camelCase", deny_unknown_fields)]
pub enum MenuEntry {
    Item {
        id: String,
        label: String,
        #[serde(default = "yes")]
        enabled: bool,
    },
    Separator,
    Submenu {
        label: String,
        #[serde(default = "yes")]
        enabled: bool,
        children: Vec<MenuEntry>,
    },
}

/// What a selection delivers to the renderer.
#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct ContextMenuSelection {
    pub open: u64,
    pub id: String,
}

struct Armed {
    open: u64,
    ids: HashSet<String>,
    at: Instant,
}

#[derive(Default)]
pub struct ContextMenuState {
    inner: Mutex<Inner>,
}

#[derive(Default)]
struct Inner {
    next: u64,
    armed: Option<Armed>,
}

fn valid_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= MAX_ID_LEN
        && id
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '.' | '_' | ':' | '-'))
}

fn valid_label(label: &str) -> bool {
    !label.trim().is_empty()
        && label.chars().count() <= MAX_LABEL_CHARS
        && !label.chars().any(char::is_control)
}

/// Checks a menu model: size, depth, id shape and uniqueness, label shape, and
/// that there is at least one actionable item (a menu of separators or empty
/// submenus is a bug in the caller, not something to show).
pub fn validate(entries: &[MenuEntry]) -> Result<(), String> {
    let mut seen = HashSet::new();
    let mut count = 0usize;
    let mut items = 0usize;
    walk(entries, 0, &mut seen, &mut count, &mut items)?;
    if items == 0 {
        return Err("context menu has no item".into());
    }
    Ok(())
}

fn walk(
    entries: &[MenuEntry],
    depth: usize,
    seen: &mut HashSet<String>,
    count: &mut usize,
    items: &mut usize,
) -> Result<(), String> {
    for e in entries {
        *count += 1;
        if *count > MAX_ENTRIES {
            return Err(format!("context menu has more than {MAX_ENTRIES} entries"));
        }
        match e {
            MenuEntry::Separator => {}
            MenuEntry::Item { id, label, .. } => {
                if !valid_id(id) {
                    return Err(format!("context menu item id {id:?} is not allowed"));
                }
                if !seen.insert(id.clone()) {
                    return Err(format!("context menu item id {id:?} is duplicated"));
                }
                if !valid_label(label) {
                    return Err("context menu item label is empty, too long or has control characters".into());
                }
                *items += 1;
            }
            MenuEntry::Submenu { label, children, .. } => {
                if depth >= 1 {
                    return Err("context menu is nested deeper than one submenu".into());
                }
                if !valid_label(label) {
                    return Err("context submenu label is empty, too long or has control characters".into());
                }
                walk(children, depth + 1, seen, count, items)?;
            }
        }
    }
    Ok(())
}

fn event_id(open: u64, id: &str) -> String {
    format!("{ID_PREFIX}{open}:{id}")
}

/// Splits a native menu event id into (open, item id); None when it is not a
/// context-menu id.
pub fn parse_event_id(event: &str) -> Option<(u64, &str)> {
    let rest = event.strip_prefix(ID_PREFIX)?;
    let (open, id) = rest.split_once(':')?;
    Some((open.parse().ok()?, id))
}

/// Every item id a model carries, in order.
fn item_ids(entries: &[MenuEntry], out: &mut HashSet<String>) {
    for e in entries {
        match e {
            MenuEntry::Item { id, .. } => {
                out.insert(id.clone());
            }
            MenuEntry::Submenu { children, .. } => item_ids(children, out),
            MenuEntry::Separator => {}
        }
    }
}

impl ContextMenuState {
    /// Arms a new menu (replacing any earlier one) and returns its open id.
    fn arm(&self, entries: &[MenuEntry], now: Instant) -> u64 {
        let mut inner = self.inner.lock().expect("context menu state");
        inner.next += 1;
        let open = inner.next;
        let mut ids = HashSet::new();
        item_ids(entries, &mut ids);
        inner.armed = Some(Armed { open, ids, at: now });
        open
    }

    /// Resolves a native menu event against the armed menu: Some only for the
    /// armed open, a known item id and within the TTL; the menu is disarmed on
    /// any hit so a duplicate selection is ignored.
    pub fn resolve(&self, event: &str, now: Instant) -> Option<ContextMenuSelection> {
        let (open, id) = parse_event_id(event)?;
        let mut inner = self.inner.lock().expect("context menu state");
        let armed = inner.armed.as_ref()?;
        if armed.open != open || !armed.ids.contains(id) || now.duration_since(armed.at) > ARMED_TTL {
            return None;
        }
        inner.armed = None;
        Some(ContextMenuSelection { open, id: id.to_string() })
    }
}

fn append_entries(
    app: &AppHandle,
    menu: &Menu<tauri::Wry>,
    entries: &[MenuEntry],
    open: u64,
) -> tauri::Result<()> {
    for e in entries {
        match e {
            MenuEntry::Separator => menu.append(&PredefinedMenuItem::separator(app)?)?,
            MenuEntry::Item { id, label, enabled } => {
                let item = MenuItemBuilder::with_id(event_id(open, id), label)
                    .enabled(*enabled)
                    .build(app)?;
                menu.append(&item)?;
            }
            MenuEntry::Submenu { label, enabled, children } => {
                let sub = Submenu::new(app, label, *enabled)?;
                for c in children {
                    match c {
                        MenuEntry::Separator => sub.append(&PredefinedMenuItem::separator(app)?)?,
                        MenuEntry::Item { id, label, enabled } => {
                            let item = MenuItemBuilder::with_id(event_id(open, id), label)
                                .enabled(*enabled)
                                .build(app)?;
                            sub.append(&item)?;
                        }
                        // validate() refuses a deeper level before this runs.
                        MenuEntry::Submenu { .. } => {}
                    }
                }
                menu.append(&sub)?;
            }
        }
    }
    Ok(())
}

/// Pops a native menu built from `entries` at the pointer (`x`, `y` in the
/// webview's CSS pixels). Returns the open id the selection event will carry.
#[tauri::command]
pub fn context_menu_show(
    app: AppHandle,
    window: Window,
    state: State<'_, ContextMenuState>,
    entries: Vec<MenuEntry>,
    x: f64,
    y: f64,
) -> Result<u64, String> {
    validate(&entries)?;
    if !(x.is_finite() && y.is_finite() && (0.0..=MAX_COORD).contains(&x) && (0.0..=MAX_COORD).contains(&y)) {
        return Err("context menu position is out of range".into());
    }
    let open = state.arm(&entries, Instant::now());
    let menu = Menu::new(&app).map_err(|e| e.to_string())?;
    append_entries(&app, &menu, &entries, open).map_err(|e| e.to_string())?;
    // The webview's CSS pixels are the window's logical pixels divided by the
    // zoom the app applies (View > Zoom), so scale back to logical.
    let zoom = crate::zoom_store::load(&app);
    menu.popup_at(window, LogicalPosition::new(x * zoom, y * zoom))
        .map_err(|e| e.to_string())?;
    Ok(open)
}

/// Called from the app-wide menu event handler: a context-menu selection is
/// forwarded to the renderer and consumed. Returns true when `event` was one.
pub fn handle_event(app: &AppHandle, event: &str) -> bool {
    if parse_event_id(event).is_none() {
        return false;
    }
    if let Some(sel) = app.state::<ContextMenuState>().resolve(event, Instant::now()) {
        let _ = app.emit_to(MAIN_WINDOW, ACTION_EVENT, sel);
    }
    true
}

#[cfg(test)]
mod tests {
    use super::*;

    fn item(id: &str, label: &str) -> MenuEntry {
        MenuEntry::Item { id: id.into(), label: label.into(), enabled: true }
    }

    #[test]
    fn context_menu_validate_table() {
        let ok = vec![item("a", "Rename"), MenuEntry::Separator, item("b", "Delete")];
        assert_eq!(validate(&ok), Ok(()));

        let long_label = "x".repeat(MAX_LABEL_CHARS + 1);
        let many: Vec<MenuEntry> = (0..=MAX_ENTRIES).map(|i| item(&format!("i{i}"), "x")).collect();
        let nested = vec![MenuEntry::Submenu {
            label: "More".into(),
            enabled: true,
            children: vec![MenuEntry::Submenu { label: "Deeper".into(), enabled: true, children: vec![item("z", "z")] }],
        }];
        let cases: Vec<(&str, Vec<MenuEntry>, &str)> = vec![
            ("empty", vec![], "no item"),
            ("only separators", vec![MenuEntry::Separator, MenuEntry::Separator], "no item"),
            ("empty submenu only", vec![MenuEntry::Submenu { label: "S".into(), enabled: true, children: vec![] }], "no item"),
            ("too many", many, "more than"),
            ("too deep", nested, "deeper"),
            ("duplicate id", vec![item("a", "A"), item("a", "B")], "duplicated"),
            ("duplicate id across a submenu", vec![item("a", "A"), MenuEntry::Submenu { label: "S".into(), enabled: true, children: vec![item("a", "B")] }], "duplicated"),
            ("empty id", vec![item("", "A")], "not allowed"),
            ("bad id char", vec![item("a b", "A")], "not allowed"),
            ("path-like id", vec![item("../etc/passwd", "A")], "not allowed"),
            ("long id", vec![item(&"a".repeat(MAX_ID_LEN + 1), "A")], "not allowed"),
            ("empty label", vec![item("a", "  ")], "label"),
            ("long label", vec![item("a", &long_label)], "label"),
            ("control character", vec![item("a", "Re\nname")], "label"),
            ("submenu control character", vec![MenuEntry::Submenu { label: "S\u{0}".into(), enabled: true, children: vec![item("a", "A")] }], "label"),
        ];
        for (name, entries, want) in cases {
            let err = validate(&entries).expect_err(name);
            assert!(err.contains(want), "{name}: {err:?} does not mention {want:?}");
        }
        // A menu whose items are all disabled is still a menu (the in-page one shows them greyed).
        let disabled = vec![MenuEntry::Item { id: "a".into(), label: "Close".into(), enabled: false }];
        assert_eq!(validate(&disabled), Ok(()));
    }

    #[test]
    fn context_menu_model_deserializes_only_its_own_fields() {
        let ok: Vec<MenuEntry> = serde_json::from_str(
            r#"[{"kind":"item","id":"a","label":"A"},{"kind":"separator"},{"kind":"submenu","label":"S","children":[{"kind":"item","id":"b","label":"B","enabled":false}]}]"#,
        )
        .unwrap();
        assert_eq!(ok.len(), 3);
        assert!(matches!(&ok[0], MenuEntry::Item { enabled: true, .. }));
        // Nothing that could name a path, a command or a URL is accepted.
        for bad in [
            r#"[{"kind":"item","id":"a","label":"A","command":"rm -rf /"}]"#,
            r#"[{"kind":"item","id":"a","label":"A","path":"/etc"}]"#,
            r#"[{"kind":"open","id":"a","label":"A"}]"#,
            r#"[{"kind":"item","label":"A"}]"#,
        ] {
            assert!(serde_json::from_str::<Vec<MenuEntry>>(bad).is_err(), "{bad} was accepted");
        }
    }

    #[test]
    fn context_menu_ids_are_namespaced_per_open_and_roundtrip() {
        assert_eq!(event_id(7, "task.rename"), "ctx:7:task.rename");
        assert_eq!(parse_event_id("ctx:7:task.rename"), Some((7, "task.rename")));
        // An id may itself contain colons: only the first separator splits.
        assert_eq!(parse_event_id("ctx:12:a:b"), Some((12, "a:b")));
        for not in ["file-quit", "ctx:", "ctx:x:a", "ctx:7", "ctx::a", "xctx:1:a", ""] {
            assert_eq!(parse_event_id(not), None, "{not:?}");
        }
    }

    #[test]
    fn context_menu_selection_of_a_stale_open_is_ignored() {
        let state = ContextMenuState::default();
        let t0 = Instant::now();
        let first = state.arm(&[item("a", "A")], t0);
        let second = state.arm(&[item("b", "B")], t0);
        assert_ne!(first, second);
        // The first menu was replaced: its selection no longer reaches anyone.
        assert_eq!(state.resolve(&event_id(first, "a"), t0), None);
        // An id the armed menu does not carry is ignored too (another menu's id under this open).
        assert_eq!(state.resolve(&event_id(second, "a"), t0), None);
        // The armed menu's own selection resolves once.
        assert_eq!(state.resolve(&event_id(second, "b"), t0), Some(ContextMenuSelection { open: second, id: "b".into() }));
        assert_eq!(state.resolve(&event_id(second, "b"), t0), None, "a duplicate selection is ignored");
    }

    #[test]
    fn context_menu_selection_after_the_ttl_is_ignored_and_dismissal_leaves_nothing_armed_for_ever() {
        let state = ContextMenuState::default();
        let t0 = Instant::now();
        let open = state.arm(&[item("a", "A")], t0);
        assert_eq!(state.resolve(&event_id(open, "a"), t0 + ARMED_TTL + Duration::from_secs(1)), None);
        // Nothing was armed by a menu that never delivered; a fresh open starts clean.
        let again = state.arm(&[item("a", "A")], t0);
        assert!(again > open);
        assert!(state.resolve(&event_id(again, "a"), t0 + Duration::from_secs(1)).is_some());
    }

    #[test]
    fn context_menu_command_takes_no_path_or_command() {
        // The command's arguments are a menu model and a position, and the model deserializes
        // only labels, opaque ids and flags (see the test above): the source names no other input.
        let src = include_str!("context_menu.rs");
        let sig = src.split("pub fn context_menu_show(").nth(1).expect("command exists");
        let sig = sig.split(") -> Result").next().unwrap();
        for forbidden in ["path", "command:", "url", "args"] {
            assert!(!sig.contains(forbidden), "context_menu_show takes {forbidden}");
        }
    }
}
