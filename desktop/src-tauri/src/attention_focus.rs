//! DT5 C/AC9-11: the web app's attention report and what it decides
//! about a banner. The shell never guesses focus from the webview URL
//! (it cannot see the composer); the web side reports
//! `{ taskId, windowFocused, composerFocused }` on every change and on a
//! heartbeat, and this module keeps the latest one in memory only.
//!
//! The rule is "never lose a banner that matters", so every way the
//! report can go wrong fails OPEN:
//! - a report older than [`REPORT_TTL`] counts as no report (a crashed
//!   webview, a sleep/wake, or a page that stopped heartbeating never
//!   suppresses anything);
//! - a banner held while the person types is delivered anyway after
//!   [`MAX_HOLD`];
//! - a banner dropped or held is never recorded as shown, so it can
//!   still notify when the person leaves.
//!
//! All of it lives in one pure, time-injected [`Focus`] behind ONE lock,
//! so "decide, then hold" and "typing stopped, flush" can never
//! interleave (a banner cannot be parked just after the flush that
//! would have released it).

use std::collections::VecDeque;
use std::sync::{Mutex, MutexGuard, OnceLock};
use std::time::{Duration, Instant};

use serde::Deserialize;
use tauri::{AppHandle, Manager, State};

use smind_daemon_client::NotifyKind;

use crate::banner;
use crate::coalesce::Item;
use crate::notify::ClickContext;
use crate::state::DesktopState;

/// How long a report stays authoritative without being renewed (the web
/// side heartbeats well inside this while the window is focused).
pub const REPORT_TTL: Duration = Duration::from_secs(20);

/// The longest a banner is held while the person types in another
/// task's composer; it shows anyway after this.
pub const MAX_HOLD: Duration = Duration::from_secs(30);

/// After a reconnect the daemon re-sends its still-pending requests;
/// held requests it does not re-send within this time are treated as
/// resolved while the shell was disconnected.
pub const RESYNC_GRACE: Duration = Duration::from_secs(15);

/// How many notifications may be parked at once; past it the OLDEST is
/// delivered (never discarded).
const PARK_CAP: usize = 32;

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

/// One parked notification: dropped or held for now, still wanted.
#[derive(Debug, Clone)]
struct Parked {
    item: Item,
    since: Instant,
    /// Set by a reconnect: the local pending set was just reset, so the
    /// daemon's re-send is what confirms the request is still pending.
    unverified_since: Option<Instant>,
}

/// The attention state: the latest report with its arrival time, and
/// the parked notifications in arrival order.
pub struct Focus {
    report: Option<(FocusReport, Instant)>,
    parked: VecDeque<Parked>,
    cap: usize,
}

impl Focus {
    pub fn new(cap: usize) -> Self {
        Self {
            report: None,
            parked: VecDeque::new(),
            cap,
        }
    }

    /// effective is the report that still counts: None when there is
    /// none or it is older than the TTL (fail open).
    pub fn effective(&self, now: Instant) -> Option<FocusReport> {
        let (report, at) = self.report?;
        (now.saturating_duration_since(at) < REPORT_TTL).then_some(report)
    }

    #[cfg(test)]
    pub fn len(&self) -> usize {
        self.parked.len()
    }

    pub fn is_empty(&self) -> bool {
        self.parked.is_empty()
    }

    /// route sends one arriving notification through the rules and
    /// returns what must show NOW: the item itself, or -- when parking
    /// it overflowed the cap -- the oldest parked item. A redelivery
    /// (same kind and request id, e.g. after a reconnect) replaces the
    /// copy already parked and confirms it.
    pub fn route(&mut self, now: Instant, item: Item) -> Vec<Item> {
        self.parked
            .retain(|p| !(p.item.kind == item.kind && p.item.request_id == item.request_id));
        match decide(self.effective(now), item.task_id) {
            Decision::Show => vec![item],
            // A failed run for the task in front is seen where it
            // happened: it is not parked and does not re-notify.
            Decision::Drop if item.kind == NotifyKind::Failure => Vec::new(),
            Decision::Drop | Decision::Hold => {
                self.parked.push_back(Parked {
                    item,
                    since: now,
                    unverified_since: None,
                });
                if self.parked.len() > self.cap {
                    self.parked
                        .pop_front()
                        .map(|p| vec![p.item])
                        .unwrap_or_default()
                } else {
                    Vec::new()
                }
            }
        }
    }

    /// apply stores a report and returns what it releases.
    pub fn apply(
        &mut self,
        now: Instant,
        report: FocusReport,
        is_pending: &dyn Fn(&str) -> bool,
    ) -> Vec<Item> {
        self.report = Some((report, now));
        self.release(now, is_pending)
    }

    /// on_reconnected marks every parked permission and question as
    /// awaiting the daemon's re-send (see [`RESYNC_GRACE`]).
    pub fn on_reconnected(&mut self, now: Instant) {
        for p in &mut self.parked {
            if p.item.kind != NotifyKind::Failure {
                p.unverified_since = Some(now);
            }
        }
    }

    /// release walks the parked items in arrival order and returns the
    /// ones that must show now, removing them:
    /// - a permission or question whose request resolved is dropped
    ///   (after a reconnect, only once the resync grace has passed
    ///   without the daemon re-sending it);
    /// - a failure for the task now in front is dropped (it was seen);
    /// - anything the current focus would show is delivered;
    /// - a held item (typing in another task) is delivered anyway once
    ///   it has been held for [`MAX_HOLD`].
    pub fn release(&mut self, now: Instant, is_pending: &dyn Fn(&str) -> bool) -> Vec<Item> {
        let effective = self.effective(now);
        let mut out = Vec::new();
        let mut keep = VecDeque::new();
        for p in std::mem::take(&mut self.parked) {
            if p.item.kind != NotifyKind::Failure {
                match p.unverified_since {
                    Some(t) if now.saturating_duration_since(t) < RESYNC_GRACE => {
                        keep.push_back(p);
                        continue;
                    }
                    Some(_) => continue,
                    None if !is_pending(&p.item.request_id) => continue,
                    None => {}
                }
            }
            match decide(effective, p.item.task_id) {
                Decision::Show => out.push(p.item),
                Decision::Drop if p.item.kind == NotifyKind::Failure => {}
                Decision::Drop => keep.push_back(p),
                Decision::Hold => {
                    if now.saturating_duration_since(p.since) >= MAX_HOLD {
                        out.push(p.item);
                    } else {
                        keep.push_back(p);
                    }
                }
            }
        }
        self.parked = keep;
        out
    }
}

static FOCUS: OnceLock<Mutex<Focus>> = OnceLock::new();

/// Locks the global cell, taking the guard even from a poisoned mutex
/// (the notify_test limiter's pattern): it holds only focus state, so a
/// panic elsewhere must not turn every later call into a panic of its
/// own.
fn focus() -> MutexGuard<'static, Focus> {
    FOCUS
        .get_or_init(|| Mutex::new(Focus::new(PARK_CAP)))
        .lock()
        .unwrap_or_else(|e| e.into_inner())
}

fn click_context(app: &AppHandle, state: &DesktopState) -> ClickContext {
    ClickContext {
        app: app.clone(),
        cache: state.cache.clone(),
        proxy_url: state.proxy_url.clone(),
    }
}

/// deliver shows released items through the coalescer.
fn deliver(app: &AppHandle, items: Vec<Item>) {
    if items.is_empty() {
        return;
    }
    let state = app.state::<DesktopState>();
    for item in items {
        banner::submit(click_context(app, &state), item);
    }
}

/// schedule_sweep re-runs the release after `delay`: the time-based
/// rules (max hold, resync grace, report expiry) need a wake-up when no
/// report arrives.
fn schedule_sweep(app: &AppHandle, delay: Duration) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(delay + Duration::from_millis(100)).await;
        sweep(&app);
    });
}

fn sweep(app: &AppHandle) {
    let state = app.state::<DesktopState>();
    let released = focus().release(Instant::now(), &|id| state.tray.is_pending(id));
    deliver(app, released);
}

/// route_notification is the watcher's entry for a banner that passed
/// consent and the per-kind switch: it shows, drops or parks it under
/// the current focus (see [`Focus::route`]).
pub fn route_notification(app: &AppHandle, item: Item) {
    let (show, parked) = {
        let mut f = focus();
        let show = f.route(Instant::now(), item);
        (show, !f.is_empty())
    };
    deliver(app, show);
    if parked {
        schedule_sweep(app, MAX_HOLD);
    }
}

/// on_reconnected is the watcher's reconnect hook: held requests wait
/// for the daemon's re-send instead of being judged by the just-reset
/// pending set.
pub fn on_reconnected(app: &AppHandle) {
    let parked = {
        let mut f = focus();
        f.on_reconnected(Instant::now());
        !f.is_empty()
    };
    if parked {
        schedule_sweep(app, RESYNC_GRACE);
    }
}

/// attention_focus_report is the web app's attention report (AC9): store
/// it, then deliver whatever it releases -- items parked for the task
/// that was in front once the person leaves, held items the moment
/// typing stops -- dropping requests that resolved meanwhile and
/// coalescing the rest like fresh banners.
#[tauri::command]
pub fn attention_focus_report(
    app: AppHandle,
    state: State<'_, DesktopState>,
    report: FocusReport,
) -> Result<(), String> {
    validate(&report)?;
    let released = focus().apply(Instant::now(), report, &|id| state.tray.is_pending(id));
    deliver(&app, released);
    Ok(())
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

    fn item(kind: NotifyKind, task_id: i64, request_id: &str) -> Item {
        Item {
            kind,
            task_id,
            chat_id: task_id * 10,
            request_id: request_id.to_string(),
            title: kind.fixed_title().to_string(),
            body: kind.fixed_body().to_string(),
        }
    }

    fn perm(task_id: i64, request_id: &str) -> Item {
        item(NotifyKind::Permission, task_id, request_id)
    }

    const PENDING: &dyn Fn(&str) -> bool = &|_| true;

    fn ids(items: &[Item]) -> Vec<&str> {
        items.iter().map(|i| i.request_id.as_str()).collect()
    }

    fn secs(n: u64) -> Duration {
        Duration::from_secs(n)
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
    fn the_command_rejects_a_non_positive_task_id() {
        assert!(validate(&report(Some(0), true, false)).is_err());
        assert!(validate(&report(Some(-4), false, false)).is_err());
        assert!(validate(&report(Some(3), true, true)).is_ok());
        assert!(validate(&report(None, false, false)).is_ok());
    }

    // -- expiry (fail open) ------------------------------------------

    #[test]
    fn a_stale_report_suppresses_nothing() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, false), PENDING);
        assert_eq!(f.effective(t0 + secs(19)).map(|r| r.task_id), Some(Some(1)));
        assert_eq!(f.effective(t0 + REPORT_TTL), None, "expired at the TTL");
        // The webview died while "focused on task 1": a banner for task
        // 1 still shows after the TTL.
        assert_eq!(
            ids(&f.route(t0 + secs(21), perm(1, "r1"))),
            vec!["r1"],
            "a stale 'focused on this task' drops nothing"
        );
    }

    #[test]
    fn a_stuck_composer_flag_does_not_hold_forever() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        // Past the TTL with no heartbeat: nothing is held.
        assert_eq!(ids(&f.route(t0 + secs(25), perm(2, "late"))), vec!["late"]);
    }

    #[test]
    fn a_heartbeat_renews_the_report() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        for beat in 0..4 {
            f.apply(t0 + secs(8 * beat), report(Some(1), true, false), PENDING);
        }
        // 24 s after the first report, 0 s after the last: still valid.
        assert_eq!(
            f.route(t0 + secs(24), perm(1, "r1")).len(),
            0,
            "task in front: dropped (parked), not shown"
        );
    }

    #[test]
    fn a_held_banner_shows_after_the_max_hold() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        assert!(f.route(t0, perm(2, "held")).is_empty(), "held while typing");
        // The page keeps heartbeating "typing" (a stuck flag): the hold
        // still ends.
        for beat in 1..=3 {
            let released = f.apply(t0 + secs(8 * beat), report(Some(1), true, true), PENDING);
            assert!(released.is_empty(), "still held at {} s", 8 * beat);
        }
        let released = f.apply(t0 + MAX_HOLD, report(Some(1), true, true), PENDING);
        assert_eq!(ids(&released), vec!["held"], "delivered at the max hold");
        assert!(f.is_empty());
    }

    // -- hold and flush ----------------------------------------------

    #[test]
    fn held_items_flush_once_when_the_composer_loses_focus() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        assert!(f.route(t0, perm(2, "req-a")).is_empty());
        assert!(f
            .route(t0, item(NotifyKind::Question, 3, "req-b"))
            .is_empty());
        let flushed = f.apply(t0 + secs(1), report(Some(1), true, false), PENDING);
        assert_eq!(
            ids(&flushed),
            vec!["req-a", "req-b"],
            "both, in arrival order"
        );
        assert!(
            f.apply(t0 + secs(2), report(Some(1), false, false), PENDING)
                .is_empty(),
            "the second flush returns nothing"
        );
    }

    #[test]
    fn held_items_resolved_meanwhile_are_dropped() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        f.route(t0, perm(2, "resolved-perm"));
        f.route(t0, item(NotifyKind::Question, 3, "resolved-question"));
        f.route(t0, item(NotifyKind::Question, 3, "still-pending"));
        let flushed = f.apply(t0 + secs(1), report(None, false, false), &|id| {
            id == "still-pending"
        });
        assert_eq!(ids(&flushed), vec!["still-pending"]);
    }

    #[test]
    fn a_held_failure_is_not_dropped() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        f.route(t0, item(NotifyKind::Failure, 2, "run-gone"));
        let flushed = f.apply(t0 + secs(1), report(Some(1), false, false), &|_| false);
        assert_eq!(
            ids(&flushed),
            vec!["run-gone"],
            "a failure has no pending state and always flushes"
        );
    }

    // -- TOCTOU: decide and hold are one step ------------------------

    #[test]
    fn a_blur_before_the_route_shows_and_one_after_releases() {
        let t0 = Instant::now();
        // Order 1: typing stops, THEN the notification arrives: it shows.
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        f.apply(t0 + secs(1), report(Some(1), true, false), PENDING);
        assert_eq!(ids(&f.route(t0 + secs(2), perm(2, "a"))), vec!["a"]);
        // Order 2: the notification arrives while typing, THEN typing
        // stops: the next apply releases it.
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        assert!(f.route(t0 + secs(1), perm(2, "b")).is_empty());
        assert_eq!(
            ids(&f.apply(t0 + secs(2), report(Some(1), true, false), PENDING)),
            vec!["b"]
        );
    }

    #[test]
    fn concurrent_route_and_blur_never_strand_an_item() {
        use std::sync::Arc;
        for _ in 0..200 {
            let t0 = Instant::now();
            let f = Arc::new(Mutex::new(Focus::new(32)));
            f.lock()
                .unwrap()
                .apply(t0, report(Some(1), true, true), PENDING);
            let shown = Arc::new(Mutex::new(Vec::<String>::new()));
            let routing = {
                let (f, shown) = (f.clone(), shown.clone());
                std::thread::spawn(move || {
                    let out = f.lock().unwrap().route(Instant::now(), perm(2, "x"));
                    shown
                        .lock()
                        .unwrap()
                        .extend(out.into_iter().map(|i| i.request_id));
                })
            };
            let blurring = {
                let (f, shown) = (f.clone(), shown.clone());
                std::thread::spawn(move || {
                    let out = f.lock().unwrap().apply(
                        Instant::now(),
                        report(Some(1), true, false),
                        PENDING,
                    );
                    shown
                        .lock()
                        .unwrap()
                        .extend(out.into_iter().map(|i| i.request_id));
                })
            };
            routing.join().unwrap();
            blurring.join().unwrap();
            let f = f.lock().unwrap();
            let shown = shown.lock().unwrap();
            assert_eq!(
                (shown.len(), f.len()),
                (1, 0),
                "whichever ran first, the item was shown exactly once and nothing is parked"
            );
        }
    }

    // -- dropped items re-arm ----------------------------------------

    #[test]
    fn a_dropped_permission_notifies_when_the_person_leaves() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, false), PENDING);
        assert!(
            f.route(t0, perm(1, "seen")).is_empty(),
            "not shown in front"
        );
        // Alt-tab away while it is still pending.
        let released = f.apply(t0 + secs(5), report(Some(1), false, false), PENDING);
        assert_eq!(ids(&released), vec!["seen"], "re-armed on blur");
    }

    #[test]
    fn a_dropped_permission_resolved_before_leaving_never_notifies() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, false), PENDING);
        f.route(t0, perm(1, "seen"));
        let released = f.apply(t0 + secs(5), report(Some(1), false, false), &|_| false);
        assert!(released.is_empty());
        assert!(f.is_empty());
    }

    #[test]
    fn a_held_item_for_the_task_that_comes_to_front_waits_for_the_person_to_leave() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        f.route(t0, perm(2, "for-two"));
        // The person opens task 2 and stops typing: they see its card.
        assert!(f
            .apply(t0 + secs(1), report(Some(2), true, false), PENDING)
            .is_empty());
        let released = f.apply(t0 + secs(2), report(Some(2), false, false), PENDING);
        assert_eq!(ids(&released), vec!["for-two"]);
    }

    #[test]
    fn a_failure_for_the_task_in_front_is_seen_not_replayed() {
        // Intent (AC10): a failed run in the task being viewed is seen
        // where it happened, so it is dropped and does NOT re-notify
        // when the person leaves (a permission or question, which stays
        // actionable, does).
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, false), PENDING);
        assert!(f
            .route(t0, item(NotifyKind::Failure, 1, "run-1"))
            .is_empty());
        assert!(f.is_empty(), "not parked");
        let released = f.apply(t0 + secs(5), report(Some(1), false, false), PENDING);
        assert!(released.is_empty());
        // A failure held while typing elsewhere, then its task comes to
        // front, is likewise discarded.
        f.apply(t0 + secs(6), report(Some(1), true, true), PENDING);
        f.route(t0 + secs(6), item(NotifyKind::Failure, 2, "run-2"));
        f.apply(t0 + secs(7), report(Some(2), true, false), PENDING);
        assert!(f.is_empty(), "discarded when its task came to front");
        assert!(f
            .apply(t0 + secs(8), report(Some(2), false, false), PENDING)
            .is_empty());
    }

    // -- reconnect ----------------------------------------------------

    #[test]
    fn a_reconnect_keeps_held_items_until_the_daemon_resends() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        f.route(t0, perm(2, "kept"));
        f.route(t0, perm(2, "gone"));
        f.on_reconnected(t0 + secs(3));
        // The tray's pending set was just reset: nothing is pending
        // locally. Typing stops; the items must NOT be discarded.
        let early = f.apply(t0 + secs(4), report(Some(1), false, false), &|_| false);
        assert!(early.is_empty(), "waits for the re-send");
        assert_eq!(f.len(), 2);
        // The daemon re-sends only "kept" (the person stopped typing, so
        // it shows at once and is confirmed).
        let shown = f.route(t0 + secs(5), perm(2, "kept"));
        assert_eq!(ids(&shown), vec!["kept"]);
        // After the grace, the one the daemon did not re-send resolved.
        let late = f.release(t0 + secs(3) + RESYNC_GRACE, &|_| false);
        assert!(late.is_empty());
        assert!(f.is_empty(), "'gone' was resolved while disconnected");
    }

    #[test]
    fn a_reconnect_does_not_strand_a_resent_item() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        f.route(t0, perm(2, "r"));
        f.on_reconnected(t0 + secs(1));
        // Re-sent while still typing: parked again, confirmed.
        assert!(f.route(t0 + secs(2), perm(2, "r")).is_empty());
        assert_eq!(f.len(), 1, "no duplicate");
        let released = f.apply(t0 + secs(20), report(Some(1), true, false), PENDING);
        assert_eq!(ids(&released), vec!["r"], "delivered after the grace too");
    }

    // -- cap -----------------------------------------------------------

    #[test]
    fn a_full_park_list_delivers_the_oldest_instead_of_dropping_it() {
        let t0 = Instant::now();
        let mut f = Focus::new(3);
        f.apply(t0, report(Some(1), true, true), PENDING);
        let mut shown = Vec::new();
        for id in ["r1", "r2", "r3", "r4", "r5"] {
            shown.extend(f.route(t0, perm(2, id)));
        }
        assert_eq!(
            ids(&shown),
            vec!["r1", "r2"],
            "the two oldest were delivered"
        );
        assert_eq!(f.len(), 3);
        let rest = f.apply(t0 + secs(1), report(None, false, false), PENDING);
        assert_eq!(ids(&rest), vec!["r3", "r4", "r5"]);
    }

    #[test]
    fn a_redelivery_replaces_the_parked_copy() {
        let t0 = Instant::now();
        let mut f = Focus::new(32);
        f.apply(t0, report(Some(1), true, true), PENDING);
        f.route(t0, perm(2, "same"));
        f.route(t0 + secs(1), perm(2, "same"));
        assert_eq!(f.len(), 1);
    }
}
