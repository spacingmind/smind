//! Native gRPC transport to the relay (ADR-0007 amendment, 2026-09-25):
//! TLS with pairing-time fingerprint pinning (ADR-0011), the admission
//! challenge-response, and the reconnect/resume loop that keeps a
//! `Channel<DataFrameTransport>` (see `crate::relay::channel`) alive
//! across transport-level drops, mirroring `internal/relay/bridge.Run`
//! (Go) — but from the "device" (mobile role) side, since the desktop
//! app is a relay-paired device like a phone, not the daemon itself.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use tokio::sync::{broadcast, mpsc, watch};
use tokio::time::Instant;
use tokio_stream::wrappers::ReceiverStream;
use tonic::metadata::MetadataValue;
use tonic::transport::{Channel as GrpcChannel, Endpoint, Uri};
use tonic::Request;

use crate::backoff::Backoff;
use crate::relay::admission::{self, Transcript};
use crate::relay::channel::ChannelError;
use crate::relay::channel::{Channel as E2eeChannel, FrameTransport};
use crate::relay::crypto::{decode_frame, encode_frame, FrameError, KeyPair, Role};
use crate::relay::device::{self, DeviceKey, HelloOutcome};
use crate::relay::liveness::{self, Action, Liveness, LivenessCfg};
use crate::relay::pairing_store::RelayPairing;
use crate::relay::relaypb::relay_client::RelayClient as GrpcRelayClient;
use crate::relay::relaypb::{AdmitChallengeRequest, AdmitRequest, Direction, Frame};

/// The fixed (session, device) pair of the single-device relay protocol
/// (milestone 1). A daemon from before device identity (ADR-0031 P12b) only
/// serves this route; a current daemon serves it only while its
/// `relay.legacyDevices` is on (off by default). This client uses it as the
/// FALLBACK for such a daemon: its normal route is the one named by its own
/// device id and a fresh session id.
pub const DEFAULT_SESSION_ID: &str = "m1-default-session";
pub const DEFAULT_DEVICE_ID: &str = "m1-default-device";

const ADMISSION_METADATA_KEY: &str = "admission-id";
const MAX_RECONNECT_BACKOFF: Duration = Duration::from_secs(30);
const MIN_STABLE_CONNECTION: Duration = Duration::from_secs(2);
const FRAME_CHANNEL_CAPACITY: usize = 64;
const ADMIT_TIMEOUT: Duration = Duration::from_secs(10);
const HANDSHAKE_TIMEOUT: Duration = Duration::from_secs(30);

/// derive_native_grpc_address converts a pairing offer's `relay` field
/// (the relay's **grpc-web** address, e.g. `https://host:7401` — see
/// `internal/relay/pairing`, mobile-only) to the relay's **native** gRPC
/// address this desktop client actually dials, by the same "native port
/// = grpc-web port ± 1" convention `cmd/smind/relay.go`'s
/// `cmdRelayOffer`/`deriveGRPCWebAddress` already documents and relies
/// on for the inverse direction. This is a known, narrow limitation
/// (breaks for a non-default, non-adjacent port deployment), matching
/// that existing accepted milestone-1 scope — not a new one, and not a
/// protocol change.
pub fn derive_native_grpc_address(relay_grpc_web_url: &str) -> Result<String, String> {
    let url = url::Url::parse(relay_grpc_web_url)
        .map_err(|e| format!("relay address {relay_grpc_web_url:?}: {e}"))?;
    let host = url
        .host_str()
        .ok_or_else(|| format!("relay address {relay_grpc_web_url:?}: missing host"))?;
    let port = url
        .port()
        .ok_or_else(|| format!("relay address {relay_grpc_web_url:?}: missing port"))?;
    let native_port = port.checked_sub(1).ok_or_else(|| {
        format!("relay address {relay_grpc_web_url:?}: port {port} has no native-1 counterpart")
    })?;
    Ok(format!("{host}:{native_port}"))
}

// --- TLS: pin the relay's certificate fingerprint (ADR-0011), no CA ---

#[derive(Debug)]
struct PinnedFingerprintVerifier {
    fingerprint: [u8; 32],
    provider: Arc<rustls::crypto::CryptoProvider>,
}

impl rustls::client::danger::ServerCertVerifier for PinnedFingerprintVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &rustls::pki_types::CertificateDer<'_>,
        _intermediates: &[rustls::pki_types::CertificateDer<'_>],
        _server_name: &rustls::pki_types::ServerName<'_>,
        _ocsp_response: &[u8],
        _now: rustls::pki_types::UnixTime,
    ) -> Result<rustls::client::danger::ServerCertVerified, rustls::Error> {
        use sha2::{Digest, Sha256};
        let sum: [u8; 32] = Sha256::digest(end_entity.as_ref()).into();
        if sum == self.fingerprint {
            Ok(rustls::client::danger::ServerCertVerified::assertion())
        } else {
            Err(rustls::Error::General(
                "relay client: certificate fingerprint does not match pin".into(),
            ))
        }
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &rustls::pki_types::CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(
            message,
            cert,
            dss,
            &self.provider.signature_verification_algorithms,
        )
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &rustls::pki_types::CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(
            message,
            cert,
            dss,
            &self.provider.signature_verification_algorithms,
        )
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        self.provider
            .signature_verification_algorithms
            .supported_schemes()
    }
}

/// dial connects to the relay at `native_addr` (host:port), pinning the
/// TLS certificate fingerprint (hex SHA-256 of the DER cert, from
/// `RelayPairing::relay_fingerprint`) instead of trusting any CA —
/// mirrors `internal/relay/client.Dial` (Go) exactly, but via a custom
/// rustls connector rather than tonic's built-in TLS feature (see the
/// ADR-0007 amendment: this is a plain native-gRPC dial, no grpc-web).
pub async fn dial(native_addr: &str, fingerprint_hex: &str) -> Result<GrpcChannel, String> {
    let fingerprint: [u8; 32] = hex_decode(fingerprint_hex)?
        .try_into()
        .map_err(|_| format!("relay client: bad fingerprint {fingerprint_hex:?}"))?;

    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let verifier = Arc::new(PinnedFingerprintVerifier {
        fingerprint,
        provider: provider.clone(),
    });
    let mut tls_config = rustls::ClientConfig::builder_with_provider(provider)
        .with_protocol_versions(&[&rustls::version::TLS13])
        .map_err(|e| format!("relay client: tls config: {e}"))?
        .dangerous()
        .with_custom_certificate_verifier(verifier)
        .with_no_client_auth();
    // grpc-go's server-side transport credentials require ALPN "h2" to
    // pick HTTP/2 -- without it the TLS handshake completes but the
    // server closes the connection as soon as we speak HTTP/2 on it
    // (observed as a broken-pipe error on the very first RPC).
    tls_config.alpn_protocols = vec![b"h2".to_vec()];
    let connector = tokio_rustls::TlsConnector::from(Arc::new(tls_config));

    let host = native_addr
        .rsplit_once(':')
        .map(|(h, _)| h.to_string())
        .unwrap_or_else(|| native_addr.to_string());
    let server_name = rustls::pki_types::ServerName::try_from(host)
        .map_err(|e| format!("relay client: server name from {native_addr:?}: {e}"))?
        .to_owned();

    let dial_addr = native_addr.to_string();
    let connector = connector.clone();
    let svc = tower::service_fn(move |_uri: Uri| {
        let connector = connector.clone();
        let dial_addr = dial_addr.clone();
        let server_name = server_name.clone();
        async move {
            let tcp = tokio::net::TcpStream::connect(&dial_addr).await?;
            let tls = connector.connect(server_name, tcp).await?;
            Ok::<_, std::io::Error>(hyper_util::rt::TokioIo::new(tls))
        }
    });

    let endpoint = Endpoint::from_shared(format!("https://{native_addr}"))
        .map_err(|e| format!("relay client: endpoint {native_addr:?}: {e}"))?;
    endpoint
        .connect_with_connector(svc)
        .await
        .map_err(|e| format!("relay client: connect: {e}"))
}

fn hex_decode(s: &str) -> Result<Vec<u8>, String> {
    if !s.len().is_multiple_of(2) {
        return Err(format!("odd-length hex string {s:?}"));
    }
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).map_err(|e| e.to_string()))
        .collect()
}

/// admit completes the admission exchange (ADR-0011) over `client`:
/// obtains a server nonce and proves possession of the workspace secret
/// with the SCRAM-style proof of `admission::compute_proof` (the secret
/// itself never leaves the process). Returns the
/// admission ID (hex) to present on subsequent streams — mirrors
/// `client.Admit` (Go).
pub async fn admit(
    client: &mut GrpcRelayClient<GrpcChannel>,
    workspace_id: &str,
    daemon_key_id: &str,
    secret: &[u8],
) -> Result<String, String> {
    let client_nonce = admission::random_nonce();
    let mut req = Request::new(AdmitChallengeRequest {
        protocol_version: admission::PROTOCOL_VERSION,
        workspace_id: workspace_id.to_string(),
        client_nonce: client_nonce.to_vec(),
        daemon_key_id: daemon_key_id.to_string(),
    });
    req.set_timeout(ADMIT_TIMEOUT);
    let challenge = client
        .admit_challenge(req)
        .await
        .map_err(|e| format!("relay client: challenge: {e}"))?
        .into_inner();

    let proof = admission::compute_proof(
        secret,
        &Transcript {
            protocol_version: admission::PROTOCOL_VERSION,
            workspace_id,
            client_nonce: &client_nonce,
            server_nonce: &challenge.server_nonce,
            daemon_key_id,
        },
    );
    let mut req = Request::new(AdmitRequest {
        protocol_version: admission::PROTOCOL_VERSION,
        workspace_id: workspace_id.to_string(),
        client_nonce: client_nonce.to_vec(),
        daemon_key_id: daemon_key_id.to_string(),
        server_nonce: challenge.server_nonce,
        // Wire field 6 is named `hmac` in relay.proto; it carries the proof.
        hmac: proof.to_vec(),
    });
    req.set_timeout(ADMIT_TIMEOUT);
    let resp = client
        .admit(req)
        .await
        .map_err(|e| format!("relay client: admit: {e}"))?
        .into_inner();
    Ok(hex_encode(&resp.admission_id))
}

fn hex_encode(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

// --- Data stream: wraps OpenData as a FrameTransport for the e2ee Channel ---

#[derive(Debug)]
pub enum DataTransportError {
    Closed,
    Status(tonic::Status),
    Frame(FrameError),
    Open(String),
}

impl std::fmt::Display for DataTransportError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            DataTransportError::Closed => write!(f, "relay client: data stream closed"),
            DataTransportError::Status(s) => write!(f, "relay client: data stream: {s}"),
            DataTransportError::Frame(e) => write!(f, "{e}"),
            DataTransportError::Open(e) => write!(f, "{e}"),
        }
    }
}

/// DataFrameTransport adapts one `OpenData` bidi stream to
/// `FrameTransport`: writes become outbound `Frame`s (payload = the
/// whole length-prefixed e2ee wire frame, opaque here too), reads return
/// inbound `Frame` payloads decoded back into `(type, payload)`.
/// `seq` is this adapter's own monotonic counter (relay buffer
/// ordering) — separate from the e2ee session's own AEAD counters,
/// mirrors `frameConn` (Go) exactly, including `reopen` keeping `seq`
/// running across a transport-level reconnect.
pub struct DataFrameTransport {
    tx: mpsc::Sender<Frame>,
    rx: tonic::Streaming<Frame>,
    workspace_id: String,
    session_id: Vec<u8>,
    device_id: String,
    direction: Direction,
    seq: u64,
    liveness: Arc<Mutex<Liveness>>,
}

impl DataFrameTransport {
    /// liveness is the keepalive state shared with `pump`: every frame the
    /// relay sends is reported to it from `recv_frame`.
    pub fn liveness(&self) -> Arc<Mutex<Liveness>> {
        self.liveness.clone()
    }

    /// send_probe writes one keepalive probe (ADR-0031 D9 flow 8): an empty
    /// frame flagged `keepalive` whose `sequence` carries this connection's
    /// nonce, which the relay echoes back. It is not counted in `seq` (the
    /// relay never buffers or forwards it).
    pub async fn send_probe(&mut self) -> Result<(), DataTransportError> {
        let nonce = self.liveness.lock().unwrap().nonce();
        let mut frame = self.build_frame(nonce, Vec::new());
        frame.keepalive = true;
        self.tx
            .send(frame)
            .await
            .map_err(|_| DataTransportError::Closed)
    }

    fn build_frame(&self, seq: u64, payload: Vec<u8>) -> Frame {
        Frame {
            workspace_id: self.workspace_id.clone(),
            session_id: self.session_id.clone(),
            device_id: self.device_id.clone(),
            direction: self.direction as i32,
            sequence: seq,
            payload,
            keepalive: false,
        }
    }

    async fn write_raw(&mut self, payload: Vec<u8>) -> Result<(), DataTransportError> {
        let frame = self.build_frame(self.seq, payload);
        self.seq += 1;
        self.tx
            .send(frame)
            .await
            .map_err(|_| DataTransportError::Closed)
    }

    /// reopen re-dials `OpenData` under `admission_id` and swaps in the
    /// fresh stream, keeping `seq` running (relay buffer ordering) —
    /// mirrors `frameConn.reopen` (Go). The e2ee `Channel` wrapping this
    /// transport is untouched by a caller doing this (see
    /// `Channel::transport_mut`), so its session/counters survive.
    ///
    /// The registration frame (the relay derives a stream's route from
    /// its first frame) is sent *concurrently* with dispatching the
    /// `OpenData` call, not after it resolves: a gRPC server for a
    /// streaming RPC typically doesn't send response headers until it
    /// has sent (or, here, received) its first message, so awaiting
    /// `open_data` to completion before ever writing to the request body
    /// would deadlock -- the relay's own `OpenData` handler blocks on its
    /// first `Recv` to learn the route before doing anything else.
    pub async fn reopen(
        &mut self,
        grpc: &mut GrpcRelayClient<GrpcChannel>,
        admission_id: &str,
    ) -> Result<(), DataTransportError> {
        let registration = self.build_frame(self.seq, Vec::new());
        self.seq += 1;
        let (tx, rx) = open_data_stream(grpc, admission_id, registration)
            .await
            .map_err(DataTransportError::Open)?;
        self.tx = tx;
        self.rx = rx;
        self.liveness.lock().unwrap().start_stream(Instant::now());
        Ok(())
    }

    /// drop_transport ends this transport's outbound stream half only
    /// (drops the sender, so the `OpenData` request body it feeds yields
    /// `None` and the server sees a clean half-close) -- the model of a
    /// network drop, mirroring `frameConn.drop`/`DataConn.DropTransport`
    /// (Go)'s `stream.CloseSend()`. It deliberately leaves the inbound
    /// response stream (`self.rx`) untouched: dropping a tonic
    /// `Streaming<T>` response reader before it's naturally exhausted
    /// sends an HTTP/2 `RST_STREAM`, which cancels the *whole* bidi
    /// stream, including whatever this side had already enqueued to
    /// send but not yet flushed onto the wire -- silently discarding it
    /// instead of letting it complete as a graceful half-close.
    pub fn drop_transport(&mut self) {
        let (dead_tx, _unused_rx) = mpsc::channel(1);
        self.tx = dead_tx;
    }
}

#[async_trait::async_trait]
impl FrameTransport for DataFrameTransport {
    type Error = DataTransportError;

    async fn send_frame(&mut self, frame_type: u8, payload: &[u8]) -> Result<(), Self::Error> {
        let buf = encode_frame(frame_type, payload);
        self.write_raw(buf).await
    }

    async fn recv_frame(&mut self) -> Result<(u8, Vec<u8>), Self::Error> {
        use tonic::codegen::tokio_stream::StreamExt;
        loop {
            let rx = &mut self.rx;
            let frame = rx
                .next()
                .await
                .ok_or(DataTransportError::Closed)?
                .map_err(DataTransportError::Status)?;
            // Liveness first: ANY frame from the relay proves the stream is
            // alive, and a keepalive frame carrying our nonce is an echo.
            self.liveness
                .lock()
                .unwrap()
                .on_frame(Instant::now(), frame.keepalive, frame.sequence);
            if frame.keepalive {
                continue; // relay plumbing (an echo, or a probe a relay forwarded), never e2ee content
            }
            // The peer's own empty-payload registration frame (relay
            // routing plumbing, not e2ee content) also arrives on this
            // stream, since the relay forwards every frame -- including
            // registration ones -- to the opposite side unconditionally.
            // Go's client absorbs this transparently because it reads a
            // byte *stream* (`io.ReadFull` just retries on a zero-byte
            // read); this transport is message-oriented instead, so it
            // has to skip an empty payload explicitly rather than trying
            // to decode it as a wire frame.
            if frame.payload.is_empty() {
                continue;
            }
            let (typ, payload) = decode_frame(&frame.payload).map_err(DataTransportError::Frame)?;
            return Ok((typ, payload.to_vec()));
        }
    }
}

/// open_data_stream opens a fresh `OpenData` stream under `admission_id`
/// and sends `first_frame` on it (the relay derives a stream's route
/// from its first frame — a bare registration frame on a fresh
/// `DataFrameTransport`, or the next frame in sequence when resuming an
/// existing one), returning the raw `(sender, receiver)` pair for the
/// caller to wrap or swap in.
///
/// `first_frame` is sent *concurrently* with `grpc.open_data(req)`
/// itself, not after it resolves: awaiting the RPC to complete before
/// ever writing anything to its request body would deadlock, since a
/// streaming RPC's response headers are typically not sent by the
/// server until it has processed a first inbound message (here, the
/// relay's `OpenData` handler blocks on its own first `Recv` to learn
/// the route before sending anything back). `mpsc::Sender::send` on an
/// empty-enough channel completes as soon as the item is buffered, not
/// waiting for the receiving end to be polled, so running the two
/// concurrently (rather than the sender being read first) is safe and
/// resolves both.
///
/// Exposed as a public building block for anyone driving the handshake/
/// reconnect steps manually (e.g. the mandatory harness interop test)
/// rather than through the full `spawn`-managed reconnect loop.
pub async fn open_data_stream(
    grpc: &mut GrpcRelayClient<GrpcChannel>,
    admission_id: &str,
    first_frame: Frame,
) -> Result<(mpsc::Sender<Frame>, tonic::Streaming<Frame>), String> {
    let (tx, rx) = mpsc::channel::<Frame>(FRAME_CHANNEL_CAPACITY);
    let mut req = Request::new(ReceiverStream::new(rx));
    req.metadata_mut().insert(
        ADMISSION_METADATA_KEY,
        MetadataValue::try_from(admission_id)
            .map_err(|e| format!("relay client: admission metadata: {e}"))?,
    );
    // Send before awaiting the RPC's own resolution, not after: a
    // streaming RPC's response headers are typically not sent by the
    // server until it has processed a first inbound message (the relay's
    // `OpenData` handler blocks on its own first `Recv` to learn the
    // route), so awaiting `open_data` to completion first would deadlock.
    // The send itself only needs buffer space, not an active reader, so
    // it resolves immediately regardless of `open_data`'s progress.
    tx.send(first_frame)
        .await
        .map_err(|_| "relay client: send first frame: channel closed immediately".to_string())?;
    let stream = grpc
        .open_data(req)
        .await
        .map_err(|e| format!("relay client: open data: {e}"))?
        .into_inner();
    Ok((tx, stream))
}

/// open_data opens a fresh `OpenData` stream under `admission_id` for
/// (workspace, session, device), sends the routing registration frame,
/// and wraps it as a `DataFrameTransport` -- the device-role counterpart
/// of `internal/relay/client.OpenData` (Go).
pub async fn open_data(
    grpc: &mut GrpcRelayClient<GrpcChannel>,
    admission_id: &str,
    workspace_id: &str,
    session_id: &str,
    device_id: &str,
) -> Result<DataFrameTransport, String> {
    let direction = Direction::DeviceToDaemon; // this client always plays the device (mobile) role
    let registration = Frame {
        workspace_id: workspace_id.to_string(),
        session_id: session_id.as_bytes().to_vec(),
        device_id: device_id.to_string(),
        direction: direction as i32,
        sequence: 0,
        payload: Vec::new(),
        keepalive: false,
    };
    let (tx, rx) = open_data_stream(grpc, admission_id, registration).await?;
    let mut liveness = Liveness::new(
        LivenessCfg::default(),
        liveness::random_nonce(),
        Instant::now(),
    );
    liveness.start_stream(Instant::now());
    Ok(DataFrameTransport {
        tx,
        rx,
        workspace_id: workspace_id.to_string(),
        session_id: session_id.as_bytes().to_vec(),
        device_id: device_id.to_string(),
        direction,
        seq: 1,
        liveness: Arc::new(Mutex::new(liveness)),
    })
}

// --- The reconnect loop: connect/admit/handshake/hello, resume-first on drop ---

/// What the relay connection is doing, for the UI (Settings → Connections).
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Status {
    Connecting,
    Connected,
    Reconnecting,
    /// The daemon knows this device but has not approved it yet: the user
    /// approves it on the daemon (`smind relay devices approve <id>`). The
    /// client keeps retrying, so approval takes effect within seconds.
    AwaitingApproval {
        device_id: String,
    },
    /// The daemon (or the relay) revoked this device.
    Revoked {
        device_id: String,
    },
    /// The daemon refused this device's hello for another reason (`code` is
    /// the daemon's machine-readable code, e.g. `too_many_devices`).
    Refused {
        code: String,
        message: String,
    },
}

impl Status {
    /// state_name is the stable identifier the UI switches on.
    pub fn state_name(&self) -> &'static str {
        match self {
            Status::Connecting => "connecting",
            Status::Connected => "connected",
            Status::Reconnecting => "reconnecting",
            Status::AwaitingApproval { .. } => "awaitingApproval",
            Status::Revoked { .. } => "revoked",
            Status::Refused { .. } => "refused",
        }
    }
}

/// Timing knobs of the relay loop; the defaults are the documented protocol
/// values (30 s probe, 75 s dead stream, jittered backoff), tests shrink them.
#[derive(Clone)]
pub struct Timing {
    pub liveness: LivenessCfg,
    pub handshake_timeout: Duration,
    pub hello_timeout: Duration,
    /// How long a resumed session gets to answer a liveness request before it
    /// is abandoned for a fresh one (the daemon may have lost its counters).
    pub resume_probe_timeout: Duration,
    pub min_stable_connection: Duration,
    /// A request the daemon has not answered (nor sent anything else) for this
    /// long makes the client suspect the daemon lost its session (a restart:
    /// the relay has no way to tell a device) and re-establish it.
    pub request_stall: Duration,
    /// With nothing received from the daemon for this long the client sends
    /// a cheap request of its own, so a restart is noticed while idle too.
    pub idle_probe: Duration,
    pub backoff_base: Duration,
    pub backoff_max: Duration,
    /// Retry period while awaiting approval / after a revoke / after another
    /// refusal.
    pub awaiting_retry: Duration,
    pub revoked_retry: Duration,
    pub refused_retry: Duration,
    /// Spreads a delay (±20 % by default) so a fleet reconnecting after a
    /// relay restart does not move in lockstep.
    pub jitter: fn(Duration) -> Duration,
}

impl Default for Timing {
    fn default() -> Self {
        Self {
            liveness: LivenessCfg::default(),
            handshake_timeout: HANDSHAKE_TIMEOUT,
            hello_timeout: Duration::from_secs(10),
            resume_probe_timeout: Duration::from_secs(10),
            min_stable_connection: MIN_STABLE_CONNECTION,
            request_stall: Duration::from_secs(15),
            idle_probe: Duration::from_secs(60),
            backoff_base: Duration::from_secs(1),
            backoff_max: MAX_RECONNECT_BACKOFF,
            awaiting_retry: Duration::from_secs(5),
            revoked_retry: Duration::from_secs(60),
            refused_retry: Duration::from_secs(30),
            jitter: default_jitter,
        }
    }
}

fn default_jitter(d: Duration) -> Duration {
    let mut b = [0u8; 4];
    getrandom::fill(&mut b).expect("OS randomness for backoff jitter");
    let unit = f64::from(u32::from_be_bytes(b)) / f64::from(u32::MAX); // 0..=1
    d.mul_f64(0.8 + 0.4 * unit)
}

/// RelayOptions configures one relay connection.
#[derive(Clone)]
pub struct RelayOptions {
    /// The connection's persistent device key. `None` = no device identity:
    /// the client then speaks only the single-device protocol (fixed route,
    /// no hello), as before ADR-0031 P12b.
    pub device_key: Option<Arc<DeviceKey>>,
    /// Label shown in `smind relay devices ls`.
    pub label: String,
    pub timing: Timing,
}

impl RelayOptions {
    pub fn identity(key: Arc<DeviceKey>) -> Self {
        Self {
            device_key: Some(key),
            label: default_device_label(),
            timing: Timing::default(),
        }
    }

    pub fn legacy_only() -> Self {
        Self {
            device_key: None,
            label: String::new(),
            timing: Timing::default(),
        }
    }
}

/// default_device_label names this install in the daemon's device list.
pub fn default_device_label() -> String {
    let host = std::env::var("COMPUTERNAME")
        .or_else(|_| std::env::var("HOSTNAME"))
        .or_else(|_| std::env::var("HOST"))
        .unwrap_or_default();
    let label = if host.trim().is_empty() {
        "sMind Desktop".to_string()
    } else {
        format!("sMind Desktop ({})", host.trim())
    };
    // The daemon rejects labels with control/invisible characters or > 64 bytes.
    let cleaned: String = label.chars().filter(|c| !c.is_control()).collect();
    let mut out = String::new();
    for c in cleaned.chars() {
        if out.len() + c.len_utf8() > 64 {
            break;
        }
        out.push(c);
    }
    if device::validate_label(&out).is_ok() {
        out
    } else {
        "sMind Desktop".to_string()
    }
}

/// A handle the proxy's `/ws` bridge and the daemon-client watcher both
/// use to send/receive JSON-RPC bytes over the single, shared relay
/// tunnel (see the plan's Decisions: one relay connection is one
/// multiplexed pipe, matching how `wsapi.ServeTransport` treats it
/// daemon-side).
#[derive(Clone)]
pub struct RelayHandle {
    outbound_tx: mpsc::Sender<Vec<u8>>,
    inbound_tx: broadcast::Sender<Vec<u8>>,
    status_rx: watch::Receiver<Status>,
}

impl RelayHandle {
    pub async fn send(&self, msg: Vec<u8>) -> Result<(), String> {
        self.outbound_tx
            .send(msg)
            .await
            .map_err(|_| "relay: connection is closed".to_string())
    }

    pub fn subscribe(&self) -> broadcast::Receiver<Vec<u8>> {
        self.inbound_tx.subscribe()
    }

    pub fn status(&self) -> watch::Receiver<Status> {
        self.status_rx.clone()
    }
}

#[cfg(test)]
pub(crate) type TestHandleParts = (
    RelayHandle,
    mpsc::Receiver<Vec<u8>>,
    broadcast::Sender<Vec<u8>>,
    watch::Sender<Status>,
);

#[cfg(test)]
/// for_test builds a `RelayHandle` wired to caller-controlled channels,
/// bypassing `spawn`'s real dial/admit/handshake -- for exercising code
/// that only depends on the `RelayHandle` surface (e.g.
/// `proxy::server::bridge_relay`) without a real relay.
pub(crate) fn test_handle() -> TestHandleParts {
    let (outbound_tx, outbound_rx) = mpsc::channel(FRAME_CHANNEL_CAPACITY);
    let (inbound_tx, _) = broadcast::channel(FRAME_CHANNEL_CAPACITY);
    let (status_tx, status_rx) = watch::channel(Status::Connected);
    (
        RelayHandle {
            outbound_tx,
            inbound_tx: inbound_tx.clone(),
            status_rx,
        },
        outbound_rx,
        inbound_tx,
        status_tx,
    )
}

/// spawn starts the background reconnect loop for `pairing` and returns
/// a handle to it. The loop runs until the returned handle (and every
/// clone/subscriber) is dropped.
pub fn spawn(
    pairing: RelayPairing,
    options: RelayOptions,
) -> (RelayHandle, tokio::task::JoinHandle<()>) {
    let (outbound_tx, outbound_rx) = mpsc::channel(FRAME_CHANNEL_CAPACITY);
    let (inbound_tx, _) = broadcast::channel(FRAME_CHANNEL_CAPACITY);
    let (status_tx, status_rx) = watch::channel(Status::Connecting);
    let inbound_tx_task = inbound_tx.clone();
    let task = tokio::spawn(run(
        pairing,
        options,
        outbound_rx,
        inbound_tx_task,
        status_tx,
    ));
    (
        RelayHandle {
            outbound_tx,
            inbound_tx,
            status_rx,
        },
        task,
    )
}

/// RouteMode is which route this client asks the daemon for.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum RouteMode {
    /// Its own device id and a fresh session id; identifies itself with
    /// `device_hello` (a current daemon).
    Identity,
    /// The fixed single-device route, no hello (a daemon from before device
    /// identity, or one that does not track devices).
    Legacy,
}

/// Why a fresh session could not be established.
#[derive(Debug)]
enum Failure {
    /// Transport, admission or handshake trouble: back off and retry.
    Retry(String),
    /// Nobody answered the handshake on the identity route: probably a
    /// daemon from before device identity (it never opens that route).
    HandshakeTimeout,
    /// The daemon answered the hello with a refusal.
    Refused { code: String, message: String },
    /// The relay itself refused the stream (PermissionDenied: revoked).
    RelayDenied(String),
    /// The daemon does not do device identity (method not found / unsupported).
    NoIdentitySupport,
}

type DataChannel = E2eeChannel<DataFrameTransport>;

fn random_session_id() -> String {
    let mut b = [0u8; 16];
    getrandom::fill(&mut b).expect("OS randomness for the session id");
    format!("dsk-{}", hex_encode(&b))
}

/// establish opens a FRESH session: a fresh ephemeral E2EE key (never reused
/// across sessions — a reused key with restarted counters would repeat
/// nonces), the E2EE handshake pinned to the daemon key, then, on the
/// identity route, `device_hello` bound to this very session.
async fn establish(
    grpc: &mut GrpcRelayClient<GrpcChannel>,
    admission_id: &str,
    pairing: &RelayPairing,
    options: &RelayOptions,
    mode: RouteMode,
) -> Result<DataChannel, Failure> {
    let (session_id, device_id) = match (mode, options.device_key.as_ref()) {
        (RouteMode::Identity, Some(key)) => (random_session_id(), key.device_id()),
        _ => (
            DEFAULT_SESSION_ID.to_string(),
            DEFAULT_DEVICE_ID.to_string(),
        ),
    };
    let transport = open_data(
        grpc,
        admission_id,
        &pairing.workspace_id,
        &session_id,
        &device_id,
    )
    .await
    .map_err(Failure::Retry)?;
    transport
        .liveness()
        .lock()
        .unwrap()
        .start_stream(Instant::now());
    let kp = KeyPair::generate();
    let mut channel = E2eeChannel::new(transport, Role::Mobile);
    let handshake = tokio::time::timeout(
        options.timing.handshake_timeout,
        channel.handshake(&kp, Some(&pairing.daemon_public_key)),
    )
    .await;
    match handshake {
        Ok(Ok(())) => {}
        Ok(Err(ChannelError::Transport(DataTransportError::Status(s))))
            if s.code() == tonic::Code::PermissionDenied =>
        {
            return Err(Failure::RelayDenied(s.message().to_string()));
        }
        Ok(Err(e)) => return Err(Failure::Retry(format!("handshake: {e}"))),
        Err(_) if mode == RouteMode::Identity => return Err(Failure::HandshakeTimeout),
        Err(_) => return Err(Failure::Retry("handshake: timed out".into())),
    }

    let (RouteMode::Identity, Some(key)) = (mode, options.device_key.as_ref()) else {
        return Ok(channel);
    };
    let hello = key.hello(&options.label, &pairing.daemon_public_key, &kp.public());
    let request_id = "relay-hello-1";
    channel
        .send(device::hello_request(request_id, &hello).as_bytes())
        .await
        .map_err(|e| Failure::Retry(format!("hello: {e}")))?;
    let outcome = tokio::time::timeout(options.timing.hello_timeout, async {
        loop {
            let msg = channel
                .receive()
                .await
                .map_err(|e| Failure::Retry(format!("hello: {e}")))?;
            let Ok(text) = String::from_utf8(msg) else {
                continue;
            };
            if let Some(outcome) = device::parse_hello_response(request_id, &text) {
                return Ok(outcome);
            }
        }
    })
    .await
    .map_err(|_| Failure::Retry("hello: timed out".into()))??;
    match outcome {
        HelloOutcome::Approved { .. } => Ok(channel),
        HelloOutcome::MethodNotFound => Err(Failure::NoIdentitySupport),
        HelloOutcome::Refused { code, .. } if code == "device_hello_unsupported" => {
            Err(Failure::NoIdentitySupport)
        }
        HelloOutcome::Refused { code, message } => Err(Failure::Refused { code, message }),
    }
}

const RESUME_PROBE_ID: &str = "relay-resume-probe";

/// confirm_alive proves a resumed session still talks to a daemon that holds
/// its E2EE state: the relay has no acknowledgements, so after a resume the
/// only evidence is a request that gets answered. Messages that arrive
/// meanwhile are forwarded as usual; the probe's own response is consumed.
async fn confirm_alive(
    channel: &mut DataChannel,
    inbound_tx: &broadcast::Sender<Vec<u8>>,
    timeout: Duration,
) -> bool {
    let request =
        serde_json::json!({"id": RESUME_PROBE_ID, "method": "workspace.list"}).to_string();
    if channel.send(request.as_bytes()).await.is_err() {
        return false;
    }
    tokio::time::timeout(timeout, async {
        loop {
            match channel.receive().await {
                Ok(msg) => {
                    if message_id(&msg).as_deref() == Some(RESUME_PROBE_ID) {
                        return true;
                    }
                    let _ = inbound_tx.send(msg);
                }
                Err(_) => return false,
            }
        }
    })
    .await
    .unwrap_or(false)
}

/// message_id returns the `id` of a JSON message that is a response (has a
/// result or an error), not an event.
fn message_id(msg: &[u8]) -> Option<String> {
    let v: serde_json::Value = serde_json::from_slice(msg).ok()?;
    if v.get("result").is_none() && v.get("error").is_none() {
        return None;
    }
    v.get("id")?.as_str().map(str::to_string)
}

/// is_hello_required reports whether `msg` is the daemon's refusal of a
/// client that skipped the hello (an error carrying `device_hello_required`).
fn is_hello_required(msg: &[u8]) -> bool {
    serde_json::from_slice::<serde_json::Value>(msg)
        .ok()
        .and_then(|v| {
            v["error"]["code"]
                .as_str()
                .map(|c| c == "device_hello_required")
        })
        .unwrap_or(false)
}

/// run is the reconnect loop: dial + admit, resume the existing e2ee
/// session on a transport-level reconnect whenever possible (falling back to
/// a fresh session when the resume cannot be confirmed or there is none),
/// identify with `device_hello`, and pump JSON-RPC bytes between the e2ee
/// channel and the outbound/inbound queues — the device-side mirror of
/// `internal/relay/bridge`'s hub, with the same stable-connection-resets-
/// backoff anti-flap rule.
async fn run(
    pairing: RelayPairing,
    options: RelayOptions,
    mut outbound_rx: mpsc::Receiver<Vec<u8>>,
    inbound_tx: broadcast::Sender<Vec<u8>>,
    status_tx: watch::Sender<Status>,
) {
    let timing = options.timing.clone();
    // The offer's daemon id IS the admission daemon_key_id (see
    // cmd/smind/relay.go's cmdRelayOffer: DaemonID = bridge.DaemonKeyID(kp),
    // the same string the daemon itself presents when admitting) -- this
    // client (playing the device/mobile role) presents that same value,
    // never re-derives its own.
    let daemon_key_id = pairing.daemon_id.clone();
    let device_id = options
        .device_key
        .as_ref()
        .map(|k| k.device_id())
        .unwrap_or_default();
    let mut backoff = Backoff::new(timing.backoff_base, timing.backoff_max);
    let mut data: Option<DataChannel> = None;
    let mut mode = if options.device_key.is_some() {
        RouteMode::Identity
    } else {
        RouteMode::Legacy
    };
    loop {
        {
            if matches!(*status_tx.borrow(), Status::Connected) {
                let _ = status_tx.send(Status::Reconnecting);
            }
            let dial_and_admit = async {
                let channel = dial(&pairing.relay_native_addr, &pairing.relay_fingerprint).await?;
                let mut grpc = GrpcRelayClient::new(channel);
                let admission_id = admit(
                    &mut grpc,
                    &pairing.workspace_id,
                    &daemon_key_id,
                    &pairing.admission_secret,
                )
                .await?;
                Ok::<_, String>((grpc, admission_id))
            };
            let (mut grpc, admission_id) = match dial_and_admit.await {
                Ok(v) => v,
                Err(e) => {
                    eprintln!("smind desktop: relay connect: {e}");
                    if sleep_or_stop(&mut outbound_rx, (timing.jitter)(backoff.next()))
                        .await
                        .is_none()
                    {
                        return;
                    }
                    continue;
                }
            };

            // Resume the established session in place when there is one.
            let mut resumed = false;
            if let Some(existing) = data.as_mut() {
                match existing
                    .transport_mut()
                    .reopen(&mut grpc, &admission_id)
                    .await
                {
                    Ok(()) => {
                        if confirm_alive(existing, &inbound_tx, timing.resume_probe_timeout).await {
                            resumed = true;
                        } else {
                            eprintln!("smind desktop: relay resume: not answered; starting a fresh session");
                        }
                    }
                    Err(e) => eprintln!("smind desktop: relay resume: open data: {e}"),
                }
                if !resumed {
                    data = None;
                }
            }
            if !resumed {
                match establish(&mut grpc, &admission_id, &pairing, &options, mode).await {
                    Ok(channel) => data = Some(channel),
                    Err(failure) => {
                        let delay = match failure {
                            Failure::Retry(e) => {
                                eprintln!("smind desktop: relay connect: {e}");
                                backoff.next()
                            }
                            Failure::HandshakeTimeout => {
                                // Nobody serves our identity route: assume a
                                // daemon from before device identity and try the
                                // fixed single-device route next.
                                eprintln!("smind desktop: relay: no daemon answered on the device route; trying the single-device route");
                                mode = RouteMode::Legacy;
                                Duration::ZERO
                            }
                            Failure::NoIdentitySupport => {
                                eprintln!("smind desktop: relay: the daemon does not support device identity; using the single-device route");
                                mode = RouteMode::Legacy;
                                Duration::ZERO
                            }
                            Failure::RelayDenied(msg) => {
                                eprintln!("smind desktop: relay refused this device: {msg}");
                                let _ = status_tx.send(Status::Revoked {
                                    device_id: device_id.clone(),
                                });
                                timing.revoked_retry
                            }
                            Failure::Refused { code, message } => {
                                let (status, delay) = match code.as_str() {
                                    "awaiting_approval" => (
                                        Status::AwaitingApproval {
                                            device_id: device_id.clone(),
                                        },
                                        timing.awaiting_retry,
                                    ),
                                    "revoked" => (
                                        Status::Revoked {
                                            device_id: device_id.clone(),
                                        },
                                        timing.revoked_retry,
                                    ),
                                    _ => (Status::Refused { code, message }, timing.refused_retry),
                                };
                                let _ = status_tx.send(status);
                                delay
                            }
                        };
                        if sleep_or_stop(&mut outbound_rx, (timing.jitter)(delay))
                            .await
                            .is_none()
                        {
                            return;
                        }
                        continue;
                    }
                }
            }
            backoff.reset();
            let _ = status_tx.send(Status::Connected);
        }

        let connected_at = Instant::now();
        let channel = data.as_mut().unwrap();
        let end = pump(channel, &mut outbound_rx, &inbound_tx, &timing).await;
        match end {
            PumpEnd::Stopped => return,
            PumpEnd::HelloRequired => {
                // A daemon that now requires identity: switch routes.
                if options.device_key.is_some() {
                    mode = RouteMode::Identity;
                    data = None;
                }
            }
            PumpEnd::Dead => eprintln!(
                "smind desktop: relay: nothing from the relay for {:?}; reconnecting",
                timing.liveness.dead_after
            ),
            PumpEnd::Stalled => eprintln!(
                "smind desktop: relay: the daemon stopped answering; re-checking the session"
            ),
            PumpEnd::Ended => {}
        }

        if connected_at.elapsed() >= timing.min_stable_connection {
            backoff.reset();
        } else if sleep_or_stop(&mut outbound_rx, (timing.jitter)(backoff.next()))
            .await
            .is_none()
        {
            return;
        }
    }
}

enum PumpEnd {
    /// Every `RelayHandle` sender was dropped: nothing left to serve.
    Stopped,
    /// The transport or the e2ee channel failed: resume.
    Ended,
    /// No frame from the relay for the dead-stream window: resume.
    Dead,
    /// The daemon stopped answering although the relay is alive (it may have
    /// restarted and lost this session): resume, and if that is not answered
    /// either, start a fresh session.
    Stalled,
    /// The daemon answered a request with `device_hello_required`.
    HelloRequired,
}

/// pump drains `outbound_rx` into the e2ee channel, forwards decrypted
/// inbound messages to `inbound_tx`, and drives the keepalive probe and the
/// dead-stream rule, until one side ends — mirrors
/// `api.ServeTransport(ctx, dc)` (Go) blocking for the life of one data
/// session.
async fn pump(
    data: &mut DataChannel,
    outbound_rx: &mut mpsc::Receiver<Vec<u8>>,
    inbound_tx: &broadcast::Sender<Vec<u8>>,
    timing: &Timing,
) -> PumpEnd {
    let liveness = data.transport_mut().liveness();
    let start = Instant::now();
    liveness.lock().unwrap().start_stream(start);
    // Application-level liveness (see `Timing::request_stall`): the relay's
    // keepalive proves the RELAY is alive, not that the daemon still holds
    // this session.
    let mut last_app_recv = start;
    let mut pending_since: Option<Instant> = None;
    loop {
        let wake = [
            liveness.lock().unwrap().next_wake(),
            Some(match pending_since {
                Some(at) => at + timing.request_stall,
                None => last_app_recv + timing.idle_probe,
            }),
        ]
        .into_iter()
        .flatten()
        .min();
        let sleep = async {
            match wake {
                Some(at) => tokio::time::sleep_until(at).await,
                None => std::future::pending().await,
            }
        };
        tokio::select! {
            msg = outbound_rx.recv() => {
                match msg {
                    Some(msg) => {
                        if pending_since.is_none() && is_request(&msg) {
                            pending_since = Some(Instant::now());
                        }
                        if let Err(e) = data.send(&msg).await {
                            eprintln!("smind desktop: relay send: {e}");
                            return PumpEnd::Ended;
                        }
                    }
                    None => return PumpEnd::Stopped,
                }
            }
            received = data.receive() => {
                match received {
                    Ok(msg) => {
                        last_app_recv = Instant::now();
                        pending_since = None;
                        if is_hello_required(&msg) {
                            return PumpEnd::HelloRequired;
                        }
                        if message_id(&msg).as_deref() == Some(IDLE_PROBE_ID) {
                            continue; // our own liveness request's answer
                        }
                        let _ = inbound_tx.send(msg);
                    }
                    Err(e) => {
                        eprintln!("smind desktop: relay receive: {e}");
                        return PumpEnd::Ended;
                    }
                }
            }
            _ = sleep => {
                let now = Instant::now();
                let action = liveness.lock().unwrap().poll(now);
                match action {
                    Action::Nothing => {}
                    Action::SendProbe => {
                        if let Err(e) = data.transport_mut().send_probe().await {
                            eprintln!("smind desktop: relay keepalive: {e}");
                            return PumpEnd::Ended;
                        }
                    }
                    Action::Dead => return PumpEnd::Dead,
                }
                match pending_since {
                    Some(at) if now >= at + timing.request_stall => return PumpEnd::Stalled,
                    None if now >= last_app_recv + timing.idle_probe => {
                        let probe = serde_json::json!({"id": IDLE_PROBE_ID, "method": "workspace.list"}).to_string();
                        if let Err(e) = data.send(probe.as_bytes()).await {
                            eprintln!("smind desktop: relay liveness request: {e}");
                            return PumpEnd::Ended;
                        }
                        pending_since = Some(now);
                    }
                    _ => {}
                }
            }
        }
    }
}

const IDLE_PROBE_ID: &str = "relay-idle-probe";

/// is_request reports whether `msg` is a request that expects a response
/// (an `id` and a `method`), as opposed to a fire-and-forget message such as
/// `task.cancel`.
fn is_request(msg: &[u8]) -> bool {
    serde_json::from_slice::<serde_json::Value>(msg)
        .map(|v| v.get("id").is_some() && v.get("method").is_some())
        .unwrap_or(false)
}

/// sleep_or_stop waits for `d`, but returns `None` if every `RelayHandle`
/// sender has been dropped (nothing left to serve), so a backing-off loop
/// doesn't keep a task alive forever after its last consumer is gone. A
/// message arriving mid-backoff only wakes the wait early.
async fn sleep_or_stop(outbound_rx: &mut mpsc::Receiver<Vec<u8>>, d: Duration) -> Option<()> {
    tokio::select! {
        _ = tokio::time::sleep(d) => Some(()),
        msg = outbound_rx.recv() => msg.map(|_| ()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn derives_native_address_from_grpc_web_offer_address() {
        assert_eq!(
            derive_native_grpc_address("https://relay.example.test:7401").unwrap(),
            "relay.example.test:7400"
        );
    }

    #[test]
    fn derive_native_address_rejects_missing_port() {
        assert!(derive_native_grpc_address("https://relay.example.test").is_err());
    }

    #[test]
    fn derive_native_address_rejects_port_zero() {
        assert!(derive_native_grpc_address("https://relay.example.test:0").is_err());
    }

    #[test]
    fn derive_native_address_rejects_garbage() {
        assert!(derive_native_grpc_address("not a url").is_err());
    }

    #[test]
    fn hex_round_trip() {
        let b = [0xde, 0xad, 0xbe, 0xef];
        assert_eq!(hex_decode(&hex_encode(&b)).unwrap(), b);
    }
}
