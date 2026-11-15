//! Task-window pool, registry and URL builder (desktop-multi-window Part 1).
//!
//! Rust owns every window name: `window_open_task` takes an integer task id,
//! maps it onto the fixed label pool `task-1..=task-8`, and builds each task
//! window's URL itself from the proxy origin and the per-launch secret. No
//! page string ever reaches a label, a capability glob or a URL.

#![allow(dead_code)] // wired by window_open_task in the next step

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
    pub fn label_for(&self, task_id: i64) -> Option<String> {
        let slots = self.slots.lock().unwrap_or_else(|e| e.into_inner());
        slots
            .iter()
            .position(|slot| *slot == Some(task_id))
            .map(pool_label)
    }
}
