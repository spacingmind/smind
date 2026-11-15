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
//! replaces the first. Tauri's menu API does not report a dismissal as an
//! event; on macOS and Windows `popup_at` blocks until the menu closes, so the
//! command's own return is the renderer's "closed" signal there. An armed menu
//! that delivers nothing expires (`ARMED_TTL`) or is replaced.

use std::collections::HashSet;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use tauri::menu::{ContextMenu, IsMenuItem, Menu, MenuItemBuilder, PredefinedMenuItem, Submenu};
use tauri::{AppHandle, Emitter, LogicalPosition, Manager, State, Window};

/// The event the bundled UI listens to (`platform.ts` `onContextMenuAction`).
pub const ACTION_EVENT: &str = "context-menu-action";

const ID_PREFIX: &str = "ctx:";
const MAX_ENTRIES: usize = 64;
const MAX_ID_LEN: usize = 64;
const MAX_LABEL_CHARS: usize = 120;
const MAX_COORD: f64 = 100_000.0;
/// How long an opened menu can still deliver a selection. A native menu blocks the main thread while it is open and
/// can be left open for a while; this only bounds a menu that never delivers, so it is generous.
const ARMED_TTL: Duration = Duration::from_secs(600);

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
    label: String,
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

/// Characters a label may not contain: by Unicode general category the controls (Cc), format characters
/// (Cf: bidi marks, embeddings and isolates, zero-width characters, the BOM, tag characters), surrogates,
/// private use and unassigned (Cs, Co, Cn), and the line and paragraph separators (Zl, Zp); plus the
/// characters that render as blank or alter neighbours without being in those categories (Hangul fillers,
/// the Braille blank, the Mongolian vowel separator, the combining grapheme joiner, variation selectors).
/// Plain space (U+0020) is the one separator allowed.
fn is_forbidden_char(c: char) -> bool {
    use unicode_properties::{GeneralCategory as G, UnicodeGeneralCategory};
    if c == ' ' {
        return false;
    }
    matches!(
        c.general_category(),
        G::Control
            | G::Format
            | G::Surrogate
            | G::PrivateUse
            | G::Unassigned
            | G::LineSeparator
            | G::ParagraphSeparator
    ) || matches!(c,
        '\u{034F}' | '\u{115F}' | '\u{1160}' | '\u{180E}' | '\u{2800}' | '\u{3164}' | '\u{FFA0}'
        | '\u{FE00}'..='\u{FE0F}' | '\u{E0100}'..='\u{E01EF}')
}

/// A character a person can see: not whitespace (any kind) and not forbidden.
fn is_visible_char(c: char) -> bool {
    !c.is_whitespace() && !is_forbidden_char(c)
}

fn valid_label(label: &str) -> bool {
    label.chars().count() <= MAX_LABEL_CHARS
        && !label.chars().any(|c| is_forbidden_char(c) && c != ' ')
        && label.chars().any(is_visible_char)
}

/// The label as the native menu shows it. Windows and GTK read `&` as a mnemonic marker, so a literal one is
/// doubled there; macOS has no mnemonics.
fn native_label(label: &str) -> String {
    if cfg!(target_os = "macos") {
        label.to_string()
    } else {
        label.replace('&', "&&")
    }
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
                    return Err(
                        "context menu item label is empty, too long or has control characters"
                            .into(),
                    );
                }
                *items += 1;
            }
            MenuEntry::Submenu {
                label, children, ..
            } => {
                if depth >= 1 {
                    return Err("context menu is nested deeper than one submenu".into());
                }
                if !valid_label(label) {
                    return Err(
                        "context submenu label is empty, too long or has control characters".into(),
                    );
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
            // A disabled item cannot be chosen natively, so its id is never armed: a forged selection finds nothing.
            MenuEntry::Item {
                id, enabled: true, ..
            } => {
                out.insert(id.clone());
            }
            MenuEntry::Item { .. } => {}
            MenuEntry::Submenu { children, .. } => item_ids(children, out),
            MenuEntry::Separator => {}
        }
    }
}

impl ContextMenuState {
    /// Arms a new menu (replacing any earlier one) and returns its open id.
    #[cfg(test)]
    fn arm(&self, entries: &[MenuEntry], now: Instant) -> u64 {
        self.arm_for(entries, now, "main")
    }

    /// Arms a new menu opened by the window `label`, replacing any earlier one.
    #[cfg(test)]
    fn arm_for(&self, entries: &[MenuEntry], now: Instant, label: &str) -> u64 {
        let open = self.next_open();
        self.arm_with(open, entries, now, label);
        open
    }

    /// Reserves the next open id without arming anything.
    fn next_open(&self) -> u64 {
        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.next += 1;
        inner.next
    }

    /// Arms `open` with the enabled items of `entries`, replacing any earlier menu.
    fn arm_with(&self, open: u64, entries: &[MenuEntry], now: Instant, label: &str) {
        let mut ids = HashSet::new();
        item_ids(entries, &mut ids);
        self.inner.lock().unwrap_or_else(|e| e.into_inner()).armed = Some(Armed {
            open,
            ids,
            at: now,
            label: label.to_string(),
        });
    }

    /// Resolves a native menu event against the armed menu: Some (the opening
    /// window's label, the selection) only for the armed open, a known item id
    /// and within the TTL; the menu is disarmed on any hit so a duplicate
    /// selection is ignored.
    pub fn resolve(&self, event: &str, now: Instant) -> Option<(String, ContextMenuSelection)> {
        let (open, id) = parse_event_id(event)?;
        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        let armed = inner.armed.as_ref()?;
        if armed.open != open || !armed.ids.contains(id) || now.duration_since(armed.at) > ARMED_TTL
        {
            return None;
        }
        let label = armed.label.clone();
        inner.armed = None;
        Some((
            label,
            ContextMenuSelection {
                open,
                id: id.to_string(),
            },
        ))
    }
}

/// Builds the native items for `entries`, recursing into submenus. One construction path for every level.
fn build_items(
    app: &AppHandle,
    entries: &[MenuEntry],
    open: u64,
) -> tauri::Result<Vec<Box<dyn IsMenuItem<tauri::Wry>>>> {
    let mut out: Vec<Box<dyn IsMenuItem<tauri::Wry>>> = Vec::with_capacity(entries.len());
    for e in entries {
        match e {
            MenuEntry::Separator => out.push(Box::new(PredefinedMenuItem::separator(app)?)),
            MenuEntry::Item { id, label, enabled } => out.push(Box::new(
                MenuItemBuilder::with_id(event_id(open, id), native_label(label))
                    .enabled(*enabled)
                    .build(app)?,
            )),
            MenuEntry::Submenu {
                label,
                enabled,
                children,
            } => {
                let sub = Submenu::new(app, native_label(label), *enabled)?;
                let kids = build_items(app, children, open)?;
                let refs: Vec<&dyn IsMenuItem<tauri::Wry>> =
                    kids.iter().map(|k| k.as_ref()).collect();
                sub.append_items(&refs)?;
                out.push(Box::new(sub));
            }
        }
    }
    Ok(out)
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
    if !(x.is_finite()
        && y.is_finite()
        && (0.0..=MAX_COORD).contains(&x)
        && (0.0..=MAX_COORD).contains(&y))
    {
        return Err("context menu position is out of range".into());
    }
    // Build first and arm after, so a build failure leaves nothing armed.
    let open = state.next_open();
    let menu = Menu::new(&app).map_err(|e| e.to_string())?;
    let items = build_items(&app, &entries, open).map_err(|e| e.to_string())?;
    let refs: Vec<&dyn IsMenuItem<tauri::Wry>> = items.iter().map(|k| k.as_ref()).collect();
    menu.append_items(&refs).map_err(|e| e.to_string())?;
    state.arm_with(open, &entries, Instant::now(), window.label());
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
    if let Some((label, sel)) = app
        .state::<ContextMenuState>()
        .resolve(event, Instant::now())
    {
        if let Err(e) = app.emit_to(label.as_str(), ACTION_EVENT, sel) {
            eprintln!("context menu: could not deliver the selection: {e}");
        }
    }
    true
}

#[cfg(test)]
mod tests {
    use super::*;

    fn item(id: &str, label: &str) -> MenuEntry {
        MenuEntry::Item {
            id: id.into(),
            label: label.into(),
            enabled: true,
        }
    }

    #[test]
    fn context_menu_validate_table() {
        let ok = vec![
            item("a", "Rename"),
            MenuEntry::Separator,
            item("b", "Delete"),
        ];
        assert_eq!(validate(&ok), Ok(()));

        let long_label = "x".repeat(MAX_LABEL_CHARS + 1);
        let many: Vec<MenuEntry> = (0..=MAX_ENTRIES)
            .map(|i| item(&format!("i{i}"), "x"))
            .collect();
        let nested = vec![MenuEntry::Submenu {
            label: "More".into(),
            enabled: true,
            children: vec![MenuEntry::Submenu {
                label: "Deeper".into(),
                enabled: true,
                children: vec![item("z", "z")],
            }],
        }];
        let cases: Vec<(&str, Vec<MenuEntry>, &str)> = vec![
            ("empty", vec![], "no item"),
            (
                "only separators",
                vec![MenuEntry::Separator, MenuEntry::Separator],
                "no item",
            ),
            (
                "empty submenu only",
                vec![MenuEntry::Submenu {
                    label: "S".into(),
                    enabled: true,
                    children: vec![],
                }],
                "no item",
            ),
            ("too many", many, "more than"),
            ("too deep", nested, "deeper"),
            (
                "duplicate id",
                vec![item("a", "A"), item("a", "B")],
                "duplicated",
            ),
            (
                "duplicate id across a submenu",
                vec![
                    item("a", "A"),
                    MenuEntry::Submenu {
                        label: "S".into(),
                        enabled: true,
                        children: vec![item("a", "B")],
                    },
                ],
                "duplicated",
            ),
            ("empty id", vec![item("", "A")], "not allowed"),
            ("bad id char", vec![item("a b", "A")], "not allowed"),
            (
                "path-like id",
                vec![item("../etc/passwd", "A")],
                "not allowed",
            ),
            (
                "long id",
                vec![item(&"a".repeat(MAX_ID_LEN + 1), "A")],
                "not allowed",
            ),
            ("empty label", vec![item("a", "  ")], "label"),
            ("long label", vec![item("a", &long_label)], "label"),
            ("control character", vec![item("a", "Re\nname")], "label"),
            (
                "submenu control character",
                vec![MenuEntry::Submenu {
                    label: "S\u{0}".into(),
                    enabled: true,
                    children: vec![item("a", "A")],
                }],
                "label",
            ),
        ];
        for (name, entries, want) in cases {
            let err = validate(&entries).expect_err(name);
            assert!(
                err.contains(want),
                "{name}: {err:?} does not mention {want:?}"
            );
        }
        // A menu whose items are all disabled is still a menu (the in-page one shows them greyed).
        let disabled = vec![MenuEntry::Item {
            id: "a".into(),
            label: "Close".into(),
            enabled: false,
        }];
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
            assert!(
                serde_json::from_str::<Vec<MenuEntry>>(bad).is_err(),
                "{bad} was accepted"
            );
        }
    }

    #[test]
    fn context_menu_ids_are_namespaced_per_open_and_roundtrip() {
        assert_eq!(event_id(7, "task.rename"), "ctx:7:task.rename");
        assert_eq!(
            parse_event_id("ctx:7:task.rename"),
            Some((7, "task.rename"))
        );
        // An id may itself contain colons: only the first separator splits.
        assert_eq!(parse_event_id("ctx:12:a:b"), Some((12, "a:b")));
        for not in [
            "file-quit",
            "ctx:",
            "ctx:x:a",
            "ctx:7",
            "ctx::a",
            "xctx:1:a",
            "",
        ] {
            assert_eq!(parse_event_id(not), None, "{not:?}");
        }
    }

    #[test]
    fn context_menu_selection_of_a_stale_open_is_ignored() {
        let state = ContextMenuState::default();
        let t0 = Instant::now();
        let first = state.arm(&[item("a", "A")], t0);
        // Both menus carry an item "a": only the open id tells them apart.
        let second = state.arm(&[item("a", "A"), item("b", "B")], t0);
        assert_ne!(first, second);
        // The first menu was replaced: its selection no longer reaches anyone.
        assert_eq!(state.resolve(&event_id(first, "a"), t0), None);
        // An id the armed menu does not carry is ignored too.
        assert_eq!(state.resolve(&event_id(second, "zzz"), t0), None);
        // The armed menu's own selection resolves once.
        assert_eq!(
            state.resolve(&event_id(second, "b"), t0).map(|(_, s)| s),
            Some(ContextMenuSelection {
                open: second,
                id: "b".into()
            })
        );
        assert_eq!(
            state.resolve(&event_id(second, "b"), t0),
            None,
            "a duplicate selection is ignored"
        );
    }

    #[test]
    fn context_menu_selection_goes_to_the_window_that_opened_it() {
        let state = ContextMenuState::default();
        let t = Instant::now();
        let open = state.arm_for(&[item("a", "A")], t, "task-2");
        assert_eq!(
            state.resolve(&event_id(open, "a"), t),
            Some((
                "task-2".to_string(),
                ContextMenuSelection {
                    open,
                    id: "a".into()
                }
            ))
        );
    }

    #[test]
    fn context_menu_a_second_open_replaces_the_first_windows_menu() {
        let state = ContextMenuState::default();
        let t = Instant::now();
        let first = state.arm_for(&[item("a", "A")], t, "task-2");
        let second = state.arm_for(&[item("a", "A")], t, "main");
        // The first menu was replaced: its selection reaches nobody, not even its own window.
        assert_eq!(state.resolve(&event_id(first, "a"), t), None);
        assert_eq!(
            state.resolve(&event_id(second, "a"), t),
            Some((
                "main".to_string(),
                ContextMenuSelection {
                    open: second,
                    id: "a".into()
                }
            ))
        );
    }

    #[test]
    fn context_menu_handle_event_never_targets_a_fixed_window() {
        let src = include_str!("context_menu.rs");
        let start = src
            .find("pub fn handle_event")
            .expect("handle_event exists");
        let rest = &src[start..];
        let end = rest.find("\n}\n").expect("handle_event body is closed");
        let body = &rest[..end];
        assert!(
            body.contains("emit_to(label"),
            "handle_event must deliver the selection to the window that opened the menu"
        );
        assert!(
            !body.contains("MAIN_WINDOW"),
            "handle_event must not name a fixed window label"
        );
    }

    #[test]
    fn context_menu_selection_after_the_ttl_is_ignored_and_dismissal_leaves_nothing_armed_for_ever()
    {
        let state = ContextMenuState::default();
        let t0 = Instant::now();
        let open = state.arm(&[item("a", "A")], t0);
        assert_eq!(
            state.resolve(
                &event_id(open, "a"),
                t0 + ARMED_TTL + Duration::from_secs(1)
            ),
            None
        );
        // Nothing was armed by a menu that never delivered; a fresh open starts clean.
        let again = state.arm(&[item("a", "A")], t0);
        assert!(again > open);
        assert!(state
            .resolve(&event_id(again, "a"), t0 + Duration::from_secs(1))
            .is_some());
    }

    #[test]
    fn context_menu_label_table_per_character_class() {
        let refused: &[(&str, &str)] = &[
            ("Cc control", "a\nb"),
            ("Cc tab", "a\tb"),
            ("Cc DEL", "a\u{7F}b"),
            ("Cc C1", "a\u{85}b"),
            ("Cf bidi override", "a\u{202E}b"),
            ("Cf bidi isolate", "a\u{2066}b"),
            ("Cf bidi mark", "a\u{200F}b"),
            ("Cf zero width space", "a\u{200B}b"),
            ("Cf zero width joiner", "a\u{200D}b"),
            ("Cf word joiner", "a\u{2060}b"),
            ("Cf BOM", "\u{FEFF}x"),
            ("Cf soft hyphen", "a\u{00AD}b"),
            ("Cf tag character", "a\u{E0041}b"),
            ("Cf Mongolian separator", "a\u{180E}b"),
            ("Zl line separator", "a\u{2028}b"),
            ("Zp paragraph separator", "a\u{2029}b"),
            ("Co private use", "a\u{E000}b"),
            ("Cn unassigned", "a\u{0378}b"),
            ("Hangul filler 3164", "a\u{3164}b"),
            ("Hangul filler 115F", "a\u{115F}b"),
            ("Hangul filler FFA0", "a\u{FFA0}b"),
            ("Braille blank", "a\u{2800}b"),
            ("grapheme joiner", "a\u{034F}b"),
            ("variation selector", "a\u{FE0F}b"),
            ("variation selector supplement", "a\u{E0100}b"),
            ("only invisible", "\u{200B}\u{2800}"),
            ("only spaces", "   "),
            ("only NBSP", "\u{00A0}\u{3000}"),
            ("empty", ""),
        ];
        for (class, label) in refused {
            assert!(!valid_label(label), "{class}: {label:?} should be refused");
        }
        for ok in [
            "Rename…",
            "Fix the bug",
            "Café",
            "日本語",
            "Move to space",
            "a\u{00A0}b",
        ] {
            assert!(valid_label(ok), "{ok:?} should be accepted");
        }
        assert!(!valid_label(&"x".repeat(MAX_LABEL_CHARS + 1)));
    }

    #[test]
    fn context_menu_state_survives_a_poisoned_lock() {
        let state = std::sync::Arc::new(ContextMenuState::default());
        let s2 = state.clone();
        let _ = std::thread::spawn(move || {
            let _guard = s2.inner.lock().unwrap();
            panic!("poison the lock");
        })
        .join();
        assert!(state.inner.is_poisoned());
        let t = Instant::now();
        let open = state.arm(&[item("a", "A")], t);
        assert!(state.resolve(&event_id(open, "a"), t).is_some());
    }

    #[test]
    fn context_menu_disabled_items_are_never_armed() {
        let state = ContextMenuState::default();
        let t = Instant::now();
        let entries = vec![
            item("on", "On"),
            MenuEntry::Item {
                id: "off".into(),
                label: "Off".into(),
                enabled: false,
            },
        ];
        let open = state.arm(&entries, t);
        assert!(state.resolve(&event_id(open, "off"), t).is_none());
        assert!(state.resolve(&event_id(open, "on"), t).is_some());
    }

    #[test]
    fn context_menu_native_label_doubles_ampersands_off_macos_only() {
        let got = native_label("Save & Close");
        if cfg!(target_os = "macos") {
            assert_eq!(got, "Save & Close");
        } else {
            assert_eq!(got, "Save && Close");
        }
    }
}
