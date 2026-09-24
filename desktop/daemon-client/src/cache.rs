//! A tiny task -> workspace lookup. `permission.pending` carries no
//! workspaceId (see ADR-0012's cross-reference and
//! `internal/wsapi/events.go`), so `client.rs` resolves it in the
//! background via `task.get` and stores it here; the notification click
//! handler (fired later, from the OS's own callback, possibly on a
//! different thread) reads it synchronously without blocking on a
//! round-trip at click time.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

#[derive(Clone, Default)]
pub struct WorkspaceCache(Arc<Mutex<HashMap<i64, i64>>>);

impl WorkspaceCache {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn insert(&self, task_id: i64, workspace_id: i64) {
        self.0.lock().unwrap().insert(task_id, workspace_id);
    }

    pub fn get(&self, task_id: i64) -> Option<i64> {
        self.0.lock().unwrap().get(&task_id).copied()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn missing_task_is_none() {
        let cache = WorkspaceCache::new();
        assert_eq!(cache.get(42), None);
    }

    #[test]
    fn insert_then_get() {
        let cache = WorkspaceCache::new();
        cache.insert(42, 7);
        assert_eq!(cache.get(42), Some(7));
    }

    #[test]
    fn reinsert_overwrites() {
        let cache = WorkspaceCache::new();
        cache.insert(1, 2);
        cache.insert(1, 3);
        assert_eq!(cache.get(1), Some(3));
    }

    #[test]
    fn clone_shares_state() {
        let cache = WorkspaceCache::new();
        let clone = cache.clone();
        clone.insert(9, 9);
        assert_eq!(cache.get(9), Some(9));
    }
}
