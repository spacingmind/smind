//! Reconnect backoff: exponential growth, capped, reset after a stable
//! connection.

use std::time::Duration;

pub struct Backoff {
    base: Duration,
    max: Duration,
    next: Duration,
}

impl Backoff {
    pub fn new(base: Duration, max: Duration) -> Self {
        Self { base, max, next: base }
    }

    /// current returns the delay to use for the upcoming reconnect
    /// attempt without consuming it.
    pub fn current(&self) -> Duration {
        self.next
    }

    /// next advances the schedule: returns the delay for this attempt
    /// and grows (2x, capped) the delay for the following one.
    pub fn next(&mut self) -> Duration {
        let out = self.next;
        self.next = self.next.saturating_mul(2).min(self.max);
        out
    }

    /// reset restores the schedule to its base delay, called after a
    /// connection has stayed up long enough to count as stable.
    pub fn reset(&mut self) {
        self.next = self.base;
    }
}

impl Default for Backoff {
    fn default() -> Self {
        Self::new(Duration::from_millis(500), Duration::from_secs(30))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn grows_and_caps() {
        let mut b = Backoff::new(Duration::from_millis(500), Duration::from_secs(2));
        assert_eq!(b.next(), Duration::from_millis(500));
        assert_eq!(b.next(), Duration::from_millis(1000));
        assert_eq!(b.next(), Duration::from_millis(2000));
        assert_eq!(b.next(), Duration::from_millis(2000)); // capped
        assert_eq!(b.next(), Duration::from_millis(2000));
    }

    #[test]
    fn resets_to_base() {
        let mut b = Backoff::new(Duration::from_millis(500), Duration::from_secs(30));
        b.next();
        b.next();
        b.reset();
        assert_eq!(b.next(), Duration::from_millis(500));
    }

    #[test]
    fn saturates_instead_of_overflow() {
        let mut b = Backoff::new(Duration::from_secs(1), Duration::from_secs(u64::MAX));
        b.next();
        // 2s -> 4s -> ... ; saturating_mul keeps this bounded and .min
        // with max keeps max authoritative.
        for _ in 0..70 {
            b.next();
        }
        assert!(b.current() >= Duration::from_secs(1));
    }

    #[test]
    fn default_schedule() {
        let mut b = Backoff::default();
        assert_eq!(b.next(), Duration::from_millis(500));
        assert_eq!(b.next(), Duration::from_secs(1));
        assert_eq!(b.next(), Duration::from_secs(2));
    }
}
