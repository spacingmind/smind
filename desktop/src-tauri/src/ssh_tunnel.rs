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
            // the child to still be running. Residual window, not closed here:
            // a local process that binds the picked port before ssh has
            // authenticated would pass the connect check for a moment (a
            // listener cannot be told apart from ssh's); ExitOnForwardFailure
            // then makes ssh exit, which this loop reports on its next pass.
            if TcpStream::connect_timeout(&addr, Duration::from_millis(200)).is_ok() && tunnel.alive() {
                break;
            }
            if Instant::now() >= deadline {
                let detail = String::from_utf8_lossy(&stderr.lock().unwrap()).trim().to_string();
                return Err(if detail.is_empty() { "the SSH tunnel did not come up in time".to_string() } else { detail });
            }
            tokio::time::sleep(POLL_INTERVAL).await;
        }
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
        let mut tunnels = self.tunnels.lock().unwrap();
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
    proxy.set_ssh_local(&conn.id, url);
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
    let saved = {
        let mut registry = proxy.registry.lock().unwrap();
        registry.add_ssh(label, &target).and_then(|conn| registry.save(path).map(|_| conn).map_err(|e| e.to_string()))
    };
    match saved {
        Ok(conn) => {
            proxy.set_ssh_local(&id, url);
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
            let listener = TcpListener::bind(("127.0.0.1", port)).unwrap();
            for conn in listener.incoming() {
                drop(conn);
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
