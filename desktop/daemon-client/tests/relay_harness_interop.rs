//! Mandatory interop test (per the desktop relay transport plan): drives
//! the real Rust relay client against the real Go relay + daemon bridge
//! test harness (`internal/relay/bridge/harness`), not a stub -- pairing
//! URL parsing, admission, the E2EE handshake (pinned to the offer's
//! daemon public key), a JSON-RPC round trip, and a transport-level
//! disconnect/resume that confirms the relay's store-and-forward buffer
//! (ADR-0007 (a)) actually delivers what was in flight during the gap.
//!
//! Requires a `go` toolchain on PATH (to `go run` the harness) and
//! network access to bind loopback TCP sockets; both are assumed
//! available wherever this crate's tests run today (no CI job currently
//! runs `cargo test` for this crate -- see the plan's Validation).

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use smind_daemon_client::relay::channel::Channel;
use smind_daemon_client::relay::client as relay_client;
use smind_daemon_client::relay::crypto::{KeyPair, Role};
use smind_daemon_client::relay::pairing::Offer;
use smind_daemon_client::relay::pairing_store::RelayPairing;
use smind_daemon_client::relay::relaypb::relay_client::RelayClient as GrpcRelayClient;

struct Harness {
    child: Child,
    pairing_url: String,
    native_addr: String,
}

impl Drop for Harness {
    fn drop(&mut self) {
        kill_group(&mut self.child);
    }
}

/// kill_group kills `child` and everything else in its process group.
/// `go run` execs the compiled binary as a *child* process rather than
/// replacing itself, so killing just `child` (the `go run` process)
/// leaves the actual harness binary running as an orphan.
/// `start_harness` puts the child in its own process group
/// (`process_group(0)`); killing the whole group (`kill -9 -<pid>`, the
/// negative-pid "process group" convention) takes both out at once.
fn kill_group(child: &mut Child) {
    #[cfg(unix)]
    {
        let _ = Command::new("kill")
            .arg("-9")
            .arg(format!("-{}", child.id()))
            .status();
    }
    let _ = child.kill();
    let _ = child.wait();
}

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../..")
}

/// start_harness runs `go run ./internal/relay/bridge/harness` and reads
/// its stdout until both the "READY <pairingUrl>" line (the harness's
/// own documented contract, also watched by
/// `mobile/src/relay/__tests__/*.node.test.ts`) and the "NATIVE <addr>"
/// line (added specifically for this test -- see the harness's own doc
/// comment on why the native gRPC address can't be derived from the
/// printed pairing URL the way production code does) have arrived.
fn start_harness() -> Harness {
    #[allow(unused_mut)]
    let mut cmd = Command::new("go");
    cmd.args(["run", "./internal/relay/bridge/harness"])
        .current_dir(repo_root())
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit());
    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        cmd.process_group(0); // own group, so Drop can kill go run + the binary it spawns together
    }
    let mut child = cmd
        .spawn()
        .expect("start relay harness: is a `go` toolchain on PATH?");

    let stdout = child.stdout.take().expect("harness stdout is piped");
    let mut reader = BufReader::new(stdout);
    let mut pairing_url = None;
    let mut native_addr = None;
    let deadline = Instant::now() + Duration::from_secs(30);
    let mut line = String::new();
    while (pairing_url.is_none() || native_addr.is_none()) && Instant::now() < deadline {
        line.clear();
        let n = reader.read_line(&mut line).expect("read harness stdout");
        if n == 0 {
            break;
        }
        let trimmed = line.trim();
        if let Some(rest) = trimmed.strip_prefix("READY ") {
            pairing_url = Some(rest.to_string());
        } else if let Some(rest) = trimmed.strip_prefix("NATIVE ") {
            native_addr = Some(rest.to_string());
        }
    }

    let pairing_url = pairing_url.unwrap_or_else(|| {
        kill_group(&mut child);
        panic!("harness never printed a READY line within 30s");
    });
    let native_addr = native_addr.unwrap_or_else(|| {
        kill_group(&mut child);
        panic!("harness never printed a NATIVE line within 30s");
    });
    Harness {
        child,
        pairing_url,
        native_addr,
    }
}

/// rpc builds a minimal JSON-RPC request envelope matching
/// `internal/wsapi`'s shape.
fn rpc(id: &str, method: &str) -> Vec<u8> {
    serde_json::json!({ "id": id, "method": method, "params": {} })
        .to_string()
        .into_bytes()
}

fn parse_response(bytes: &[u8], expect_id: &str) -> serde_json::Value {
    let v: serde_json::Value = serde_json::from_slice(bytes)
        .unwrap_or_else(|e| panic!("response is not valid JSON: {e}: {bytes:?}"));
    assert_eq!(v["id"], expect_id, "response id mismatch: {v}");
    assert!(
        v.get("error").is_none(),
        "unexpected error in response: {v}"
    );
    assert!(v.get("result").is_some(), "expected a result field: {v}");
    v
}

#[tokio::test]
async fn pairs_handshakes_round_trips_and_resumes_after_a_transport_drop() {
    let harness = tokio::task::spawn_blocking(start_harness)
        .await
        .expect("spawn_blocking start_harness");

    eprintln!(
        "[interop] harness ready: pairing_url={} native_addr={}",
        harness.pairing_url, harness.native_addr
    );

    // --- pairing URL, same format mobile accepts ---
    let offer = Offer::parse_url(&harness.pairing_url).expect("parse the harness's pairing URL");
    let pairing = RelayPairing::from_offer(&offer, harness.native_addr.clone());
    eprintln!(
        "[interop] parsed offer: daemon_id={} workspace_id={}",
        pairing.daemon_id, pairing.workspace_id
    );

    // --- admission (ADR-0011) over native gRPC (ADR-0007 amendment) ---
    let grpc_channel = step(
        "dial",
        relay_client::dial(&pairing.relay_native_addr, &pairing.relay_fingerprint),
    )
    .await;
    eprintln!("[interop] dialed native gRPC listener");
    let mut grpc = GrpcRelayClient::new(grpc_channel);
    let admission_id = step(
        "admit",
        relay_client::admit(
            &mut grpc,
            &pairing.workspace_id,
            &pairing.daemon_id,
            &pairing.admission_secret,
        ),
    )
    .await;
    eprintln!("[interop] admitted: admission_id={admission_id}");

    // --- E2EE handshake, pinned to the offer's daemon public key ---
    let transport = step(
        "open_data",
        relay_client::open_data(
            &mut grpc,
            &admission_id,
            &pairing.workspace_id,
            relay_client::DEFAULT_SESSION_ID,
            relay_client::DEFAULT_DEVICE_ID,
        ),
    )
    .await;
    eprintln!("[interop] opened data stream");
    let kp = KeyPair::generate();
    let mut channel = Channel::new(transport, Role::Mobile);
    step(
        "handshake",
        channel.handshake(&kp, Some(&pairing.daemon_public_key)),
    )
    .await;
    eprintln!("[interop] handshake established");
    assert!(channel.established());
    assert_eq!(
        channel.peer_public_key().unwrap(),
        pairing.daemon_public_key
    );

    // --- a real JSON-RPC round trip through the bridged channel ---
    step(
        "send workspace.list",
        channel.send(&rpc("1", "workspace.list")),
    )
    .await;
    let resp = step("receive workspace.list response", channel.receive()).await;
    parse_response(&resp, "1");
    eprintln!("[interop] round trip 1 ok");

    // --- disconnect: send a second request, then vanish before reading
    // the reply, so the relay's store-and-forward buffer (ADR-0007 (a),
    // 200-frame bound) is what has to hold it, not our own receive loop.
    step(
        "send second request",
        channel.send(&rpc("2", "workspace.list")),
    )
    .await;
    // A graceful half-close: only the sender is replaced (the `OpenData`
    // request body it feeds ends, the server sees a clean EOF), matching
    // Go's `DataConn.DropTransport`/`stream.CloseSend()` -- see
    // `drop_transport`'s doc comment for why the response reader must be
    // left alone here (dropping it too would cancel the whole bidi
    // stream via RST_STREAM, discarding the request above before it's
    // even flushed onto the wire). `grpc` (the connection) is also left
    // alive, not dropped: dropping it immediately can tear down the
    // connection's background I/O-driving task before it ever gets
    // scheduled to notice the sender is gone and flush the half-close.
    // The short sleep gives that task a chance to run first.
    channel.transport_mut().drop_transport();
    tokio::time::sleep(Duration::from_millis(20)).await;
    eprintln!("[interop] dropped transport mid-flight");

    // --- reconnect: fresh dial + admit, then RESUME the same e2ee
    // session (same key, same counters) by swapping only the transport
    // -- mirrors internal/relay/client.DataConn.Resume / bridge.Run's
    // resume-first policy (ADR-0007 (e) amendment).
    let grpc_channel2 = step(
        "re-dial",
        relay_client::dial(&pairing.relay_native_addr, &pairing.relay_fingerprint),
    )
    .await;
    let mut grpc2 = GrpcRelayClient::new(grpc_channel2);
    let admission_id2 = step(
        "re-admit",
        relay_client::admit(
            &mut grpc2,
            &pairing.workspace_id,
            &pairing.daemon_id,
            &pairing.admission_secret,
        ),
    )
    .await;
    step(
        "resume (reopen)",
        channel.transport_mut().reopen(&mut grpc2, &admission_id2),
    )
    .await;
    eprintln!("[interop] resumed");

    let buffered = step("receive buffered response", channel.receive()).await;
    parse_response(&buffered, "2");
    eprintln!("[interop] buffered response delivered after resume");

    // --- the resumed connection is still fully healthy for further calls ---
    step(
        "send third request",
        channel.send(&rpc("3", "workspace.list")),
    )
    .await;
    let resp3 = step("receive third response", channel.receive()).await;
    parse_response(&resp3, "3");
    eprintln!("[interop] round trip 3 ok, test complete");
}

const STEP_TIMEOUT: Duration = Duration::from_secs(20);

/// step awaits `fut` with a generous but bounded timeout, panicking with
/// a clear "<name> timed out"/"<name> failed: <err>" message instead of
/// letting a stuck step hang the whole test (and the harness subprocess
/// with it) indefinitely.
async fn step<T, E: std::fmt::Display>(
    name: &'static str,
    fut: impl std::future::Future<Output = Result<T, E>>,
) -> T {
    match tokio::time::timeout(STEP_TIMEOUT, fut).await {
        Ok(Ok(v)) => v,
        Ok(Err(e)) => panic!("{name} failed: {e}"),
        Err(_) => panic!("{name} timed out after {STEP_TIMEOUT:?}"),
    }
}
