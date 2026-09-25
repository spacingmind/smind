//! The tray's "pending approvals" state machine (quick-wins AC4): which
//! tasks currently have an unresolved `permission.pending`.
//!
//! No `permission.resolved` event exists (see
//! `docs/plans/active/desktop-quick-wins.md`'s Context). A task's entry
//! clears when its run transitions back to "running" -- the same signal
//! `web/packages/ui/src/hooks/use-task-attention.ts` uses to clear its
//! own permission badge -- or on a fresh reconnect, which resets the
//! whole state rather than trying to resync it against `run.list`/
//! `run.logs` (that RPC pair is what the web hook uses for its own
//! resync pass; duplicating it here was judged not worth the extra
//! round-trips for a tray tooltip).

use std::collections::HashMap;

#[derive(Default)]
pub struct Attention {
    /// requestIds in arrival order, most recent last; a repeat of the
    /// same requestId does not push a second entry (see `on_pending`).
    order: Vec<String>,
    task_of: HashMap<String, i64>,
}

impl Attention {
    pub fn new() -> Self {
        Self::default()
    }

    /// on_pending records a still-unresolved permission request. A
    /// repeat of the same requestId (e.g. redelivered after a reconnect)
    /// does not grow the count.
    pub fn on_pending(&mut self, task_id: i64, request_id: &str) {
        if !self.task_of.contains_key(request_id) {
            self.order.push(request_id.to_string());
        }
        self.task_of.insert(request_id.to_string(), task_id);
    }

    /// on_run_running clears every pending entry for a task whose run
    /// just (re)started -- the signal that a permission on it got
    /// resolved.
    pub fn on_run_running(&mut self, task_id: i64) {
        let task_of = &self.task_of;
        self.order.retain(|rid| task_of.get(rid) != Some(&task_id));
        self.task_of.retain(|_, tid| *tid != task_id);
    }

    /// reset clears all state -- called on every fresh connection: a
    /// stale in-memory guess is worse than an empty one until fresh
    /// events arrive.
    pub fn reset(&mut self) {
        self.order.clear();
        self.task_of.clear();
    }

    pub fn count(&self) -> usize {
        self.order.len()
    }

    /// most_recent_task is the task whose permission arrived last --
    /// what clicking the tray's attention item opens.
    pub fn most_recent_task(&self) -> Option<i64> {
        self.order.last().and_then(|rid| self.task_of.get(rid).copied())
    }
}

/// label formats the tray tooltip/menu text for a pending count.
pub fn label(count: usize) -> String {
    match count {
        0 => "No approvals waiting".to_string(),
        1 => "1 approval waiting".to_string(),
        n => format!("{n} approvals waiting"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pending_counts_up() {
        let mut a = Attention::new();
        assert_eq!(a.count(), 0);
        a.on_pending(1, "req-1");
        assert_eq!(a.count(), 1);
        a.on_pending(2, "req-2");
        assert_eq!(a.count(), 2);
    }

    #[test]
    fn duplicate_request_id_counted_once() {
        let mut a = Attention::new();
        a.on_pending(1, "req-1");
        a.on_pending(1, "req-1");
        assert_eq!(a.count(), 1);
    }

    #[test]
    fn run_running_clears_its_task_only() {
        let mut a = Attention::new();
        a.on_pending(1, "req-1");
        a.on_pending(2, "req-2");
        a.on_run_running(1);
        assert_eq!(a.count(), 1);
        assert_eq!(a.most_recent_task(), Some(2));
    }

    #[test]
    fn reset_clears_everything() {
        let mut a = Attention::new();
        a.on_pending(1, "req-1");
        a.on_pending(2, "req-2");
        a.reset();
        assert_eq!(a.count(), 0);
        assert_eq!(a.most_recent_task(), None);
    }

    #[test]
    fn most_recent_task_is_latest_arrival() {
        let mut a = Attention::new();
        a.on_pending(1, "req-1");
        a.on_pending(2, "req-2");
        a.on_pending(3, "req-3");
        assert_eq!(a.most_recent_task(), Some(3));
        a.on_run_running(3);
        assert_eq!(a.most_recent_task(), Some(2));
    }

    #[test]
    fn tray_label_formatting() {
        assert_eq!(label(0), "No approvals waiting");
        assert_eq!(label(1), "1 approval waiting");
        assert_eq!(label(5), "5 approvals waiting");
    }
}
