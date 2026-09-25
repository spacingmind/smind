//! Native gRPC transport to the relay (ADR-0007 amendment, 2026-09-25):
//! TLS with pairing-time fingerprint pinning (ADR-0011), the admission
//! challenge-response, and the reconnect/resume loop that keeps a
//! `Channel<DataFrameTransport>` (see `crate::relay::channel`) alive
//! across transport-level drops, mirroring `internal/relay/bridge.Run`
//! (Go) — but from the "device" (mobile role) side, since the desktop
//! app is a relay-paired device like a phone, not the daemon itself.

use std::sync::Arc;
use std::time::{Duration, Instant};

use tokio::sync::{broadcast, mpsc, watch};
use tokio_stream::wrappers::ReceiverStream;
use tonic::metadata::MetadataValue;
use tonic::transport::{Channel as GrpcChannel, Endpoint, Uri};
use tonic::Request;

use crate::backoff::Backoff;
use crate::relay::admission::{self, Transcript};
use crate::relay::channel::{Channel as E2eeChannel, FrameTransport};
use crate::relay::crypto::{decode_frame, encode_frame, FrameError, KeyPair, Role};
use crate::relay::pairing_store::RelayPairing;
use crate::relay::relaypb::relay_client::RelayClient as GrpcRelayClient;
use crate::relay::relaypb::{AdmitChallengeRequest, AdmitRequest, Direction, Frame};

/// The single hardcoded (session, device) pair the daemon's own relay
/// bridge (`internal/relay/bridge.go`) serves per workspace today — a
/// documented milestone-1 simplification, not something this client can
/// change unilaterally (it isn't a wire-protocol constant, just a value
/// both ends must agree on to land on the same relay route). The desktop
/// client MUST use these same values to reach the daemon's bridge at
/// all. See the plan's "Known limitation" section: a mobile device and a
/// desktop relay connection to the same workspace cannot both be
/// attached at once as a result.
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
    let tls_config = rustls::ClientConfig::builder_with_provider(provider)
        .with_protocol_versions(&[&rustls::version::TLS13])
        .map_err(|e| format!("relay client: tls config: {e}"))?
        .dangerous()
        .with_custom_certificate_verifier(verifier)
        .with_no_client_auth();
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
/// via `admission::compute_hmac`'s canonical transcript. Returns the
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

    let hmac = admission::compute_hmac(
        &admission::hash_secret(secret),
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
        hmac: hmac.to_vec(),
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
}

impl std::fmt::Display for DataTransportError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            DataTransportError::Closed => write!(f, "relay client: data stream closed"),
            DataTransportError::Status(s) => write!(f, "relay client: data stream: {s}"),
            DataTransportError::Frame(e) => write!(f, "{e}"),
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
}

impl DataFrameTransport {
    async fn write_raw(&mut self, payload: Vec<u8>) -> Result<(), DataTransportError> {
        let frame = Frame {
            workspace_id: self.workspace_id.clone(),
            session_id: self.session_id.clone(),
            device_id: self.device_id.clone(),
            direction: self.direction as i32,
            sequence: self.seq,
            payload,
        };
        self.seq += 1;
        self.tx
            .send(frame)
            .await
            .map_err(|_| DataTransportError::Closed)
    }

    /// register sends the routing frame the relay requires as a stream's
    /// first frame (payload empty — the e2ee handshake follows as
    /// ordinary frames), mirroring `frameConn.register` (Go).
    async fn register(&mut self) -> Result<(), DataTransportError> {
        self.write_raw(Vec::new()).await
    }

    /// reopen swaps the underlying stream after a transport drop,
    /// keeping `seq` (relay buffer ordering) and re-registering the
    /// route — mirrors `frameConn.reopen` (Go). The e2ee `Channel`
    /// wrapping this transport is untouched by a caller doing this (see
    /// `Channel::transport_mut`), so its session/counters survive.
    pub async fn reopen(
        &mut self,
        tx: mpsc::Sender<Frame>,
        rx: tonic::Streaming<Frame>,
    ) -> Result<(), DataTransportError> {
        self.tx = tx;
        self.rx = rx;
        self.register().await
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
        let frame = self
            .rx
            .next()
            .await
            .ok_or(DataTransportError::Closed)?
            .map_err(DataTransportError::Status)?;
        let (typ, payload) = decode_frame(&frame.payload).map_err(DataTransportError::Frame)?;
        Ok((typ, payload.to_vec()))
    }
}

/// open_data opens a fresh `OpenData` stream under `admission_id` for
/// (workspace, session, device), returning the raw `(sender, receiver)`
/// pair for the caller to either wrap fresh (`open_data`) or feed into
/// an existing `DataFrameTransport::reopen` (a transport-level resume,
/// which needs a fresh stream but must NOT touch the workspace/session/
/// device/sequence state already stored on the transport it's resuming).
/// Exposed as a public building block for anyone driving the handshake/
/// reconnect steps manually (e.g. the mandatory harness interop test)
/// rather than through the full `spawn`-managed reconnect loop.
pub async fn open_data_stream(
    grpc: &mut GrpcRelayClient<GrpcChannel>,
    admission_id: &str,
) -> Result<(mpsc::Sender<Frame>, tonic::Streaming<Frame>), String> {
    let (tx, rx) = mpsc::channel::<Frame>(FRAME_CHANNEL_CAPACITY);
    let mut req = Request::new(ReceiverStream::new(rx));
    req.metadata_mut().insert(
        ADMISSION_METADATA_KEY,
        MetadataValue::try_from(admission_id)
            .map_err(|e| format!("relay client: admission metadata: {e}"))?,
    );
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
    let (tx, rx) = open_data_stream(grpc, admission_id).await?;
    let mut transport = DataFrameTransport {
        tx,
        rx,
        workspace_id: workspace_id.to_string(),
        session_id: session_id.as_bytes().to_vec(),
        device_id: device_id.to_string(),
        direction,
        seq: 0,
    };
    transport.register().await.map_err(|e| e.to_string())?;
    Ok(transport)
}

// --- The reconnect loop: connect/admit/handshake, resume-first on drop ---

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Status {
    Connecting,
    Connected,
    Reconnecting,
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
pub fn spawn(pairing: RelayPairing) -> (RelayHandle, tokio::task::JoinHandle<()>) {
    let (outbound_tx, outbound_rx) = mpsc::channel(FRAME_CHANNEL_CAPACITY);
    let (inbound_tx, _) = broadcast::channel(FRAME_CHANNEL_CAPACITY);
    let (status_tx, status_rx) = watch::channel(Status::Connecting);
    let inbound_tx_task = inbound_tx.clone();
    let task = tokio::spawn(run(pairing, outbound_rx, inbound_tx_task, status_tx));
    (
        RelayHandle {
            outbound_tx,
            inbound_tx,
            status_rx,
        },
        task,
    )
}

/// run is the daemon-side-mirrored reconnect loop: dial + admit, resume
/// the existing e2ee session on a transport-level reconnect whenever
/// possible (falling back to a fresh handshake only when Resume itself
/// fails or there's no prior session), and pump JSON-RPC bytes between
/// the e2ee channel and the outbound/inbound queues — mirrors
/// `bridge.Run` (Go)'s structure exactly, including its
/// stable-connection-resets-backoff anti-flap rule.
///
/// `grpc`'s initial `None` and `need_connect`'s `false` branch are true
/// dead stores by the same structural reason they are in `bridge.Run`
/// (Go): both are unconditionally overwritten before the loop's next
/// read, on every iteration after the first, by design (kept 1:1 with
/// Go rather than restructured away).
#[allow(unused_assignments)]
async fn run(
    pairing: RelayPairing,
    mut outbound_rx: mpsc::Receiver<Vec<u8>>,
    inbound_tx: broadcast::Sender<Vec<u8>>,
    status_tx: watch::Sender<Status>,
) {
    let kp = KeyPair::generate();
    // The offer's daemon id IS the admission daemon_key_id (see
    // cmd/smind/relay.go's cmdRelayOffer: DaemonID = bridge.DaemonKeyID(kp),
    // the same string the daemon itself presents when admitting) -- this
    // client (playing the device/mobile role) presents that same value,
    // never re-derives its own.
    let daemon_key_id = pairing.daemon_id.clone();
    let mut backoff = Backoff::new(Duration::from_secs(1), MAX_RECONNECT_BACKOFF);
    let mut data: Option<E2eeChannel<DataFrameTransport>> = None;
    let mut grpc: Option<GrpcRelayClient<GrpcChannel>> = None;
    let mut need_connect = true;

    loop {
        if need_connect {
            let _ = status_tx.send(Status::Reconnecting);
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
            let (new_grpc, admission_id) = match dial_and_admit.await {
                Ok(v) => v,
                Err(e) => {
                    eprintln!("smind desktop: relay connect: {e}");
                    if sleep_or_stop(&mut outbound_rx, backoff.next())
                        .await
                        .is_none()
                    {
                        return;
                    }
                    continue;
                }
            };
            grpc = Some(new_grpc);

            let mut resumed = false;
            if let Some(existing) = data.as_mut() {
                // A fresh stream, not a fresh transport: `reopen` sends
                // its own registration frame, so building a throwaway
                // `DataFrameTransport` here (via `open_data`) would send
                // it twice.
                match open_data_stream(grpc.as_mut().unwrap(), &admission_id).await {
                    Ok((tx, rx)) => {
                        if existing.transport_mut().reopen(tx, rx).await.is_ok() {
                            resumed = true;
                        }
                    }
                    Err(e) => eprintln!("smind desktop: relay resume: open data: {e}"),
                }
            }
            if !resumed {
                match open_data(
                    grpc.as_mut().unwrap(),
                    &admission_id,
                    &pairing.workspace_id,
                    DEFAULT_SESSION_ID,
                    DEFAULT_DEVICE_ID,
                )
                .await
                {
                    Ok(transport) => {
                        let mut channel = E2eeChannel::new(transport, Role::Mobile);
                        let handshake = tokio::time::timeout(
                            HANDSHAKE_TIMEOUT,
                            channel.handshake(&kp, Some(&pairing.daemon_public_key)),
                        )
                        .await;
                        match handshake {
                            Ok(Ok(())) => data = Some(channel),
                            Ok(Err(e)) => {
                                eprintln!("smind desktop: relay handshake: {e}");
                                if sleep_or_stop(&mut outbound_rx, backoff.next())
                                    .await
                                    .is_none()
                                {
                                    return;
                                }
                                continue;
                            }
                            Err(_) => {
                                eprintln!("smind desktop: relay handshake: timed out");
                                if sleep_or_stop(&mut outbound_rx, backoff.next())
                                    .await
                                    .is_none()
                                {
                                    return;
                                }
                                continue;
                            }
                        }
                    }
                    Err(e) => {
                        eprintln!("smind desktop: relay handshake: open data: {e}");
                        if sleep_or_stop(&mut outbound_rx, backoff.next())
                            .await
                            .is_none()
                        {
                            return;
                        }
                        continue;
                    }
                }
            }
            need_connect = false;
            let _ = status_tx.send(Status::Connected);
        }

        let connected_at = Instant::now();
        pump(data.as_mut().unwrap(), &mut outbound_rx, &inbound_tx).await;

        if connected_at.elapsed() >= MIN_STABLE_CONNECTION {
            backoff.reset();
        } else if sleep_or_stop(&mut outbound_rx, backoff.next())
            .await
            .is_none()
        {
            return;
        }
        need_connect = true;
    }
}

/// pump drains `outbound_rx` into the e2ee channel and forwards decrypted
/// inbound messages to `inbound_tx`, until either side errors — mirrors
/// `api.ServeTransport(ctx, dc)` (Go) blocking for the life of one data
/// session.
async fn pump(
    data: &mut E2eeChannel<DataFrameTransport>,
    outbound_rx: &mut mpsc::Receiver<Vec<u8>>,
    inbound_tx: &broadcast::Sender<Vec<u8>>,
) {
    loop {
        tokio::select! {
            msg = outbound_rx.recv() => {
                match msg {
                    Some(msg) => {
                        if let Err(e) = data.send(&msg).await {
                            eprintln!("smind desktop: relay send: {e}");
                            return;
                        }
                    }
                    None => return, // every RelayHandle sender dropped
                }
            }
            received = data.receive() => {
                match received {
                    Ok(msg) => {
                        let _ = inbound_tx.send(msg);
                    }
                    Err(e) => {
                        eprintln!("smind desktop: relay receive: {e}");
                        return;
                    }
                }
            }
        }
    }
}

/// sleep_or_stop waits for `d`, but returns early with `None` if every
/// `RelayHandle` sender has already been dropped (nothing left to serve),
/// so a backing-off loop doesn't keep a task alive forever after its
/// last consumer is gone.
async fn sleep_or_stop(outbound_rx: &mut mpsc::Receiver<Vec<u8>>, d: Duration) -> Option<()> {
    tokio::select! {
        _ = tokio::time::sleep(d) => Some(()),
        _ = outbound_rx.recv() => Some(()), // a send arriving mid-backoff is fine, just wakes us early
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
