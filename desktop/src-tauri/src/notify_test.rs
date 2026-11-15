//! DT5 B/AC8: the consent-flow test banner. The Settings UI calls this
//! exactly when the person turns desktop notifications on: sending the
//! first notification is what makes macOS ask for its own permission,
//! so it deliberately does not check the consent setting itself.

use std::sync::{Mutex, MutexGuard, OnceLock};
use std::time::{Duration, Instant};

use tauri::{AppHandle, State};

use crate::notify::{self, ClickContext, Target};
use crate::state::DesktopState;

/// How long after one test banner the next is refused. The command takes no
/// argument and skips the consent check by design, so without a limit a page
/// script (or a render loop) could flood the OS with banners.
const TEST_WINDOW: Duration = Duration::from_secs(5);

/// The pure limiter: `allow` is true at most once per `window`, measured from
/// the last ALLOWED call (a refused call does not extend the window). Time is
/// passed in so the policy is unit-tested without sleeping.
struct Limiter {
    last: Option<Instant>,
    window: Duration,
}

impl Limiter {
    fn new(window: Duration) -> Self {
        Self { last: None, window }
    }

    fn allow(&mut self, now: Instant) -> bool {
        if let Some(last) = self.last {
            if now.saturating_duration_since(last) < self.window {
                return false;
            }
        }
        self.last = Some(now);
        true
    }
}

/// Locks the limiter, taking the guard even from a poisoned mutex: the limiter holds only a timestamp, so a panic
/// elsewhere must not turn every later call into a panic of its own.
fn locked(limiter: &Mutex<Limiter>) -> MutexGuard<'_, Limiter> {
    limiter.lock().unwrap_or_else(|e| e.into_inner())
}

#[tauri::command]
pub fn notify_test(app: AppHandle, state: State<'_, DesktopState>) -> Result<(), String> {
    static LIMITER: OnceLock<Mutex<Limiter>> = OnceLock::new();
    let allowed =
        locked(LIMITER.get_or_init(|| Mutex::new(Limiter::new(TEST_WINDOW)))).allow(Instant::now());
    if !allowed {
        return Err("A test notification was just sent. Wait a few seconds and try again.".into());
    }
    let ctx = ClickContext {
        app: app.clone(),
        cache: state.cache.clone(),
        proxy_url: state.proxy_url.clone(),
    };
    notify::show(
        ctx,
        "sMind: notifications are on".into(),
        "You will see a banner like this when an agent needs you.".into(),
        Target::task(0),
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_second_call_inside_the_window_is_refused() {
        let t0 = Instant::now();
        let mut l = Limiter::new(Duration::from_secs(5));
        assert!(l.allow(t0), "the first call is allowed");
        assert!(!l.allow(t0 + Duration::from_secs(1)));
        assert!(!l.allow(t0 + Duration::from_millis(4999)));
    }

    #[test]
    fn a_call_after_the_window_is_allowed_again() {
        let t0 = Instant::now();
        let mut l = Limiter::new(Duration::from_secs(5));
        assert!(l.allow(t0));
        assert!(
            l.allow(t0 + Duration::from_secs(5)),
            "exactly at the window is allowed"
        );
    }

    #[test]
    fn a_refused_call_does_not_extend_the_window() {
        let t0 = Instant::now();
        let mut l = Limiter::new(Duration::from_secs(5));
        assert!(l.allow(t0));
        assert!(!l.allow(t0 + Duration::from_secs(4)), "refused");
        assert!(
            l.allow(t0 + Duration::from_secs(5)),
            "still measured from the last allowed call"
        );
    }

    #[test]
    fn a_poisoned_lock_still_limits() {
        let t0 = Instant::now();
        let limiter = Mutex::new(Limiter::new(Duration::from_secs(5)));
        // Poison it: a thread panics while holding the guard.
        std::thread::scope(|scope| {
            let result = scope
                .spawn(|| {
                    let _guard = limiter.lock().unwrap();
                    panic!("poison the limiter");
                })
                .join();
            assert!(result.is_err());
        });
        assert!(limiter.is_poisoned());
        assert!(
            locked(&limiter).allow(t0),
            "a poisoned lock does not panic the next call"
        );
        assert!(
            !locked(&limiter).allow(t0 + Duration::from_secs(1)),
            "and it still limits"
        );
    }

    #[test]
    fn a_burst_of_calls_yields_one_banner() {
        let t0 = Instant::now();
        let mut l = Limiter::new(TEST_WINDOW);
        let allowed = (0..200)
            .filter(|i| l.allow(t0 + Duration::from_millis(*i)))
            .count();
        assert_eq!(allowed, 1);
    }
}
