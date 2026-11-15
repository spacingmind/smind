//! DT5 C/AC9-11: the web app's attention report and what it decides
//! about a banner. The shell never guesses focus from the webview URL
//! (it cannot see the composer); the web side reports
//! `{ taskId, windowFocused, composerFocused }` on every change and
//! this module keeps the latest one in memory only. A missing report
//! means no suppression: a bug on the web side must never hide a
//! banner (fail open).

use std::collections::VecDeque;
use std::sync::{Mutex, MutexGuard, OnceLock};

use serde::Deserialize;

use smind_daemon_client::NotifyKind;

/// The web app's attention state (AC9), exactly as the IPC command
/// receives it: the task in front (null when none), whether the window
/// and the composer have focus.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct FocusReport {
    pub task_id: Option<i64>,
    pub window_focused: bool,
    pub composer_focused: bool,
}

/// validate rejects a report whose taskId is neither null nor a
/// positive task id (the command returns the error to the caller).
pub fn validate(r: &FocusReport) -> Result<(), String> {
    if r.task_id.is_some_and(|id| id <= 0) {
        Err("taskId must be null or a positive task id".to_string())
    } else {
        Ok(())
    }
}

/// What happens to a notification under the current focus (AC10/AC11):
/// show it, drop it (the task in front is its own task), or hold it
/// (the person is typing in another task's composer).
#[derive(Debug, PartialEq, Eq)]
pub enum Decision {
    Show,
    Drop,
    Hold,
}

/// decide applies the focus rules to one notification: no report means
/// no suppression; the window focused on the notification's own task
/// drops it (a failed run there too); the window focused with the
/// composer focused holds a banner for another task; anything else
/// shows.
pub fn decide(report: Option<FocusReport>, task_id: i64) -> Decision {
    let Some(r) = report else {
        return Decision::Show;
    };
    if r.window_focused && r.task_id == Some(task_id) {
        Decision::Drop
    } else if r.window_focused && r.composer_focused {
        Decision::Hold
    } else {
        Decision::Show
    }
}

/// One notification parked while the person typed (AC11): everything a
/// later flush needs to show it as if it had just arrived.
#[derive(Debug, Clone)]
pub struct HeldItem {
    pub kind: NotifyKind,
    pub task_id: i64,
    pub chat_id: i64,
    pub request_id: String,
    pub title: String,
    pub body: String,
}

/// The bounded hold list: oldest dropped past `cap`, flushed at most
/// once (a flush empties it).
pub struct Held {
    items: VecDeque<HeldItem>,
    cap: usize,
}

impl Held {
    pub fn new(cap: usize) -> Self {
        Self {
            items: VecDeque::new(),
            cap,
        }
    }

    /// push parks an item, dropping the OLDEST when the list is over
    /// its cap.
    pub fn push(&mut self, item: HeldItem) {
        if self.items.len() >= self.cap {
            self.items.pop_front();
        }
        self.items.push_back(item);
    }

    pub fn len(&self) -> usize {
        self.items.len()
    }

    pub fn is_empty(&self) -> bool {
        self.items.is_empty()
    }

    /// drop_task removes every held item of the task that just came to
    /// the front (AC11: nothing replays over the task being viewed).
    pub fn drop_task(&mut self, task_id: i64) {
        self.items.retain(|item| item.task_id != task_id);
    }

    /// take_wanted empties the list and returns, in arrival order, the
    /// items still worth showing: a permission or question only while
    /// its request is still pending, a failure always (it has no
    /// pending state).
    pub fn take_wanted(&mut self, is_pending: impl Fn(&str) -> bool) -> Vec<HeldItem> {
        std::mem::take(&mut self.items)
            .into_iter()
            .filter(|item| match item.kind {
                NotifyKind::Permission | NotifyKind::Question => is_pending(&item.request_id),
                NotifyKind::Failure => true,
            })
            .collect()
    }
}

/// How many notifications may be parked while the person types (AC11).
const HELD_CAP: usize = 32;

static LAST_REPORT: OnceLock<Mutex<Option<FocusReport>>> = OnceLock::new();
static HELD: OnceLock<Mutex<Held>> = OnceLock::new();

/// Locks one of the global cells, taking the guard even from a poisoned
/// mutex (the notify_test limiter's pattern): the cells hold only focus
/// state, so a panic elsewhere must not turn every later call into a
/// panic of its own.
fn locked<T>(cell: &Mutex<T>) -> MutexGuard<'_, T> {
    cell.lock().unwrap_or_else(|e| e.into_inner())
}

/// current is the latest report, or None when the web side has not sent
/// one yet (no suppression then).
pub fn current() -> Option<FocusReport> {
    *locked(LAST_REPORT.get_or_init(|| Mutex::new(None)))
}

/// hold parks a notification while the person types.
pub fn hold(item: HeldItem) {
    locked(HELD.get_or_init(|| Mutex::new(Held::new(HELD_CAP)))).push(item);
}

#[cfg(test)]
mod tests {
    use super::*;

    fn report(task_id: Option<i64>, window: bool, composer: bool) -> FocusReport {
        FocusReport {
            task_id,
            window_focused: window,
            composer_focused: composer,
        }
    }

    fn held_item(kind: NotifyKind, task_id: i64, request_id: &str) -> HeldItem {
        HeldItem {
            kind,
            task_id,
            chat_id: task_id * 10,
            request_id: request_id.to_string(),
            title: kind.fixed_title().to_string(),
            body: kind.fixed_body().to_string(),
        }
    }

    #[test]
    fn no_banner_when_focused_on_that_task() {
        let focused_on_it = report(Some(1), true, false);
        assert_eq!(decide(Some(focused_on_it), 1), Decision::Drop);
        // Typing in that very task's composer changes nothing: the
        // task-in-front rule comes first.
        let typing_in_it = report(Some(1), true, true);
        assert_eq!(decide(Some(typing_in_it), 1), Decision::Drop);
    }

    #[test]
    fn a_banner_for_another_task_shows() {
        let focused_on_1 = report(Some(1), true, false);
        assert_eq!(decide(Some(focused_on_1), 2), Decision::Show);
    }

    #[test]
    fn no_report_means_no_suppression() {
        assert_eq!(decide(None, 1), Decision::Show);
    }

    #[test]
    fn held_while_the_composer_is_focused() {
        let typing = report(Some(1), true, true);
        assert_eq!(decide(Some(typing), 2), Decision::Hold);
    }

    #[test]
    fn an_unfocused_window_shows_everything() {
        for task_id in [1, 2] {
            for composer in [false, true] {
                let blurred = report(Some(1), false, composer);
                assert_eq!(
                    decide(Some(blurred), task_id),
                    Decision::Show,
                    "task {task_id}, composer {composer}: a blurred window never suppresses"
                );
            }
        }
    }

    #[test]
    fn held_items_flush_once_when_the_composer_loses_focus() {
        let mut held = Held::new(32);
        held.push(held_item(NotifyKind::Permission, 2, "req-a"));
        held.push(held_item(NotifyKind::Question, 3, "req-b"));
        let flushed = held.take_wanted(|_| true);
        assert_eq!(
            flushed
                .iter()
                .map(|i| i.request_id.as_str())
                .collect::<Vec<_>>(),
            vec!["req-a", "req-b"],
            "both, in arrival order"
        );
        assert!(
            held.take_wanted(|_| true).is_empty(),
            "the second flush returns nothing"
        );
        assert_eq!(held.len(), 0);
    }

    #[test]
    fn held_items_resolved_meanwhile_are_dropped() {
        let mut held = Held::new(32);
        held.push(held_item(NotifyKind::Permission, 2, "resolved-perm"));
        held.push(held_item(NotifyKind::Question, 3, "resolved-question"));
        held.push(held_item(NotifyKind::Question, 3, "still-pending"));
        let flushed = held.take_wanted(|id| id == "still-pending");
        assert_eq!(
            flushed
                .iter()
                .map(|i| i.request_id.as_str())
                .collect::<Vec<_>>(),
            vec!["still-pending"],
            "a resolved permission and question are not replayed"
        );
    }

    #[test]
    fn a_held_failure_is_not_dropped() {
        let mut held = Held::new(32);
        held.push(held_item(NotifyKind::Failure, 2, "run-gone"));
        let flushed = held.take_wanted(|_| false);
        assert_eq!(
            flushed
                .iter()
                .map(|i| i.request_id.as_str())
                .collect::<Vec<_>>(),
            vec!["run-gone"],
            "a failure has no pending state and always flushes"
        );
    }

    #[test]
    fn the_held_list_is_bounded() {
        let mut held = Held::new(3);
        for id in ["r1", "r2", "r3", "r4", "r5"] {
            held.push(held_item(NotifyKind::Permission, 2, id));
        }
        assert_eq!(held.len(), 3, "cap 3 with 5 pushes");
        let flushed = held.take_wanted(|_| true);
        assert_eq!(
            flushed
                .iter()
                .map(|i| i.request_id.as_str())
                .collect::<Vec<_>>(),
            vec!["r3", "r4", "r5"],
            "the two oldest are gone"
        );
    }

    #[test]
    fn drop_task_removes_that_tasks_items() {
        let mut held = Held::new(32);
        held.push(held_item(NotifyKind::Permission, 1, "t1-a"));
        held.push(held_item(NotifyKind::Permission, 2, "t2-a"));
        held.push(held_item(NotifyKind::Question, 1, "t1-b"));
        held.drop_task(1);
        let flushed = held.take_wanted(|_| true);
        assert_eq!(
            flushed
                .iter()
                .map(|i| i.request_id.as_str())
                .collect::<Vec<_>>(),
            vec!["t2-a"],
            "only the other task's items survive"
        );
    }

    #[test]
    fn the_command_rejects_a_non_positive_task_id() {
        assert!(validate(&report(Some(0), true, false)).is_err());
        assert!(validate(&report(Some(-4), false, false)).is_err());
        assert!(validate(&report(Some(3), true, true)).is_ok());
        assert!(validate(&report(None, false, false)).is_ok());
    }
}
