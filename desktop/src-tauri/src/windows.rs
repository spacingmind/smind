//! Task-window pool, registry and URL builder (desktop-multi-window Part 1).
//!
//! Rust owns every window name: `window_open_task` takes an integer task id,
//! maps it onto the fixed label pool `task-1..=task-8`, and builds each task
//! window's URL itself from the proxy origin and the per-launch secret. No
//! page string ever reaches a label, a capability glob or a URL.

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
}
