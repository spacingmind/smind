//! DT5 C/AC12: the process-wide coalescer cell and the single entry
//! point every banner goes through -- the watcher's shown notifications
//! and the held list's flush alike. The lock is held only around
//! `offer`/`flush_due`, never across `notify::show` or an await.

use std::sync::{Mutex, MutexGuard, OnceLock};
use std::time::{Duration, Instant};

use tauri::Manager;

use smind_daemon_client::dedupe::Dedupe;
use smind_daemon_client::NotifyKind;

use crate::coalesce::{self, Banner, Coalescer};
use crate::notify::{self, ClickContext, Target};
use crate::state::DesktopState;

/// DT5 B/AC6, moved here by DT5 C: how many distinct (kind, id)
/// notifications the process remembers as already SHOWN (FIFO-evicted
/// past this). A notification is recorded when it is offered to the
/// coalescer, never when it is dropped or held: a held or dropped
/// request that is still pending must be able to notify later.
const DEDUPE_CAP: usize = 256;

static SHOWN: OnceLock<Mutex<Dedupe>> = OnceLock::new();

fn shown() -> MutexGuard<'static, Dedupe> {
    SHOWN
        .get_or_init(|| Mutex::new(Dedupe::new(DEDUPE_CAP)))
        .lock()
        .unwrap_or_else(|e| e.into_inner())
}

/// already_shown is whether a banner for this (kind, id) was already
/// offered; it does not record anything.
pub fn already_shown(kind: NotifyKind, id: &str) -> bool {
    shown().has_seen(kind, id)
}

/// still_wanted_fn is the flush-time re-check: a queued permission or
/// question is dropped when its request is no longer pending.
fn still_wanted_fn(app: &tauri::AppHandle) -> impl Fn(&coalesce::Item) -> bool {
    let app = app.clone();
    move |item| {
        let tray = app.state::<DesktopState>().tray.clone();
        item.still_wanted(&|id| tray.is_pending(id))
    }
}

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
    // Recorded here, at the moment a banner is actually going out; a
    // second submission of the same (kind, id) is silent.
    if !shown().first_time(item.kind, &item.request_id) {
        return;
    }
    let wanted = still_wanted_fn(&ctx.app);
    let banners = {
        let mut coalescer = locked(COALESCER.get_or_init(|| Mutex::new(Coalescer::new(WINDOW))));
        coalescer.offer(Instant::now(), item, &wanted)
    };
    // offer returns an empty list exactly when the item queued inside
    // the open window (a due window flushes first and the item then
    // shows at once), so this is "nothing was shown for this item".
    let queued = banners.is_empty();
    for banner in banners {
        show(&ctx, banner);
    }
    if queued {
        let wanted = still_wanted_fn(&ctx.app);
        tauri::async_runtime::spawn(async move {
            tokio::time::sleep(FLUSH_AFTER).await;
            let banner = {
                let mut coalescer =
                    locked(COALESCER.get_or_init(|| Mutex::new(Coalescer::new(WINDOW))));
                coalescer.flush_due(Instant::now(), &wanted)
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
