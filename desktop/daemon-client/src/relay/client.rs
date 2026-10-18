//! Native gRPC transport to the relay (ADR-0007 amendment, 2026-09-25):
//! TLS with pairing-time fingerprint pinning (ADR-0011) or, for a relay with
//! a real certificate, system-roots CA verification (ADR-0031 P12c), the admission
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
use crate::relay::password::PasswordCell;
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

// --- TLS: pin the relay's certificate fingerprint (ADR-0011), or CA mode ---

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

/// How the relay's certificate is verified (mirrors Go's
/// `internal/relay/client.Dial`, ADR-0031 P12c).
#[derive(Debug, PartialEq, Eq)]
enum TlsMode {
    /// Empty fingerprint: the full chain and the host name against the
    /// system roots, as for any HTTPS site. Nothing is skipped.
    Ca,
    /// A 64-hex fingerprint: pin the leaf (SHA-256 of the DER end-entity)
    /// and trust no CA.
    Pin([u8; 32]),
}

/// parse_tls_mode is the three-way decision. A malformed fingerprint is an
/// error, NEVER a fallback to CA mode.
fn parse_tls_mode(fingerprint_hex: &str) -> Result<TlsMode, String> {
    if fingerprint_hex.is_empty() {
        return Ok(TlsMode::Ca);
    }
    let bad = || format!("relay client: bad fingerprint {fingerprint_hex:?}");
    // Exactly 64 ASCII hex digits (checked first: hex_decode slices by byte).
    if fingerprint_hex.len() != 64 || !fingerprint_hex.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err(bad());
    }
    let fingerprint: [u8; 32] = hex_decode(fingerprint_hex).map_err(|_| bad())?.try_into().map_err(|_| bad())?;
    Ok(TlsMode::Pin(fingerprint))
}

/// native_roots loads the operating system's trust store (the same roots the
/// Go client uses, so a relay whose certificate comes from a CA the user
/// installed in their OS verifies). Deliberately not webpki-roots (it would
/// ignore the OS store) and not rustls-platform-verifier (Apple/Windows
/// policy engines add rules, such as macOS's private-CA lifetime limits, that
/// would make behaviour platform-dependent). The cost: no OS revocation
/// checking (no CRL/OCSP). Loaded per dial attempt; reconnects are rare.
fn native_roots() -> Result<rustls::RootCertStore, String> {
    let loaded = rustls_native_certs::load_native_certs();
    let mut roots = rustls::RootCertStore::empty();
    let (added, _ignored) = roots.add_parsable_certificates(loaded.certs);
    if added == 0 {
        return Err("relay client: no system root certificates could be loaded".to_string());
    }
    Ok(roots)
}

/// tls_config builds the client TLS configuration for `mode`. In CA mode
/// `roots` replaces the system roots (tests with a private CA); `None` loads
/// them. In pin mode `roots` is ignored: no CA is trusted.
fn tls_config(mode: TlsMode, roots: Option<Arc<rustls::RootCertStore>>) -> Result<rustls::ClientConfig, String> {
    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let builder = rustls::ClientConfig::builder_with_provider(provider.clone())
        .with_protocol_versions(&[&rustls::version::TLS13])
        .map_err(|e| format!("relay client: tls config: {e}"))?;
    let mut tls_config = match mode {
        TlsMode::Pin(fingerprint) => builder
            .dangerous()
            .with_custom_certificate_verifier(Arc::new(PinnedFingerprintVerifier { fingerprint, provider }))
            .with_no_client_auth(),
        TlsMode::Ca => {
            let roots = match roots {
                Some(roots) => roots,
                None => Arc::new(native_roots()?),
            };
            let verifier = rustls::client::WebPkiServerVerifier::builder_with_provider(roots, provider)
                .build()
                .map_err(|e| format!("relay client: tls verifier: {e}"))?;
            builder.with_webpki_verifier(verifier).with_no_client_auth()
        }
    };
    // grpc-go's server-side transport credentials require ALPN "h2" to
    // pick HTTP/2 -- without it the TLS handshake completes but the
    // server closes the connection as soon as we speak HTTP/2 on it
    // (observed as a broken-pipe error on the very first RPC).
    tls_config.alpn_protocols = vec![b"h2".to_vec()];
    Ok(tls_config)
}

/// server_name is the TLS server name for `native_addr` (host:port): a DNS
/// name, or an IP literal (verified against the certificate's IP SANs).
fn server_name(native_addr: &str) -> Result<rustls::pki_types::ServerName<'static>, String> {
    let host = native_addr.rsplit_once(':').map(|(h, _)| h).unwrap_or(native_addr);
    let host = host.strip_prefix('[').and_then(|h| h.strip_suffix(']')).unwrap_or(host);
    Ok(rustls::pki_types::ServerName::try_from(host.to_string())
        .map_err(|e| format!("relay client: server name from {native_addr:?}: {e}"))?
        .to_owned())
}

/// dial connects to the relay at `native_addr` (host:port). With a pairing's
/// `relay_fingerprint` (hex SHA-256 of the DER leaf) it pins that leaf and
/// trusts no CA (ADR-0011); with an EMPTY fingerprint it is CA mode: the
/// chain and host name are verified against the system roots over TLS 1.3
/// (ADR-0031 P12c). A malformed fingerprint is an error, never CA mode. This
/// mirrors `internal/relay/client.Dial` (Go), but via a custom rustls
/// connector rather than tonic's built-in TLS feature (see the ADR-0007
/// amendment: this is a plain native-gRPC dial, no grpc-web).
pub async fn dial(native_addr: &str, fingerprint_hex: &str) -> Result<GrpcChannel, String> {
    dial_with_roots(native_addr, fingerprint_hex, None).await
}

/// dial_with_roots is `dial` with CA mode's roots replaceable (a private CA in
/// tests); `None` loads the system roots.
pub async fn dial_with_roots(
    native_addr: &str,
    fingerprint_hex: &str,
    roots: Option<Arc<rustls::RootCertStore>>,
) -> Result<GrpcChannel, String> {
    let tls_config = tls_config(parse_tls_mode(fingerprint_hex)?, roots)?;
    let connector = tokio_rustls::TlsConnector::from(Arc::new(tls_config));
    let server_name = server_name(native_addr)?;

    let dial_addr = native_addr.to_string();
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
        .map_err(|e| format!("relay client: connect: {}", error_chain(&e)))
}

/// error_chain renders `e` with its sources ("transport error: ... invalid
/// peer certificate: UnknownIssuer"): tonic's own Display stops at
/// "transport error", which hides why a certificate was refused.
fn error_chain(e: &dyn std::error::Error) -> String {
    let mut out = e.to_string();
    let mut source = e.source();
    while let Some(s) = source {
        let text = s.to_string();
        if !out.contains(&text) {
            out.push_str(": ");
            out.push_str(&text);
        }
        source = s.source();
    }
    out
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

/// Inbound is the receiving half of an `OpenData` stream. A tonic client call
/// resolves only once the server has sent response HEADERS, and the relay
/// sends none until it has a frame for this side — so awaiting the call would
/// block a resume that has nothing buffered until the peer happens to speak
/// (grpc-go, which the Go client uses, returns the stream at once). The call
/// therefore runs in the background and the stream is picked up on the first
/// read; the send half works from the start.
pub enum Inbound {
    Ready(tonic::Streaming<Frame>),
    Pending(PendingCall),
    /// The call failed; further reads report the stream closed.
    Failed,
}

/// PendingCall is a background `OpenData` call; dropping it aborts the call,
/// so a dead path does not keep a connection and a task alive.
pub struct PendingCall {
    resolved: tokio::sync::oneshot::Receiver<Result<tonic::Streaming<Frame>, tonic::Status>>,
    task: tokio::task::JoinHandle<()>,
}

impl Drop for PendingCall {
    fn drop(&mut self) {
        self.task.abort();
    }
}

impl Inbound {
    async fn next_frame(&mut self) -> Result<Frame, DataTransportError> {
        use tonic::codegen::tokio_stream::StreamExt;
        loop {
            match self {
                Inbound::Ready(stream) => {
                    return stream
                        .next()
                        .await
                        .ok_or(DataTransportError::Closed)?
                        .map_err(DataTransportError::Status);
                }
                Inbound::Pending(call) => {
                    let result = (&mut call.resolved).await;
                    // Whatever the outcome, the call is finished: replace the
                    // pending state before returning, so a re-poll can never
                    // await a consumed receiver.
                    *self = Inbound::Failed;
                    let stream = result
                        .map_err(|_| DataTransportError::Closed)?
                        .map_err(DataTransportError::Status)?;
                    *self = Inbound::Ready(stream);
                }
                Inbound::Failed => return Err(DataTransportError::Closed),
            }
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
    rx: Inbound,
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
    /// token, which the relay echoes back. It is not counted in `seq` (the
    /// relay never buffers or forwards it).
    pub async fn send_probe(&mut self) -> Result<(), DataTransportError> {
        let token = self.liveness.lock().unwrap().token();
        let mut frame = self.build_frame(token, Vec::new());
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
        let (tx, rx) = open_data_stream(grpc, admission_id, registration).await?;
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
        loop {
            let frame = self.rx.next_frame().await?;
            // Liveness first: ANY frame from the relay proves the stream is
            // alive, and a keepalive frame carrying our token is an echo.
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
) -> Result<(mpsc::Sender<Frame>, Inbound), DataTransportError> {
    let (tx, rx) = mpsc::channel::<Frame>(FRAME_CHANNEL_CAPACITY);
    let mut req = Request::new(ReceiverStream::new(rx));
    req.metadata_mut().insert(
        ADMISSION_METADATA_KEY,
        MetadataValue::try_from(admission_id)
            .map_err(|e| DataTransportError::Open(format!("relay client: admission metadata: {e}")))?,
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
        .map_err(|_| {
            DataTransportError::Open("relay client: send first frame: channel closed immediately".to_string())
        })?;
    // The call is NOT awaited here (see `Inbound`): a status the relay refuses
    // the stream with (PermissionDenied for a revoked device) surfaces on the
    // first read instead.
    let (resolved_tx, resolved_rx) = tokio::sync::oneshot::channel();
    let mut grpc = grpc.clone();
    let task = tokio::spawn(async move {
        let result = grpc.open_data(req).await.map(|r| r.into_inner());
        let _ = resolved_tx.send(result);
    });
    Ok((
        tx,
        Inbound::Pending(PendingCall {
            resolved: resolved_rx,
            task,
        }),
    ))
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
) -> Result<DataFrameTransport, DataTransportError> {
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
        liveness::random_token(),
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
    /// The daemon has a password (P13c) and this connection has none: the user
    /// must enter it. The client does not retry by itself; entering a password
    /// reconnects at once.
    PasswordRequired,
    /// The daemon rejected the password (it has been forgotten): enter it again.
    IncorrectPassword,
    /// Too many wrong passwords: the daemon asks the client to wait. After
    /// `retry_after_secs` the state becomes `PasswordRequired`.
    PasswordLocked {
        retry_after_secs: u64,
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
            Status::PasswordRequired => "passwordRequired",
            Status::IncorrectPassword => "incorrectPassword",
            Status::PasswordLocked { .. } => "passwordLocked",
            Status::Refused { .. } => "refused",
        }
    }
}

/// Timing knobs of the relay loop; the defaults are the documented protocol
/// values (30 s probe, 75 s dead stream, jittered backoff), tests shrink them.
#[derive(Clone)]
pub struct Timing {
    pub liveness: LivenessCfg,
    /// Bound on dial + TLS + admission: a path that swallows traffic would
    /// otherwise hang a reconnect forever.
    pub connect_timeout: Duration,
    pub handshake_timeout: Duration,
    pub hello_timeout: Duration,
    /// How long the daemon gets to send its `relay.device_challenge` after the
    /// handshake. A daemon from before hello v2 never does: it waits for a
    /// v1 hello this client no longer speaks.
    pub challenge_timeout: Duration,
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
            connect_timeout: Duration::from_secs(20),
            handshake_timeout: HANDSHAKE_TIMEOUT,
            hello_timeout: Duration::from_secs(10),
            challenge_timeout: Duration::from_secs(5),
            resume_probe_timeout: Duration::from_secs(15),
            min_stable_connection: MIN_STABLE_CONNECTION,
            // Long enough that a slow request (a silent tool run in a task) is
            // not mistaken for a lost session; the probe that follows a stall
            // is answered concurrently by a healthy daemon anyway.
            request_stall: Duration::from_secs(30),
            idle_probe: Duration::from_secs(90),
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
    if getrandom::fill(&mut b).is_err() {
        return d; // no randomness: an unjittered delay beats a panic
    }
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
    /// Why the device key could not be loaded. With this set the client does
    /// not connect at all and says so (`Status::Refused`), rather than quietly
    /// speaking the single-device protocol a current daemon refuses.
    pub key_error: Option<String>,
    /// The daemon password for `device_hello` (P13c), shared with the
    /// connection's `RelayHandle`. Empty until the user enters one.
    pub password: PasswordCell,
}

impl RelayOptions {
    pub fn identity(key: Arc<DeviceKey>) -> Self {
        Self {
            device_key: Some(key),
            label: default_device_label(),
            timing: Timing::default(),
            key_error: None,
            password: PasswordCell::default(),
        }
    }

    /// key_unavailable is for a connection whose device key cannot be read.
    pub fn key_unavailable(message: String) -> Self {
        Self {
            device_key: None,
            label: String::new(),
            timing: Timing::default(),
            key_error: Some(message),
            password: PasswordCell::default(),
        }
    }

    pub fn legacy_only() -> Self {
        Self {
            device_key: None,
            label: String::new(),
            timing: Timing::default(),
            key_error: None,
            password: PasswordCell::default(),
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
    stats: Arc<RelayStats>,
    password: PasswordCell,
}

/// Counters of how sessions were (re)established, for tests and diagnostics.
#[derive(Default)]
pub struct RelayStats {
    /// Fresh E2EE sessions established (a new handshake and, on the identity
    /// route, a new hello).
    pub fresh_sessions: std::sync::atomic::AtomicU64,
    /// Transport drops survived by resuming the existing session.
    pub resumes: std::sync::atomic::AtomicU64,
    /// Whether the relay has echoed one of this connection's keepalive
    /// probes on the current stream (so the 75 s dead-stream rule is armed).
    pub keepalive_confirmed: std::sync::atomic::AtomicBool,
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

    pub fn stats(&self) -> &RelayStats {
        &self.stats
    }

    /// Gives the connection the daemon password (P13c) and wakes it: a loop
    /// waiting for one reconnects at once. Held in memory only.
    pub fn set_password(&self, password: String) {
        self.password.set(password);
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
            stats: Arc::default(),
            password: PasswordCell::default(),
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
    let stats = Arc::new(RelayStats::default());
    let password = options.password.clone();
    let task = tokio::spawn(run(
        pairing,
        options,
        outbound_rx,
        inbound_tx_task,
        status_tx,
        stats.clone(),
    ));
    (
        RelayHandle {
            outbound_tx,
            inbound_tx,
            status_rx,
            stats,
            password,
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

/// The longest wait the client will honour from a daemon's `retry_after`.
const MAX_RETRY_AFTER_SECS: u64 = 3600;

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
    /// The daemon's password check refused the hello (P13c): `password_required`,
    /// `incorrect_password` or `too_many_attempts` (with its `retry_after`).
    Password {
        code: String,
        retry_after: Option<u64>,
        /// The `PasswordCell` generation this hello carried: an `incorrect_password`
        /// answer forgets that password and nothing newer.
        sent_generation: u64,
    },
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
    .map_err(|e| match e {
        // The relay itself refuses a revoked device's stream.
        DataTransportError::Status(s) if is_revoked_by_relay(&s) => {
            Failure::RelayDenied(s.message().to_string())
        }
        e => Failure::Retry(format!("open data: {e}")),
    })?;
    {
        let liveness = transport.liveness();
        let mut l = liveness.lock().unwrap();
        l.set_cfg(options.timing.liveness);
        l.start_stream(Instant::now());
    }
    let kp = KeyPair::generate();
    let mut channel = E2eeChannel::new(transport, Role::Mobile);
    let handshake = tokio::time::timeout(
        options.timing.handshake_timeout,
        channel.handshake(&kp, Some(&pairing.daemon_public_key)),
    )
    .await;
    match handshake {
        Ok(Ok(())) => {}
        Ok(Err(ChannelError::Transport(DataTransportError::Status(s)))) if is_revoked_by_relay(&s) => {
            return Err(Failure::RelayDenied(s.message().to_string()));
        }
        Ok(Err(e)) => return Err(Failure::Retry(format!("handshake: {e}"))),
        Err(_) if mode == RouteMode::Identity => return Err(Failure::HandshakeTimeout),
        Err(_) => return Err(Failure::Retry("handshake: timed out".into())),
    }

    let (RouteMode::Identity, Some(key)) = (mode, options.device_key.as_ref()) else {
        return Ok(channel);
    };
    identify_device(
        &mut channel,
        key,
        &options.label,
        &pairing.daemon_public_key,
        &kp.public(),
        &session_id,
        &options.timing,
        &options.password,
    )
    .await?;
    Ok(channel)
}

/// identify_device runs the hello v2 exchange on an established channel:
/// wait for the daemon's challenge, then send the hello bound to it. The
/// hello is sent only after a well-formed challenge, so a daemon that sends
/// none (one from before v2) never receives anything from this client.
// Eight inputs that are all distinct facts of one hello (key, label, both public keys, session, timing,
// password); a struct would only rename them.
#[allow(clippy::too_many_arguments)]
async fn identify_device<T: FrameTransport>(
    channel: &mut E2eeChannel<T>,
    key: &DeviceKey,
    label: &str,
    daemon_pub: &[u8; 32],
    mobile_pub: &[u8; 32],
    session_id: &str,
    timing: &Timing,
    password: &PasswordCell,
) -> Result<(), Failure> {
    let challenge = await_challenge(channel, timing.challenge_timeout).await?;
    let hello = key.hello(label, daemon_pub, mobile_pub, session_id, &challenge);
    let request_id = "relay-hello-1";
    // The password (when the user entered one this run) goes into the encrypted
    // request only; the copy zeroizes when this function returns.
    let (pw, sent_generation) = password.snapshot();
    let request = device::hello_request_with_password(request_id, &hello, pw.as_ref().map(|p| p.as_str()));
    drop(pw);
    channel
        .send(request.as_bytes())
        .await
        .map_err(|e| Failure::Retry(format!("hello: {e}")))?;
    let outcome = tokio::time::timeout(timing.hello_timeout, async {
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
        HelloOutcome::Approved { .. } => Ok(()),
        HelloOutcome::MethodNotFound => Err(Failure::NoIdentitySupport),
        HelloOutcome::Refused { ref code, .. } if code == "device_hello_unsupported" => {
            Err(Failure::NoIdentitySupport)
        }
        HelloOutcome::Refused { code, retry_after, .. }
            if matches!(code.as_str(), "password_required" | "incorrect_password" | "too_many_attempts") =>
        {
            Err(Failure::Password { code, retry_after, sent_generation })
        }
        HelloOutcome::Refused { code, message, .. } => Err(Failure::Refused { code, message }),
    }
}

/// await_challenge waits for the daemon's per-session `relay.device_challenge`
/// (hello v2). Silence means a daemon that predates it: reported as
/// `daemon_outdated`, never answered with the retired v1 hello.
async fn await_challenge<T: FrameTransport>(
    channel: &mut E2eeChannel<T>,
    timeout: Duration,
) -> Result<[u8; device::CHALLENGE_LEN], Failure> {
    tokio::time::timeout(timeout, async {
        loop {
            let msg = channel
                .receive()
                .await
                .map_err(|e| Failure::Retry(format!("challenge: {e}")))?;
            let Ok(text) = String::from_utf8(msg) else {
                continue;
            };
            match device::parse_challenge(&text) {
                Some(Ok(challenge)) => return Ok(challenge),
                Some(Err(reason)) => {
                    // A challenge of another version: this client and the
                    // daemon disagree on the hello; say so, don't loop.
                    return Err(Failure::Refused {
                        code: "daemon_outdated".into(),
                        message: format!(
                            "The daemon and this app use different device hellos ({reason}); update the older one."
                        ),
                    });
                }
                None => {}
            }
        }
    })
    .await
    .unwrap_or_else(|_| {
        Err(Failure::Refused {
            code: "daemon_outdated".into(),
            message: "The daemon did not send a device challenge: update it (smind v0.12.0 and older cannot talk to this app version)."
                .into(),
        })
    })
}

/// is_revoked_by_relay: the relay refuses a revoked device's stream with
/// PermissionDenied "relay: device revoked". Other PermissionDenied statuses
/// (a host-binding mismatch after a rotated secret, say) are not a revocation.
fn is_revoked_by_relay(s: &tonic::Status) -> bool {
    s.code() == tonic::Code::PermissionDenied && s.message().contains("revoked")
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

/// MAX_UNSERVED_FLIPS bounds how often the client switches between the
/// identity route and the single-device route without ever being served in
/// between; past it the cause is shown instead of flipping again.
const MAX_UNSERVED_FLIPS: u32 = 2;

/// ModeMachine decides which route a FRESH session asks for. Moves between the
/// two routes are rare, bounded and never free:
///
/// * the identity route is the default and is tried first, on every start;
/// * it flips to the single-device route when the daemon says it does not do
///   device identity, or when nobody answers the handshake on the identity
///   route (a daemon from before device identity never opens it);
/// * the single-device route flips back to identity when it is refused
///   (`device_hello_required`) or fails before ever being served — it is only
///   kept while it has proven to work;
/// * after `MAX_UNSERVED_FLIPS` flips without a served session in between the
///   machine is *stuck*: the caller shows the cause and retries slowly, instead
///   of ping-ponging between two daemon answers that contradict each other.
#[derive(Debug)]
struct ModeMachine {
    mode: RouteMode,
    has_key: bool,
    legacy_proven: bool,
    unserved_flips: u32,
}

#[derive(Debug, PartialEq, Eq)]
enum Transition {
    Flipped(RouteMode),
    Stuck { code: &'static str, message: &'static str },
}

impl ModeMachine {
    fn new(has_key: bool) -> Self {
        Self {
            mode: if has_key { RouteMode::Identity } else { RouteMode::Legacy },
            has_key,
            legacy_proven: false,
            unserved_flips: 0,
        }
    }

    fn mode(&self) -> RouteMode {
        self.mode
    }

    /// served: a session on the current route got an answer from the daemon.
    fn served(&mut self) {
        self.unserved_flips = 0;
        if self.mode == RouteMode::Legacy {
            self.legacy_proven = true;
        }
    }

    fn flip(&mut self, to: RouteMode, code: &'static str, message: &'static str) -> Transition {
        if !self.has_key || self.unserved_flips >= MAX_UNSERVED_FLIPS {
            self.mode = if self.has_key { RouteMode::Identity } else { RouteMode::Legacy };
            return Transition::Stuck { code, message };
        }
        self.unserved_flips += 1;
        self.mode = to;
        self.legacy_proven = false;
        Transition::Flipped(to)
    }

    /// The identity route's handshake got no answer.
    fn identity_unanswered(&mut self) -> Transition {
        self.flip(
            RouteMode::Legacy,
            "daemon_not_answering",
            "no daemon answered on this device's route",
        )
    }

    /// The daemon does not do device identity (unsupported / method not found).
    fn identity_unsupported(&mut self) -> Transition {
        self.flip(
            RouteMode::Legacy,
            "device_hello_unsupported",
            "the daemon does not track relay devices",
        )
    }

    /// The single-device route was answered with `device_hello_required`.
    fn hello_required(&mut self) -> Transition {
        self.flip(
            RouteMode::Identity,
            "device_hello_required",
            "the daemon requires a device identity but cannot identify this device",
        )
    }

    /// A fresh session on the single-device route failed or ended without ever
    /// being served: unless it has proven to work, go back to the identity
    /// route. `None` = stay.
    fn legacy_unserved(&mut self) -> Option<Transition> {
        if self.mode == RouteMode::Legacy && !self.legacy_proven && self.has_key {
            Some(self.flip(
                RouteMode::Identity,
                "daemon_not_answering",
                "no daemon answered on the single-device route",
            ))
        } else {
            None
        }
    }
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
    stats: Arc<RelayStats>,
) {
    use std::sync::atomic::Ordering;
    let timing = options.timing.clone();
    if let Some(message) = options.key_error.clone() {
        // No device key: say so, and do not connect (a keyless client would
        // only be refused by a current daemon).
        eprintln!("smind desktop: relay: device key unavailable: {message}");
        let _ = status_tx.send(Status::Refused {
            code: "device_key_unavailable".into(),
            message,
        });
        while sleep_or_stop(&mut outbound_rx, Duration::from_secs(60))
            .await
            .is_some()
        {}
        return;
    }
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
    let mut modes = ModeMachine::new(options.device_key.is_some());
    let mut waiting_attempts: u32 = 0;
    loop {
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
        let dial_and_admit = async {
            tokio::time::timeout(timing.connect_timeout, dial_and_admit)
                .await
                .unwrap_or_else(|_| Err("timed out connecting to the relay".to_string()))
        };
        let (mut grpc, admission_id) = match dial_and_admit.await {
            Ok(v) => v,
            Err(e) => {
                eprintln!("smind desktop: relay connect: {e}");
                // An unreachable relay is the news, not an old refusal.
                if !matches!(*status_tx.borrow(), Status::Connecting | Status::Reconnecting) {
                    let _ = status_tx.send(Status::Reconnecting);
                }
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
                        stats.resumes.fetch_add(1, Ordering::SeqCst);
                    } else {
                        eprintln!(
                            "smind desktop: relay resume: not answered; starting a fresh session"
                        );
                    }
                }
                Err(e) => eprintln!("smind desktop: relay resume: open data: {e}"),
            }
            if !resumed {
                data = None;
            }
        }
        if !resumed {
            match establish(&mut grpc, &admission_id, &pairing, &options, modes.mode()).await {
                Ok(channel) => {
                    data = Some(channel);
                    stats.fresh_sessions.fetch_add(1, Ordering::SeqCst);
                }
                Err(Failure::Password { code, retry_after, sent_generation }) => {
                    // The daemon's password check (P13c). A missing or rejected password
                    // needs the user, so neither retries by itself, and a rejected one is
                    // never re-sent (it would only count against the daemon's limit).
                    eprintln!("smind desktop: relay: the daemon's password check answered {code}");
                    match code.as_str() {
                        "too_many_attempts" => {
                            // The daemon looked at the rate limit, not the password: nothing
                            // was rejected, so the password (if any) is kept and tried again
                            // after the wait. (Discarding it would let anyone who can hold the
                            // daemon's global limit make every desktop forget a correct one.)
                            // The wait is the daemon's, clamped to a sane range.
                            let wait = retry_after.unwrap_or(60).clamp(5, MAX_RETRY_AFTER_SECS);
                            let _ = status_tx.send(Status::PasswordLocked { retry_after_secs: wait });
                            if sleep_or_stop(&mut outbound_rx, Duration::from_secs(wait)).await.is_none() {
                                return;
                            }
                            if options.password.is_set() {
                                let _ = status_tx.send(Status::Connecting);
                                continue;
                            }
                            let _ = status_tx.send(Status::PasswordRequired);
                        }
                        "incorrect_password" => {
                            options.password.clear_if(sent_generation);
                            let _ = status_tx.send(Status::IncorrectPassword);
                        }
                        _ => {
                            let _ = status_tx.send(Status::PasswordRequired);
                        }
                    }
                    if wait_for_password(&mut outbound_rx, &options.password).await.is_none() {
                        return;
                    }
                    waiting_attempts = 0;
                    backoff.reset();
                    let _ = status_tx.send(Status::Connecting);
                    continue;
                }
                Err(failure) => {
                    let (status, delay) = match failure {
                        // Handled by the arm above; kept so the match stays exhaustive.
                        Failure::Password { .. } => continue,
                        Failure::Retry(e) => {
                            eprintln!("smind desktop: relay connect: {e}");
                            if let Some(t) = modes.legacy_unserved() {
                                (transition_status(&t), transition_delay(&t, &timing, &mut backoff))
                            } else {
                                (None, backoff.next())
                            }
                        }
                        Failure::HandshakeTimeout => {
                            eprintln!("smind desktop: relay: no daemon answered on the device route");
                            let t = modes.identity_unanswered();
                            (transition_status(&t), transition_delay(&t, &timing, &mut backoff))
                        }
                        Failure::NoIdentitySupport => {
                            eprintln!("smind desktop: relay: the daemon does not support device identity");
                            let t = modes.identity_unsupported();
                            (transition_status(&t), transition_delay(&t, &timing, &mut backoff))
                        }
                        Failure::RelayDenied(msg) => {
                            eprintln!("smind desktop: relay refused this device: {msg}");
                            (
                                Some(Status::Revoked {
                                    device_id: device_id.clone(),
                                }),
                                timing.revoked_retry,
                            )
                        }
                        Failure::Refused { code, message } => match code.as_str() {
                            "awaiting_approval" => {
                                waiting_attempts += 1;
                                // Retry briskly at first (approval lands within
                                // seconds), then settle to a polite rate: an
                                // unattended desktop must not hammer the relay
                                // and the daemon's registry for days.
                                let delay = if waiting_attempts <= 12 {
                                    timing.awaiting_retry
                                } else {
                                    timing.awaiting_retry * 6
                                };
                                (
                                    Some(Status::AwaitingApproval {
                                        device_id: device_id.clone(),
                                    }),
                                    delay,
                                )
                            }
                            "revoked" => (
                                Some(Status::Revoked {
                                    device_id: device_id.clone(),
                                }),
                                timing.revoked_retry,
                            ),
                            _ => (Some(Status::Refused { code, message }), timing.refused_retry),
                        },
                    };
                    if let Some(status) = status {
                        let _ = status_tx.send(status);
                    }
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
        waiting_attempts = 0;
        let _ = status_tx.send(Status::Connected);

        let connected_at = Instant::now();
        let mut served = false;
        let channel = data.as_mut().unwrap();
        let end = pump(
            channel,
            &mut outbound_rx,
            &inbound_tx,
            &timing,
            &mut served,
            &stats,
        )
        .await;
        if served {
            modes.served();
        }
        match end {
            PumpEnd::Stopped => return,
            PumpEnd::HelloRequired => {
                // The single-device route was refused: identify instead.
                data = None;
                let t = modes.hello_required();
                if let Some(status) = transition_status(&t) {
                    let _ = status_tx.send(status);
                    let delay = transition_delay(&t, &timing, &mut backoff);
                    if sleep_or_stop(&mut outbound_rx, (timing.jitter)(delay))
                        .await
                        .is_none()
                    {
                        return;
                    }
                    continue;
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
        if !served && modes.mode() == RouteMode::Legacy {
            // A single-device session that never got an answer: do not stay on
            // that route on a hunch.
            if let Some(t) = modes.legacy_unserved() {
                data = None;
                if let Some(status) = transition_status(&t) {
                    let _ = status_tx.send(status);
                }
            }
        }

        // The backoff resets only after a connection that stayed up long
        // enough to count as stable: one that keeps ending within moments must
        // escalate, not retry at the base rate forever.
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

/// transition_status: a flip is silent; being stuck shows why.
fn transition_status(t: &Transition) -> Option<Status> {
    match t {
        Transition::Flipped(_) => None,
        Transition::Stuck { code, message } => Some(Status::Refused {
            code: (*code).to_string(),
            message: (*message).to_string(),
        }),
    }
}

/// transition_delay: a flip waits the ordinary backoff (never zero); being
/// stuck retries at the slow refusal rate.
fn transition_delay(t: &Transition, timing: &Timing, backoff: &mut Backoff) -> Duration {
    match t {
        Transition::Flipped(_) => backoff.next(),
        Transition::Stuck { .. } => timing.refused_retry,
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
    served: &mut bool,
    stats: &RelayStats,
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
                        *served = true;
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
                stats.keepalive_confirmed.store(
                    liveness.lock().unwrap().support() == liveness::Support::Yes,
                    std::sync::atomic::Ordering::SeqCst,
                );
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

/// sleep_or_stop waits for `d` and returns `None` if every `RelayHandle`
/// sender has been dropped (nothing left to serve), so a backing-off loop
/// doesn't keep a task alive forever after its last consumer is gone. It never
/// consumes a queued message and is never cut short by one: a retry delay is
/// the delay, however many requests the UI sends meanwhile.
/// wait_for_password parks the loop until the user enters a password (a
/// `PasswordCell::set`), returning `None` if the connection is dropped first.
/// No connection is held open meanwhile.
async fn wait_for_password(outbound_rx: &mut mpsc::Receiver<Vec<u8>>, password: &PasswordCell) -> Option<()> {
    loop {
        if outbound_rx.is_closed() {
            return None;
        }
        tokio::select! {
            () = password.changed() => {
                if password.is_set() {
                    return Some(());
                }
            }
            () = tokio::time::sleep(Duration::from_millis(250)) => {}
        }
    }
}

async fn sleep_or_stop(outbound_rx: &mut mpsc::Receiver<Vec<u8>>, d: Duration) -> Option<()> {
    let end = Instant::now() + d;
    loop {
        if outbound_rx.is_closed() {
            return None;
        }
        let left = end.saturating_duration_since(Instant::now());
        if left.is_zero() {
            return Some(());
        }
        tokio::time::sleep(left.min(Duration::from_millis(250))).await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::relay::channel::test_pipe::pipe;

    // --- hello v2: the daemon's challenge ----------------------------------

    async fn established_pair() -> (
        E2eeChannel<crate::relay::channel::test_pipe::PipeEnd>,
        E2eeChannel<crate::relay::channel::test_pipe::PipeEnd>,
        KeyPair,
        KeyPair,
    ) {
        let (a, b) = pipe();
        let mut daemon = E2eeChannel::new(a, Role::Daemon);
        let mut mobile = E2eeChannel::new(b, Role::Mobile);
        let (dkp, mkp) = (KeyPair::from_seed([1; 32]), KeyPair::from_seed([2; 32]));
        let (r1, r2) = tokio::join!(daemon.handshake(&dkp, None), mobile.handshake(&mkp, None));
        r1.unwrap();
        r2.unwrap();
        (daemon, mobile, dkp, mkp)
    }

    /// A daemon from before hello v2 sends nothing after the handshake: the
    /// client reports `daemon_outdated` once `challenge_timeout` passes (a
    /// paused clock, so no real 5 s) and sends NOTHING — in particular never
    /// the retired v1 hello.
    #[tokio::test(start_paused = true)]
    async fn a_silent_daemon_is_outdated_and_is_never_sent_a_hello() {
        let (mut daemon, mut mobile, dkp, mkp) = established_pair().await;
        let key = DeviceKey::from_seed([7; 32]);
        let timing = Timing::default();
        assert_eq!(timing.challenge_timeout, Duration::from_secs(5));
        let started = Instant::now();
        let err = identify_device(
            &mut mobile, &key, "laptop", &dkp.public(), &mkp.public(), "sess", &timing, &PasswordCell::default(),
        )
        .await
        .unwrap_err();
        match err {
            Failure::Refused { code, .. } => assert_eq!(code, "daemon_outdated"),
            other => panic!("expected daemon_outdated, got {other:?}"),
        }
        assert_eq!(started.elapsed(), timing.challenge_timeout, "waited exactly the challenge window");
        let got = tokio::time::timeout(Duration::from_secs(60), daemon.receive()).await;
        assert!(got.is_err(), "the silent daemon must not have been sent anything");
    }

    /// A challenge of another version is `daemon_outdated` at once (no wait),
    /// and also gets no hello.
    #[tokio::test(start_paused = true)]
    async fn a_challenge_of_another_version_is_outdated_without_a_hello() {
        let (mut daemon, mut mobile, dkp, mkp) = established_pair().await;
        daemon
            .send(br#"{"event":"relay.device_challenge","params":{"v":3,"challenge":""}}"#)
            .await
            .unwrap();
        let key = DeviceKey::from_seed([7; 32]);
        let timing = Timing::default();
        let started = Instant::now();
        let err = identify_device(
            &mut mobile, &key, "laptop", &dkp.public(), &mkp.public(), "sess", &timing, &PasswordCell::default(),
        )
        .await
        .unwrap_err();
        assert!(matches!(err, Failure::Refused { ref code, .. } if code == "daemon_outdated"), "{err:?}");
        assert!(started.elapsed() < timing.challenge_timeout);
        assert!(tokio::time::timeout(Duration::from_secs(60), daemon.receive()).await.is_err());
    }

    /// The happy path sends a v2 hello signed over this session id and the
    /// challenge the daemon chose.
    #[tokio::test(start_paused = true)]
    async fn a_challenge_gets_a_v2_hello_bound_to_session_and_challenge() {
        let (mut daemon, mut mobile, dkp, mkp) = established_pair().await;
        let challenge = [9u8; device::CHALLENGE_LEN];
        let encoded = {
            use base64::Engine as _;
            base64::engine::general_purpose::STANDARD.encode(challenge)
        };
        let event = serde_json::json!({"event": device::CHALLENGE_EVENT,
            "params": {"v": 2, "challenge": encoded}})
        .to_string();
        daemon.send(event.as_bytes()).await.unwrap();
        let key = DeviceKey::from_seed([7; 32]);
        let timing = Timing::default();
        let (dpub, mpub) = (dkp.public(), mkp.public());
        let no_password = PasswordCell::default();
        let (res, got) = tokio::join!(
            identify_device(&mut mobile, &key, "laptop", &dpub, &mpub, "sess-1", &timing, &no_password),
            async {
                let msg = daemon.receive().await.unwrap();
                let v: serde_json::Value = serde_json::from_slice(&msg).unwrap();
                let reply = serde_json::json!({"id": v["id"], "result": {"v": 2, "device_id": key.device_id(), "state": "approved"}})
                    .to_string();
                daemon.send(reply.as_bytes()).await.unwrap();
                v
            }
        );
        res.unwrap();
        assert_eq!(got["method"], "relay.device_hello");
        assert_eq!(got["params"]["v"], 2);
        let expect = key.hello("laptop", &dkp.public(), &mkp.public(), "sess-1", &challenge);
        assert_eq!(got["params"], serde_json::to_value(&expect).unwrap());
    }

    /// P13c: with a password in the cell the hello carries it in params (only
    /// there), and nothing else about the request changes.
    #[tokio::test(start_paused = true)]
    async fn the_hello_carries_the_password_when_the_user_entered_one() {
        let (mut daemon, mut mobile, dkp, mkp) = established_pair().await;
        let challenge = [9u8; device::CHALLENGE_LEN];
        let encoded = {
            use base64::Engine as _;
            base64::engine::general_purpose::STANDARD.encode(challenge)
        };
        let event = serde_json::json!({"event": device::CHALLENGE_EVENT,
            "params": {"v": 2, "challenge": encoded}})
        .to_string();
        daemon.send(event.as_bytes()).await.unwrap();
        let key = DeviceKey::from_seed([7; 32]);
        let timing = Timing::default();
        let (dpub, mpub) = (dkp.public(), mkp.public());
        let password = PasswordCell::default();
        password.set("a-long-daemon-password".into());
        let (res, got) = tokio::join!(
            identify_device(&mut mobile, &key, "laptop", &dpub, &mpub, "sess-1", &timing, &password),
            async {
                let msg = daemon.receive().await.unwrap();
                let v: serde_json::Value = serde_json::from_slice(&msg).unwrap();
                let reply = serde_json::json!({"id": v["id"], "error": {"message": "wrong", "code": "incorrect_password"}})
                    .to_string();
                daemon.send(reply.as_bytes()).await.unwrap();
                v
            }
        );
        match res {
            Err(Failure::Password { code, retry_after: None, .. }) => assert_eq!(code, "incorrect_password"),
            other => panic!("expected a password refusal, got {other:?}"),
        }
        assert_eq!(got["params"]["password"], "a-long-daemon-password");
        let expect = key.hello("laptop", &dkp.public(), &mkp.public(), "sess-1", &challenge);
        let mut params = got["params"].clone();
        params.as_object_mut().unwrap().remove("password");
        assert_eq!(params, serde_json::to_value(&expect).unwrap(), "the signed hello is unchanged");
    }

    #[tokio::test(start_paused = true)]
    async fn a_parked_loop_wakes_when_a_password_arrives_and_stops_when_dropped() {
        let (tx, mut rx) = mpsc::channel::<Vec<u8>>(1);
        let cell = PasswordCell::default();
        let waiter = {
            let cell = cell.clone();
            tokio::spawn(async move {
                let woke = wait_for_password(&mut rx, &cell).await;
                (woke, rx)
            })
        };
        tokio::time::sleep(Duration::from_secs(30)).await;
        assert!(!waiter.is_finished(), "must not return without a password");
        cell.set("a-password-123456".into());
        let (woke, rx) = waiter.await.unwrap();
        assert_eq!(woke, Some(()));
        // An empty set (a clear) does not wake; dropping the connection stops it.
        let mut rx = rx;
        let cell2 = PasswordCell::default();
        let waiter = tokio::spawn(async move { wait_for_password(&mut rx, &cell2).await });
        drop(tx);
        assert_eq!(waiter.await.unwrap(), None);
    }

    #[test]
    fn password_states_have_stable_names() {
        assert_eq!(Status::PasswordRequired.state_name(), "passwordRequired");
        assert_eq!(Status::IncorrectPassword.state_name(), "incorrectPassword");
        assert_eq!(Status::PasswordLocked { retry_after_secs: 8 }.state_name(), "passwordLocked");
    }

    // --- ModeMachine -----------------------------------------------------

    #[test]
    fn identity_route_first_then_the_single_device_route_when_unsupported() {
        let mut m = ModeMachine::new(true);
        assert_eq!(m.mode(), RouteMode::Identity);
        assert_eq!(m.identity_unsupported(), Transition::Flipped(RouteMode::Legacy));
        assert_eq!(m.mode(), RouteMode::Legacy);
    }

    #[test]
    fn contradicting_daemon_answers_do_not_ping_pong() {
        // "no device identity here" then "device identity required" then again:
        // a daemon whose registry is unreadable while legacy clients are off.
        let mut m = ModeMachine::new(true);
        assert_eq!(m.identity_unsupported(), Transition::Flipped(RouteMode::Legacy));
        assert_eq!(m.hello_required(), Transition::Flipped(RouteMode::Identity));
        let t = m.identity_unsupported();
        assert!(matches!(t, Transition::Stuck { code: "device_hello_unsupported", .. }), "{t:?}");
        assert_eq!(m.mode(), RouteMode::Identity, "stuck stays on the identity route");
        // ...and stays stuck until a session is actually served.
        assert!(matches!(m.hello_required(), Transition::Stuck { .. }));
        m.served();
        assert_eq!(m.identity_unsupported(), Transition::Flipped(RouteMode::Legacy));
    }

    #[test]
    fn the_single_device_route_is_kept_only_while_it_has_proven_to_work() {
        let mut m = ModeMachine::new(true);
        m.identity_unanswered(); // an old daemon: nobody answers on the identity route
        assert_eq!(m.mode(), RouteMode::Legacy);
        m.served(); // the single-device route works
        assert_eq!(m.legacy_unserved(), None, "a proven route is kept across a blip");
        assert_eq!(m.mode(), RouteMode::Legacy);

        // A daemon outage must not leave the client on the single-device route
        // for good: an unproven legacy session that fails goes back to identity.
        let mut m = ModeMachine::new(true);
        m.identity_unanswered();
        assert_eq!(m.legacy_unserved(), Some(Transition::Flipped(RouteMode::Identity)));
        assert_eq!(m.mode(), RouteMode::Identity);
    }

    #[test]
    fn a_client_without_a_key_never_switches_routes() {
        let mut m = ModeMachine::new(false);
        assert_eq!(m.mode(), RouteMode::Legacy);
        assert!(matches!(m.hello_required(), Transition::Stuck { .. }));
        assert_eq!(m.legacy_unserved(), None);
    }

    #[test]
    fn a_flip_waits_the_ordinary_backoff_never_zero() {
        let timing = Timing::default();
        let mut backoff = Backoff::new(timing.backoff_base, timing.backoff_max);
        let d = transition_delay(&Transition::Flipped(RouteMode::Legacy), &timing, &mut backoff);
        assert!(d >= timing.backoff_base);
        let stuck = Transition::Stuck { code: "x", message: "y" };
        assert_eq!(transition_delay(&stuck, &timing, &mut backoff), timing.refused_retry);
    }

    // --- run loop pieces ---------------------------------------------------

    fn sample_pairing() -> RelayPairing {
        RelayPairing {
            daemon_id: "d".into(),
            daemon_public_key: [7; 32],
            relay_native_addr: "127.0.0.1:1".into(), // nothing listens: any dial would fail
            relay_fingerprint: "00".repeat(32),
            workspace_id: "ws".into(),
            admission_secret: vec![1; 32],
        }
    }

    #[tokio::test]
    async fn an_unreadable_device_key_is_a_visible_status_and_nothing_connects() {
        let (handle, task) = spawn(
            sample_pairing(),
            RelayOptions::key_unavailable("cannot read it".into()),
        );
        let mut rx = handle.status();
        tokio::time::timeout(Duration::from_secs(5), async {
            while !matches!(&*rx.borrow(), Status::Refused { .. }) {
                rx.changed().await.unwrap();
            }
        })
        .await
        .expect("the key problem must reach the status");
        match &*rx.borrow() {
            Status::Refused { code, message } => {
                assert_eq!(code, "device_key_unavailable");
                assert_eq!(message, "cannot read it");
            }
            other => panic!("{other:?}"),
        }
        // It stays that way: it never tries to connect (a dial would turn the
        // status into Reconnecting).
        tokio::time::sleep(Duration::from_millis(300)).await;
        assert!(matches!(&*handle.status().borrow(), Status::Refused { .. }));
        task.abort();
    }

    #[tokio::test(start_paused = true)]
    async fn a_retry_delay_is_never_cut_short_nor_a_message_swallowed() {
        let (tx, mut rx) = mpsc::channel::<Vec<u8>>(8);
        for i in 0..3u8 {
            tx.send(vec![i]).await.unwrap();
        }
        let start = Instant::now();
        assert_eq!(sleep_or_stop(&mut rx, Duration::from_secs(5)).await, Some(()));
        assert!(start.elapsed() >= Duration::from_secs(5), "cut short by queued messages");
        for i in 0..3u8 {
            assert_eq!(rx.try_recv().unwrap(), vec![i], "a queued message was swallowed");
        }
        drop(tx);
        assert_eq!(
            sleep_or_stop(&mut rx, Duration::from_secs(5)).await,
            None,
            "no consumers left: stop"
        );
    }

    #[test]
    fn only_a_revocation_is_read_as_revoked() {
        let revoked = tonic::Status::permission_denied("relay: device revoked");
        let binding = tonic::Status::permission_denied("relay: relay host binding mismatch");
        let other = tonic::Status::unavailable("relay: device revoked");
        assert!(is_revoked_by_relay(&revoked));
        assert!(!is_revoked_by_relay(&binding), "a rotated host secret is not a revoke");
        assert!(!is_revoked_by_relay(&other));
    }

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

#[cfg(test)]
mod tls_tests {
    use super::*;
    use rcgen::{BasicConstraints, CertificateParams, IsCa, Issuer, KeyPair, SanType};
    use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer, ServerName};
    use std::net::{IpAddr, Ipv4Addr};

    /// A private CA that signs leaf certificates for the tests.
    struct TestCa {
        params: CertificateParams,
        cert: rcgen::Certificate,
        key: KeyPair,
    }

    struct Leaf {
        der: CertificateDer<'static>,
        key: PrivateKeyDer<'static>,
    }

    impl TestCa {
        fn new() -> TestCa {
            Self::named("test relay CA")
        }

        fn named(cn: &str) -> TestCa {
            let mut params = CertificateParams::new(Vec::<String>::new()).unwrap();
            params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
            params.distinguished_name.push(rcgen::DnType::CommonName, cn);
            let key = KeyPair::generate().unwrap();
            let cert = params.self_signed(&key).unwrap();
            TestCa { params, cert, key }
        }

        fn roots(&self) -> Arc<rustls::RootCertStore> {
            let mut roots = rustls::RootCertStore::empty();
            roots.add(self.cert.der().clone()).unwrap();
            Arc::new(roots)
        }

        fn leaf_with(&self, sans: Vec<SanType>, tweak: impl FnOnce(&mut CertificateParams)) -> Leaf {
            let mut params = CertificateParams::new(Vec::<String>::new()).unwrap();
            params.subject_alt_names = sans;
            tweak(&mut params);
            let key = KeyPair::generate().unwrap();
            let issuer = Issuer::from_params(&self.params, &self.key);
            let cert = params.signed_by(&key, &issuer).unwrap();
            Leaf { der: cert.der().clone(), key: PrivateKeyDer::Pkcs8(PrivatePkcs8KeyDer::from(key.serialize_der())) }
        }
    }

    fn self_signed(sans: Vec<SanType>) -> Leaf {
        let mut params = CertificateParams::new(Vec::<String>::new()).unwrap();
        params.subject_alt_names = sans;
        let key = KeyPair::generate().unwrap();
        let cert = params.self_signed(&key).unwrap();
        Leaf { der: cert.der().clone(), key: PrivateKeyDer::Pkcs8(PrivatePkcs8KeyDer::from(key.serialize_der())) }
    }

    fn dns(name: &str) -> SanType {
        SanType::DnsName(name.try_into().unwrap())
    }

    fn fingerprint(der: &CertificateDer<'_>) -> String {
        use sha2::{Digest, Sha256};
        Sha256::digest(der.as_ref()).iter().map(|b| format!("{b:02x}")).collect()
    }

    /// Serves TLS 1.3 (ALPN h2) with `leaf` on a free port; completes handshakes only.
    async fn tls_server(leaf: &Leaf) -> std::net::SocketAddr {
        let provider = Arc::new(rustls::crypto::ring::default_provider());
        let mut config = rustls::ServerConfig::builder_with_provider(provider)
            .with_protocol_versions(&[&rustls::version::TLS13])
            .unwrap()
            .with_no_client_auth()
            .with_single_cert(vec![leaf.der.clone()], leaf.key.clone_key())
            .unwrap();
        config.alpn_protocols = vec![b"h2".to_vec()];
        let acceptor = tokio_rustls::TlsAcceptor::from(Arc::new(config));
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            while let Ok((tcp, _)) = listener.accept().await {
                let acceptor = acceptor.clone();
                tokio::spawn(async move {
                    if let Ok(mut tls) = acceptor.accept(tcp).await {
                        use tokio::io::AsyncReadExt;
                        let mut buf = [0u8; 1];
                        let _ = tls.read(&mut buf).await;
                    }
                });
            }
        });
        addr
    }

    /// handshake dials `addr` presenting `name`, with the config `tls_config`
    /// builds for `fingerprint` and `roots`.
    async fn handshake(addr: std::net::SocketAddr, name: ServerName<'static>, fingerprint: &str, roots: Option<Arc<rustls::RootCertStore>>) -> Result<(), String> {
        let config = tls_config(parse_tls_mode(fingerprint)?, roots)?;
        let connector = tokio_rustls::TlsConnector::from(Arc::new(config));
        let tcp = tokio::net::TcpStream::connect(addr).await.map_err(|e| e.to_string())?;
        let mut tls = connector.connect(name, tcp).await.map_err(|e| e.to_string())?;
        // TLS 1.3 reports a rejected certificate on the first read after the
        // handshake as an alert; a verified one just waits for the server.
        let _ = &mut tls;
        Ok(())
    }

    fn name(n: &str) -> ServerName<'static> {
        ServerName::try_from(n.to_string()).unwrap()
    }

    #[tokio::test]
    async fn ca_mode_accepts_a_valid_chain_for_the_host() {
        let ca = TestCa::new();
        let leaf = ca.leaf_with(vec![dns("relay.test")], |_| {});
        let addr = tls_server(&leaf).await;
        handshake(addr, name("relay.test"), "", Some(ca.roots())).await.expect("a chain from the trusted CA for the right host");
    }

    #[tokio::test]
    async fn ca_mode_rejects_wrong_host() {
        let ca = TestCa::new();
        let leaf = ca.leaf_with(vec![dns("relay.test")], |_| {});
        let addr = tls_server(&leaf).await;
        let err = handshake(addr, name("other.test"), "", Some(ca.roots())).await.unwrap_err();
        assert!(err.to_lowercase().contains("not valid for") || err.contains("NotValidForName"), "{err}");
    }

    #[tokio::test]
    async fn ca_mode_rejects_expired_certificate() {
        let ca = TestCa::new();
        let leaf = ca.leaf_with(vec![dns("relay.test")], |p| {
            p.not_before = rcgen::date_time_ymd(2000, 1, 1);
            p.not_after = rcgen::date_time_ymd(2001, 1, 1);
        });
        let addr = tls_server(&leaf).await;
        let err = handshake(addr, name("relay.test"), "", Some(ca.roots())).await.unwrap_err();
        assert!(err.to_lowercase().contains("expired"), "{err}");
    }

    #[tokio::test]
    async fn ca_mode_rejects_self_signed_without_a_pin() {
        let leaf = self_signed(vec![dns("relay.test")]);
        let addr = tls_server(&leaf).await;
        let ca = TestCa::named("some other CA"); // trusts some CA, just not this certificate
        let err = handshake(addr, name("relay.test"), "", Some(ca.roots())).await.unwrap_err();
        assert!(err.to_lowercase().contains("unknownissuer") || err.to_lowercase().contains("unknown"), "{err}");
    }

    #[tokio::test]
    async fn ca_mode_rejects_a_chain_from_an_unknown_ca() {
        let signing_ca = TestCa::new();
        let trusted_ca = TestCa::named("some other CA");
        let leaf = signing_ca.leaf_with(vec![dns("relay.test")], |_| {});
        let addr = tls_server(&leaf).await;
        let err = handshake(addr, name("relay.test"), "", Some(trusted_ca.roots())).await.unwrap_err();
        assert!(err.to_lowercase().contains("unknown"), "{err}");
    }

    #[tokio::test]
    async fn ca_mode_accepts_ip_san_for_an_ip_target() {
        let ca = TestCa::new();
        let ip = IpAddr::V4(Ipv4Addr::LOCALHOST);
        let leaf = ca.leaf_with(vec![SanType::IpAddress(ip)], |_| {});
        let addr = tls_server(&leaf).await;
        // The target is an IP literal: the server name is an IP address.
        let n = server_name(&addr.to_string()).unwrap();
        assert!(matches!(n, ServerName::IpAddress(_)));
        handshake(addr, n, "", Some(ca.roots())).await.expect("IP SAN matches the IP target");
        // A DNS-only certificate does not satisfy an IP target.
        let dns_only = ca.leaf_with(vec![dns("relay.test")], |_| {});
        let addr2 = tls_server(&dns_only).await;
        assert!(handshake(addr2, server_name(&addr2.to_string()).unwrap(), "", Some(ca.roots())).await.is_err());
    }

    #[tokio::test]
    async fn pin_mode_still_accepts_the_pinned_leaf_and_rejects_another() {
        let leaf = self_signed(vec![dns("relay.test")]);
        let addr = tls_server(&leaf).await;
        let pin = fingerprint(&leaf.der);
        handshake(addr, name("relay.test"), &pin, None).await.expect("the pinned leaf");
        // The pin does not look at names either (as before).
        handshake(addr, name("anything.test"), &pin, None).await.expect("pin mode ignores names");
        let other = self_signed(vec![dns("relay.test")]);
        let err = handshake(addr, name("relay.test"), &fingerprint(&other.der), None).await.unwrap_err();
        assert!(err.contains("does not match pin"), "{err}");
    }

    #[tokio::test]
    async fn pin_mode_ignores_roots() {
        let ca = TestCa::new();
        let leaf = ca.leaf_with(vec![dns("relay.test")], |_| {});
        let addr = tls_server(&leaf).await;
        let wrong_pin = "ab".repeat(32);
        let err = handshake(addr, name("relay.test"), &wrong_pin, Some(ca.roots())).await.unwrap_err();
        assert!(err.contains("does not match pin"), "a trusted CA must not rescue a wrong pin: {err}");
        // And the right pin works with no roots at all.
        handshake(addr, name("relay.test"), &fingerprint(&leaf.der), None).await.unwrap();
    }

    #[tokio::test]
    async fn malformed_fingerprint_is_an_error_not_ca_mode() {
        let ca = TestCa::new();
        let leaf = ca.leaf_with(vec![dns("relay.test")], |_| {});
        let addr = tls_server(&leaf).await;
        let valid = fingerprint(&leaf.der);
        for bad in [
            &valid[..63],                    // odd length
            &format!("{}zz", &valid[..62]),  // non-hex
            &valid[..62],                    // 31 bytes
            &format!("{valid}00"),           // 33 bytes
            " ",
            "é".repeat(32).as_str(),         // multi-byte, even length
        ] {
            let err = handshake(addr, name("relay.test"), bad, Some(ca.roots())).await.unwrap_err();
            assert!(err.contains("bad fingerprint"), "{bad:?} -> {err}");
            // The public entry point refuses too, before dialing anything.
            let dialed = dial_with_roots(&addr.to_string(), bad, Some(ca.roots())).await.unwrap_err();
            assert!(dialed.contains("bad fingerprint"), "{dialed}");
        }
        assert_eq!(parse_tls_mode("").unwrap(), TlsMode::Ca);
    }
}
