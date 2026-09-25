//! The E2EE handshake/send/receive state machine, generic over a
//! frame-oriented transport. Mirrors `internal/relay/e2ee.Channel` (Go)
//! 1:1, including its rotation/retry rules (ADR-0007 (e)).

use crate::relay::crypto::{
    self, FrameError, HelloError, KeyPair, Role, Session, SessionError, FRAME_DATA, FRAME_HELLO,
    FRAME_READY, PUBLIC_KEY_SIZE,
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
    Protocol(String),
    /// A peer re-handshaked with a *different* key on an established (or
    /// mid-handshake) session — rotation always means a new session, per
    /// ADR-0007 (e).
    KeyRotation(String),
    Replay {
        got: u64,
        want: u64,
    },
    Decrypt,
    NotEstablished,
    AlreadyEstablished,
}

impl<E: std::fmt::Display> std::fmt::Display for ChannelError<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ChannelError::Transport(e) => write!(f, "e2ee: transport: {e}"),
            ChannelError::Frame(e) => write!(f, "{e}"),
            ChannelError::Protocol(s) => write!(f, "e2ee: protocol violation: {s}"),
            ChannelError::KeyRotation(s) => {
                write!(f, "e2ee: peer re-handshaked with a different key: {s}")
            }
            ChannelError::Replay { got, want } => {
                write!(
                    f,
                    "e2ee: frame counter replayed or out of order: got counter {got}, want {want}"
                )
            }
            ChannelError::Decrypt => write!(f, "e2ee: frame failed to decrypt"),
            ChannelError::NotEstablished => write!(f, "e2ee: handshake not completed"),
            ChannelError::AlreadyEstablished => write!(f, "e2ee: handshake already completed"),
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

/// One end of an E2EE session carried over `T`. Owns the keypair, role,
/// and (once established) the session and peer public key.
pub struct Channel<T: FrameTransport> {
    transport: T,
    role: Role,
    peer_pub: Option<[u8; PUBLIC_KEY_SIZE]>,
    session: Option<Session>,
    established: bool,
}

impl<T: FrameTransport> Channel<T> {
    pub fn new(transport: T, role: Role) -> Self {
        Self {
            transport,
            role,
            peer_pub: None,
            session: None,
            established: false,
        }
    }

    pub fn established(&self) -> bool {
        self.established
    }

    pub fn peer_public_key(&self) -> Option<[u8; PUBLIC_KEY_SIZE]> {
        self.peer_pub
    }

    /// Runs the X25519 handshake: send hello, read the peer's hello,
    /// derive the session, then exchange ready frames. Starts a FRESH
    /// session — for a transport-level reconnect that should keep the
    /// existing session, don't call this again; see `crate::relay::client`.
    pub async fn handshake(&mut self, kp: &KeyPair) -> Result<(), ChannelError<T::Error>> {
        if self.established {
            return Err(ChannelError::AlreadyEstablished);
        }

        let hello = crypto::hello_payload(self.role, &kp.public());
        self.transport
            .send_frame(FRAME_HELLO, &hello)
            .await
            .map_err(ChannelError::Transport)?;

        let (peer_role, peer_pub) = self.read_hello().await?;
        let (daemon_pub, mobile_pub) = if self.role == Role::Mobile {
            (peer_pub, kp.public())
        } else {
            (kp.public(), peer_pub)
        };
        let session = Session::new(kp, &peer_pub, self.role, &daemon_pub, &mobile_pub);
        let _ = peer_role;

        self.transport
            .send_frame(FRAME_READY, &[])
            .await
            .map_err(ChannelError::Transport)?;
        self.await_ready(&peer_pub).await?;

        self.peer_pub = Some(peer_pub);
        self.session = Some(session);
        self.established = true;
        Ok(())
    }

    /// Encrypts and sends one application message.
    pub async fn send(&mut self, msg: &[u8]) -> Result<(), ChannelError<T::Error>> {
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
    /// it. A hello frame arriving on an established session is a retry
    /// (same key) or a rejected rotation attempt (different key); a
    /// duplicate ready is silently ignored.
    pub async fn receive(&mut self) -> Result<Vec<u8>, ChannelError<T::Error>> {
        if self.session.is_none() {
            return Err(ChannelError::NotEstablished);
        }
        loop {
            let (typ, payload) = self.recv_frame().await?;
            match typ {
                FRAME_DATA => {
                    if payload.len() < 8 {
                        return Err(ChannelError::Protocol(format!(
                            "data frame is {} bytes, want at least 8",
                            payload.len()
                        )));
                    }
                    let counter = u64::from_be_bytes(payload[0..8].try_into().unwrap());
                    let session = self.session.as_mut().expect("checked above");
                    return Ok(session.open(counter, &payload[8..])?);
                }
                FRAME_HELLO => {
                    let (_, new_pub) = parse_hello_checked(&payload, self.role)?;
                    if Some(new_pub) != self.peer_pub {
                        return Err(ChannelError::KeyRotation(
                            "rotation requires a new session, not an in-session rekey".into(),
                        ));
                    }
                    self.transport
                        .send_frame(FRAME_READY, &[])
                        .await
                        .map_err(ChannelError::Transport)?;
                }
                FRAME_READY => {
                    // Duplicate ready: harmless, the peer retried its handshake.
                }
                other => {
                    return Err(ChannelError::Protocol(format!(
                        "unknown frame type 0x{other:02x}"
                    )));
                }
            }
        }
    }

    async fn recv_frame(&mut self) -> Result<(u8, Vec<u8>), ChannelError<T::Error>> {
        self.transport
            .recv_frame()
            .await
            .map_err(ChannelError::Transport)
    }

    async fn read_hello(
        &mut self,
    ) -> Result<(Role, [u8; PUBLIC_KEY_SIZE]), ChannelError<T::Error>> {
        let (typ, payload) = self.recv_frame().await?;
        if typ != FRAME_HELLO {
            return Err(ChannelError::Protocol(format!(
                "expected hello, got frame type 0x{typ:02x}"
            )));
        }
        parse_hello_checked(&payload, self.role)
    }

    async fn await_ready(
        &mut self,
        peer_pub: &[u8; PUBLIC_KEY_SIZE],
    ) -> Result<(), ChannelError<T::Error>> {
        loop {
            let (typ, payload) = self.recv_frame().await?;
            match typ {
                FRAME_READY => return Ok(()),
                FRAME_HELLO => {
                    let (_, retry_pub) = parse_hello_checked(&payload, self.role)?;
                    if &retry_pub != peer_pub {
                        return Err(ChannelError::KeyRotation(
                            "peer changed keys mid-handshake".into(),
                        ));
                    }
                    self.transport
                        .send_frame(FRAME_READY, &[])
                        .await
                        .map_err(ChannelError::Transport)?;
                }
                other => {
                    return Err(ChannelError::Protocol(format!(
                        "expected ready, got frame type 0x{other:02x}"
                    )));
                }
            }
        }
    }
}

fn parse_hello_checked<E>(
    payload: &[u8],
    own_role: Role,
) -> Result<(Role, [u8; PUBLIC_KEY_SIZE]), ChannelError<E>> {
    crypto::parse_hello(payload, own_role).map_err(|e| {
        ChannelError::Protocol(match e {
            HelloError::WrongLength(n) => {
                format!("hello is {n} bytes, want {}", crypto::HELLO_PAYLOAD_LEN)
            }
            HelloError::WrongVersion(v) => format!(
                "peer protocol version {v}, want {}",
                crypto::PROTOCOL_VERSION
            ),
            HelloError::UnknownRole(r) => format!("peer sent unknown role {r}"),
            HelloError::SameRole(r) => format!("peer claims the same role ({r:?}) as this end"),
        })
    })
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

#[cfg(test)]
mod tests {
    use super::test_pipe::{pipe, send_raw};
    use super::*;
    use crate::relay::crypto::{hello_payload, KeyPair};

    fn kp(seed: u8) -> KeyPair {
        KeyPair::from_seed([seed; 32])
    }

    #[tokio::test]
    async fn handshake_and_round_trip() {
        let (a, b) = pipe();
        let mut daemon = Channel::new(a, Role::Daemon);
        let mut mobile = Channel::new(b, Role::Mobile);
        let daemon_kp = kp(1);
        let mobile_kp = kp(2);

        let (r1, r2) = tokio::join!(daemon.handshake(&daemon_kp), mobile.handshake(&mobile_kp));
        r1.unwrap();
        r2.unwrap();
        assert!(daemon.established());
        assert!(mobile.established());
        assert_eq!(daemon.peer_public_key().unwrap(), mobile_kp.public());
        assert_eq!(mobile.peer_public_key().unwrap(), daemon_kp.public());

        daemon.send(b"hello mobile").await.unwrap();
        let got = mobile.receive().await.unwrap();
        assert_eq!(got, b"hello mobile");

        mobile.send(b"hello daemon").await.unwrap();
        let got = daemon.receive().await.unwrap();
        assert_eq!(got, b"hello daemon");
    }

    #[tokio::test]
    async fn duplicate_hello_with_same_key_is_tolerated_before_ready() {
        let (a, b) = pipe();
        let mut daemon = Channel::new(a, Role::Daemon);
        let daemon_kp = kp(1);
        let mobile_kp = kp(2);

        // Simulate the mobile side racing two hellos with the *same* key
        // (peer never observed our ready) before ever sending ready.
        let hello = hello_payload(Role::Mobile, &mobile_kp.public());
        send_raw(&b, crypto::FRAME_HELLO, hello.clone());
        send_raw(&b, crypto::FRAME_HELLO, hello);
        send_raw(&b, crypto::FRAME_READY, vec![]);

        daemon.handshake(&daemon_kp).await.unwrap();
        assert!(daemon.established());
    }

    #[tokio::test]
    async fn hello_with_different_key_before_ready_is_rejected() {
        let (a, b) = pipe();
        let mut daemon = Channel::new(a, Role::Daemon);
        let daemon_kp = kp(1);
        let mobile_kp = kp(2);
        let other_kp = kp(3);

        send_raw(
            &b,
            crypto::FRAME_HELLO,
            hello_payload(Role::Mobile, &mobile_kp.public()),
        );
        send_raw(
            &b,
            crypto::FRAME_HELLO,
            hello_payload(Role::Mobile, &other_kp.public()),
        );

        let err = daemon.handshake(&daemon_kp).await.unwrap_err();
        assert!(matches!(err, ChannelError::KeyRotation(_)), "got {err:?}");
    }

    #[tokio::test]
    async fn rehello_with_same_key_on_established_session_is_tolerated() {
        let (a, b) = pipe();
        let raw_to_daemon = b.raw_sender(); // pushes onto the queue `a` (daemon) reads
        let mut daemon = Channel::new(a, Role::Daemon);
        let mut mobile = Channel::new(b, Role::Mobile);
        let daemon_kp = kp(1);
        let mobile_kp = kp(2);
        let (r1, r2) = tokio::join!(daemon.handshake(&daemon_kp), mobile.handshake(&mobile_kp));
        r1.unwrap();
        r2.unwrap();

        // A duplicate hello with the *same* key, as if mobile never saw
        // our ready and retried: daemon must tolerate it (re-ack with
        // ready, session stays usable), not tear the channel down.
        raw_to_daemon.send(
            crypto::FRAME_HELLO,
            hello_payload(Role::Mobile, &mobile_kp.public()),
        );
        mobile.send(b"still works").await.unwrap();
        let got = daemon.receive().await.unwrap();
        assert_eq!(got, b"still works");
    }

    #[tokio::test]
    async fn rehello_with_different_key_after_established_is_rejected() {
        let (a, b) = pipe();
        let raw_to_daemon = b.raw_sender();
        let mut daemon = Channel::new(a, Role::Daemon);
        let mut mobile = Channel::new(b, Role::Mobile);
        let daemon_kp = kp(1);
        let mobile_kp = kp(2);
        let other_kp = kp(3);
        let (r1, r2) = tokio::join!(daemon.handshake(&daemon_kp), mobile.handshake(&mobile_kp));
        r1.unwrap();
        r2.unwrap();

        // A rogue re-handshake with a *different* key: daemon must reject
        // it as rotation, per ADR-0007 (e) (a new session, never an
        // in-place rekey).
        raw_to_daemon.send(
            crypto::FRAME_HELLO,
            hello_payload(Role::Mobile, &other_kp.public()),
        );
        let err = daemon.receive().await.unwrap_err();
        assert!(matches!(err, ChannelError::KeyRotation(_)), "got {err:?}");
    }
}
