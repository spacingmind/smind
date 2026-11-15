//! The open task and tab come back after a quit (desktop-native-feel PR (c), single window).
//!
//! The web app keeps its active task and tab in the URL hash (`#/workspace/<w>/task/<t>/<tab>`) and restores them from it,
//! but the shell opens the main window with no hash, so a relaunch landed on the home screen. On `ExitRequested`
//! the shell now reads the main webview's own current URL, keeps a validated route fragment together with the id of the
//! current connection, and stores it per window label in `window-layout.json`; at launch the same fragment is appended
//! to the initial URL when the connection is still the same. No IPC command, no capability and no web change are
//! involved: it reuses the route the web already writes and already reads.

use std::collections::BTreeMap;
use std::path::Path;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager};
use url::Url;

use crate::state::DesktopState;
use crate::MAIN_WINDOW;

pub const FILE_NAME: &str = "window-layout.json";

/// Longest fragment kept (routes are short; a file path inside one is the only long part).
const MAX_FRAGMENT: usize = 400;

/// Route kinds the web writes (`lib/route.ts`): the four base kinds plus file and chat.
const KINDS: &[&str] = &["files", "diff", "terminal", "pr", "file", "chat"];

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
struct Saved {
    /// The connection the route belongs to: task ids are per daemon.
    connection: String,
    /// `/workspace/<w>/task/<t>/<kind>[/...]`, no leading `#`, no query.
    fragment: String,
}

type Layouts = BTreeMap<String, Saved>;

fn is_route_char(c: char) -> bool {
    c.is_ascii_alphanumeric() || matches!(c, '.' | '_' | '~' | '%' | '/' | '-')
}

/// The route in `url`'s fragment, if it has one in the exact shape the web writes. Anything else (another origin, a
/// settings view, a traversal, control characters, a query such as a notification's `card=`) is dropped.
pub fn route_fragment(url: &Url) -> Option<String> {
    if url.host_str() != Some("127.0.0.1") {
        return None;
    }
    let raw = url.fragment()?;
    let path = raw.split('?').next().unwrap_or("");
    let path = path.strip_prefix('/')?;
    if path.is_empty() || path.len() + 1 > MAX_FRAGMENT || !path.chars().all(is_route_char) {
        return None;
    }
    let segments: Vec<&str> = path.split('/').collect();
    if segments.len() < 5
        || segments
            .iter()
            .any(|s| s.is_empty() || *s == "." || *s == "..")
    {
        return None;
    }
    let digits = |s: &str| !s.is_empty() && s.len() <= 12 && s.chars().all(|c| c.is_ascii_digit());
    if segments[0] != "workspace"
        || !digits(segments[1])
        || segments[2] != "task"
        || !digits(segments[3])
        || !KINDS.contains(&segments[4])
    {
        return None;
    }
    Some(format!("/{path}"))
}

fn read(path: &Path) -> Layouts {
    std::fs::read_to_string(path)
        .ok()
        .and_then(|raw| serde_json::from_str(&raw).ok())
        .unwrap_or_default()
}

/// The fragment saved for `label`, when it was saved on `connection` and is still a valid route.
pub fn load(path: &Path, label: &str, connection: &str) -> Option<String> {
    let saved = read(path).remove(label)?;
    if saved.connection != connection {
        return None;
    }
    let probe: Url = format!("http://127.0.0.1/#{}", saved.fragment)
        .parse()
        .ok()?;
    route_fragment(&probe)
}

/// Records `fragment` for `label` (or clears the entry with `None`: a quit from the home screen restores the home screen).
/// Other labels are kept. Best effort; the write goes through a temp file so a crash cannot leave half a file.
pub fn save(path: &Path, label: &str, connection: &str, fragment: Option<&str>) {
    let mut layouts = read(path);
    match fragment {
        Some(fragment) => {
            layouts.insert(
                label.to_string(),
                Saved {
                    connection: connection.to_string(),
                    fragment: fragment.to_string(),
                },
            );
        }
        None => {
            layouts.remove(label);
        }
    }
    let Ok(body) = serde_json::to_string_pretty(&layouts) else {
        return;
    };
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    let tmp = path.with_extension("json.tmp");
    if std::fs::write(&tmp, body).is_ok() && std::fs::rename(&tmp, path).is_err() {
        let _ = std::fs::remove_file(&tmp);
    }
}

/// `url` with `fragment` as its fragment (the `?k=` secret query is kept).
pub fn with_fragment(mut url: Url, fragment: &str) -> Url {
    url.set_fragment(Some(fragment));
    url
}

/// Quit hook (`RunEvent::ExitRequested`, windows still alive): stores the main webview's current route, or clears the
/// entry when it is showing none. Best effort; a missing window, URL or state saves nothing.
pub fn save_main(app: &AppHandle) {
    let Some(win) = app.get_webview_window(MAIN_WINDOW) else {
        return;
    };
    let Ok(url) = win.url() else { return };
    let Some(state) = app.try_state::<DesktopState>() else {
        return;
    };
    let connection = state.proxy.registry.lock().unwrap().current().id.clone();
    let Ok(dir) = app.path().app_data_dir() else {
        return;
    };
    save(
        &dir.join(FILE_NAME),
        MAIN_WINDOW,
        &connection,
        route_fragment(&url).as_deref(),
    );
}

#[cfg(test)]
mod tests {
    use super::*;

    fn u(s: &str) -> Url {
        s.parse().unwrap()
    }

    #[test]
    fn route_fragment_table() {
        let ok: &[(&str, &str)] = &[
            (
                "http://127.0.0.1:5000/?k=s#/workspace/1/task/2/chat/3",
                "/workspace/1/task/2/chat/3",
            ),
            (
                "http://127.0.0.1:5000/#/workspace/1/task/2/files",
                "/workspace/1/task/2/files",
            ),
            (
                "http://127.0.0.1:5000/#/workspace/10/task/20/file/src/a%20b.ts",
                "/workspace/10/task/20/file/src/a%20b.ts",
            ),
            (
                "http://127.0.0.1:5000/#/workspace/1/task/2/chat/3?card=abc",
                "/workspace/1/task/2/chat/3",
            ),
            (
                "http://127.0.0.1:5000/#/workspace/1/task/2/pr",
                "/workspace/1/task/2/pr",
            ),
        ];
        for (url, want) in ok {
            assert_eq!(route_fragment(&u(url)).as_deref(), Some(*want), "{url}");
        }
        let long = format!(
            "http://127.0.0.1:5000/#/workspace/1/task/2/file/{}",
            "a".repeat(400)
        );
        let bad: &[&str] = &[
            "http://127.0.0.1:5000/",
            "http://127.0.0.1:5000/#",
            "http://127.0.0.1:5000/#/",
            "http://127.0.0.1:5000/#/settings",
            "http://127.0.0.1:5000/#/workspace/x/task/2/chat/3",
            "http://127.0.0.1:5000/#/workspace/1/task/y/chat/3",
            "http://127.0.0.1:5000/#/workspace/1/tasks/2/chat/3",
            "http://127.0.0.1:5000/#/workspace/1/task/2/plugin/p",
            "http://127.0.0.1:5000/#/workspace/1/task/2",
            "http://127.0.0.1:5000/#/workspace/1/task/2/file/../../etc/passwd",
            "http://127.0.0.1:5000/#/workspace/1/task/2/file//x",
            "http://127.0.0.1:5000/#/workspace/1/task/2/file/a\\b",
            "http://127.0.0.1:5000/#/workspace/1/task/2/file/a;b",
            "http://127.0.0.1:5000/#/workspace/1/task/2/file/a=b",
            "http://127.0.0.1:5000/#/workspace/1234567890123/task/2/chat/3",
            "http://example.com/#/workspace/1/task/2/chat/3",
            "http://localhost:5000/#/workspace/1/task/2/chat/3",
            &long,
        ];
        for url in bad {
            assert_eq!(route_fragment(&u(url)), None, "{url}");
        }
    }

    #[test]
    fn save_then_load_round_trips_for_the_same_connection() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(FILE_NAME);
        save(&path, "main", "local", Some("/workspace/1/task/2/chat/3"));
        assert_eq!(
            load(&path, "main", "local").as_deref(),
            Some("/workspace/1/task/2/chat/3")
        );
    }

    #[test]
    fn a_different_connection_does_not_restore() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(FILE_NAME);
        save(&path, "main", "local", Some("/workspace/1/task/2/files"));
        assert_eq!(load(&path, "main", "relay-1"), None);
    }

    #[test]
    fn a_quit_with_no_route_clears_the_entry() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(FILE_NAME);
        save(&path, "main", "local", Some("/workspace/1/task/2/files"));
        save(&path, "main", "local", None);
        assert_eq!(load(&path, "main", "local"), None);
    }

    #[test]
    fn corrupt_or_missing_file_restores_nothing() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(FILE_NAME);
        assert_eq!(load(&path, "main", "local"), None);
        std::fs::write(&path, "{not json").unwrap();
        assert_eq!(load(&path, "main", "local"), None);
        // ...and a later save replaces the corrupt file.
        save(&path, "main", "local", Some("/workspace/1/task/2/diff"));
        assert_eq!(
            load(&path, "main", "local").as_deref(),
            Some("/workspace/1/task/2/diff")
        );
    }

    #[test]
    fn a_saved_fragment_that_is_no_longer_a_valid_route_is_not_replayed() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(FILE_NAME);
        std::fs::write(
            &path,
            r#"{"main":{"connection":"local","fragment":"/workspace/1/task/2/file/../../x"}}"#,
        )
        .unwrap();
        assert_eq!(load(&path, "main", "local"), None);
        std::fs::write(
            &path,
            r#"{"main":{"connection":"local","fragment":"javascript:alert(1)"}}"#,
        )
        .unwrap();
        assert_eq!(load(&path, "main", "local"), None);
    }

    #[test]
    fn other_labels_are_left_alone_when_main_saves() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(FILE_NAME);
        save(&path, "win-2", "local", Some("/workspace/5/task/6/files"));
        save(&path, "main", "local", Some("/workspace/1/task/2/files"));
        save(&path, "main", "local", None);
        assert_eq!(
            load(&path, "win-2", "local").as_deref(),
            Some("/workspace/5/task/6/files")
        );
    }

    #[test]
    fn apply_fragment_keeps_the_secret_query() {
        let url = with_fragment(
            u("http://127.0.0.1:5000/?k=secret"),
            "/workspace/1/task/2/chat/3",
        );
        assert_eq!(
            url.as_str(),
            "http://127.0.0.1:5000/?k=secret#/workspace/1/task/2/chat/3"
        );
    }

    #[test]
    fn the_exit_handler_saves_before_exit() {
        let src = include_str!("lifecycle.rs");
        let handler = src
            .split("pub fn handle_run_event")
            .nth(1)
            .expect("handler exists");
        let first = handler.split("RunEvent::Reopen").next().unwrap();
        assert!(
            first.contains("RunEvent::ExitRequested")
                && first.contains("window_layout::save_main(app)")
        );
    }

    #[test]
    fn the_initial_url_is_built_with_the_saved_fragment() {
        let src = include_str!("lib.rs");
        let setup = src
            .split("window_layout::load(")
            .nth(1)
            .expect("lib.rs loads the saved layout");
        let before_build = setup.split(".build()?").next().unwrap();
        assert!(before_build.contains("window_layout::with_fragment(initial_url"));
        assert!(src
            .split("WebviewWindowBuilder::new(")
            .nth(1)
            .unwrap()
            .contains("tauri::WebviewUrl::External(initial_url)"));
    }
}
