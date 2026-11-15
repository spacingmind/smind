//! DT5 C/AC12: the process-wide coalescer cell and the single entry
//! point every banner goes through -- the watcher's shown notifications
//! and the held list's flush alike. The lock is held only around
//! `offer`/`flush_due`, never across `notify::show` or an await.

use std::sync::{Mutex, MutexGuard, OnceLock};
use std::time::{Duration, Instant};

use crate::coalesce::{self, Banner, Coalescer};
use crate::notify::{self, ClickContext, Target};

/// The coalescing window (AC12).
const WINDOW: Duration = Duration::from_secs(3);

/// How long after a queued offer the scheduled flush task wakes: past
/// the window plus slack. Extra tasks are harmless -- `flush_due`
/// returns None when the window is not due or nothing queued.
const FLUSH_AFTER: Duration = Duration::from_millis(3100);

static COALESCER: OnceLock<Mutex<Coalescer>> = OnceLock::new();

/// Locks the coalescer, taking the guard even from a poisoned mutex
/// (the notify_test limiter's pattern): the cell holds only window
/// state, so a panic elsewhere must not turn every later banner into a
/// panic of its own.
fn locked(cell: &Mutex<Coalescer>) -> MutexGuard<'_, Coalescer> {
    cell.lock().unwrap_or_else(|e| e.into_inner())
}

/// submit offers one item to the coalescer and shows whatever comes
/// back at once; an item that queued inside the open window schedules
/// exactly one later flush.
pub fn submit(ctx: ClickContext, item: coalesce::Item) {
    let banners = {
        let mut coalescer = locked(COALESCER.get_or_init(|| Mutex::new(Coalescer::new(WINDOW))));
        coalescer.offer(Instant::now(), item)
    };
    // offer returns an empty list exactly when the item queued inside
    // the open window (a due window flushes first and the item then
    // shows at once), so this is "nothing was shown for this item".
    let queued = banners.is_empty();
    for banner in banners {
        show(&ctx, banner);
    }
    if queued {
        tauri::async_runtime::spawn(async move {
            tokio::time::sleep(FLUSH_AFTER).await;
            let banner = {
                let mut coalescer =
                    locked(COALESCER.get_or_init(|| Mutex::new(Coalescer::new(WINDOW))));
                coalescer.flush_due(Instant::now())
            };
            if let Some(banner) = banner {
                show(&ctx, banner);
            }
        });
    }
}

/// show displays one banner through the platform notification path,
/// with the item's own click target or, for a summary, the latest
/// item's.
fn show(ctx: &ClickContext, banner: Banner) {
    match banner {
        Banner::One(item) => notify::show(
            ctx.clone(),
            item.title,
            item.body,
            Target {
                task_id: item.task_id,
                chat_id: item.chat_id,
                request_id: Some(item.request_id),
            },
        ),
        Banner::Many {
            task_id,
            chat_id,
            request_id,
            title,
            body,
            ..
        } => notify::show(
            ctx.clone(),
            title,
            body,
            Target {
                task_id,
                chat_id,
                request_id: Some(request_id),
            },
        ),
    }
}
