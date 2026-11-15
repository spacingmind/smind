//! The tray's "waiting for you" state machine: which tasks currently
//! have an unresolved `permission.pending` or `question.pending`
//! (quick-wins AC4, DT5 B/AC7).
//!
//! An entry clears on the daemon's `permission.resolved` or
//! `question.resolved` event (both by request id, through the same
//! clear; a daemon that predates them never sends one), when its run
//! transitions back to "running" or ends -- the same signals
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
    chat_of: HashMap<String, i64>,
}

/// What the tray's attention item opens: the task, the chat whose tab
/// holds the request's card, and the request id itself (the `?card=`
/// the web route carries).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PendingRef {
    pub task_id: i64,
    pub chat_id: i64,
    pub request_id: String,
}

impl Attention {
    pub fn new() -> Self {
        Self::default()
    }

    /// on_pending records a still-unresolved permission or question
    /// request. A repeat of the same requestId (e.g. redelivered after a
    /// reconnect) does not grow the count.
    pub fn on_pending(&mut self, task_id: i64, chat_id: i64, request_id: &str) {
        if !self.task_of.contains_key(request_id) {
            self.order.push(request_id.to_string());
        }
        self.task_of.insert(request_id.to_string(), task_id);
        self.chat_of.insert(request_id.to_string(), chat_id);
    }

    /// on_permission_resolved clears exactly the request that was
    /// resolved; an unknown id (already cleared, or never seen) is a no-op.
    pub fn on_permission_resolved(&mut self, request_id: &str) {
        if self.task_of.remove(request_id).is_some() {
            self.order.retain(|rid| rid != request_id);
            self.chat_of.remove(request_id);
        }
    }

    /// on_run_running clears every pending entry for a task whose run
    /// just (re)started -- the signal that a permission on it got
    /// resolved.
    pub fn on_run_running(&mut self, task_id: i64) {
        let task_of = &self.task_of;
        self.order.retain(|rid| task_of.get(rid) != Some(&task_id));
        self.task_of.retain(|_, tid| *tid != task_id);
        let task_of = &self.task_of;
        self.chat_of.retain(|rid, _| task_of.contains_key(rid));
    }

    /// on_run_ended clears a task's entries when its run stopped, failed
    /// or finished: nothing can still be waiting on a permission there.
    pub fn on_run_ended(&mut self, task_id: i64) {
        self.on_run_running(task_id);
    }

    /// reset clears all state -- called on every fresh connection: a
    /// stale in-memory guess is worse than an empty one until fresh
    /// events arrive.
    pub fn reset(&mut self) {
        self.order.clear();
        self.task_of.clear();
        self.chat_of.clear();
    }

    pub fn count(&self) -> usize {
        self.order.len()
    }

    /// most_recent is the request whose permission arrived last -- what
    /// clicking the tray's attention item opens (task, chat and request).
    pub fn most_recent(&self) -> Option<PendingRef> {
        self.order.last().and_then(|rid| {
            Some(PendingRef {
                task_id: self.task_of.get(rid).copied()?,
                chat_id: self.chat_of.get(rid).copied().unwrap_or(0),
                request_id: rid.clone(),
            })
        })
    }

    /// most_recent_task is most_recent's task id alone, for callers that
    /// only navigate to the task.
    pub fn most_recent_task(&self) -> Option<i64> {
        self.most_recent().map(|p| p.task_id)
    }
}

/// label formats the tray tooltip/menu text for a pending count.
pub fn label(count: usize) -> String {
    match count {
        0 => "Nothing waiting for you".to_string(),
        1 => "1 waiting for you".to_string(),
        n => format!("{n} waiting for you"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pending_counts_up() {
        let mut a = Attention::new();
        assert_eq!(a.count(), 0);
        a.on_pending(1, 7, "req-1");
        assert_eq!(a.count(), 1);
        a.on_pending(2, 8, "req-2");
        assert_eq!(a.count(), 2);
    }

    #[test]
    fn duplicate_request_id_counted_once() {
        let mut a = Attention::new();
        a.on_pending(1, 7, "req-1");
        a.on_pending(1, 7, "req-1");
        assert_eq!(a.count(), 1);
    }

    #[test]
    fn run_running_clears_its_task_only() {
        let mut a = Attention::new();
        a.on_pending(1, 7, "req-1");
        a.on_pending(2, 8, "req-2");
        a.on_run_running(1);
        assert_eq!(a.count(), 1);
        assert_eq!(a.most_recent_task(), Some(2));
    }

    #[test]
    fn resolved_clears_only_that_request() {
        let mut a = Attention::new();
        a.on_pending(1, 7, "req-1");
        a.on_pending(1, 8, "req-2");
        a.on_permission_resolved("req-1");
        assert_eq!(a.count(), 1);
        assert_eq!(a.most_recent_task(), Some(1));
        a.on_permission_resolved("req-2");
        assert_eq!(a.count(), 0);
    }

    #[test]
    fn resolved_for_unknown_request_is_a_noop() {
        let mut a = Attention::new();
        a.on_pending(1, 7, "req-1");
        a.on_permission_resolved("nope");
        assert_eq!(a.count(), 1);
    }

    #[test]
    fn run_ended_clears_its_task_only() {
        let mut a = Attention::new();
        a.on_pending(1, 7, "req-1");
        a.on_pending(2, 8, "req-2");
        a.on_run_ended(2);
        assert_eq!(a.count(), 1);
        assert_eq!(a.most_recent_task(), Some(1));
    }

    #[test]
    fn most_recent_task_falls_back_after_resolve() {
        let mut a = Attention::new();
        a.on_pending(1, 7, "req-1");
        a.on_pending(2, 8, "req-2");
        a.on_permission_resolved("req-2");
        assert_eq!(a.most_recent_task(), Some(1));
    }

    #[test]
    fn reset_clears_everything() {
        let mut a = Attention::new();
        a.on_pending(1, 7, "req-1");
        a.on_pending(2, 8, "req-2");
        a.reset();
        assert_eq!(a.count(), 0);
        assert_eq!(a.most_recent_task(), None);
    }

    #[test]
    fn most_recent_task_is_latest_arrival() {
        let mut a = Attention::new();
        a.on_pending(1, 7, "req-1");
        a.on_pending(2, 8, "req-2");
        a.on_pending(3, 9, "req-3");
        assert_eq!(a.most_recent_task(), Some(3));
        a.on_run_running(3);
        assert_eq!(a.most_recent_task(), Some(2));
    }

    #[test]
    fn most_recent_returns_chat_and_request() {
        let mut a = Attention::new();
        assert_eq!(a.most_recent(), None);
        a.on_pending(1, 7, "req-1");
        a.on_pending(2, 8, "req-2");
        assert_eq!(
            a.most_recent(),
            Some(PendingRef { task_id: 2, chat_id: 8, request_id: "req-2".to_string() })
        );
        a.on_permission_resolved("req-2");
        assert_eq!(
            a.most_recent(),
            Some(PendingRef { task_id: 1, chat_id: 7, request_id: "req-1".to_string() })
        );
    }

    #[test]
    fn tray_label_formatting() {
        assert_eq!(label(0), "Nothing waiting for you");
        assert_eq!(label(1), "1 waiting for you");
        assert_eq!(label(5), "5 waiting for you");
    }

    #[test]
    fn questions_count_and_clear() {
        let mut a = Attention::new();
        a.on_pending(3, 9, "question-1");
        assert_eq!(a.count(), 1, "a waiting question counts");
        // question.resolved routes to the same by-request-id clear as
        // permission.resolved.
        a.on_permission_resolved("question-1");
        assert_eq!(a.count(), 0);
    }

    #[test]
    fn run_ended_clears_a_question_entry() {
        let mut a = Attention::new();
        a.on_pending(3, 9, "question-1");
        a.on_pending(4, 10, "perm-1");
        a.on_run_ended(3);
        assert_eq!(a.count(), 1);
        assert_eq!(a.most_recent(), Some(PendingRef { task_id: 4, chat_id: 10, request_id: "perm-1".to_string() }));
    }
}
