//! The client end of the E2EE v2 handshake/send/receive state machine
//! (ADR-0035), generic over a frame-oriented transport. Mirrors the
//! initiator side of `internal/relay/e2ee.Channel` (Go), including its
//! retry rules (§4) and typed errors. Every client is the initiator: the
//! daemon's responder lives in Go; a test-only responder for this crate's
//! own tests is in `test_daemon` below.

use crate::relay::crypto::{
    self, EphemeralSecret, FrameError, HelloError, KeyAgreementError, ReadyError, Role, Schedule,
    Session, SessionError, FRAME_DATA, FRAME_HELLO, FRAME_READY, MAX_HANDSHAKE_FRAME_LEN,
    PUBLIC_KEY_SIZE,
};

/// One raw frame transport: each `send`/`recv` call carries exactly one
/// complete e2ee wire frame's bytes — no partial reads/writes. A tonic
/// `OpenData` stream (one `relaypb.Frame.payload` per e2ee frame, see
/// `crate::relay::client`) and an in-memory pipe (tests) both implement
/// this directly. `#[async_trait]` (rather than native async-fn-in-trait)
/// so the resulting futures are `Send` and this can run inside
/// `tokio::spawn`.
#[async_trait::async_trait]
pub trait FrameTransport: Send {
    type Error: std::fmt::Display + Send;

    async fn send_frame(&mut self, frame_type: u8, payload: &[u8]) -> Result<(), Self::Error>;
    async fn recv_frame(&mut self) -> Result<(u8, Vec<u8>), Self::Error>;
}

#[derive(Debug)]
pub enum ChannelError<E> {
    Transport(E),
    Frame(FrameError),
    /// A wire-level protocol violation: a malformed or oversized frame, an
    /// unexpected frame type, an empty ready, a role mismatch.
    Protocol(String),
    /// The peer's hello carried another E2EE protocol version (ADR-0035 §6).
    /// `peer < 2` means the daemon is too old (update it); `peer > 2` means
    /// this app is. Never answered with a retry, an older protocol or another
    /// route.
    UnsupportedVersion {
        peer: u8,
    },
    /// A peer re-handshaked with a *different* hello on an established (or
    /// mid-handshake) session — rotation always means a new session, per
    /// ADR-0007 (e).
    KeyRotation(String),
    /// The daemon hello carried a static public key different from the one
    /// pinned via `handshake`'s `pin_daemon_public_key` (the pairing
    /// offer's daemon key). Checked before any key agreement.
    PinMismatch,
    /// Key confirmation failed: the peer's ready MAC did not verify, so the
    /// two ends did not derive the same keys (a tampered ephemeral, a wrong
    /// static key, a forged peer).
    HandshakeConfirm,
    /// An X25519 exchange produced the all-zero output (a low-order point).
    KeyAgreement,
    Replay {
        got: u64,
        want: u64,
    },
    Decrypt,
    NotEstablished,
    AlreadyEstablished,
    /// The channel failed or was cancelled mid-handshake; open a new one.
    Closed,
}

impl<E: std::fmt::Display> std::fmt::Display for ChannelError<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ChannelError::Transport(e) => write!(f, "e2ee: transport: {e}"),
            ChannelError::Frame(e) => write!(f, "{e}"),
            ChannelError::Protocol(s) => write!(f, "e2ee: protocol violation: {s}"),
            ChannelError::UnsupportedVersion { peer } => write!(
                f,
                "e2ee: peer speaks protocol version {peer}, this end speaks {}",
                crypto::PROTOCOL_VERSION
            ),
            ChannelError::KeyRotation(s) => {
                write!(f, "e2ee: peer re-handshaked with a different key: {s}")
            }
            ChannelError::PinMismatch => {
                write!(
                    f,
                    "e2ee: peer public key does not match the pinned pairing offer"
                )
            }
            ChannelError::HandshakeConfirm => write!(f, "e2ee: handshake key confirmation failed"),
            ChannelError::KeyAgreement => write!(f, "e2ee: X25519 key agreement failed"),
            ChannelError::Replay { got, want } => {
                write!(
                    f,
                    "e2ee: frame counter replayed or out of order: got counter {got}, want {want}"
                )
            }
            ChannelError::Decrypt => write!(f, "e2ee: frame failed to decrypt"),
            ChannelError::NotEstablished => write!(f, "e2ee: handshake not completed"),
            ChannelError::AlreadyEstablished => write!(f, "e2ee: handshake already completed"),
            ChannelError::Closed => write!(f, "e2ee: channel closed"),
        }
    }
}

impl<E> From<SessionError> for ChannelError<E> {
    fn from(e: SessionError) -> Self {
        match e {
            SessionError::Replay { got, want } => ChannelError::Replay { got, want },
            SessionError::Decrypt => ChannelError::Decrypt,
            SessionError::CounterExhausted => {
                ChannelError::Protocol("frame counter exhausted".into())
            }
        }
    }
}

impl<E> From<KeyAgreementError> for ChannelError<E> {
    fn from(_: KeyAgreementError) -> Self {
        ChannelError::KeyAgreement
    }
}

fn hello_error<E>(e: HelloError) -> ChannelError<E> {
    match e {
        HelloError::UnsupportedVersion(peer) => ChannelError::UnsupportedVersion { peer },
        other => ChannelError::Protocol(other.to_string()),
    }
}

fn ready_error<E>(e: ReadyError) -> ChannelError<E> {
    match e {
        ReadyError::WrongLength(n) => {
            ChannelError::Protocol(format!("ready is {n} bytes, want {}", crypto::READY_LEN))
        }
        ReadyError::Mismatch => ChannelError::HandshakeConfirm,
    }
}

/// What an established session retains (ADR-0035 §4): the public handshake
/// values, `th`, and the peer's M2/R_d so a byte-identical repeat is
/// tolerated and anything else is not.
struct Established {
    daemon_pub: [u8; PUBLIC_KEY_SIZE],
    daemon_eph_pub: [u8; PUBLIC_KEY_SIZE],
    client_eph_pub: [u8; PUBLIC_KEY_SIZE],
    th: [u8; 32],
    m2: Vec<u8>,
    r_d: [u8; 32],
}

/// One end (the client/initiator) of an E2EE session carried over `T`.
/// Generates its own ephemeral key inside `handshake`; no API takes one.
pub struct Channel<T: FrameTransport> {
    transport: T,
    session: Option<Session>,
    established: Option<Established>,
    /// Set when a handshake starts and cleared only when it succeeds, so a
    /// failed or cancelled handshake leaves a dead channel (no second
    /// handshake over a transport that has already seen M1). A protocol
    /// violation on an established session sets it too.
    closed: bool,
    /// Test hook: the seed of the next handshake's ephemeral (vectors).
    #[cfg(test)]
    test_ephemeral_seed: Option<[u8; 32]>,
}

impl<T: FrameTransport> Channel<T> {
    pub fn new(transport: T) -> Self {
        Self {
            transport,
            session: None,
            established: None,
            closed: false,
            #[cfg(test)]
            test_ephemeral_seed: None,
        }
    }

    pub fn established(&self) -> bool {
        self.established.is_some() && !self.closed
    }

    /// The handshake transcript hash `th` (ADR-0035 §2) once established:
    /// public data that the device hello signature binds to.
    pub fn transcript_hash(&self) -> Option<[u8; 32]> {
        self.established.as_ref().map(|e| e.th)
    }

    pub fn daemon_public_key(&self) -> Option<[u8; PUBLIC_KEY_SIZE]> {
        self.established.as_ref().map(|e| e.daemon_pub)
    }

    pub fn daemon_ephemeral_public_key(&self) -> Option<[u8; PUBLIC_KEY_SIZE]> {
        self.established.as_ref().map(|e| e.daemon_eph_pub)
    }

    pub fn client_ephemeral_public_key(&self) -> Option<[u8; PUBLIC_KEY_SIZE]> {
        self.established.as_ref().map(|e| e.client_eph_pub)
    }

    /// Gives a reconnect loop access to the underlying transport so it
    /// can be swapped out after a transport-level drop (e.g.
    /// `DataFrameTransport::reopen`) WITHOUT touching the established
    /// session/counters — mirrors `frameConn.reopen` (Go), which the
    /// `client.DataConn.Resume` wrapper calls without going anywhere
    /// near `e2ee.Channel` itself.
    pub fn transport_mut(&mut self) -> &mut T {
        &mut self.transport
    }

    #[cfg(test)]
    fn seed_ephemeral(&mut self, seed: [u8; 32]) {
        self.test_ephemeral_seed = Some(seed);
    }

    fn fresh_ephemeral(&mut self) -> EphemeralSecret {
        #[cfg(test)]
        if let Some(seed) = self.test_ephemeral_seed.take() {
            return EphemeralSecret::from_seed(seed);
        }
        EphemeralSecret::generate()
    }

    fn fail<E>(&mut self, e: ChannelError<E>) -> ChannelError<E> {
        self.closed = true;
        e
    }

    /// Runs the ADR-0035 handshake: generate a fresh ephemeral key, send M1,
    /// read M2 and R_d, pin-check the daemon key, derive the session, verify
    /// R_d, send R_c. Returns only once key confirmation succeeded, so no
    /// application frame is sent or accepted before the keys are confirmed.
    /// Starts a FRESH session — for a transport-level reconnect that should
    /// keep the existing session, don't call this again; see
    /// `crate::relay::client`.
    ///
    /// `pin_daemon_public_key`, when `Some`, rejects a daemon hello whose
    /// static public key doesn't match — the pairing-offer pinning check: a
    /// device connecting to a specific paired daemon must refuse to complete
    /// a handshake with anything else, even if the relay let the connection
    /// through. It is checked before any key agreement.
    ///
    /// Any error leaves the channel closed. A concurrent second handshake is
    /// impossible by construction (`&mut self`).
    pub async fn handshake(
        &mut self,
        pin_daemon_public_key: Option<&[u8; PUBLIC_KEY_SIZE]>,
    ) -> Result<(), ChannelError<T::Error>> {
        if self.established.is_some() {
            return Err(ChannelError::AlreadyEstablished);
        }
        if self.closed {
            return Err(ChannelError::Closed);
        }
        self.closed = true;
        let (session, established) = self.handshake_inner(pin_daemon_public_key).await?;
        self.session = Some(session);
        self.established = Some(established);
        self.closed = false;
        Ok(())
    }

    async fn handshake_inner(
        &mut self,
        pin: Option<&[u8; PUBLIC_KEY_SIZE]>,
    ) -> Result<(Session, Established), ChannelError<T::Error>> {
        let eph = self.fresh_ephemeral();
        let client_eph_pub = eph.public();
        self.transport
            .send_frame(FRAME_HELLO, &crypto::client_hello_payload(&client_eph_pub))
            .await
            .map_err(ChannelError::Transport)?;

        let (typ, m2) = recv_handshake_frame(&mut self.transport).await?;
        if typ != FRAME_HELLO {
            return Err(ChannelError::Protocol(format!(
                "expected hello, got frame type 0x{typ:02x}"
            )));
        }
        let (daemon_pub, daemon_eph_pub) = crypto::parse_daemon_hello(&m2).map_err(hello_error)?;
        // The pin is checked before any key agreement.
        if let Some(expected) = pin {
            if &daemon_pub != expected {
                return Err(ChannelError::PinMismatch);
            }
        }

        let es = eph.dh(&daemon_pub)?;
        let ee = eph.dh(&daemon_eph_pub)?;
        let sched: Schedule =
            crypto::derive_schedule(&es, &ee, &daemon_pub, &daemon_eph_pub, &client_eph_pub);
        drop((es, ee, eph));
        let want_r_d = crypto::confirm_tag(&sched.confirm_d, &sched.th);
        let r_c = crypto::confirm_tag(&sched.confirm_m, &sched.th);

        loop {
            let (typ, payload) = recv_handshake_frame(&mut self.transport).await?;
            match typ {
                FRAME_READY => {
                    crypto::check_ready(&payload, &want_r_d).map_err(ready_error)?;
                    break;
                }
                FRAME_HELLO => check_repeated_hello(&payload, &m2)?,
                other => {
                    return Err(ChannelError::Protocol(format!(
                        "expected ready, got frame type 0x{other:02x}"
                    )))
                }
            }
        }
        self.transport
            .send_frame(FRAME_READY, &r_c)
            .await
            .map_err(ChannelError::Transport)?;

        let session = Session::new(Role::Mobile, &sched);
        let established = Established {
            daemon_pub,
            daemon_eph_pub,
            client_eph_pub,
            th: sched.th,
            m2,
            r_d: want_r_d,
        };
        Ok((session, established))
    }

    /// Encrypts and sends one application message.
    pub async fn send(&mut self, msg: &[u8]) -> Result<(), ChannelError<T::Error>> {
        if self.closed {
            return Err(ChannelError::Closed);
        }
        let session = self.session.as_mut().ok_or(ChannelError::NotEstablished)?;
        let (counter, ciphertext) = session.seal(msg)?;
        let mut payload = Vec::with_capacity(8 + ciphertext.len());
        payload.extend_from_slice(&counter.to_be_bytes());
        payload.extend_from_slice(&ciphertext);
        self.transport
            .send_frame(FRAME_DATA, &payload)
            .await
            .map_err(ChannelError::Transport)
    }

    /// Reads the next application message, decrypting and replay-checking
    /// it. A replayed or out-of-order frame returns `Replay` and leaves the
    /// channel usable; a protocol violation closes it. Handshake frames
    /// arriving on an established session are handled here (ADR-0035 §4): a
    /// byte-identical repeat of M2 or R_d is ignored, a different hello is
    /// `KeyRotation`, a different ready is `HandshakeConfirm`.
    pub async fn receive(&mut self) -> Result<Vec<u8>, ChannelError<T::Error>> {
        if self.closed {
            return Err(ChannelError::Closed);
        }
        if self.session.is_none() {
            return Err(ChannelError::NotEstablished);
        }
        loop {
            let (typ, payload) = self
                .transport
                .recv_frame()
                .await
                .map_err(ChannelError::Transport)?;
            let est = self
                .established
                .as_ref()
                .expect("established with the session");
            match typ {
                FRAME_DATA => {
                    if payload.len() < 8 {
                        let e = ChannelError::Protocol(format!(
                            "data frame is {} bytes, want at least 8",
                            payload.len()
                        ));
                        return Err(self.fail(e));
                    }
                    let counter = u64::from_be_bytes(payload[0..8].try_into().unwrap());
                    let session = self.session.as_mut().expect("checked above");
                    return Ok(session.open(counter, &payload[8..])?);
                }
                FRAME_HELLO => {
                    if let Err(e) = check_repeated_hello(&payload, &est.m2) {
                        return Err(self.fail(e));
                    }
                }
                FRAME_READY => {
                    if let Err(e) = crypto::check_ready(&payload, &est.r_d).map_err(ready_error) {
                        return Err(self.fail(e));
                    }
                }
                other => {
                    let e = ChannelError::Protocol(format!("unknown frame type 0x{other:02x}"));
                    return Err(self.fail(e));
                }
            }
        }
    }
}

/// Reads one frame during a handshake, refusing anything over the 128-byte
/// handshake cap (ADR-0035 §1) so an unauthenticated peer cannot make either
/// end handle more than the largest legitimate message (M2).
async fn recv_handshake_frame<T: FrameTransport>(
    transport: &mut T,
) -> Result<(u8, Vec<u8>), ChannelError<T::Error>> {
    let (typ, payload) = transport
        .recv_frame()
        .await
        .map_err(ChannelError::Transport)?;
    if 1 + payload.len() > MAX_HANDSHAKE_FRAME_LEN {
        return Err(ChannelError::Protocol(format!(
            "handshake frame of {} bytes exceeds the {MAX_HANDSHAKE_FRAME_LEN} byte limit",
            1 + payload.len()
        )));
    }
    Ok((typ, payload))
}

/// ADR-0035 §4 for a daemon hello that arrives after the first: a
/// byte-identical repeat is a transport retry, anything else is
/// `KeyRotation`. Parsed first, so a v1 or malformed repeat is reported as
/// such rather than as rotation.
fn check_repeated_hello<E>(payload: &[u8], first: &[u8]) -> Result<(), ChannelError<E>> {
    crypto::parse_daemon_hello(payload).map_err(hello_error)?;
    if payload != first {
        return Err(ChannelError::KeyRotation(
            "rotation requires a new session, not an in-session rekey".into(),
        ));
    }
    Ok(())
}

#[cfg(test)]
pub mod test_pipe {
    //! An in-memory duplex pipe pair, mirroring Go's `pipe_test.go`
    //! helpers, so the handshake state machine is testable without any
    //! network or gRPC harness.
    use super::FrameTransport;
    use std::collections::VecDeque;
    use std::sync::{Arc, Mutex};
    use tokio::sync::Notify;

    type Frame = (u8, Vec<u8>);

    #[derive(Clone)]
    struct Queue {
        items: Arc<Mutex<VecDeque<Frame>>>,
        notify: Arc<Notify>,
    }

    impl Queue {
        fn new() -> Self {
            Self {
                items: Arc::new(Mutex::new(VecDeque::new())),
                notify: Arc::new(Notify::new()),
            }
        }
        fn push(&self, typ: u8, payload: Vec<u8>) {
            self.items.lock().unwrap().push_back((typ, payload));
            self.notify.notify_one();
        }
        async fn pop(&self) -> (u8, Vec<u8>) {
            loop {
                if let Some(item) = self.items.lock().unwrap().pop_front() {
                    return item;
                }
                self.notify.notified().await;
            }
        }
    }

    pub struct PipeEnd {
        tx: Queue,
        rx: Queue,
    }

    #[async_trait::async_trait]
    impl FrameTransport for PipeEnd {
        type Error = std::convert::Infallible;

        async fn send_frame(&mut self, frame_type: u8, payload: &[u8]) -> Result<(), Self::Error> {
            self.tx.push(frame_type, payload.to_vec());
            Ok(())
        }

        async fn recv_frame(&mut self) -> Result<(u8, Vec<u8>), Self::Error> {
            Ok(self.rx.pop().await)
        }
    }

    impl PipeEnd {
        /// raw_sender clones a handle onto this end's outbound queue, so
        /// a test can keep injecting hand-assembled frames toward the
        /// peer even after this `PipeEnd` itself has been moved into a
        /// `Channel` (e.g. to run a real handshake first, then inject an
        /// adversarial re-hello afterward).
        pub fn raw_sender(&self) -> RawSender {
            RawSender(self.tx.clone())
        }
    }

    pub struct RawSender(Queue);

    impl RawSender {
        pub fn send(&self, frame_type: u8, payload: Vec<u8>) {
            self.0.push(frame_type, payload);
        }
    }

    /// pipe returns two connected ends: whatever one sends, the other
    /// receives.
    pub fn pipe() -> (PipeEnd, PipeEnd) {
        let a = Queue::new();
        let b = Queue::new();
        (
            PipeEnd {
                tx: a.clone(),
                rx: b.clone(),
            },
            PipeEnd { tx: b, rx: a },
        )
    }

    /// send_raw injects a hand-assembled frame directly onto `end`'s
    /// outbound queue, bypassing the Channel state machine — for
    /// asserting how the *other* end's Channel reacts to a malformed or
    /// adversarial frame, before `end` is ever wrapped in a Channel.
    pub fn send_raw(end: &PipeEnd, frame_type: u8, payload: Vec<u8>) {
        end.tx.push(frame_type, payload);
    }
}

/// A test-only responder (the daemon end), mirroring Go's `handshakeDaemon`
/// on the shared pure functions in `crypto`. Production daemons are Go; this
/// exists so the client can be driven end to end in-process and so the
/// vector file's `endpoint: "daemon"` negatives run through a real
/// handshake state machine.
#[cfg(test)]
pub(crate) mod test_daemon {
    use super::*;

    pub struct DaemonSide {
        pub session: Session,
        pub th: [u8; 32],
        pub client_eph_pub: [u8; PUBLIC_KEY_SIZE],
    }

    /// Responder handshake: read M1, send M2 and R_d, wait for a valid R_c.
    /// The daemon seals nothing until R_c verifies.
    pub async fn handshake<T: FrameTransport>(
        transport: &mut T,
        daemon_static: &EphemeralSecret,
        daemon_eph: EphemeralSecret,
    ) -> Result<DaemonSide, ChannelError<T::Error>> {
        let (typ, m1) = recv_handshake_frame(transport).await?;
        if typ != FRAME_HELLO {
            return Err(ChannelError::Protocol(format!(
                "expected hello, got frame type 0x{typ:02x}"
            )));
        }
        let client_eph_pub = crypto::parse_client_hello(&m1).map_err(hello_error)?;
        let (daemon_pub, daemon_eph_pub) = (daemon_static.public(), daemon_eph.public());
        let es = daemon_static.dh(&client_eph_pub)?;
        let ee = daemon_eph.dh(&client_eph_pub)?;
        let sched =
            crypto::derive_schedule(&es, &ee, &daemon_pub, &daemon_eph_pub, &client_eph_pub);
        let m2 = crypto::daemon_hello_payload(&daemon_pub, &daemon_eph_pub);
        let r_d = crypto::confirm_tag(&sched.confirm_d, &sched.th);
        let want_r_c = crypto::confirm_tag(&sched.confirm_m, &sched.th);
        let send = |typ, payload: Vec<u8>| (typ, payload);
        for (typ, payload) in [
            send(FRAME_HELLO, m2.to_vec()),
            send(FRAME_READY, r_d.to_vec()),
        ] {
            transport
                .send_frame(typ, &payload)
                .await
                .map_err(ChannelError::Transport)?;
        }
        loop {
            let (typ, payload) = recv_handshake_frame(transport).await?;
            match typ {
                FRAME_READY => {
                    crypto::check_ready(&payload, &want_r_c).map_err(ready_error)?;
                    return Ok(DaemonSide {
                        session: Session::new(Role::Daemon, &sched),
                        th: sched.th,
                        client_eph_pub,
                    });
                }
                FRAME_HELLO => {
                    crypto::parse_client_hello(&payload).map_err(hello_error)?;
                    if payload != m1 {
                        return Err(ChannelError::KeyRotation("different M1".into()));
                    }
                    transport
                        .send_frame(FRAME_HELLO, &m2)
                        .await
                        .map_err(ChannelError::Transport)?;
                    transport
                        .send_frame(FRAME_READY, &r_d)
                        .await
                        .map_err(ChannelError::Transport)?;
                }
                other => {
                    return Err(ChannelError::Protocol(format!(
                        "expected ready, got frame type 0x{other:02x}"
                    )))
                }
            }
        }
    }

    /// An established test daemon over `T`, with the message-level `send` /
    /// `receive` the hello tests drive (the daemon seals nothing before
    /// R_c verified, and these only exist after it).
    pub struct DaemonPeer<T: FrameTransport> {
        pub transport: T,
        pub side: DaemonSide,
        pub daemon_pub: [u8; PUBLIC_KEY_SIZE],
    }

    impl<T: FrameTransport> DaemonPeer<T> {
        /// Runs the responder handshake with the static key from `static_seed`
        /// and a fresh daemon ephemeral; panics if it fails.
        pub async fn establish(mut transport: T, static_seed: [u8; 32]) -> Self {
            let static_secret = EphemeralSecret::from_seed(static_seed);
            let side = handshake(&mut transport, &static_secret, EphemeralSecret::generate())
                .await
                .unwrap_or_else(|e| panic!("test daemon handshake: {}", e));
            Self {
                transport,
                side,
                daemon_pub: static_secret.public(),
            }
        }

        pub async fn send(&mut self, msg: &[u8]) -> Result<(), T::Error> {
            let payload = self.side.seal_frame(msg);
            self.transport.send_frame(FRAME_DATA, &payload).await
        }

        pub async fn receive(&mut self) -> Vec<u8> {
            loop {
                let (typ, payload) = self.transport.recv_frame().await.ok().expect("daemon recv");
                if typ == FRAME_DATA {
                    return self.side.open_frame(&payload);
                }
            }
        }
    }

    impl DaemonSide {
        pub fn seal_frame(&mut self, msg: &[u8]) -> Vec<u8> {
            let (counter, ct) = self.session.seal(msg).unwrap();
            let mut payload = counter.to_be_bytes().to_vec();
            payload.extend_from_slice(&ct);
            payload
        }

        pub fn open_frame(&mut self, payload: &[u8]) -> Vec<u8> {
            let counter = u64::from_be_bytes(payload[..8].try_into().unwrap());
            self.session.open(counter, &payload[8..]).unwrap()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::test_daemon;
    use super::test_pipe::pipe;
    use super::*;
    use crate::relay::crypto::testvec::{arr32, bytes, hex_decode, hex_encode, load, seed};
    use crate::relay::crypto::{decode_frame, encode_frame};
    use serde_json::Value;
    use std::collections::VecDeque;

    // ---- scripted raw transport: what the peer sends, as raw wire bytes ----

    #[derive(Debug)]
    enum RawErr {
        /// The relay envelope did not hold exactly one whole frame.
        Frame(FrameError),
        /// The script ran out: the peer's stream ended.
        Closed,
    }

    impl std::fmt::Display for RawErr {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            match self {
                RawErr::Frame(e) => write!(f, "{e}"),
                RawErr::Closed => write!(f, "stream closed"),
            }
        }
    }

    struct RawTransport {
        incoming: VecDeque<Vec<u8>>,
        sent: Vec<(u8, Vec<u8>)>,
    }

    impl RawTransport {
        fn new(frames: Vec<Vec<u8>>) -> Self {
            Self {
                incoming: frames.into(),
                sent: vec![],
            }
        }
    }

    #[async_trait::async_trait]
    impl FrameTransport for RawTransport {
        type Error = RawErr;

        async fn send_frame(&mut self, frame_type: u8, payload: &[u8]) -> Result<(), RawErr> {
            self.sent.push((frame_type, payload.to_vec()));
            Ok(())
        }

        async fn recv_frame(&mut self) -> Result<(u8, Vec<u8>), RawErr> {
            let raw = self.incoming.pop_front().ok_or(RawErr::Closed)?;
            let (typ, payload) = decode_frame(&raw).map_err(RawErr::Frame)?;
            Ok((typ, payload.to_vec()))
        }
    }

    fn kind<T>(r: &Result<T, ChannelError<RawErr>>) -> String {
        match r {
            Ok(_) => "none".into(),
            Err(ChannelError::UnsupportedVersion { .. }) => "unsupported_version".into(),
            Err(ChannelError::PinMismatch) => "peer_key_mismatch".into(),
            Err(ChannelError::HandshakeConfirm) => "handshake_confirm".into(),
            Err(ChannelError::KeyAgreement) => "key_agreement".into(),
            Err(ChannelError::Protocol(_) | ChannelError::Frame(_)) => "protocol".into(),
            Err(ChannelError::Transport(RawErr::Frame(_))) => "protocol".into(),
            Err(e) => format!("other: {e}"),
        }
    }

    /// Which `FrameError` the transport hit, if any: the vector only says
    /// `protocol`, so the framing negatives are also pinned to their variant.
    fn frame_error<T>(r: &Result<T, ChannelError<RawErr>>) -> Option<&'static str> {
        match r {
            Err(ChannelError::Transport(RawErr::Frame(e))) => Some(match e {
                FrameError::Truncated => "truncated",
                FrameError::ZeroLength => "zero_length",
                FrameError::TooLarge(_) => "too_large",
            }),
            _ => None,
        }
    }

    /// The `FrameError` each framing negative must produce; every other negative none.
    fn expected_frame_error(name: &str) -> Option<&'static str> {
        match name {
            "zero_length_frame_to_daemon" | "zero_length_frame_to_client" => Some("zero_length"),
            "oversized_frame_to_daemon" | "oversized_frame_to_client" => Some("too_large"),
            "truncated_frame_to_daemon" | "truncated_frame_to_client" => Some("truncated"),
            _ => None,
        }
    }

    fn written(sent: &[(u8, Vec<u8>)]) -> Vec<&'static str> {
        sent.iter()
            .map(|(t, _)| match *t {
                FRAME_HELLO => "hello",
                FRAME_READY => "ready",
                _ => "data",
            })
            .collect()
    }

    fn frame(v: &Value, key: &str) -> Vec<u8> {
        bytes(v, key)
    }

    fn scripted_client(v: &Value, frames: Vec<Vec<u8>>) -> Channel<RawTransport> {
        let mut c = Channel::new(RawTransport::new(frames));
        c.seed_ephemeral(seed(v, "client_eph"));
        c
    }

    fn pin(v: &Value) -> [u8; 32] {
        arr32(&v["public_keys"], "daemon_static")
    }

    fn flip(frame: &[u8], offset: usize) -> Vec<u8> {
        let mut f = frame.to_vec();
        f[offset] ^= 0x01;
        f
    }

    // ---- every vector in the file ----

    /// Reproduces session A byte for byte through the real client channel:
    /// M1/R_c written, th and keys equal the vector, and the vector's sealed
    /// data frames decrypt (daemon to client) and re-seal (client to daemon).
    #[tokio::test]
    async fn client_handshake_reproduces_the_vector_session() {
        let v = load();
        let mut c = scripted_client(
            &v,
            vec![frame(&v, "m2_frame_hex"), frame(&v, "r_d_frame_hex")],
        );
        c.handshake(Some(&pin(&v))).await.unwrap();
        assert!(c.established());
        assert_eq!(c.transcript_hash().unwrap(), arr32(&v, "th"));
        assert_eq!(c.daemon_public_key().unwrap(), pin(&v));
        assert_eq!(
            c.client_ephemeral_public_key().unwrap(),
            arr32(&v["public_keys"], "client_eph")
        );
        assert_eq!(
            c.daemon_ephemeral_public_key().unwrap(),
            arr32(&v["public_keys"], "daemon_eph")
        );

        let sent: Vec<Vec<u8>> = c
            .transport_mut()
            .sent
            .iter()
            .map(|(t, p)| encode_frame(*t, p))
            .collect();
        assert_eq!(
            sent,
            vec![frame(&v, "m1_frame_hex"), frame(&v, "r_c_frame_hex")]
        );

        for (d, m) in v["data"]["d2m"]
            .as_array()
            .unwrap()
            .iter()
            .zip(v["data"]["m2d"].as_array().unwrap())
        {
            c.transport_mut().incoming.push_back(frame(d, "frame_hex"));
            assert_eq!(c.receive().await.unwrap(), bytes(d, "plaintext_hex"));
            c.send(&bytes(m, "plaintext_hex")).await.unwrap();
            let (t, p) = c.transport_mut().sent.last().unwrap().clone();
            assert_eq!(
                hex_encode(&encode_frame(t, &p)),
                m["frame_hex"].as_str().unwrap()
            );
        }
    }

    /// Every entry of `negative` runs through a real channel; none is skipped.
    /// `endpoint: "client"` drives `Channel::handshake`; `endpoint: "daemon"`
    /// drives the test-only responder in `test_daemon` (production daemons are
    /// Go), both over `RawTransport`, where each `frames_hex` entry is one
    /// relay envelope. The wire-framing negatives (zero length, oversized
    /// header, truncated frame) therefore surface as a transport-level
    /// `FrameError`, which maps to kind `protocol` exactly as Go's length
    /// checks do; `close_after` has no separate effect because the script
    /// ending is already a closed stream.
    #[tokio::test]
    async fn every_negative_vector_runs_through_a_real_handshake() {
        let v = load();
        let negatives = v["negative"].as_array().unwrap();
        assert!(negatives.len() >= 30, "vector file lost its negatives");
        let mut consumed = 0;
        for n in negatives {
            let name = n["name"].as_str().unwrap();
            let frames: Vec<Vec<u8>> = n["frames_hex"]
                .as_array()
                .unwrap()
                .iter()
                .map(|f| hex_decode(f.as_str().unwrap()))
                .collect();
            let (kind_got, sent, peer_version, frame_err) = match n["endpoint"].as_str().unwrap() {
                "client" => {
                    let mut c = scripted_client(&v, frames);
                    let pin_bytes = n
                        .get("pin_hex")
                        .and_then(Value::as_str)
                        .filter(|s| !s.is_empty());
                    let pin: Option<[u8; 32]> =
                        pin_bytes.map(|h| hex_decode(h).try_into().unwrap());
                    let r = c.handshake(pin.as_ref()).await;
                    assert!(!c.established(), "{name}: must not establish");
                    let version = match &r {
                        Err(ChannelError::UnsupportedVersion { peer }) => Some(*peer as i64),
                        _ => None,
                    };
                    (
                        kind(&r),
                        c.transport_mut().sent.clone(),
                        version,
                        frame_error(&r),
                    )
                }
                "daemon" => {
                    let mut t = RawTransport::new(frames);
                    let static_secret = EphemeralSecret::from_seed(seed(&v, "daemon_static"));
                    let eph = EphemeralSecret::from_seed(seed(&v, "daemon_eph"));
                    let r = test_daemon::handshake(&mut t, &static_secret, eph).await;
                    let version = match &r {
                        Err(ChannelError::UnsupportedVersion { peer }) => Some(*peer as i64),
                        _ => None,
                    };
                    (kind(&r), t.sent.clone(), version, frame_error(&r))
                }
                other => panic!("{name}: unknown endpoint {other}"),
            };
            assert_eq!(
                kind_got,
                n["expect"].as_str().unwrap(),
                "{name}: error kind"
            );
            assert_eq!(
                frame_err,
                expected_frame_error(name),
                "{name}: framing error variant"
            );
            if let Some(want) = n.get("expect_peer_version").and_then(Value::as_i64) {
                assert_eq!(peer_version, Some(want), "{name}: peer version");
            }
            let want: Vec<&str> = n["expect_written"]
                .as_array()
                .unwrap()
                .iter()
                .map(|w| match w.as_str().unwrap() {
                    "m1" | "m2" => "hello",
                    "r_d" | "r_c" => "ready",
                    other => other,
                })
                .collect();
            assert_eq!(written(&sent), want, "{name}: frames written");
            consumed += 1;
        }
        assert_eq!(consumed, negatives.len());
    }

    // ---- named behaviours (ADR-0035 §7/§8, plan test list) ----

    #[tokio::test]
    async fn handshake_and_round_trip_with_a_responder() {
        let v = load();
        let (a, mut b) = pipe();
        let mut client = Channel::new(a);
        let daemon_static = EphemeralSecret::from_seed(seed(&v, "daemon_static"));
        let daemon_eph = EphemeralSecret::from_seed(seed(&v, "daemon_eph"));
        let daemon_pub = daemon_static.public();

        let (r1, r2) = tokio::join!(
            client.handshake(Some(&daemon_pub)),
            test_daemon::handshake(&mut b, &daemon_static, daemon_eph)
        );
        r1.unwrap();
        let mut daemon = r2.unwrap();
        assert_eq!(client.transcript_hash().unwrap(), daemon.th);
        assert_eq!(
            client.client_ephemeral_public_key().unwrap(),
            daemon.client_eph_pub
        );

        let payload = daemon.seal_frame(b"hello client");
        crate::relay::channel::test_pipe::send_raw(&b, FRAME_DATA, payload);
        assert_eq!(client.receive().await.unwrap(), b"hello client");
        client.send(b"hello daemon").await.unwrap();
        let (typ, payload) = b.recv_frame().await.unwrap();
        assert_eq!(typ, FRAME_DATA);
        assert_eq!(daemon.open_frame(&payload), b"hello daemon");
    }

    #[tokio::test]
    async fn tampered_daemon_eph_fails_handshake() {
        let v = load();
        let m2 = frame(&v, "m2_frame_hex");
        // frame header (5) + version/role (2) + daemon_pub (32): first byte of daemon_eph.
        let mut c = scripted_client(&v, vec![flip(&m2, 5 + 2 + 32), frame(&v, "r_d_frame_hex")]);
        let r = c.handshake(Some(&pin(&v))).await;
        assert!(matches!(r, Err(ChannelError::HandshakeConfirm)), "{r:?}");
        assert!(!c.established());
    }

    #[tokio::test]
    async fn wrong_static_key_or_pin_mismatch_fails_before_any_dh() {
        let v = load();
        // M2 from the real daemon, pin = someone else: nothing but M1 goes out.
        let other = EphemeralSecret::from_seed([0x55; 32]).public();
        let mut c = scripted_client(
            &v,
            vec![frame(&v, "m2_frame_hex"), frame(&v, "r_d_frame_hex")],
        );
        let r = c.handshake(Some(&other)).await;
        assert!(matches!(r, Err(ChannelError::PinMismatch)), "{r:?}");
        assert_eq!(written(&c.transport_mut().sent), ["hello"]);
        // A low-order daemon static key with a mismatching pin is a pin error,
        // not a key agreement error: the pin comes first.
        let zero_m2 = encode_frame(
            FRAME_HELLO,
            &crypto::daemon_hello_payload(&[0; 32], &arr32(&v["public_keys"], "daemon_eph")),
        );
        let mut c = scripted_client(&v, vec![zero_m2]);
        let r = c.handshake(Some(&pin(&v))).await;
        assert!(matches!(r, Err(ChannelError::PinMismatch)), "{r:?}");
    }

    #[tokio::test]
    async fn forged_r_d_fails() {
        let v = load();
        let r_d = frame(&v, "r_d_frame_hex");
        let mut c = scripted_client(
            &v,
            vec![frame(&v, "m2_frame_hex"), flip(&r_d, r_d.len() - 1)],
        );
        let r = c.handshake(Some(&pin(&v))).await;
        assert!(matches!(r, Err(ChannelError::HandshakeConfirm)), "{r:?}");
        assert_eq!(
            written(&c.transport_mut().sent),
            ["hello"],
            "no R_c for a forged R_d"
        );
    }

    #[tokio::test]
    async fn downgrade_refused_v1_hello_from_daemon() {
        let v = load();
        let mut v1 = vec![1u8, 1];
        v1.extend_from_slice(&pin(&v));
        let mut c = scripted_client(&v, vec![encode_frame(FRAME_HELLO, &v1)]);
        let r = c.handshake(None).await;
        assert!(
            matches!(r, Err(ChannelError::UnsupportedVersion { peer: 1 })),
            "{r:?}"
        );
        assert_eq!(written(&c.transport_mut().sent), ["hello"]);
        // The channel is dead: no second handshake over it.
        let again = c.handshake(None).await;
        assert!(matches!(again, Err(ChannelError::Closed)), "{again:?}");
    }

    #[tokio::test]
    async fn downgrade_refused_v1_hello_to_daemon_and_newer_peer_is_distinguished() {
        let v = load();
        let mut t = RawTransport::new(vec![encode_frame(
            FRAME_HELLO,
            &[&[1u8, 2][..], &arr32(&v["public_keys"], "client_eph")].concat(),
        )]);
        let static_secret = EphemeralSecret::from_seed(seed(&v, "daemon_static"));
        let r = test_daemon::handshake(
            &mut t,
            &static_secret,
            EphemeralSecret::from_seed(seed(&v, "daemon_eph")),
        )
        .await;
        assert!(matches!(
            r,
            Err(ChannelError::UnsupportedVersion { peer: 1 })
        ));
        assert!(t.sent.is_empty(), "a v1 client gets no reply at all");

        let mut v3 = vec![3u8, 1];
        v3.extend_from_slice(&pin(&v));
        let mut c = scripted_client(&v, vec![encode_frame(FRAME_HELLO, &v3)]);
        let r = c.handshake(None).await;
        assert!(
            matches!(r, Err(ChannelError::UnsupportedVersion { peer: 3 })),
            "{r:?}"
        );
    }

    #[tokio::test]
    async fn empty_ready_is_protocol_error() {
        let v = load();
        let mut c = scripted_client(
            &v,
            vec![frame(&v, "m2_frame_hex"), encode_frame(FRAME_READY, &[])],
        );
        let r = c.handshake(Some(&pin(&v))).await;
        assert!(matches!(r, Err(ChannelError::Protocol(_))), "{r:?}");
    }

    #[tokio::test]
    async fn low_order_ephemeral_rejected_on_both_dh_outputs() {
        let v = load();
        let real_static = pin(&v);
        let real_eph = arr32(&v["public_keys"], "daemon_eph");
        // ee = DH(client eph, zero) and es = DH(client eph, zero), unpinned.
        for m2 in [
            crypto::daemon_hello_payload(&real_static, &[0; 32]),
            crypto::daemon_hello_payload(&[0; 32], &real_eph),
        ] {
            let mut c = scripted_client(&v, vec![encode_frame(FRAME_HELLO, &m2)]);
            let r = c.handshake(None).await;
            assert!(matches!(r, Err(ChannelError::KeyAgreement)), "{r:?}");
            assert_eq!(written(&c.transport_mut().sent), ["hello"]);
        }
    }

    #[tokio::test]
    async fn duplicate_m2_and_r_d_are_tolerated_but_a_different_m2_is_key_rotation() {
        let v = load();
        let m2 = frame(&v, "m2_frame_hex");
        let r_d = frame(&v, "r_d_frame_hex");

        // Before R_d: an identical M2 repeat is ignored.
        let mut c = scripted_client(&v, vec![m2.clone(), m2.clone(), r_d.clone()]);
        c.handshake(Some(&pin(&v))).await.unwrap();

        // After establishment: identical M2 and R_d repeats are ignored, data still flows.
        c.transport_mut().incoming.extend([
            m2.clone(),
            r_d.clone(),
            frame(&v["data"]["d2m"][0], "frame_hex"),
        ]);
        assert_eq!(
            c.receive().await.unwrap(),
            bytes(&v["data"]["d2m"][0], "plaintext_hex")
        );

        // A different R_d on the established session fails confirmation and closes it.
        c.transport_mut()
            .incoming
            .push_back(flip(&r_d, r_d.len() - 1));
        let r = c.receive().await;
        assert!(matches!(r, Err(ChannelError::HandshakeConfirm)), "{r:?}");
        assert!(matches!(c.receive().await, Err(ChannelError::Closed)));

        // A different M2 before R_d is rotation.
        let other = flip(&m2, m2.len() - 1);
        let mut c = scripted_client(&v, vec![m2.clone(), other.clone()]);
        let r = c.handshake(None).await;
        assert!(matches!(r, Err(ChannelError::KeyRotation(_))), "{r:?}");

        // A different M2 on an established session is rotation too.
        let mut c = scripted_client(&v, vec![m2, r_d]);
        c.handshake(Some(&pin(&v))).await.unwrap();
        c.transport_mut().incoming.push_back(other);
        let r = c.receive().await;
        assert!(matches!(r, Err(ChannelError::KeyRotation(_))), "{r:?}");
    }

    #[tokio::test]
    async fn no_data_before_ready() {
        let v = load();
        // A data frame where R_d is expected.
        let mut c = scripted_client(
            &v,
            vec![
                frame(&v, "m2_frame_hex"),
                encode_frame(FRAME_DATA, &[0; 40]),
            ],
        );
        let r = c.handshake(Some(&pin(&v))).await;
        assert!(matches!(r, Err(ChannelError::Protocol(_))), "{r:?}");
        assert!(!c.established());
        // Before (or after a failed) handshake the channel neither sends nor receives.
        let mut fresh = scripted_client(&v, vec![]);
        assert!(matches!(
            fresh.send(b"x").await,
            Err(ChannelError::NotEstablished)
        ));
        assert!(matches!(
            fresh.receive().await,
            Err(ChannelError::NotEstablished)
        ));
        assert!(matches!(c.send(b"x").await, Err(ChannelError::Closed)));
        assert!(fresh.transport_mut().sent.is_empty());
    }

    #[tokio::test]
    async fn handshake_frames_are_capped_at_128_bytes() {
        let v = load();
        let base = crypto::daemon_hello_payload(&pin(&v), &arr32(&v["public_keys"], "daemon_eph"));
        // type + payload = 128 passes the cap (and then fails parsing on length);
        // type + payload = 129 is refused by the cap itself.
        for (payload_len, capped) in [(127, false), (128, true)] {
            let mut big = base.to_vec();
            big.resize(payload_len, 0);
            let mut c = scripted_client(&v, vec![encode_frame(FRAME_HELLO, &big)]);
            let r = c.handshake(None).await;
            match &r {
                Err(ChannelError::Protocol(m)) => assert_eq!(
                    m.contains("exceeds the 128 byte limit"),
                    capped,
                    "payload {payload_len}: {m}"
                ),
                other => panic!("payload {payload_len}: {other:?}"),
            }
        }
    }

    #[tokio::test]
    async fn client_ephemeral_is_never_reused_across_handshakes() {
        // Two real (unseeded) handshakes: each draws its own ephemeral.
        let mut m1s = vec![];
        for _ in 0..2 {
            let mut c = Channel::new(RawTransport::new(vec![]));
            let _ = c.handshake(None).await; // fails on the empty script, after M1 went out
            let (t, p) = c.transport_mut().sent[0].clone();
            assert_eq!(t, FRAME_HELLO);
            m1s.push(p);
        }
        assert_ne!(
            m1s[0], m1s[1],
            "two handshakes sent the same client ephemeral"
        );
        // And one channel cannot be handshaken twice.
        let mut c = Channel::new(RawTransport::new(vec![]));
        let _ = c.handshake(None).await;
        assert!(matches!(c.handshake(None).await, Err(ChannelError::Closed)));
        assert_eq!(c.transport_mut().sent.len(), 1);
    }

    /// Replaying a recorded M1 into a new responder yields different keys and
    /// the responder writes only M2 and R_d, never a data frame; without the
    /// client's ephemeral secret the replayer cannot produce R_c (the vector
    /// `wrong_r_c` covers the rejection byte for byte).
    #[tokio::test]
    async fn replayed_m1_gets_no_data_and_fresh_keys() {
        let v = load();
        let m1 = frame(&v, "m1_frame_hex");
        let static_secret = EphemeralSecret::from_seed(seed(&v, "daemon_static"));
        let mut th = vec![];
        for eph_seed in ["daemon_eph", "daemon_eph_replay"] {
            let mut t = RawTransport::new(vec![m1.clone()]);
            let r = test_daemon::handshake(
                &mut t,
                &static_secret,
                EphemeralSecret::from_seed(seed(&v, eph_seed)),
            )
            .await;
            assert!(
                matches!(r, Err(ChannelError::Transport(RawErr::Closed))),
                "waiting for R_c"
            );
            assert_eq!(written(&t.sent), ["hello", "ready"]);
            let (dpub, deph) = crypto::parse_daemon_hello(&t.sent[0].1).unwrap();
            th.push(crypto::transcript_hash(
                &dpub,
                &deph,
                &arr32(&v["public_keys"], "client_eph"),
            ));
        }
        assert_ne!(th[0], th[1]);
    }
}
