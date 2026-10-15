//! Multi-device interop (ADR-0031 P13a): real Rust relay clients against the
//! real Go relay + daemon device hub (`internal/relay/bridge/harness` in hub
//! mode), with the harness's own Go grpc-web "mobile" client attached at the
//! same time. Covers device identity (`device_hello`, approval, revocation),
//! keepalive, reconnect/resume, a restarted daemon and a daemon without device
//! identity.
//!
//! Requires a `go` toolchain on PATH (the harness is built once per test run)
//! and loopback TCP, like `relay_harness_interop.rs`.

use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering};
use std::sync::{mpsc, Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use smind_daemon_client::relay::client::{
    spawn, RelayHandle, RelayOptions, Status, Timing,
};
use smind_daemon_client::relay::device::DeviceKey;
use smind_daemon_client::relay::liveness::LivenessCfg;
use smind_daemon_client::relay::pairing::Offer;
use smind_daemon_client::relay::pairing_store::RelayPairing;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::watch;

// ---------------------------------------------------------------- harness

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../..")
}

/// The harness binary, built once per test process (`go run` would leave the
/// compiled binary orphaned on kill, and a rebuild per test is slow).
fn harness_binary() -> &'static PathBuf {
    static BIN: OnceLock<PathBuf> = OnceLock::new();
    BIN.get_or_init(|| {
        let dir = std::env::temp_dir().join(format!("smind-p13a-harness-{}", std::process::id()));
        std::fs::create_dir_all(&dir).expect("create harness build dir");
        let exe = dir.join(if cfg!(windows) { "harness.exe" } else { "harness" });
        let status = Command::new("go")
            .args(["build", "-o"])
            .arg(&exe)
            .arg("./internal/relay/bridge/harness")
            .current_dir(repo_root())
            .status()
            .expect("run `go build` for the harness: is a go toolchain on PATH?");
        assert!(status.success(), "go build of the relay harness failed");
        exe
    })
}

#[derive(Default)]
struct Mobile {
    oks: AtomicU64,
    lines: Mutex<Vec<String>>,
}

struct Ctl {
    stdin: ChildStdin,
    replies: mpsc::Receiver<String>,
}

struct Harness {
    child: Child,
    ctl: Mutex<Ctl>,
    mobile: Arc<Mobile>,
    pairing_url: String,
    native_addr: String,
}

impl Drop for Harness {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl Harness {
    fn start(flags: &[&str]) -> Harness {
        let mut child = Command::new(harness_binary())
            .args(flags)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()
            .expect("start the relay harness");
        let stdin = child.stdin.take().unwrap();
        let mut reader = BufReader::new(child.stdout.take().unwrap());
        let hub = flags.contains(&"-hub");
        let (mut ready, mut native, mut home, mut daemon_pub) = (None, None, false, false);
        let deadline = Instant::now() + Duration::from_secs(60);
        let mobile = Arc::new(Mobile::default());
        let mut line = String::new();
        while (ready.is_none() || native.is_none() || !home || (hub && !daemon_pub))
            && Instant::now() < deadline
        {
            line.clear();
            if reader.read_line(&mut line).expect("read harness stdout") == 0 {
                break;
            }
            let t = line.trim();
            if let Some(r) = t.strip_prefix("READY ") {
                ready = Some(r.to_string());
            } else if let Some(r) = t.strip_prefix("NATIVE ") {
                native = Some(r.to_string());
            } else if t.starts_with("HOME ") {
                home = true;
            } else if t.starts_with("DAEMONPUB ") {
                daemon_pub = true;
            } else if t.starts_with("MOBILE ") {
                record_mobile(&mobile, t);
            }
        }
        let (tx, rx) = mpsc::channel();
        let mobile_t = mobile.clone();
        std::thread::spawn(move || {
            let mut line = String::new();
            loop {
                line.clear();
                match reader.read_line(&mut line) {
                    Ok(0) | Err(_) => return,
                    Ok(_) => {}
                }
                let t = line.trim().to_string();
                if t.starts_with("MOBILE ") {
                    record_mobile(&mobile_t, &t);
                } else if t.starts_with("OK ") || t.starts_with("ERR ") || t.starts_with("DEVICES ")
                {
                    let _ = tx.send(t);
                }
            }
        });
        Harness {
            child,
            ctl: Mutex::new(Ctl { stdin, replies: rx }),
            mobile,
            pairing_url: ready.expect("harness printed READY"),
            native_addr: native.expect("harness printed NATIVE"),
        }
    }

    /// cmd sends one stdin command and returns its single reply line.
    fn cmd(&self, line: &str) -> String {
        let mut ctl = self.ctl.lock().unwrap();
        writeln!(ctl.stdin, "{line}").expect("write harness command");
        ctl.stdin.flush().ok();
        ctl.replies
            .recv_timeout(Duration::from_secs(90))
            .unwrap_or_else(|_| panic!("no reply to harness command {line:?}"))
    }

    fn pairing(&self) -> RelayPairing {
        let offer = Offer::parse_url(&self.pairing_url).expect("parse the harness's pairing URL");
        RelayPairing::from_offer(&offer, self.native_addr.clone())
    }

    /// devices returns the registry as the daemon reports it.
    fn devices(&self) -> Vec<serde_json::Value> {
        let reply = self.cmd("list");
        let json = reply.strip_prefix("DEVICES ").unwrap_or_else(|| panic!("list reply: {reply}"));
        serde_json::from_str(json).expect("DEVICES json")
    }

    fn mobile_oks(&self) -> u64 {
        self.mobile.oks.load(Ordering::SeqCst)
    }
}

fn record_mobile(m: &Mobile, line: &str) {
    if let Some(n) = line.strip_prefix("MOBILE ok ") {
        if let Ok(n) = n.trim().parse::<u64>() {
            m.oks.fetch_max(n, Ordering::SeqCst);
        }
    }
    m.lines.lock().unwrap().push(line.to_string());
}

// ---------------------------------------------------------------- proxy

/// A severable, blackhole-able TCP proxy in front of the relay: only the
/// clients dialled through it are affected.
struct Proxy {
    addr: String,
    kill: Arc<watch::Sender<u64>>,
    hole: Arc<AtomicBool>,
}

impl Proxy {
    async fn start(target: String) -> Proxy {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap().to_string();
        let kill = Arc::new(watch::channel(0u64).0);
        let kill_t = kill.clone();
        let hole = Arc::new(AtomicBool::new(false));
        let hole_t = hole.clone();
        tokio::spawn(async move {
            loop {
                let Ok((client, _)) = listener.accept().await else { return };
                let target = target.clone();
                // A fresh receiver per connection: it only sees kills from now on.
                let mut kill_rx = kill_t.subscribe();
                let hole = hole_t.clone();
                tokio::spawn(async move {
                    let Ok(upstream) = TcpStream::connect(&target).await else { return };
                    let (mut cr, mut cw) = client.into_split();
                    let (mut ur, mut uw) = upstream.into_split();
                    let (mut k1, mut k2) = (kill_rx.clone(), kill_rx.clone());
                    let h1 = hole.clone();
                    let up = async move {
                        let mut buf = vec![0u8; 16 * 1024];
                        loop {
                            tokio::select! {
                                _ = k1.changed() => return,
                                n = cr.read(&mut buf) => match n {
                                    Ok(0) | Err(_) => return,
                                    Ok(n) => if !h1.load(Ordering::SeqCst) && uw.write_all(&buf[..n]).await.is_err() { return },
                                },
                            }
                        }
                    };
                    let down = async move {
                        let mut buf = vec![0u8; 16 * 1024];
                        loop {
                            tokio::select! {
                                _ = k2.changed() => return,
                                n = ur.read(&mut buf) => match n {
                                    Ok(0) | Err(_) => return,
                                    Ok(n) => if !hole.load(Ordering::SeqCst) && cw.write_all(&buf[..n]).await.is_err() { return },
                                },
                            }
                        }
                    };
                    tokio::select! {
                        _ = up => {},
                        _ = down => {},
                        _ = kill_rx.changed() => {},
                    }
                });
            }
        });
        Proxy { addr, kill, hole }
    }

    fn kill_all(&self) {
        self.kill.send_modify(|g| *g += 1);
    }

    fn blackhole(&self, on: bool) {
        self.hole.store(on, Ordering::SeqCst);
    }
}

// ---------------------------------------------------------------- clients

static JITTER_CALLS: AtomicUsize = AtomicUsize::new(0);

/// The tests share a process-wide jitter counter and each starts a Go
/// harness plus several clients, so they run one at a time (cargo runs tests
/// in parallel by default, which on a loaded CI machine starves the shrunk
/// protocol timeouts).
static ONE_AT_A_TIME: Mutex<()> = Mutex::new(());

fn serial() -> std::sync::MutexGuard<'static, ()> {
    ONE_AT_A_TIME.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

fn counting_jitter(d: Duration) -> Duration {
    JITTER_CALLS.fetch_add(1, Ordering::SeqCst);
    d
}

/// Timing with every protocol interval shrunk ~20-30x (with margins wide enough for a loaded CI runner) so liveness, stalls and
/// retries happen in seconds; the state machines are the production ones.
fn fast_timing() -> Timing {
    Timing {
        liveness: LivenessCfg {
            interval: Duration::from_millis(500),
            dead_after: Duration::from_millis(2500),
            probe_timeout: Duration::from_millis(1000),
        },
        connect_timeout: Duration::from_secs(3),
        handshake_timeout: Duration::from_secs(8),
        hello_timeout: Duration::from_secs(8),
        resume_probe_timeout: Duration::from_millis(4000),
        min_stable_connection: Duration::from_millis(200),
        request_stall: Duration::from_millis(4000),
        idle_probe: Duration::from_millis(5000),
        backoff_base: Duration::from_millis(100),
        backoff_max: Duration::from_millis(500),
        awaiting_retry: Duration::from_millis(300),
        revoked_retry: Duration::from_millis(500),
        refused_retry: Duration::from_millis(500),
        jitter: counting_jitter,
    }
}

struct Client {
    handle: RelayHandle,
    task: tokio::task::JoinHandle<()>,
    device_id: String,
    /// True once any status other than Connected was seen after the first
    /// Connected (a flap).
    left_connected: Arc<AtomicBool>,
}

impl Drop for Client {
    fn drop(&mut self) {
        self.task.abort();
    }
}

fn start_client(pairing: &RelayPairing, addr: Option<&str>) -> Client {
    start_client_with(pairing, addr, |_| {})
}

fn start_client_with(
    pairing: &RelayPairing,
    addr: Option<&str>,
    tweak: impl FnOnce(&mut Timing),
) -> Client {
    let key = Arc::new(DeviceKey::generate());
    let device_id = key.device_id();
    let mut options = RelayOptions::identity(key);
    options.timing = fast_timing();
    tweak(&mut options.timing);
    let mut p = pairing.clone();
    if let Some(a) = addr {
        p.relay_native_addr = a.to_string();
    }
    let (handle, task) = spawn(p, options);
    let left_connected = Arc::new(AtomicBool::new(false));
    let flag = left_connected.clone();
    let mut rx = handle.status();
    tokio::spawn(async move {
        let mut seen_connected = false;
        while rx.changed().await.is_ok() {
            let s = rx.borrow().clone();
            if s == Status::Connected {
                seen_connected = true;
            } else if seen_connected {
                flag.store(true, Ordering::SeqCst);
            }
        }
    });
    Client { handle, task, device_id, left_connected }
}

async fn wait_status(handle: &RelayHandle, secs: u64, pred: impl Fn(&Status) -> bool) -> Status {
    let mut rx = handle.status();
    let deadline = tokio::time::Instant::now() + Duration::from_secs(secs);
    loop {
        {
            let s = rx.borrow();
            if pred(&s) {
                return s.clone();
            }
        }
        let left = deadline.saturating_duration_since(tokio::time::Instant::now());
        match tokio::time::timeout(left, rx.changed()).await {
            Ok(Ok(())) => {}
            _ => panic!("timed out ({secs}s) waiting for a status; last was {:?}", rx.borrow()),
        }
    }
}

/// call sends one request through the relay handle and returns its response.
async fn call(handle: &RelayHandle, id: &str, method: &str) -> Result<serde_json::Value, String> {
    let mut rx = handle.subscribe();
    let req = serde_json::json!({"id": id, "method": method, "params": {}}).to_string();
    handle.send(req.into_bytes()).await?;
    tokio::time::timeout(Duration::from_secs(10), async {
        loop {
            match rx.recv().await {
                Ok(bytes) => {
                    let Ok(v) = serde_json::from_slice::<serde_json::Value>(&bytes) else { continue };
                    if v["id"] == id && (v.get("result").is_some() || v.get("error").is_some()) {
                        return Ok(v);
                    }
                }
                Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => continue,
                Err(e) => return Err(e.to_string()),
            }
        }
    })
    .await
    .map_err(|_| format!("{method}: no response in 10s"))?
}

/// served asserts a request through `c` gets a result (not an error).
async fn served(c: &Client, id: &str) {
    let v = call(&c.handle, id, "workspace.list").await.unwrap_or_else(|e| panic!("{id}: {e}"));
    assert!(v.get("result").is_some(), "{id}: expected a result, got {v}");
}

/// eventually_served retries until a request is answered (the client may be
/// mid-reconnect), failing after `secs`.
async fn eventually_served(c: &Client, secs: u64, what: &str) {
    let deadline = Instant::now() + Duration::from_secs(secs);
    let mut n = 0;
    loop {
        n += 1;
        if let Ok(v) = call(&c.handle, &format!("{what}-{n}"), "workspace.list").await {
            if v.get("result").is_some() {
                return;
            }
        }
        assert!(Instant::now() < deadline, "{what}: never served within {secs}s");
        tokio::time::sleep(Duration::from_millis(300)).await;
    }
}

async fn wait_until(secs: u64, what: &str, mut cond: impl FnMut() -> bool) {
    let deadline = Instant::now() + Duration::from_secs(secs);
    while !cond() {
        assert!(Instant::now() < deadline, "timed out ({secs}s) waiting for {what}");
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
}

fn harness_with_mobile() -> Harness {
    let h = Harness::start(&["-hub", "-mobile-stub"]);
    let start = Instant::now();
    while h.mobile_oks() < 2 {
        assert!(start.elapsed() < Duration::from_secs(40), "the Go mobile stub never got served");
        std::thread::sleep(Duration::from_millis(100));
    }
    h
}

fn connected_count(h: &Harness) -> usize {
    h.devices().iter().filter(|d| d["connected"] == true && d["state"] == "approved").count()
}

// ---------------------------------------------------------------- tests

#[tokio::test]
async fn three_devices_two_rust_one_go_stay_attached() {
    let _serial = serial();
    let h = tokio::task::spawn_blocking(harness_with_mobile).await.unwrap();
    assert!(h.cmd("window 2 600").starts_with("OK window"));
    let pairing = h.pairing();
    let a = start_client(&pairing, None);
    let b = start_client(&pairing, None);
    wait_status(&a.handle, 40, |s| *s == Status::Connected).await;
    wait_status(&b.handle, 40, |s| *s == Status::Connected).await;
    served(&a, "a-1").await;
    served(&b, "b-1").await;

    // All three are listed by the daemon, approved and connected at once.
    let hh = &h;
    wait_until(15, "three connected devices", || connected_count(hh) == 3).await;
    let ids: Vec<String> =
        h.devices().iter().map(|d| d["device_id"].as_str().unwrap().to_string()).collect();
    assert!(ids.contains(&a.device_id) && ids.contains(&b.device_id), "{ids:?}");

    // They stay attached together: nobody is evicted and nobody ping-pongs.
    let oks_before = h.mobile_oks();
    for i in 0..10 {
        served(&a, &format!("a-live-{i}")).await;
        served(&b, &format!("b-live-{i}")).await;
        tokio::time::sleep(Duration::from_millis(400)).await;
    }
    assert!(h.mobile_oks() >= oks_before + 5, "the Go mobile client stalled: {} -> {}", oks_before, h.mobile_oks());
    assert!(!a.left_connected.load(Ordering::SeqCst), "client A left Connected");
    assert!(!b.left_connected.load(Ordering::SeqCst), "client B left Connected");
    assert_eq!(connected_count(&h), 3);
}

#[tokio::test]
async fn three_devices_two_rust_one_go_stay_attached_after_one_reconnects() {
    let _serial = serial();
    let h = tokio::task::spawn_blocking(harness_with_mobile).await.unwrap();
    h.cmd("window 2 600");
    let pairing = h.pairing();
    let proxy = Proxy::start(h.native_addr.clone()).await;
    let a = start_client(&pairing, Some(&proxy.addr)); // only A goes through the proxy
    let b = start_client(&pairing, None);
    wait_status(&a.handle, 40, |s| *s == Status::Connected).await;
    wait_status(&b.handle, 40, |s| *s == Status::Connected).await;
    served(&a, "a-1").await;
    let hh = &h;
    wait_until(15, "three connected devices", || connected_count(hh) == 3).await;

    // A's connection is cut. B and the mobile client are not disturbed, A comes
    // back by itself — as the SAME device (no second registry entry) — and is
    // served again.
    let oks = h.mobile_oks();
    proxy.kill_all();
    served(&b, "b-during").await;
    eventually_served(&a, 30, "a-after").await;
    served(&b, "b-after").await;
    wait_until(15, "the mobile client to keep going", || hh.mobile_oks() >= oks + 3).await;
    assert!(!b.left_connected.load(Ordering::SeqCst), "B was disturbed by A's reconnect");
    let devices = h.devices();
    assert_eq!(devices.len(), 3, "a reconnect must not create a second device: {devices:?}");
    assert_eq!(devices.iter().filter(|d| d["device_id"] == a.device_id.as_str()).count(), 1);
    wait_until(15, "three connected devices again", || connected_count(hh) == 3).await;
}

#[tokio::test]
async fn three_devices_two_rust_one_go_stay_attached_after_daemon_restart() {
    let _serial = serial();
    let h = tokio::task::spawn_blocking(harness_with_mobile).await.unwrap();
    h.cmd("window 2 600");
    let pairing = h.pairing();
    let a = start_client(&pairing, None);
    let b = start_client(&pairing, None);
    wait_status(&a.handle, 40, |s| *s == Status::Connected).await;
    wait_status(&b.handle, 40, |s| *s == Status::Connected).await;
    served(&a, "a-1").await;
    served(&b, "b-1").await;

    // The daemon restarts and loses every E2EE session. The relay cannot tell
    // the devices; each client notices that the daemon stopped answering,
    // fails to resume, opens a fresh session and — already approved — is
    // served again, without a pairing window.
    assert!(h.cmd("restart-daemon").starts_with("OK restarted"));
    eventually_served(&a, 60, "a-restart").await;
    eventually_served(&b, 60, "b-restart").await;
    let oks = h.mobile_oks();
    let hh = &h;
    let start = Instant::now();
    while h.mobile_oks() <= oks {
        if start.elapsed() > Duration::from_secs(40) {
            let lines = h.mobile.lines.lock().unwrap();
            let tail: Vec<&String> = lines.iter().rev().take(12).collect();
            panic!("the Go mobile client was not served again within 40s; its last lines: {tail:#?}");
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
    wait_until(30, "three connected devices", || connected_count(hh) == 3).await;
    assert_eq!(h.devices().len(), 3, "a restart must not create new devices");
}

#[tokio::test]
async fn three_devices_two_rust_one_go_stay_attached_after_revoke_only_that_device_is_refused() {
    let _serial = serial();
    let h = tokio::task::spawn_blocking(harness_with_mobile).await.unwrap();
    h.cmd("window 2 600");
    let pairing = h.pairing();
    let a = start_client(&pairing, None);
    let b = start_client(&pairing, None);
    wait_status(&a.handle, 40, |s| *s == Status::Connected).await;
    wait_status(&b.handle, 40, |s| *s == Status::Connected).await;
    served(&a, "a-1").await;
    served(&b, "b-1").await;

    let reply = h.cmd(&format!("revoke {}", a.device_id));
    assert!(reply.starts_with("OK revoked"), "{reply}");
    // Only A is refused, and it is told so.
    let st = wait_status(&a.handle, 40, |s| matches!(s, Status::Revoked { .. })).await;
    assert_eq!(st, Status::Revoked { device_id: a.device_id.clone() });
    let oks = h.mobile_oks();
    for i in 0..5 {
        served(&b, &format!("b-after-{i}")).await;
        tokio::time::sleep(Duration::from_millis(300)).await;
    }
    let hh = &h;
    wait_until(15, "the mobile client to keep going", || hh.mobile_oks() >= oks + 3).await;
    assert!(!b.left_connected.load(Ordering::SeqCst), "B was disturbed by A's revocation");

    // A human re-approves it; the client's own retry brings it back.
    assert!(h.cmd(&format!("approve {}", a.device_id)).starts_with("OK approved"));
    wait_status(&a.handle, 40, |s| *s == Status::Connected).await;
    eventually_served(&a, 20, "a-reapproved").await;
}

#[tokio::test]
async fn unknown_device_outside_the_window_waits_for_approval_then_connects() {
    let _serial = serial();
    let h = tokio::task::spawn_blocking(harness_with_mobile).await.unwrap(); // the stub used the only window
    let pairing = h.pairing();
    let a = start_client(&pairing, None);
    let st = wait_status(&a.handle, 40, |s| matches!(s, Status::AwaitingApproval { .. })).await;
    assert_eq!(st, Status::AwaitingApproval { device_id: a.device_id.clone() });
    let pending = h.devices().into_iter().find(|d| d["device_id"] == a.device_id.as_str());
    assert_eq!(pending.expect("the pending device is listed")["state"], "pending");

    // The client keeps retrying by itself: approval takes effect within seconds.
    assert!(h.cmd(&format!("approve {}", a.device_id)).starts_with("OK approved"));
    wait_status(&a.handle, 40, |s| *s == Status::Connected).await;
    served(&a, "a-approved").await;
}

#[tokio::test]
async fn client_reconnects_after_silence_with_jitter() {
    let _serial = serial();
    let h = tokio::task::spawn_blocking(harness_with_mobile).await.unwrap();
    h.cmd("window 1 600");
    let pairing = h.pairing();
    let proxy = Proxy::start(h.native_addr.clone()).await;
    // Only the keepalive dead-stream rule may notice the silence: the daemon
    // stall checks are pushed out of reach so they cannot do its job.
    let a = start_client_with(&pairing, Some(&proxy.addr), |t| {
        t.request_stall = Duration::from_secs(3600);
        t.idle_probe = Duration::from_secs(3600);
    });
    wait_status(&a.handle, 40, |s| *s == Status::Connected).await;
    served(&a, "a-1").await;
    // The rule only applies once the relay has echoed a probe (otherwise it is
    // a relay without keepalive and the client stays quiet by design).
    let stats = a.handle.stats();
    wait_until(20, "the relay's keepalive echo", || {
        stats.keepalive_confirmed.load(Ordering::SeqCst)
    })
    .await;

    // The path goes silent (nothing closed, nothing arrives): within the
    // dead-stream window the client notices; while it stays silent every
    // reconnect attempt times out and each retry delay goes through the jitter.
    let jitter_before = JITTER_CALLS.load(Ordering::SeqCst);
    proxy.blackhole(true);
    wait_status(&a.handle, 10, |s| *s != Status::Connected).await; // dead_after is 2.5 s
    tokio::time::sleep(Duration::from_millis(4500)).await; // > connect_timeout: at least one failed attempt
    // The path heals; the client comes back as the same device and is served.
    proxy.blackhole(false);
    proxy.kill_all();
    eventually_served(&a, 40, "a-healed").await;
    assert!(
        JITTER_CALLS.load(Ordering::SeqCst) > jitter_before,
        "reconnect delays must go through the jitter"
    );
    assert_eq!(h.devices().iter().filter(|d| d["device_id"] == a.device_id.as_str()).count(), 1);
}

#[tokio::test]
async fn client_resumes_after_transport_drop_keeping_ids() {
    let _serial = serial();
    let h = tokio::task::spawn_blocking(harness_with_mobile).await.unwrap();
    h.cmd("window 1 600");
    let pairing = h.pairing();
    let proxy = Proxy::start(h.native_addr.clone()).await;
    let a = start_client(&pairing, Some(&proxy.addr));
    wait_status(&a.handle, 40, |s| *s == Status::Connected).await;
    served(&a, "a-1").await;
    assert_eq!(a.handle.stats().fresh_sessions.load(Ordering::SeqCst), 1);
    assert_eq!(a.handle.stats().resumes.load(Ordering::SeqCst), 0);

    // A transport drop with the daemon untouched and nothing in flight: the
    // SAME E2EE session is resumed in place (no new handshake, no new hello),
    // under the same ids. The test sends nothing until the client is back: a
    // request handed to a stream at the instant it dies burns an E2EE counter
    // the daemon never sees (the relay has no acknowledgements), which is
    // exactly the case the client's fresh-session fallback exists for — see
    // `three_devices_..._after_one_reconnects`, which does send immediately.
    proxy.kill_all();
    let stats = a.handle.stats();
    wait_until(30, "the client to reconnect", || {
        stats.resumes.load(Ordering::SeqCst) + stats.fresh_sessions.load(Ordering::SeqCst) >= 2
    })
    .await;
    assert_eq!(stats.resumes.load(Ordering::SeqCst), 1, "the drop was not resumed in place");
    assert_eq!(
        stats.fresh_sessions.load(Ordering::SeqCst),
        1,
        "a transport drop must not start a fresh session"
    );
    wait_status(&a.handle, 10, |s| *s == Status::Connected).await;
    served(&a, "a-resumed").await;
    let devices = h.devices();
    assert_eq!(devices.iter().filter(|d| d["device_id"] == a.device_id.as_str()).count(), 1);
    assert_eq!(devices.len(), 2, "the mobile stub and this client only: {devices:?}");
}

#[tokio::test]
async fn legacy_daemon_still_works() {
    let _serial = serial();
    // The default harness is a daemon that does not track device identity: it
    // answers the hello "unsupported" (a daemon from before P12b would answer
    // "method not found" or never open the identity route at all). The client
    // falls back to the fixed single-device route and works as before.
    let h = tokio::task::spawn_blocking(|| Harness::start(&[])).await.unwrap();
    let pairing = h.pairing();
    let a = start_client(&pairing, None);
    wait_status(&a.handle, 60, |s| *s == Status::Connected).await;
    eventually_served(&a, 30, "legacy").await;
}
