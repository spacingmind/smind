//! DT5 C/AC12: banner coalescing. The first banner of a 3-second
//! window shows at once; banners that arrive inside the window queue
//! and, when it ends, one queued banner shows as itself while two or
//! more collapse into a single "sMind: N things need you" banner whose
//! click opens the most recent card. Time is injected throughout so
//! the window logic is unit-tested without sleeping.

use std::time::{Duration, Instant};

use smind_daemon_client::NotifyKind;

/// The fixed body of a collapsed banner: it never names what is
/// waiting, only that something is (AC12).
pub const SUMMARY_BODY: &str = "Open sMind to see what is waiting.";

/// One showable notification, decoupled from its kind (the coalescer
/// only moves text and a click target).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Item {
    pub kind: NotifyKind,
    pub task_id: i64,
    pub chat_id: i64,
    pub request_id: String,
    pub title: String,
    pub body: String,
}

/// What a flush shows: a single item as itself, or the collapse of
/// several. `Many`'s task/chat/request are the LATEST item's (the most
/// recent card, per AC12), with the fixed summary text.
#[derive(Debug, PartialEq, Eq)]
pub enum Banner {
    One(Item),
    Many {
        count: usize,
        task_id: i64,
        chat_id: i64,
        request_id: String,
        title: String,
        body: String,
    },
}

impl Item {
    /// still_wanted is whether showing this item is still worth it: a
    /// permission or question only while its request is pending, a failed
    /// run always (it has no pending state). `is_pending` is asked by
    /// request id.
    pub fn still_wanted(&self, is_pending: &dyn Fn(&str) -> bool) -> bool {
        match self.kind {
            NotifyKind::Permission | NotifyKind::Question => is_pending(&self.request_id),
            NotifyKind::Failure => true,
        }
    }
}

/// summary_title is the collapsed banner's title for `count` queued
/// items.
fn summary_title(count: usize) -> String {
    format!("sMind: {count} things need you")
}

pub struct Coalescer {
    window: Duration,
    start: Option<Instant>,
    queued: Vec<Item>,
}

impl Coalescer {
    pub fn new(window: Duration) -> Self {
        Self {
            window,
            start: None,
            queued: Vec::new(),
        }
    }

    /// offer feeds one item in: anything due flushes first (a window
    /// that just ended does not swallow the new arrival), then the item
    /// either shows at once -- opening a new window -- or queues inside
    /// the open one.
    pub fn offer(
        &mut self,
        now: Instant,
        item: Item,
        wanted: &dyn Fn(&Item) -> bool,
    ) -> Vec<Banner> {
        let mut banners = Vec::new();
        if let Some(banner) = self.flush_due(now, wanted) {
            banners.push(banner);
        }
        if self.start.is_none() {
            self.start = Some(now);
            banners.push(Banner::One(item));
        } else {
            self.queued.push(item);
        }
        banners
    }

    /// flush_due collapses the queue when the window has ended: nothing
    /// queued closes it silently, one item shows as itself, two or more
    /// become one summary banner aimed at the latest item's card. The
    /// window then closes; the next offer opens a fresh one.
    ///
    /// Queued items are re-checked with `wanted` at flush time (a request
    /// that resolved while queued is dropped, so a summary never aims at
    /// a resolved card).
    pub fn flush_due(&mut self, now: Instant, wanted: &dyn Fn(&Item) -> bool) -> Option<Banner> {
        let start = self.start?;
        if now.duration_since(start) < self.window {
            return None;
        }
        self.start = None;
        let mut queued = std::mem::take(&mut self.queued);
        queued.retain(|item| wanted(item));
        let latest = queued.last()?;
        let count = queued.len();
        if count == 1 {
            return Some(Banner::One(latest.clone()));
        }
        Some(Banner::Many {
            count,
            task_id: latest.task_id,
            chat_id: latest.chat_id,
            request_id: latest.request_id.clone(),
            title: summary_title(count),
            body: SUMMARY_BODY.to_string(),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const WINDOW: Duration = Duration::from_secs(3);

    const ALL: &dyn Fn(&Item) -> bool = &|_| true;

    fn item(task_id: i64, tag: &str) -> Item {
        Item {
            kind: NotifyKind::Permission,
            task_id,
            chat_id: task_id * 10,
            request_id: format!("req-{tag}"),
            title: format!("sMind: {tag}"),
            body: format!("body {tag}"),
        }
    }

    #[test]
    fn first_shows_rest_collapse() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        assert_eq!(
            c.offer(t0, item(1, "a"), ALL),
            vec![Banner::One(item(1, "a"))]
        );
        assert!(c
            .offer(t0 + Duration::from_secs(1), item(2, "b"), ALL)
            .is_empty());
        assert!(c
            .offer(t0 + Duration::from_secs(2), item(3, "c"), ALL)
            .is_empty());
        assert_eq!(
            c.flush_due(t0 + WINDOW, ALL),
            Some(Banner::Many {
                count: 2,
                task_id: 3,
                chat_id: 30,
                request_id: "req-c".to_string(),
                title: "sMind: 2 things need you".to_string(),
                body: SUMMARY_BODY.to_string(),
            })
        );
    }

    #[test]
    fn one_queued_item_shows_as_itself() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        c.offer(t0, item(1, "a"), ALL);
        c.offer(t0 + Duration::from_secs(1), item(2, "b"), ALL);
        assert_eq!(
            c.flush_due(t0 + WINDOW, ALL),
            Some(Banner::One(item(2, "b")))
        );
    }

    #[test]
    fn nothing_queued_closes_the_window_silently() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        c.offer(t0, item(1, "a"), ALL);
        assert_eq!(c.flush_due(t0 + WINDOW, ALL), None);
        assert_eq!(
            c.offer(t0 + Duration::from_secs(4), item(2, "b"), ALL),
            vec![Banner::One(item(2, "b"))],
            "a following offer starts a new window and shows at once"
        );
    }

    #[test]
    fn the_window_restarts_after_a_flush() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        c.offer(t0, item(1, "a"), ALL);
        c.offer(t0 + Duration::from_secs(1), item(2, "b"), ALL);
        assert_eq!(
            c.flush_due(t0 + WINDOW, ALL),
            Some(Banner::One(item(2, "b")))
        );
        // The flushed window is gone: the next item opens a fresh one.
        assert_eq!(
            c.offer(t0 + Duration::from_millis(3500), item(3, "c"), ALL),
            vec![Banner::One(item(3, "c"))]
        );
        assert!(
            c.offer(t0 + Duration::from_millis(4000), item(4, "d"), ALL)
                .is_empty(),
            "inside the new window, queued"
        );
        assert_eq!(
            c.flush_due(t0 + Duration::from_millis(6500), ALL),
            Some(Banner::One(item(4, "d")))
        );
    }

    #[test]
    fn the_summary_targets_the_latest() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        c.offer(t0, item(1, "first"), ALL);
        c.offer(t0 + Duration::from_millis(100), item(2, "middle"), ALL);
        c.offer(t0 + Duration::from_millis(200), item(3, "later"), ALL);
        c.offer(t0 + Duration::from_millis(300), item(7, "last"), ALL);
        let Some(Banner::Many {
            count,
            task_id,
            chat_id,
            request_id,
            ..
        }) = c.flush_due(t0 + WINDOW, ALL)
        else {
            panic!("three queued items collapse into one banner");
        };
        assert_eq!(count, 3);
        assert_eq!((task_id, chat_id, request_id.as_str()), (7, 70, "req-last"));
    }

    #[test]
    fn the_summary_text_is_fixed() {
        let t0 = Instant::now();
        for count in [2usize, 5] {
            let mut c = Coalescer::new(WINDOW);
            for i in 0..=count {
                let item = Item {
                    kind: NotifyKind::Permission,
                    task_id: 1,
                    chat_id: 1,
                    request_id: format!("MARKER-req-{i}"),
                    title: format!("MARKER-title-{i}"),
                    body: format!("MARKER-body-{i}"),
                };
                if i == 0 {
                    c.offer(t0, item, ALL);
                } else {
                    c.offer(t0 + Duration::from_millis(100 * i as u64), item, ALL);
                }
            }
            let Some(Banner::Many { title, body, .. }) = c.flush_due(t0 + WINDOW, ALL) else {
                panic!("{count} queued items collapse into one banner");
            };
            assert_eq!(title, format!("sMind: {count} things need you"));
            assert_eq!(body, SUMMARY_BODY);
            assert!(
                !title.contains("MARKER"),
                "no item text appears in the title"
            );
            assert!(!body.contains("MARKER"), "no item text appears in the body");
        }
    }

    #[test]
    fn an_offer_after_the_window_flushes_first() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        c.offer(t0, item(1, "a"), ALL);
        assert!(c
            .offer(t0 + Duration::from_secs(1), item(2, "b"), ALL)
            .is_empty());
        assert_eq!(
            c.offer(t0 + Duration::from_secs(4), item(3, "c"), ALL),
            vec![Banner::One(item(2, "b")), Banner::One(item(3, "c"))],
            "the queued item flushes, then the new one opens a window"
        );
    }

    #[test]
    fn a_request_resolved_while_queued_is_not_the_click_target() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        c.offer(t0, item(1, "a"), ALL);
        c.offer(t0 + Duration::from_millis(100), item(2, "b"), ALL);
        c.offer(t0 + Duration::from_millis(200), item(3, "c"), ALL);
        c.offer(t0 + Duration::from_millis(300), item(4, "latest"), ALL);
        // The latest request resolved while it waited in the window.
        let pending = |id: &str| id != "req-latest";
        let wanted = |i: &Item| i.still_wanted(&pending);
        let Some(Banner::Many {
            count, request_id, ..
        }) = c.flush_due(t0 + WINDOW, &wanted)
        else {
            panic!("two still-pending items collapse");
        };
        assert_eq!(count, 2);
        assert_eq!(request_id, "req-c", "the resolved card is not the target");
    }

    #[test]
    fn everything_resolved_while_queued_shows_nothing() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        c.offer(t0, item(1, "a"), ALL);
        c.offer(t0 + Duration::from_millis(100), item(2, "b"), ALL);
        let none_pending = |_: &str| false;
        let wanted = |i: &Item| i.still_wanted(&none_pending);
        assert_eq!(c.flush_due(t0 + WINDOW, &wanted), None);
    }

    #[test]
    fn a_queued_failure_survives_the_pending_check() {
        let t0 = Instant::now();
        let mut c = Coalescer::new(WINDOW);
        c.offer(t0, item(1, "a"), ALL);
        let mut failed = item(2, "run-9");
        failed.kind = NotifyKind::Failure;
        c.offer(t0 + Duration::from_millis(100), failed.clone(), ALL);
        let none_pending = |_: &str| false;
        let wanted = |i: &Item| i.still_wanted(&none_pending);
        assert_eq!(c.flush_due(t0 + WINDOW, &wanted), Some(Banner::One(failed)));
    }
}
