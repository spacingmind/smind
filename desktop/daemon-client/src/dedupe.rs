//! Bounded first-time-only memory for notifications (DT5 B/AC6): after
//! a reconnect the daemon redelivers still-pending requests, and each
//! (kind, request id -- run id for a failure) must notify exactly once.
//! Memory is bounded by evicting in FIFO order once over cap, so an id
//! last seen far enough ago may legitimately notify again -- a bounded
//! session's worth of re-notifications beats unbounded growth.

use std::collections::{HashSet, VecDeque};

use crate::protocol::NotifyKind;

pub struct Dedupe {
    seen: HashSet<(u8, String)>,
    order: VecDeque<(u8, String)>,
    cap: usize,
}

impl Dedupe {
    pub fn new(cap: usize) -> Self {
        Self { seen: HashSet::new(), order: VecDeque::new(), cap }
    }

    /// first_time reports whether this (kind, id) has not been seen
    /// before, recording it as seen.
    pub fn first_time(&mut self, kind: NotifyKind, id: &str) -> bool {
        let key = (kind as u8, id.to_string());
        if !self.seen.insert(key.clone()) {
            return false;
        }
        self.order.push_back(key);
        while self.order.len() > self.cap {
            if let Some(oldest) = self.order.pop_front() {
                self.seen.remove(&oldest);
            }
        }
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_redelivery_notifies_once() {
        let mut d = Dedupe::new(16);
        assert!(d.first_time(NotifyKind::Permission, "req-1"));
        assert!(!d.first_time(NotifyKind::Permission, "req-1"), "a redelivery is silent");
        assert!(d.first_time(NotifyKind::Permission, "req-2"));
        assert!(!d.first_time(NotifyKind::Permission, "req-2"));
    }

    #[test]
    fn the_same_id_under_another_kind_is_separate() {
        let mut d = Dedupe::new(16);
        assert!(d.first_time(NotifyKind::Permission, "same"));
        assert!(d.first_time(NotifyKind::Question, "same"));
        assert!(d.first_time(NotifyKind::Failure, "same"));
        assert!(!d.first_time(NotifyKind::Question, "same"));
    }

    #[test]
    fn the_memory_is_bounded() {
        let mut d = Dedupe::new(3);
        for id in ["id-1", "id-2", "id-3", "id-4", "id-5"] {
            d.first_time(NotifyKind::Permission, id);
        }
        assert!(d.seen.len() <= 3, "the set stays at the cap: {}", d.seen.len());
        assert!(d.order.len() <= 3, "the queue stays at the cap: {}", d.order.len());
        assert!(!d.first_time(NotifyKind::Permission, "id-5"), "the newest is still remembered");
        assert!(d.first_time(NotifyKind::Permission, "id-1"), "the oldest was evicted and notifies again");
    }
}
