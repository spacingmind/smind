//! Application-level liveness for a relay data stream (ADR-0031 D9, flow 8) —
//! the Rust twin of `internal/relay/client/keepalive.go`.
//!
//! A proxy in front of the relay (Cloudflare: 125 s read timeout) drops a
//! stream that carries no bytes, so an idle session sends a
//! `Frame{keepalive}` every 30 s, which the relay answers with a keepalive
//! frame on the same stream; a stream the relay has not said anything on for
//! 75 s is treated as dead so the caller reconnects and resumes instead of
//! waiting on a connection nothing flows through. HTTP/2 PINGs are
//! deliberately not used: a relay from before this feature answers a ping
//! more often than every 5 minutes with GOAWAY.
//!
//! Negotiation is by probing, never by version: the probe carries a
//! per-connection random nonce in `Frame.sequence` and the relay echoes it.
//! Only an echo with OUR nonce proves the relay supports keepalive. A relay
//! without it forwards the probe to the peer instead (and may forward the
//! peer's probes to us): those are keepalive frames too, but carry somebody
//! else's nonce, so they count as liveness only. The first echo must arrive
//! within `probe_timeout` or keepalive stays off for that stream; every
//! (re)opened stream probes once more, so one slow echo cannot switch the
//! protection off for the session's lifetime.
//!
//! This module is a pure state machine over explicit `Instant`s so the 30 s /
//! 75 s rules are tested without sleeping; `client::pump` drives it.

use std::time::Duration;

use tokio::time::Instant;

pub const DEFAULT_INTERVAL: Duration = Duration::from_secs(30);
pub const DEFAULT_DEAD_AFTER: Duration = Duration::from_secs(75);
pub const DEFAULT_PROBE_TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct LivenessCfg {
    pub interval: Duration,
    pub dead_after: Duration,
    pub probe_timeout: Duration,
}

impl Default for LivenessCfg {
    fn default() -> Self {
        Self {
            interval: DEFAULT_INTERVAL,
            dead_after: DEFAULT_DEAD_AFTER,
            probe_timeout: DEFAULT_PROBE_TIMEOUT,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Support {
    /// No echo seen yet on this stream.
    Unknown,
    /// The relay echoed one of our probes.
    Yes,
    /// The first probe went unanswered: this relay predates keepalive.
    No,
}

/// What the driver must do now.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Action {
    Nothing,
    /// Write one probe frame (`Frame{keepalive:true, sequence: nonce}`).
    SendProbe,
    /// The stream has been silent for `dead_after`: drop it and resume.
    Dead,
}

#[derive(Debug)]
pub struct Liveness {
    cfg: LivenessCfg,
    nonce: u64,
    support: Support,
    stream_start: Instant,
    last_recv: Instant,
    next_send: Instant,
}

impl Liveness {
    /// new starts liveness for a connection; `nonce` must be non-zero and
    /// random per connection (see [`random_nonce`]).
    pub fn new(cfg: LivenessCfg, nonce: u64, now: Instant) -> Self {
        assert_ne!(nonce, 0, "keepalive nonce must be non-zero");
        Self {
            cfg,
            nonce,
            support: Support::Unknown,
            stream_start: now,
            last_recv: now,
            next_send: now,
        }
    }

    /// set_cfg replaces the timing (the connection's configured values; a
    /// transport starts with the defaults).
    pub fn set_cfg(&mut self, cfg: LivenessCfg) {
        self.cfg = cfg;
    }

    pub fn nonce(&self) -> u64 {
        self.nonce
    }

    pub fn support(&self) -> Support {
        self.support
    }

    /// start_stream restarts the clocks for a freshly opened (or resumed)
    /// stream. A verdict of "no echo" belonged to the previous stream; "echo
    /// seen" stands (the relay is known to support keepalive).
    pub fn start_stream(&mut self, now: Instant) {
        self.stream_start = now;
        self.last_recv = now;
        self.next_send = now; // probe immediately
        if self.support == Support::No {
            self.support = Support::Unknown;
        }
    }

    /// on_frame records that the relay sent a frame: any frame is evidence the
    /// stream is alive, and a keepalive frame carrying our nonce is an echo.
    pub fn on_frame(&mut self, now: Instant, keepalive: bool, sequence: u64) {
        self.last_recv = now;
        if keepalive && sequence == self.nonce {
            self.support = Support::Yes;
        }
    }

    /// poll advances the state machine to `now` and says what to do. It is
    /// idempotent: call it again after acting.
    pub fn poll(&mut self, now: Instant) -> Action {
        match self.support {
            Support::No => return Action::Nothing,
            Support::Unknown => {
                if now >= self.stream_start + self.cfg.probe_timeout {
                    // Never echoed: the relay predates keepalive.
                    self.support = Support::No;
                    return Action::Nothing;
                }
            }
            Support::Yes => {
                if now >= self.last_recv + self.cfg.dead_after {
                    return Action::Dead;
                }
            }
        }
        if now >= self.next_send {
            self.next_send = now + self.cfg.interval;
            return Action::SendProbe;
        }
        Action::Nothing
    }

    /// next_wake is when the driver must call `poll` again; `None` when
    /// keepalive is off for this stream.
    pub fn next_wake(&self) -> Option<Instant> {
        let mut wake = self.next_send;
        match self.support {
            Support::No => return None,
            Support::Unknown => wake = wake.min(self.stream_start + self.cfg.probe_timeout),
            Support::Yes => wake = wake.min(self.last_recv + self.cfg.dead_after),
        }
        Some(wake)
    }
}

/// random_nonce returns a non-zero random probe nonce for one connection.
pub fn random_nonce() -> u64 {
    loop {
        let mut b = [0u8; 8];
        getrandom::fill(&mut b).expect("OS randomness for the keepalive nonce");
        let n = u64::from_be_bytes(b);
        if n != 0 {
            return n;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn secs(n: u64) -> Duration {
        Duration::from_secs(n)
    }

    /// A relay that echoes: probe at t0, echo at t0, then a probe every 30 s.
    #[test]
    fn client_sends_keepalive_every_30s() {
        let t0 = Instant::now();
        let mut l = Liveness::new(LivenessCfg::default(), 42, t0);
        assert_eq!(l.poll(t0), Action::SendProbe, "first probe at once");
        assert_eq!(l.poll(t0), Action::Nothing);
        l.on_frame(t0, true, 42);
        assert_eq!(l.support(), Support::Yes);
        for i in 1..=5u64 {
            let t = t0 + secs(30 * i);
            assert_eq!(
                l.poll(t - Duration::from_millis(1)),
                Action::Nothing,
                "not before 30 s (i={i})"
            );
            assert_eq!(l.poll(t), Action::SendProbe, "probe due at {}s", 30 * i);
            l.on_frame(t, true, 42);
        }
    }

    #[test]
    fn dead_after_75s_of_silence_and_not_before() {
        let t0 = Instant::now();
        let mut l = Liveness::new(LivenessCfg::default(), 7, t0);
        assert_eq!(l.poll(t0), Action::SendProbe);
        l.on_frame(t0, true, 7); // echoed once, then the relay goes silent
        assert_eq!(l.poll(t0 + secs(30)), Action::SendProbe);
        assert_eq!(l.poll(t0 + secs(60)), Action::SendProbe);
        assert_eq!(l.poll(t0 + secs(74)), Action::Nothing, "alive at 74 s");
        assert_eq!(l.poll(t0 + secs(75)), Action::Dead, "dead at 75 s");
    }

    #[test]
    fn any_relay_frame_resets_the_dead_timer() {
        let t0 = Instant::now();
        let mut l = Liveness::new(LivenessCfg::default(), 7, t0);
        l.poll(t0);
        l.on_frame(t0, true, 7);
        l.on_frame(t0 + secs(60), false, 0); // a data frame
        assert_ne!(
            l.poll(t0 + secs(134)),
            Action::Dead,
            "74 s after the data frame"
        );
        assert_eq!(l.poll(t0 + secs(135)), Action::Dead, "75 s after it");
    }

    #[test]
    fn a_forwarded_peer_probe_is_not_an_echo() {
        // A relay without keepalive support forwards the peer's probe to us:
        // a keepalive frame, but with the PEER's nonce.
        let t0 = Instant::now();
        let mut l = Liveness::new(LivenessCfg::default(), 7, t0);
        l.poll(t0);
        l.on_frame(t0 + secs(1), true, 8);
        assert_eq!(l.support(), Support::Unknown);
        assert_eq!(l.poll(t0 + secs(10)), Action::Nothing);
        assert_eq!(l.support(), Support::No, "no echo within the probe window");
        assert_eq!(
            l.poll(t0 + secs(1000)),
            Action::Nothing,
            "and then never declared dead"
        );
        assert_eq!(l.next_wake(), None);
    }

    #[test]
    fn each_new_stream_probes_again_after_a_no() {
        let t0 = Instant::now();
        let mut l = Liveness::new(LivenessCfg::default(), 7, t0);
        l.poll(t0);
        l.poll(t0 + secs(10));
        assert_eq!(l.support(), Support::No);
        // Resume onto a stream whose relay does echo: the verdict was per stream.
        let t1 = t0 + secs(40);
        l.start_stream(t1);
        assert_eq!(l.support(), Support::Unknown);
        assert_eq!(l.poll(t1), Action::SendProbe);
        l.on_frame(t1 + secs(1), true, 7);
        assert_eq!(l.support(), Support::Yes);
    }

    #[test]
    fn echo_verdict_survives_a_resume() {
        let t0 = Instant::now();
        let mut l = Liveness::new(LivenessCfg::default(), 7, t0);
        l.poll(t0);
        l.on_frame(t0, true, 7);
        let t1 = t0 + secs(100);
        l.start_stream(t1);
        assert_eq!(l.support(), Support::Yes);
        assert_eq!(l.poll(t1), Action::SendProbe);
        assert_eq!(
            l.poll(t1 + secs(74)),
            Action::SendProbe,
            "30 s ticks continue"
        );
    }

    #[test]
    fn next_wake_is_the_earliest_deadline() {
        let t0 = Instant::now();
        let mut l = Liveness::new(LivenessCfg::default(), 7, t0);
        assert_eq!(l.next_wake(), Some(t0), "first probe due now");
        l.poll(t0);
        assert_eq!(
            l.next_wake(),
            Some(t0 + secs(10)),
            "probe window before the next tick"
        );
        l.on_frame(t0, true, 7);
        assert_eq!(l.next_wake(), Some(t0 + secs(30)));
    }

    #[test]
    fn nonce_is_nonzero_and_varies() {
        let a = random_nonce();
        let b = random_nonce();
        assert!(a != 0 && b != 0);
        assert_ne!(a, b, "two random 64-bit nonces collided");
    }
}
