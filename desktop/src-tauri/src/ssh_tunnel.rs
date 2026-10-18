//! ADR-0031 D7 (b2): an SSH-tunnel connection. The app shells out to the
//! system `ssh -N -L 127.0.0.1:<free port>:127.0.0.1:<daemon port> -- <host>`
//! and keeps that ONE long-lived child per connection (a single
//! authentication, HTTP keep-alive works), unlike Paseo's per-connection
//! `ssh -W` (`refs/paseo/packages/cli/src/ssh/ssh-tunnel.ts`, whose 8 KiB
//! stderr capture and failure detail this ports). `BatchMode=yes`: ssh never
//! prompts, so key/agent auth must already work. Only children this module
//! spawned are ever signalled.

use std::collections::HashMap;
use std::io::Read;
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use tauri::State;

use smind_daemon_client::proxy::{Connection, ConnectionKind, ProxyState, SshTarget};

use crate::state::DesktopState;

const STDERR_LIMIT: usize = 8192;
const STARTUP_TIMEOUT: Duration = Duration::from_secs(12);
const POLL_INTERVAL: Duration = Duration::from_millis(50);
const TUNNEL_DIED_AT_START: &str = "the SSH tunnel stopped right after it started; try again";
const MISSING_SSH: &str = "ssh was not found on this computer: install the OpenSSH client and try again";

/// tunnel_args is the argv after the program: `--` ends options so a host can
/// never be read as one (the host is also validated, never starting with `-`).
pub fn tunnel_args(target: &SshTarget, local_port: u16) -> Vec<String> {
    let mut args: Vec<String> = [
        "-N",
        "-T",
        "-o",
        "BatchMode=yes",
        "-o",
        "ConnectTimeout=10",
        "-o",
        "ExitOnForwardFailure=yes",
        "-o",
        "ServerAliveInterval=15",
        "-o",
        "ServerAliveCountMax=3",
        // The user's ssh_config must not turn this into a mux client or a
        // backgrounded master: our child has to own its own -L listener
        // (the owner check below compares the listener's pid with it).
        "-o",
        "ControlMaster=no",
        "-o",
        "ControlPath=none",
        "-o",
        "ControlPersist=no",
    ]
    .map(String::from)
    .to_vec();
    if let Some(port) = target.ssh_port {
        args.push("-p".to_string());
        args.push(port.to_string());
    }
    args.push("-L".to_string());
    args.push(format!("127.0.0.1:{local_port}:127.0.0.1:{}", target.daemon_port));
    args.push("--".to_string());
    args.push(target.host.clone());
    args
}

/// A running `ssh -N -L` child. Dropping it kills and reaps the child.
struct Tunnel {
    child: Arc<Mutex<Child>>,
    local_port: u16,
    /// Set when we stop the child ourselves, so the exit watcher stays quiet.
    stopped: Arc<AtomicBool>,
}

impl Tunnel {
    fn alive(&self) -> bool {
        matches!(self.child.lock().unwrap().try_wait(), Ok(None))
    }

    fn url(&self) -> String {
        format!("http://127.0.0.1:{}", self.local_port)
    }
}

impl Drop for Tunnel {
    fn drop(&mut self) {
        self.stopped.store(true, Ordering::SeqCst);
        let mut child = self.child.lock().unwrap();
        let _ = child.kill();
        let _ = child.wait();
    }
}

/// Called with (connection id, tunnel local URL) when a tunnel's ssh child
/// exits on its own.
type ExitHook = Arc<dyn Fn(&str, &str) + Send + Sync>;

/// SshTunnels owns the tunnels of the `ssh`-kind connections, by id.
pub struct SshTunnels {
    program: PathBuf,
    tunnels: Mutex<HashMap<String, Tunnel>>,
    /// One async lock per connection id: concurrent `start`s for the same id
    /// run one after the other, so the second finds the first's tunnel
    /// instead of spawning a second authenticated ssh child.
    start_locks: Mutex<HashMap<String, Arc<tokio::sync::Mutex<()>>>>,
    exit_hook: Mutex<Option<ExitHook>>,
}

impl Default for SshTunnels {
    fn default() -> Self {
        Self::with_program(if cfg!(windows) { "ssh.exe" } else { "ssh" })
    }
}

/// Outcome of asking the OS who owns the local port picked for a tunnel.
#[derive(Debug, PartialEq, Eq)]
enum OwnerCheck {
    Ours,
    Foreign(u32),
    Unidentified,
    Unavailable,
}

/// `found` is every pid the platform helper reports listening on the local
/// port. The port is ours only when ALL of them are the ssh child: a second
/// listener (a wildcard or IPv6 bind, or a later same-port bind) next to ssh's
/// is a squatter. `fail_closed` says what a failed lookup means: on macOS and
/// Windows the helper must work, so a failure refuses (`Unidentified`); on
/// other platforms there is no helper and the tunnel falls back to the
/// `/healthz` answer alone (`Unavailable`, the weaker check).
fn classify_owner(found: std::io::Result<Vec<u32>>, child_pid: u32, fail_closed: bool) -> OwnerCheck {
    match found {
        Ok(pids) if pids.is_empty() => OwnerCheck::Unidentified,
        Ok(pids) => match pids.iter().find(|pid| **pid != child_pid) {
            Some(other) => OwnerCheck::Foreign(*other),
            None => OwnerCheck::Ours,
        },
        Err(_) if fail_closed => OwnerCheck::Unidentified,
        Err(_) => OwnerCheck::Unavailable,
    }
}

/// Whether the platform has a port-owner helper this check can rely on.
const OWNER_LOOKUP_FAIL_CLOSED: bool = cfg!(any(target_os = "macos", target_os = "windows"));

/// Who listens on the local port, per the OS's own port-owner helper.
fn port_owners(port: u16) -> std::io::Result<Vec<u32>> {
    #[cfg(target_os = "macos")]
    {
        smind_daemon_client::daemon_manager::native::find_port_owners(port)
    }
    #[cfg(target_os = "windows")]
    {
        smind_daemon_client::daemon_manager::windows_native::find_port_owners(port)
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        let _ = port;
        Err(std::io::Error::new(
            std::io::ErrorKind::Unsupported,
            "no port-owner helper on this platform (the tunnel is checked by its /healthz answer only)",
        ))
    }
}

/// Whether a tunnel whose local port answers may be used: the listener must
/// be our ssh child (or unverifiable but answering as a sMind daemon), and
/// anything that answers and is not a sMind daemon is refused.
fn verdict(owner: &OwnerCheck, reachable: bool, service: Option<&str>) -> Result<(), String> {
    match owner {
        OwnerCheck::Foreign(pid) => {
            Err(format!("another program (process {pid}) is listening on the local port picked for the SSH tunnel, not ssh; refusing to use it -- try again"))
        }
        OwnerCheck::Unidentified => {
            Err("the program listening on the local port picked for the SSH tunnel could not be identified as ssh (or could not be looked up); refusing to use it -- try again".to_string())
        }
        OwnerCheck::Ours | OwnerCheck::Unavailable => {
            if reachable && service != Some("smind") {
                Err("the SSH tunnel is up, but what answers behind it is not a sMind daemon -- check the daemon port".to_string())
            } else if !reachable && matches!(owner, OwnerCheck::Unavailable) {
                Err("the SSH tunnel is up but nothing answered /healthz through it, and the listener could not be verified -- is the daemon running on the remote host?".to_string())
            } else {
                Ok(())
            }
        }
    }
}

fn free_port() -> Result<u16, String> {
    let listener = TcpListener::bind("127.0.0.1:0").map_err(|e| format!("could not find a free local port: {e}"))?;
    Ok(listener.local_addr().map_err(|e| e.to_string())?.port())
}

/// capture drains the child's stderr on a thread, keeping the last 8 KiB.
fn capture(mut stderr: std::process::ChildStderr) -> (Arc<Mutex<Vec<u8>>>, std::thread::JoinHandle<()>) {
    let buf = Arc::new(Mutex::new(Vec::new()));
    let sink = buf.clone();
    let handle = std::thread::spawn(move || {
        let mut chunk = [0u8; 1024];
        while let Ok(n) = stderr.read(&mut chunk) {
            if n == 0 {
                break;
            }
            let mut b = sink.lock().unwrap();
            b.extend_from_slice(&chunk[..n]);
            let excess = b.len().saturating_sub(STDERR_LIMIT);
            b.drain(..excess);
        }
    });
    (buf, handle)
}

/// failure_detail is what the user sees when ssh exits before the forward is
/// up: its own stderr if it said anything, else the exit status.
fn failure_detail(stderr: &[u8], status: std::process::ExitStatus) -> String {
    let text = String::from_utf8_lossy(stderr);
    let text = text.trim();
    if !text.is_empty() {
        return text.to_string();
    }
    match status.code() {
        Some(code) => format!("ssh exited with code {code}"),
        None => "ssh was terminated by a signal".to_string(),
    }
}

impl SshTunnels {
    pub fn with_program(program: impl Into<PathBuf>) -> Self {
        Self {
            program: program.into(),
            tunnels: Mutex::new(HashMap::new()),
            start_locks: Mutex::new(HashMap::new()),
            exit_hook: Mutex::new(None),
        }
    }

    /// bind_proxy makes a tunnel whose ssh child dies on its own drop its
    /// mapping in `proxy` (if it still points at that tunnel): the proxy then
    /// answers "tunnel is not running", and a stranger binding the freed
    /// local port can never receive webview traffic or a session bearer.
    pub fn bind_proxy(&self, proxy: Arc<ProxyState>) {
        let hook: ExitHook = Arc::new(move |id, url| {
            if proxy.ssh_local(id).as_deref() == Some(url) {
                proxy.clear_ssh_local(id);
            }
        });
        *self.exit_hook.lock().unwrap() = Some(hook);
    }

    fn start_lock(&self, id: &str) -> Arc<tokio::sync::Mutex<()>> {
        self.start_locks.lock().unwrap().entry(id.to_string()).or_default().clone()
    }

    /// watch_exit reports (once) when `tunnel`'s child exits by itself.
    fn watch_exit(&self, id: &str, tunnel: &Tunnel) {
        let Some(hook) = self.exit_hook.lock().unwrap().clone() else { return };
        let (child, stopped, url, id) = (tunnel.child.clone(), tunnel.stopped.clone(), tunnel.url(), id.to_string());
        std::thread::spawn(move || loop {
            if stopped.load(Ordering::SeqCst) {
                return;
            }
            if !matches!(child.lock().unwrap().try_wait(), Ok(None)) {
                if !stopped.load(Ordering::SeqCst) {
                    hook(&id, &url);
                }
                return;
            }
            std::thread::sleep(Duration::from_millis(100));
        });
    }

    /// local_url is the tunnel's local end if its ssh child is still running;
    /// a dead child is found here (lazily) and forgotten.
    pub fn local_url(&self, id: &str) -> Option<String> {
        let mut tunnels = self.tunnels.lock().unwrap();
        let alive = tunnels.get(id)?.alive();
        if alive {
            tunnels.get(id).map(Tunnel::url)
        } else {
            tunnels.remove(id);
            None
        }
    }

    /// start brings the tunnel for `id` up (or returns the running one) and
    /// returns its local URL. It resolves only once the local port accepts
    /// connections, i.e. ssh authenticated and bound the forward; ssh
    /// exiting first is the error, carrying its stderr.
    pub async fn start(&self, id: &str, target: &SshTarget) -> Result<String, String> {
        let lock = self.start_lock(id);
        let _serialized = lock.lock().await;
        if let Some(url) = self.local_url(id) {
            return Ok(url);
        }
        let target = target.validate()?;
        let local_port = free_port()?;
        let mut command = Command::new(&self.program);
        command.args(tunnel_args(&target, local_port)).stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::piped());
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x0800_0000); // CREATE_NO_WINDOW
        }
        let mut child = command.spawn().map_err(|e| {
            if e.kind() == std::io::ErrorKind::NotFound {
                MISSING_SSH.to_string()
            } else {
                format!("could not start ssh: {e}")
            }
        })?;
        let (stderr, reader) = capture(child.stderr.take().expect("stderr is piped"));
        let tunnel = Tunnel { child: Arc::new(Mutex::new(child)), local_port, stopped: Arc::new(AtomicBool::new(false)) };

        let addr = SocketAddr::from(([127, 0, 0, 1], local_port));
        let deadline = Instant::now() + STARTUP_TIMEOUT;
        loop {
            let exited = tunnel.child.lock().unwrap().try_wait();
            if let Ok(Some(status)) = exited {
                // Let the reader see the last of stderr.
                for _ in 0..20 {
                    if reader.is_finished() {
                        break;
                    }
                    tokio::time::sleep(Duration::from_millis(10)).await;
                }
                return Err(failure_detail(&stderr.lock().unwrap(), status));
            }
            // The port accepting is not proof it is ssh's forward: also require
            // the child to still be running. A local process that binds the
            // picked port before ssh has authenticated still passes this
            // connect check; that residual window is closed by the
            // post-start owner + healthz check below.
            if TcpStream::connect_timeout(&addr, Duration::from_millis(200)).is_ok() && tunnel.alive() {
                break;
            }
            if Instant::now() >= deadline {
                let detail = String::from_utf8_lossy(&stderr.lock().unwrap()).trim().to_string();
                return Err(if detail.is_empty() { "the SSH tunnel did not come up in time".to_string() } else { detail });
            }
            tokio::time::sleep(POLL_INTERVAL).await;
        }
        // The listener on the local port must be OUR ssh child and answer as
        // a sMind daemon; otherwise a local squatter would receive the
        // proxied traffic (and any bearer). Dropping `tunnel` kills + reaps
        // the child; nothing is inserted.
        let health = crate::probe::probe(&tunnel.url()).await;
        // The probe can take seconds; ssh dying meanwhile frees the port for
        // anyone, so liveness and ownership are checked AFTER it, closest to
        // use (a pid of a child that is still unreaped cannot be reused).
        if !tunnel.alive() {
            return Err("the ssh process exited while the SSH tunnel was being verified".to_string());
        }
        let child_pid = tunnel.child.lock().unwrap().id();
        let owner = classify_owner(port_owners(local_port), child_pid, OWNER_LOOKUP_FAIL_CLOSED);
        verdict(&owner, health.reachable, health.service.as_deref())?;
        let url = tunnel.url();
        self.watch_exit(id, &tunnel);
        self.tunnels.lock().unwrap().insert(id.to_string(), tunnel);
        Ok(url)
    }

    pub fn stop(&self, id: &str) {
        self.tunnels.lock().unwrap().remove(id);
    }

    pub fn stop_all(&self) {
        self.tunnels.lock().unwrap().clear();
    }

    #[cfg(test)]
    fn kill_for_test(&self, id: &str) {
        let tunnels = self.tunnels.lock().unwrap();
        let t = tunnels.get(id).expect("tunnel exists");
        let mut child = t.child.lock().unwrap();
        child.kill().unwrap();
        child.wait().unwrap();
    }
}

/// ensure_tunnel makes sure `conn` (an `ssh` connection) has a running
/// tunnel and tells the proxy where its local end is.
pub async fn ensure_tunnel(proxy: &ProxyState, tunnels: &SshTunnels, conn: &Connection) -> Result<(), String> {
    debug_assert_eq!(conn.kind, ConnectionKind::Ssh);
    let target = SshTarget::from_uri(&conn.base_url)?;
    let url = tunnels.start(&conn.id, &target).await?;
    proxy.set_ssh_local(&conn.id, url.clone());
    // ssh may have died between the start's last check and the mapping: the
    // exit hook only clears a mapping that already exists, so look again now
    // and never leave a dead tunnel's port mapped.
    if tunnels.local_url(&conn.id).as_deref() != Some(url.as_str()) {
        forget_tunnel(proxy, tunnels, &conn.id);
        return Err(TUNNEL_DIED_AT_START.to_string());
    }
    Ok(())
}

/// forget_tunnel stops `id`'s tunnel (if any) and unmaps it in the proxy.
pub fn forget_tunnel(proxy: &ProxyState, tunnels: &SshTunnels, id: &str) {
    tunnels.stop(id);
    proxy.clear_ssh_local(id);
}

/// add_ssh_connection starts the tunnel first and saves the connection only
/// if it came up: a wrong host or key leaves nothing behind.
pub async fn add_ssh_connection(
    proxy: &ProxyState,
    tunnels: &SshTunnels,
    path: &std::path::Path,
    label: &str,
    target: &SshTarget,
) -> Result<Connection, String> {
    let target = target.validate()?;
    let id = target.connection_id();
    let url = tunnels.start(&id, &target).await?;
    // Nothing is saved for a tunnel that is already gone.
    if tunnels.local_url(&id).as_deref() != Some(url.as_str()) {
        forget_tunnel(proxy, tunnels, &id);
        return Err(TUNNEL_DIED_AT_START.to_string());
    }
    let saved = {
        let mut registry = proxy.registry.lock().unwrap();
        registry.add_ssh(label, &target).and_then(|conn| registry.save(path).map(|_| conn).map_err(|e| e.to_string()))
    };
    match saved {
        Ok(conn) => {
            proxy.set_ssh_local(&id, url.clone());
            // Died between the check above and the mapping: the entry is
            // saved, so unmap the dead port (the proxy answers 502 until the
            // connection is selected again, which restarts the tunnel).
            if tunnels.local_url(&id).as_deref() != Some(url.as_str()) {
                forget_tunnel(proxy, tunnels, &id);
            }
            Ok(conn)
        }
        Err(e) => {
            forget_tunnel(proxy, tunnels, &id);
            Err(e)
        }
    }
}

#[tauri::command]
pub async fn connections_add_ssh(
    state: State<'_, DesktopState>,
    label: String,
    host: String,
    ssh_port: Option<u16>,
    daemon_port: u16,
) -> Result<Connection, String> {
    let target = SshTarget { host, ssh_port, daemon_port };
    add_ssh_connection(&state.proxy, &state.ssh_tunnels, &state.connections_path, &label, &target).await
}

#[cfg(test)]
mod tests {
    use super::*;

    fn target() -> SshTarget {
        SshTarget { host: "me@lab".into(), ssh_port: Some(2222), daemon_port: 4648 }
    }

    #[test]
    fn owner_check_ours_when_the_pid_matches() {
        assert_eq!(classify_owner(Ok(vec![7]), 7, true), OwnerCheck::Ours);
    }

    #[test]
    fn owner_check_ours_when_every_listener_is_our_child() {
        assert_eq!(classify_owner(Ok(vec![7, 7]), 7, true), OwnerCheck::Ours);
    }

    #[test]
    fn owner_check_foreign_when_the_pid_differs() {
        assert_eq!(classify_owner(Ok(vec![9]), 7, true), OwnerCheck::Foreign(9));
    }

    #[test]
    fn owner_check_foreign_when_any_listener_is_not_ours() {
        // A wildcard/IPv6 squatter next to ssh's own listener, in either order.
        assert_eq!(classify_owner(Ok(vec![7, 9]), 7, true), OwnerCheck::Foreign(9));
        assert_eq!(classify_owner(Ok(vec![9, 7]), 7, true), OwnerCheck::Foreign(9));
    }

    #[test]
    fn owner_check_unidentified_when_nobody_is_found() {
        assert_eq!(classify_owner(Ok(vec![]), 7, true), OwnerCheck::Unidentified);
        assert_eq!(classify_owner(Ok(vec![]), 7, false), OwnerCheck::Unidentified);
    }

    #[test]
    fn owner_check_unavailable_when_the_helper_errors_and_there_is_no_helper() {
        assert_eq!(
            classify_owner(Err(std::io::Error::other("boom")), 7, false),
            OwnerCheck::Unavailable
        );
    }

    #[test]
    fn owner_check_refuses_a_failed_lookup_where_the_helper_must_work() {
        // macOS / Windows: lsof or the IP Helper failing means we cannot vouch
        // for the listener, so the tunnel is refused rather than waved through.
        assert_eq!(
            classify_owner(Err(std::io::Error::other("boom")), 7, true),
            OwnerCheck::Unidentified
        );
    }

    #[test]
    fn tunnel_args_never_multiplex() {
        let args = tunnel_args(&target(), 51000).join(" ");
        for want in ["ControlMaster=no", "ControlPath=none", "ControlPersist=no"] {
            assert!(args.contains(want), "{args}");
        }
    }

    #[test]
    fn verdict_refuses_foreign_and_unidentified_owners() {
        for owner in [&OwnerCheck::Foreign(42), &OwnerCheck::Unidentified] {
            let err = verdict(owner, true, Some("smind")).unwrap_err();
            assert!(err.contains("refusing"), "{err}");
        }
    }

    #[test]
    fn verdict_accepts_our_ssh_even_when_the_remote_daemon_is_down() {
        assert_eq!(verdict(&OwnerCheck::Ours, false, None), Ok(()));
    }

    #[test]
    fn verdict_refuses_a_non_smind_service() {
        let err = verdict(&OwnerCheck::Ours, true, Some("other")).unwrap_err();
        assert!(err.contains("not a sMind daemon"), "{err}");
        assert!(verdict(&OwnerCheck::Unavailable, true, Some("other")).is_err());
    }

    #[test]
    fn verdict_requires_a_smind_answer_when_the_owner_could_not_be_verified() {
        assert!(verdict(&OwnerCheck::Unavailable, false, None).is_err());
        assert_eq!(
            verdict(&OwnerCheck::Unavailable, true, Some("smind")),
            Ok(())
        );
    }

    #[test]
    fn ssh_tunnel_args_use_batchmode_and_double_dash() {
        let args = tunnel_args(&target(), 51000);
        assert_eq!(
            args,
            [
                "-N",
                "-T",
                "-o",
                "BatchMode=yes",
                "-o",
                "ConnectTimeout=10",
                "-o",
                "ExitOnForwardFailure=yes",
                "-o",
                "ServerAliveInterval=15",
                "-o",
                "ServerAliveCountMax=3",
                "-o",
                "ControlMaster=no",
                "-o",
                "ControlPath=none",
                "-o",
                "ControlPersist=no",
                "-p",
                "2222",
                "-L",
                "127.0.0.1:51000:127.0.0.1:4648",
                "--",
                "me@lab",
            ]
        );
        // No ssh port: no -p; the host is always last, after `--`.
        let args = tunnel_args(&SshTarget { ssh_port: None, ..target() }, 1);
        assert!(!args.contains(&"-p".to_string()));
        assert_eq!(&args[args.len() - 2..], ["--", "me@lab"]);
    }

    #[tokio::test]
    async fn ssh_missing_binary_reports_clearly() {
        let dir = tempfile::tempdir().unwrap();
        let tunnels = SshTunnels::with_program(dir.path().join("no-such-ssh"));
        let err = tunnels.start("ssh:me@lab:default:4648", &target()).await.unwrap_err();
        assert_eq!(err, "ssh was not found on this computer: install the OpenSSH client and try again");
    }

    #[cfg(unix)]
    mod with_fake_ssh {
        use super::*;
        use smind_daemon_client::proxy::Registry;
        use std::io::Write;
        use std::os::unix::fs::PermissionsExt;
        use std::path::Path;

        /// The fake `ssh` is a script that records its argv and then execs
        /// THIS test binary running `fake_ssh_listener`, which listens on the
        /// `-L` local port -- hermetic: no nc/python, no real ssh.
        fn fake_ssh(dir: &Path) -> PathBuf {
            let exe = std::env::current_exe().unwrap();
            let script = dir.join("fake-ssh");
            std::fs::write(
                &script,
                format!(
                    "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{argv}'\nFAKE_SSH_ARGV='{argv}' exec '{exe}' --exact ssh_tunnel::tests::with_fake_ssh::fake_ssh_listener --ignored --nocapture\n",
                    argv = dir.join("argv.txt").display(),
                    exe = exe.display()
                ),
            )
            .unwrap();
            std::fs::set_permissions(&script, std::fs::Permissions::from_mode(0o755)).unwrap();
            script
        }

        fn script(dir: &Path, name: &str, body: &str) -> PathBuf {
            let path = dir.join(name);
            std::fs::write(&path, format!("#!/bin/sh\n{body}\n")).unwrap();
            std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o755)).unwrap();
            path
        }

        /// Not a test: the body of the fake ssh process (see `fake_ssh`).
        #[test]
        #[ignore]
        fn fake_ssh_listener() {
            let Ok(argv) = std::env::var("FAKE_SSH_ARGV") else { return };
            let argv = std::fs::read_to_string(argv).unwrap();
            let args: Vec<&str> = argv.lines().collect();
            let spec = args[args.iter().position(|a| *a == "-L").unwrap() + 1];
            let port: u16 = spec.split(':').nth(1).unwrap().parse().unwrap();
            let service = std::env::var("FAKE_SSH_SERVICE").unwrap_or_else(|_| "smind".into());
            let listener = TcpListener::bind(("127.0.0.1", port)).unwrap();
            for conn in listener.incoming() {
                let Ok(mut stream) = conn else { continue };
                let service = service.clone();
                std::thread::spawn(move || {
                    // The readiness check and `reachable()` connect and close
                    // without sending anything: read until the end of the
                    // request headers, or a bare close, without hanging.
                    let mut seen = Vec::new();
                    let mut chunk = [0u8; 1024];
                    loop {
                        match stream.read(&mut chunk) {
                            Ok(0) | Err(_) => return,
                            Ok(n) => {
                                seen.extend_from_slice(&chunk[..n]);
                                if seen.windows(4).any(|w| w == b"\r\n\r\n") {
                                    break;
                                }
                            }
                        }
                    }
                    if service.as_str() == "down" {
                        return;
                    }
                    // Lets a test hold start() inside its /healthz probe.
                    if let Some(ms) = std::env::var("FAKE_SSH_HEALTHZ_DELAY_MS").ok().and_then(|v| v.parse::<u64>().ok()) {
                        std::thread::sleep(Duration::from_millis(ms));
                    }
                    let body = format!(
                        "{{\"service\":\"{service}\",\"status\":\"ok\",\"version\":\"test\"}}"
                    );
                    let _ = stream.write_all(
                        format!(
                            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                            body.len(),
                            body
                        )
                        .as_bytes(),
                    );
                });
            }
        }

        fn proxy() -> Arc<ProxyState> {
            let local = url::Url::parse("http://127.0.0.1:4648").unwrap();
            ProxyState::new("s".into(), Registry::new(&local), Box::new(crate::assets::EmbeddedAssets))
        }

        fn reachable(url: &str) -> bool {
            let port: u16 = url.rsplit(':').next().unwrap().parse().unwrap();
            TcpStream::connect_timeout(&SocketAddr::from(([127, 0, 0, 1], port)), Duration::from_millis(500)).is_ok()
        }

        #[tokio::test]
        async fn start_spawns_ssh_with_the_forward_and_waits_for_the_port() {
            let dir = tempfile::tempdir().unwrap();
            let tunnels = SshTunnels::with_program(fake_ssh(dir.path()));
            let id = target().connection_id();
            let url = tunnels.start(&id, &target()).await.unwrap();
            assert!(reachable(&url));
            let argv = std::fs::read_to_string(dir.path().join("argv.txt")).unwrap();
            let port = url.rsplit(':').next().unwrap();
            assert!(argv.contains(&format!("127.0.0.1:{port}:127.0.0.1:4648")), "{argv}");
            assert!(argv.ends_with("--\nme@lab\n"), "{argv}");
            // Asking again reuses the running tunnel.
            assert_eq!(tunnels.start(&id, &target()).await.unwrap(), url);
            tunnels.stop_all();
            assert!(!reachable(&url));
        }

        #[tokio::test]
        async fn ssh_failure_carries_stderr_or_the_exit_code() {
            let dir = tempfile::tempdir().unwrap();
            let denied = script(dir.path(), "denied", "echo 'me@lab: Permission denied (publickey).' >&2\nexit 255");
            let err = SshTunnels::with_program(denied).start("x", &target()).await.unwrap_err();
            assert_eq!(err, "me@lab: Permission denied (publickey).");
            let silent = script(dir.path(), "silent", "exit 3");
            let err = SshTunnels::with_program(silent).start("x", &target()).await.unwrap_err();
            assert_eq!(err, "ssh exited with code 3");
            // Invalid targets never spawn anything.
            let never = SshTunnels::with_program(dir.path().join("never"));
            let bad = SshTarget { host: "-oProxyCommand=x".into(), ..target() };
            assert_eq!(never.start("x", &bad).await.unwrap_err(), "SSH host is invalid");
        }

        #[tokio::test]
        async fn add_ssh_saves_only_after_the_tunnel_is_up() {
            let dir = tempfile::tempdir().unwrap();
            let path = dir.path().join("connections.json");
            let denied = script(dir.path(), "denied", "echo nope >&2\nexit 255");
            let p = proxy();
            let err = add_ssh_connection(&p, &SshTunnels::with_program(denied), &path, "Lab", &target()).await.unwrap_err();
            assert_eq!(err, "nope");
            assert!(!path.exists());
            assert_eq!(p.registry.lock().unwrap().list().len(), 1);

            let tunnels = SshTunnels::with_program(fake_ssh(dir.path()));
            let conn = add_ssh_connection(&p, &tunnels, &path, "Lab", &target()).await.unwrap();
            assert_eq!(conn.kind, ConnectionKind::Ssh);
            assert!(path.exists());
            assert_eq!(p.ssh_local(&conn.id), tunnels.local_url(&conn.id));
        }

        #[tokio::test]
        async fn ssh_tunnel_closed_when_connection_removed() {
            let dir = tempfile::tempdir().unwrap();
            let path = dir.path().join("connections.json");
            let p = proxy();
            let tunnels = SshTunnels::with_program(fake_ssh(dir.path()));
            let conn = add_ssh_connection(&p, &tunnels, &path, "Lab", &target()).await.unwrap();
            let url = p.ssh_local(&conn.id).unwrap();
            assert!(reachable(&url));

            crate::commands::remove_connection(&p, &tunnels, &path, &conn.id).unwrap();
            assert!(!reachable(&url), "the ssh child was killed");
            assert_eq!(tunnels.local_url(&conn.id), None);
            assert_eq!(p.ssh_local(&conn.id), None);
            assert!(!std::fs::read_to_string(&path).unwrap().contains("ssh:"));
        }

        /// A fake ssh that takes `delay` seconds to "authenticate".
        fn slow_fake_ssh(dir: &Path, delay: &str) -> PathBuf {
            let fast = fake_ssh(dir);
            script(dir, "slow-ssh", &format!("sleep {delay}\nexec '{}' \"$@\"", fast.display()))
        }

        async fn wait_until(mut f: impl FnMut() -> bool) -> bool {
            for _ in 0..60 {
                if f() {
                    return true;
                }
                tokio::time::sleep(Duration::from_millis(50)).await;
            }
            false
        }

        #[tokio::test]
        async fn ssh_exit_clears_the_proxy_mapping_so_a_squatter_gets_nothing() {
            let dir = tempfile::tempdir().unwrap();
            let path = dir.path().join("connections.json");
            let p = proxy();
            let tunnels = SshTunnels::with_program(fake_ssh(dir.path()));
            tunnels.bind_proxy(p.clone());
            let conn = add_ssh_connection(&p, &tunnels, &path, "Lab", &target()).await.unwrap();
            assert!(p.ssh_local(&conn.id).is_some());

            tunnels.kill_for_test(&conn.id);
            assert!(wait_until(|| p.ssh_local(&conn.id).is_none()).await, "the mapping outlived its ssh child");

            // A mapping that already points elsewhere (a newer tunnel) is left alone.
            let conn2 = add_ssh_connection(&p, &tunnels, &path, "Lab", &target()).await.unwrap();
            p.set_ssh_local(&conn2.id, "http://127.0.0.1:1".into());
            tunnels.kill_for_test(&conn2.id);
            tokio::time::sleep(Duration::from_millis(400)).await;
            assert_eq!(p.ssh_local(&conn2.id).as_deref(), Some("http://127.0.0.1:1"));
        }

        #[tokio::test]
        async fn concurrent_starts_for_one_connection_share_one_tunnel() {
            let dir = tempfile::tempdir().unwrap();
            let tunnels = SshTunnels::with_program(slow_fake_ssh(dir.path(), "0.2"));
            let id = target().connection_id();
            let t = target();
            let (a, b) = tokio::join!(tunnels.start(&id, &t), tunnels.start(&id, &t));
            assert_eq!(a.unwrap(), b.unwrap(), "the second start reused the first's tunnel");
            assert_eq!(tunnels.tunnels.lock().unwrap().len(), 1);
        }

        #[tokio::test]
        async fn select_of_a_connection_removed_meanwhile_leaves_no_tunnel() {
            let dir = tempfile::tempdir().unwrap();
            let path = dir.path().join("connections.json");
            let p = proxy();
            let tunnels = SshTunnels::with_program(slow_fake_ssh(dir.path(), "0.4"));
            let conn = p.registry.lock().unwrap().add_ssh("Lab", &target()).unwrap();
            let (selected, _) = tokio::join!(crate::commands::select_connection(&p, &tunnels, &path, &conn.id), async {
                tokio::time::sleep(Duration::from_millis(150)).await;
                p.registry.lock().unwrap().remove(&conn.id).unwrap();
            });
            assert_eq!(selected.unwrap_err(), "this connection was removed");
            assert_eq!(tunnels.local_url(&conn.id), None);
            assert_eq!(p.ssh_local(&conn.id), None);
        }

        #[tokio::test]
        async fn remove_stops_the_tunnel_even_when_saving_fails() {
            let dir = tempfile::tempdir().unwrap();
            let p = proxy();
            let tunnels = SshTunnels::with_program(fake_ssh(dir.path()));
            let conn = add_ssh_connection(&p, &tunnels, &dir.path().join("connections.json"), "Lab", &target()).await.unwrap();
            let url = p.ssh_local(&conn.id).unwrap();
            assert!(reachable(&url));
            let unwritable = dir.path().join("a-directory");
            std::fs::create_dir(&unwritable).unwrap(); // writing a file over a directory fails
            assert!(crate::commands::remove_connection(&p, &tunnels, &unwritable, &conn.id).is_err());
            assert!(!reachable(&url), "the ssh child was stopped");
            assert_eq!(p.ssh_local(&conn.id), None);
        }

        fn serve_healthz(listener: TcpListener, service: &'static str) {
            std::thread::spawn(move || {
                for conn in listener.incoming() {
                    let Ok(mut stream) = conn else { continue };
                    std::thread::spawn(move || {
                        let mut seen = Vec::new();
                        let mut chunk = [0u8; 1024];
                        loop {
                            match stream.read(&mut chunk) {
                                Ok(0) | Err(_) => return,
                                Ok(n) => {
                                    seen.extend_from_slice(&chunk[..n]);
                                    if seen.windows(4).any(|w| w == b"\r\n\r\n") {
                                        break;
                                    }
                                }
                            }
                        }
                        let body = format!(
                            "{{\"service\":\"{service}\",\"status\":\"ok\",\"version\":\"test\"}}"
                        );
                        let _ = stream.write_all(
                            format!(
                                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                                body.len(),
                                body
                            )
                            .as_bytes(),
                        );
                    });
                }
            });
        }

        /// A fake ssh that never listens; the test binds the picked port
        /// itself, so only the OWNER check can reject it.
        #[tokio::test]
        async fn a_squatter_on_the_picked_port_is_refused() {
            let dir = tempfile::tempdir().unwrap();
            let argv_path = dir.path().join("argv.txt");
            let idle = script(
                dir.path(),
                "idle-ssh",
                &format!(
                    "printf '%s\\n' \"$@\" > '{}'\nexec sleep 30",
                    argv_path.display()
                ),
            );
            let tunnels = std::sync::Arc::new(SshTunnels::with_program(idle));
            let id = target().connection_id();
            let t = tunnels.clone();
            let started = {
                let id = id.clone();
                tokio::spawn(async move { t.start(&id, &target()).await })
            };

            // Wait for the fake ssh to report the -L spec, then squat the port.
            let mut squatter = None;
            for _ in 0..100 {
                if let Ok(argv) = std::fs::read_to_string(&argv_path) {
                    if let Some(line) = argv.lines().find(|l| l.starts_with("127.0.0.1:")) {
                        let port: u16 = line.split(':').nth(1).unwrap().parse().unwrap();
                        if let Ok(listener) = TcpListener::bind(("127.0.0.1", port)) {
                            squatter = Some(listener);
                            break;
                        }
                    }
                }
                tokio::time::sleep(Duration::from_millis(50)).await;
            }
            let listener = squatter.expect("bound the picked port");
            serve_healthz(listener, "smind");

            let result = started.await.unwrap();
            let err = result.unwrap_err();
            assert!(
                err.contains("another program") || err.contains("could not be identified"),
                "{err}"
            );
            assert!(tunnels.local_url(&id).is_none());
        }

        /// A wildcard (0.0.0.0) listener on the picked port next to ssh's own
        /// 127.0.0.1 listener (macOS lets both coexist): connections still reach
        /// ssh's, but the port has TWO owners, so the check must refuse. The
        /// fake daemon answers /healthz slowly, holding start() inside its probe
        /// while the squatter binds NEXT to the already-listening fake ssh.
        #[cfg(target_os = "macos")]
        #[tokio::test]
        async fn a_wildcard_listener_next_to_ssh_is_refused() {
            let dir = tempfile::tempdir().unwrap();
            let fast = fake_ssh(dir.path());
            let slow = script(
                dir.path(),
                "slow-healthz-ssh",
                &format!("FAKE_SSH_HEALTHZ_DELAY_MS=1200 exec '{}' \"$@\"", fast.display()),
            );
            let tunnels = std::sync::Arc::new(SshTunnels::with_program(slow));
            let id = target().connection_id();
            let t = tunnels.clone();
            let started = {
                let id = id.clone();
                tokio::spawn(async move { t.start(&id, &target()).await })
            };
            let argv_path = dir.path().join("argv.txt");
            let mut squatter = None;
            for _ in 0..200 {
                if let Ok(argv) = std::fs::read_to_string(&argv_path) {
                    if let Some(line) = argv.lines().find(|l| l.starts_with("127.0.0.1:")) {
                        let port: u16 = line.split(':').nth(1).unwrap().parse().unwrap();
                        // Only once ssh's own listener is up: the squatter joins it.
                        if reachable(&format!("http://127.0.0.1:{port}")) {
                            squatter = TcpListener::bind(("0.0.0.0", port)).ok();
                            break;
                        }
                    }
                }
                tokio::time::sleep(Duration::from_millis(20)).await;
            }
            let squatter = squatter.expect("bound the wildcard next to ssh's listener");
            let err = started.await.unwrap().unwrap_err();
            drop(squatter);
            assert!(err.contains("another program"), "{err}");
            assert!(tunnels.local_url(&id).is_none());
        }

        #[tokio::test]
        async fn a_non_smind_service_behind_the_tunnel_is_refused() {
            let dir = tempfile::tempdir().unwrap();
            let fast = fake_ssh(dir.path());
            script(
                dir.path(),
                "other-ssh",
                &format!("FAKE_SSH_SERVICE='other' exec '{}' \"$@\"", fast.display()),
            );
            let tunnels = SshTunnels::with_program(dir.path().join("other-ssh"));
            let id = target().connection_id();
            let err = tunnels.start(&id, &target()).await.unwrap_err();
            assert!(err.contains("not a sMind daemon"), "{err}");
            assert!(tunnels.local_url(&id).is_none());
        }

        #[cfg(target_os = "macos")]
        #[tokio::test]
        async fn ssh_that_owns_the_port_is_accepted_even_when_the_daemon_behind_it_is_down() {
            let dir = tempfile::tempdir().unwrap();
            let fast = fake_ssh(dir.path());
            script(
                dir.path(),
                "down-ssh",
                &format!("FAKE_SSH_SERVICE='down' exec '{}' \"$@\"", fast.display()),
            );
            let tunnels = SshTunnels::with_program(dir.path().join("down-ssh"));
            let id = target().connection_id();
            let url = tunnels.start(&id, &target()).await.unwrap();
            assert!(reachable(&url));
        }

        #[tokio::test]
        async fn ssh_select_restarts_a_dead_tunnel() {
            let dir = tempfile::tempdir().unwrap();
            let path = dir.path().join("connections.json");
            let p = proxy();
            let tunnels = SshTunnels::with_program(fake_ssh(dir.path()));
            let conn = add_ssh_connection(&p, &tunnels, &path, "Lab", &target()).await.unwrap();
            let first = p.ssh_local(&conn.id).unwrap();

            tunnels.kill_for_test(&conn.id);
            assert_eq!(tunnels.local_url(&conn.id), None, "a dead child is noticed when asked");
            assert!(!reachable(&first));

            let selected = crate::commands::select_connection(&p, &tunnels, &path, &conn.id).await.unwrap();
            assert_eq!(selected.id, conn.id);
            assert_eq!(p.registry.lock().unwrap().current().id, conn.id);
            let second = p.ssh_local(&conn.id).unwrap();
            assert!(reachable(&second), "restarted");

            // A failing restart leaves the selection where it was.
            tunnels.stop_all();
            let denied = script(dir.path(), "denied", "echo 'host key verification failed' >&2\nexit 255");
            let broken = SshTunnels::with_program(denied);
            p.registry.lock().unwrap().select("local").unwrap();
            let err = crate::commands::select_connection(&p, &broken, &path, &conn.id).await.unwrap_err();
            assert_eq!(err, "host key verification failed");
            assert_eq!(p.registry.lock().unwrap().current().id, "local");
        }
    }
}
