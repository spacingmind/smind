//! Windows-native daemon management (ADR-0030): start `smind.exe serve`
//! detached with no console window, find who owns a port, and stop the
//! daemon via its API with a terminate fallback -- mirroring `native.rs`'s
//! shape so `src-tauri` can wire both hosts the same way. The binary itself
//! is the sidecar the installer ships; `src-tauri` copies it into
//! `Layout::bin_path` (nothing is downloaded here). Parameterized on
//! a base directory (normally `%LOCALAPPDATA%\smind`) so tests run against
//! a temp dir.
//!
//! No `netstat`, no `tasklist`: the port owner comes from the IP Helper
//! TCP table and a pid's image from `QueryFullProcessImageNameW`. The app
//! never signals a process it cannot positively identify as the managed
//! `smind.exe` (`check_termination_target` refuses without a managed
//! record, a matching live port owner, and a matching image path).

use std::fs::{self, File};
use std::io;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use windows_sys::Win32::Foundation::{CloseHandle, ERROR_INSUFFICIENT_BUFFER, GetLastError};
use windows_sys::Win32::NetworkManagement::IpHelper::{
    GetExtendedTcpTable, MIB_TCP6ROW_OWNER_PID, MIB_TCP6TABLE_OWNER_PID, MIB_TCPROW_OWNER_PID, MIB_TCPTABLE_OWNER_PID, TCP_TABLE_OWNER_PID_ALL,
};
use windows_sys::Win32::System::Threading::{
    CREATE_BREAKAWAY_FROM_JOB, CREATE_NEW_PROCESS_GROUP, CREATE_NO_WINDOW, GetExitCodeProcess, OpenProcess, PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_TERMINATE, QueryFullProcessImageNameW, TerminateProcess,
};

use super::managed::{self, ManagedRecord};

/// winnt.h: a process exit code of 259 means "still running".
const STILL_ACTIVE: u32 = 259;
/// A TCP row in LISTEN state.
const MIB_TCP_STATE_LISTEN: u32 = 2;
/// winnt.h address families (as GetExtendedTcpTable's `ulAf` wants them).
const AF_INET: u32 = 2;
const AF_INET6: u32 = 23;
const ERROR_ACCESS_DENIED: u32 = 5;

#[derive(Debug, Clone)]
pub struct Layout {
    pub bin_path: PathBuf,
    pub log_path: PathBuf,
    pub state_path: PathBuf,
}

impl Layout {
    pub fn new(base_dir: &Path) -> Self {
        Self { bin_path: base_dir.join("bin").join("smind.exe"), log_path: base_dir.join("smind.log"), state_path: base_dir.join("managed.json") }
    }
}

fn installed_at_now() -> String {
    SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs().to_string()).unwrap_or_default()
}

/// spawn_detached starts `layout.bin_path serve` with no console window,
/// detached from this process (its own process group; it breaks out of the
/// app's job object when allowed), stdin null and stdout/stderr appended
/// to `smind.log`, then records pid/version/install time/image path in
/// `managed.json`. See `native::spawn_detached` for the matching macOS
/// shape; the daemon picks its port from its own config, so like there,
/// `serve` takes no arguments.
pub fn spawn_detached(layout: &Layout, version: &str) -> io::Result<Child> {
    let log_out = File::options().create(true).append(true).open(&layout.log_path)?;
    let log_err = log_out.try_clone()?;

    let mut cmd = Command::new(&layout.bin_path);
    cmd.arg("serve")
        .stdout(Stdio::from(log_out))
        .stderr(Stdio::from(log_err))
        .stdin(Stdio::null());
    {
        use std::os::windows::process::CommandExt;
        // CREATE_NO_WINDOW without DETACHED_PROCESS: the daemon gets a
        // *hidden* console that every console program it starts (git,
        // agent CLIs) inherits. With DETACHED_PROCESS (which also overrides
        // CREATE_NO_WINDOW) the daemon has no console and each of those
        // children allocates a fresh one -- a conhost.exe and, on a real
        // desktop, a flashing window per git call (seen in the AC10 run:
        // new conhost.exe processes for every task created). The daemon
        // still outlives the app: nothing ties it to the app's lifetime.
        let base = CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP;
        // Breaking away from the app's job object lets the daemon survive
        // the app being killed by a job-terminating launcher; jobs that
        // disallow breakaway reject the spawn with access denied, so retry
        // without it.
        cmd.creation_flags(base | CREATE_BREAKAWAY_FROM_JOB);
        let child = match cmd.spawn() {
            Err(e) if e.raw_os_error() == Some(ERROR_ACCESS_DENIED as i32) => {
                cmd.creation_flags(base);
                cmd.spawn()?
            }
            other => other?,
        };
        let record = ManagedRecord {
            pid: child.id(),
            version: version.to_string(),
            installed_at: installed_at_now(),
            exe_path: layout.bin_path.display().to_string(),
        };
        managed::save(&layout.state_path, &record)?;
        Ok(child)
    }
}

fn tcp_table_owner_pids(family: u32, port: u16) -> io::Result<Vec<u32>> {
    // dwLocalPort is network byte order in the low 16 bits.
    let target_port = port.swap_bytes() as u32;
    let mut size: u32 = 0;
    unsafe {
        // First call sizes the buffer; it always "fails" with
        // ERROR_INSUFFICIENT_BUFFER, which is not an error here.
        GetExtendedTcpTable(std::ptr::null_mut(), &mut size, 0, family, TCP_TABLE_OWNER_PID_ALL, 0);
    }
    // The table can grow between the sizing call and the read (a socket
    // opened in between), so retry with the size the API reports. The buffer
    // is u32-backed because the rows are u32 structs; a Vec<u8> carries no
    // alignment guarantee for them.
    let mut buf: Vec<u32>;
    let mut attempts = 0;
    loop {
        buf = vec![0u32; (size as usize).div_ceil(4)];
        let rc = unsafe { GetExtendedTcpTable(buf.as_mut_ptr().cast(), &mut size, 0, family, TCP_TABLE_OWNER_PID_ALL, 0) };
        if rc == 0 {
            break;
        }
        attempts += 1;
        if rc != ERROR_INSUFFICIENT_BUFFER || attempts >= 5 {
            return Err(io::Error::other(format!("GetExtendedTcpTable failed with error {rc}")));
        }
    }
    let buf_len = buf.len() * std::mem::size_of::<u32>();
    let mut pids: Vec<u32> = Vec::new();
    if family == AF_INET {
        // Table header (entry count) followed by fixed-size rows; validate
        // the buffer is large enough for the declared rows before reading.
        let table = buf.as_ptr().cast::<MIB_TCPTABLE_OWNER_PID>();
        let count = unsafe { (*table).dwNumEntries } as usize;
        if buf_len < std::mem::size_of::<u32>() + count * std::mem::size_of::<MIB_TCPROW_OWNER_PID>() {
            return Err(io::Error::other("GetExtendedTcpTable returned a truncated IPv4 table"));
        }
        let rows = unsafe { std::slice::from_raw_parts((&(*table).table) as *const MIB_TCPROW_OWNER_PID, count) };
        for row in rows {
            if row.dwState == MIB_TCP_STATE_LISTEN && (row.dwLocalPort & 0xffff) == target_port && !pids.contains(&row.dwOwningPid) {
                pids.push(row.dwOwningPid);
            }
        }
    } else {
        let table = buf.as_ptr().cast::<MIB_TCP6TABLE_OWNER_PID>();
        let count = unsafe { (*table).dwNumEntries } as usize;
        if buf_len < std::mem::size_of::<u32>() + count * std::mem::size_of::<MIB_TCP6ROW_OWNER_PID>() {
            return Err(io::Error::other("GetExtendedTcpTable returned a truncated IPv6 table"));
        }
        let rows = unsafe { std::slice::from_raw_parts((&(*table).table) as *const MIB_TCP6ROW_OWNER_PID, count) };
        for row in rows {
            if row.dwState == MIB_TCP_STATE_LISTEN && (row.dwLocalPort & 0xffff) == target_port && !pids.contains(&row.dwOwningPid) {
                pids.push(row.dwOwningPid);
            }
        }
    }
    Ok(pids)
}

/// find_port_owner asks the IP Helper TCP table (listener rows, IPv4 and
/// IPv6) which pid is bound to `port` -- the same primitive `lsof`
/// provides on macOS, with no `netstat` child process.
pub fn find_port_owner(port: u16) -> io::Result<Option<u32>> {
    if let Some(pid) = tcp_table_owner_pids(AF_INET, port)?.first() {
        return Ok(Some(*pid));
    }
    Ok(tcp_table_owner_pids(AF_INET6, port)?.first().copied())
}

/// find_port_owners is every pid with a listener row for `port` (IPv4 rows
/// first, then IPv6), without duplicates. An SSH tunnel's local port is
/// trusted only when ALL of them are our own ssh child: Windows lets a later
/// bind of the same port coexist with ssh's, and the first row alone would
/// not show it.
pub fn find_port_owners(port: u16) -> io::Result<Vec<u32>> {
    let mut pids = tcp_table_owner_pids(AF_INET, port)?;
    for pid in tcp_table_owner_pids(AF_INET6, port)? {
        if !pids.contains(&pid) {
            pids.push(pid);
        }
    }
    Ok(pids)
}

/// exe_path_for_pid resolves the image a pid was launched from via
/// `QueryFullProcessImageNameW` (buffer grown past MAX_PATH as needed) --
/// no `tasklist`. Gone or inaccessible processes are `Ok(None)`.
pub fn exe_path_for_pid(pid: u32) -> io::Result<Option<String>> {
    // PROCESS_QUERY_LIMITED_INFORMATION is enough for the image name and
    // works for processes we did not start.
    let handle = unsafe { OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid) };
    if handle.is_null() {
        return Ok(None);
    }
    let mut len: u32 = 1024;
    loop {
        let mut buf = vec![0u16; len as usize];
        let ok = unsafe { QueryFullProcessImageNameW(handle, 0, buf.as_mut_ptr(), &mut len) };
        if ok != 0 {
            let path = String::from_utf16_lossy(&buf[..len as usize]);
            unsafe { CloseHandle(handle) };
            return Ok(Some(path));
        }
        let err = unsafe { GetLastError() };
        if err == ERROR_INSUFFICIENT_BUFFER {
            len = len.saturating_mul(2);
            continue;
        }
        unsafe { CloseHandle(handle) };
        return Ok(None);
    }
}

/// exe_matches compares a resolved image path against the expected one.
/// Both are absolute; windows path comparison is case-insensitive.
pub fn exe_matches(actual: &str, expected: &Path) -> bool {
    actual.trim().eq_ignore_ascii_case(&expected.display().to_string())
}

pub fn is_pid_alive(pid: u32) -> bool {
    let handle = unsafe { OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid) };
    if handle.is_null() {
        return false;
    }
    let mut exit_code: u32 = 0;
    let ok = unsafe { GetExitCodeProcess(handle, &mut exit_code) };
    unsafe { CloseHandle(handle) };
    ok != 0 && exit_code == STILL_ACTIVE
}

/// kill_process terminates a pid. Only ever called after
/// `check_termination_target` positively identified it as the managed
/// smind.exe -- never call this on an unverified pid.
pub fn kill_process(pid: u32) -> io::Result<()> {
    let handle = unsafe { OpenProcess(PROCESS_TERMINATE, 0, pid) };
    if handle.is_null() {
        return Err(io::Error::other(format!("cannot open process {pid} for termination (error {})", unsafe { GetLastError() })));
    }
    let ok = unsafe { TerminateProcess(handle, 1) };
    let err = if ok == 0 { Some(unsafe { GetLastError() }) } else { None };
    unsafe { CloseHandle(handle) };
    match err {
        None => Ok(()),
        Some(err) => Err(io::Error::other(format!("terminating process {pid} failed with error {err}"))),
    }
}

/// token_path is where the daemon persists its bearer token
/// (`internal/auth`): `$SMIND_HOME/token`, else `~/.smind/token`, matching
/// `internal/config.Dir`.
pub fn token_path() -> PathBuf {
    if let Ok(home) = std::env::var("SMIND_HOME") {
        if !home.is_empty() {
            return Path::new(&home).join("token");
        }
    }
    let profile = std::env::var("USERPROFILE").unwrap_or_default();
    Path::new(&profile).join(".smind").join("token")
}

pub fn read_token(path: &Path) -> io::Result<String> {
    Ok(fs::read_to_string(path)?.trim().to_string())
}

/// check_termination_target decides whether the recorded pid may be
/// terminated: the live port owner must still be exactly that pid, and its
/// resolved image must be the record's `smind.exe` (`managed::safe_to_kill`
/// with the identity check computed here). Anything less -- no record, a
/// different owner, an unresolvable image -- refuses with an error; the
/// app never terminates a process it cannot positively identify.
pub fn check_termination_target(layout: &Layout, port: u16) -> Result<u32, String> {
    let Some(record) = managed::load(&layout.state_path) else {
        return Err("no managed daemon record -- refusing to terminate an unidentified process".to_string());
    };
    let owner = find_port_owner(port).map_err(|e| format!("cannot determine the owner of port {port}: {e}"))?;
    let exe_matches = match owner {
        Some(pid) => match exe_path_for_pid(pid) {
            Ok(Some(image)) => exe_matches(&image, Path::new(&record.exe_path)),
            Ok(None) => false,
            Err(e) => return Err(format!("cannot resolve the image of pid {pid}: {e}")),
        },
        None => false,
    };
    if managed::safe_to_kill(&record, owner, exe_matches) {
        Ok(record.pid)
    } else {
        Err(format!(
            "refusing to terminate pid {}: the process on port {port} could not be positively identified as the managed smind.exe",
            record.pid
        ))
    }
}

/// terminate_fallback is stop()'s last resort after the daemon ignored
/// `POST /api/shutdown` past the timeout: terminate the recorded pid, but
/// only once `check_termination_target` verified it.
pub fn terminate_fallback(layout: &Layout, port: u16) -> Result<u32, String> {
    let pid = check_termination_target(layout, port)?;
    kill_process(pid).map_err(|e| format!("terminating the managed daemon (pid {pid}) failed: {e}"))?;
    Ok(pid)
}

/// stop asks the daemon to shut down (`POST /api/shutdown` with its bearer
/// token), then polls `/healthz` until it stops answering. Only if the
/// daemon is still answering past `timeout` does it fall back to
/// terminating the recorded pid -- behind the identity check above. A
/// daemon that was never answering returns Ok immediately.
pub async fn stop(client: &reqwest::Client, layout: &Layout, port: u16, token: &str, timeout: Duration) -> Result<(), String> {
    let base = format!("http://127.0.0.1:{port}");
    // Best-effort: a dead daemon fails here, and the healthz poll below
    // confirms it is down.
    let _ = client.post(format!("{base}/api/shutdown")).bearer_auth(token).send().await;
    let deadline = Instant::now() + timeout;
    loop {
        let answering = client.get(format!("{base}/healthz")).timeout(Duration::from_millis(500)).send().await.is_ok();
        if !answering {
            return Ok(());
        }
        if Instant::now() >= deadline {
            break;
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
    terminate_fallback(layout, port).map(|_| ())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::TcpListener;
    use std::sync::atomic::{AtomicBool, Ordering};

    #[test]
    fn exe_matches_is_case_insensitive() {
        assert!(exe_matches(" C:\\Users\\Me\\AppData\\Local\\smind\\Bin\\SMIND.EXE ", Path::new(r"C:\Users\Me\AppData\Local\smind\bin\smind.exe")));
        assert!(!exe_matches(r"C:\Windows\System32\svchost.exe", Path::new(r"C:\local\smind\bin\smind.exe")));
    }

    #[test]
    fn port_owner_via_iphelper_finds_listening_pid() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let owner = find_port_owner(port).unwrap();
        assert_eq!(owner, Some(std::process::id()));
        drop(listener);
    }

    #[test]
    fn exe_path_for_pid_returns_image_path() {
        let pid = std::process::id();
        let image = exe_path_for_pid(pid).unwrap().expect("own pid must resolve");
        let current = std::env::current_exe().unwrap();
        assert!(exe_matches(&image, &current), "resolved {image:?}, running {current:?}");
    }

    #[test]
    fn smind_spawn_detached_helper() {
        // Helper entry for windows_spawn_detached_has_no_console_and_
        // survives_parent: when invoked as the test binary with
        // SMIND_TEST_HELPER_BIN/ROOT set, spawn the daemon via the real
        // spawn_detached and exit, leaving the daemon parentless.
        let Ok(bin) = std::env::var("SMIND_TEST_HELPER_BIN") else { return };
        let root = PathBuf::from(std::env::var("SMIND_TEST_HELPER_ROOT").unwrap());
        assert!(Path::new(&bin).exists());
        let layout = Layout::new(&root);
        spawn_detached(&layout, "0.7.0").expect("smind desktop: helper must spawn the daemon");
        std::process::exit(0);
    }

    #[test]
    fn windows_spawn_detached_has_no_console_and_survives_parent() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path().to_path_buf();
        let layout = Layout::new(&root);
        let bin_dir = layout.bin_path.parent().unwrap();
        fs::create_dir_all(bin_dir).unwrap();

        // A stand-in smind.exe: `waitfor serve` blocks until signaled, so
        // the spawned "daemon" stays alive for inspection.
        let system_root = std::env::var("SystemRoot").unwrap_or_else(|_| r"C:\Windows".to_string());
        let waitfor = Path::new(&system_root).join("System32").join("waitfor.exe");
        if !waitfor.exists() {
            eprintln!("smind desktop: {waitfor:?} not present, skipping");
            return;
        }
        fs::copy(&waitfor, &layout.bin_path).unwrap();

        // The helper process (this very test binary, filtered to the
        // marker test) spawns the daemon via spawn_detached and dies; the
        // daemon must survive it.
        let mut helper = Command::new(std::env::current_exe().unwrap());
        helper.arg("smind_spawn_detached_helper").arg("-q");
        helper.env("SMIND_TEST_HELPER_BIN", &layout.bin_path).env("SMIND_TEST_HELPER_ROOT", &root);
        crate::windows_process::no_window(&mut helper);
        let status = helper.status().expect("smind desktop: helper must spawn");
        assert!(status.success(), "helper exited with {status}");

        let record = managed::load(&layout.state_path).expect("spawn_detached wrote managed.json");
        let pid = record.pid;
        assert!(is_pid_alive(pid), "daemon died with its parent");
        assert_eq!(record.version, "0.7.0");
        assert!(exe_matches(&record.exe_path, &layout.bin_path));
        let image = exe_path_for_pid(pid).unwrap().expect("daemon image must resolve");
        assert!(exe_matches(&image, &layout.bin_path), "resolved {image:?}");
        assert!(layout.log_path.exists());

        // Cleanup: kill the daemon we spawned so the test never leaks a
        // process even if it stays detached from this one.
        kill_process(pid).unwrap();
        for _ in 0..50 {
            if !is_pid_alive(pid) {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        assert!(!is_pid_alive(pid), "spawned daemon was not cleaned up");
    }

    async fn fake_daemon_fixture(token: &'static str) -> (u16, std::sync::Arc<AtomicBool>, tokio::task::JoinHandle<()>) {
        use axum::http::HeaderMap;
        use axum::routing::{get, post};
        use axum::Router;

        let shutdown_seen = std::sync::Arc::new(AtomicBool::new(false));
        let flag = shutdown_seen.clone();
        let app = Router::new()
            .route("/healthz", get(|| async { "ok" }))
            .route(
                "/api/shutdown",
                post(move |headers: HeaderMap| {
                    let flag = flag.clone();
                    async move {
                        let authorized = headers
                            .get(axum::http::header::AUTHORIZATION)
                            .and_then(|v| v.to_str().ok())
                            .map(|v| v == format!("Bearer {token}"))
                            .unwrap_or(false);
                        if authorized {
                            flag.store(true, Ordering::SeqCst);
                            (axum::http::StatusCode::ACCEPTED, "")
                        } else {
                            (axum::http::StatusCode::UNAUTHORIZED, "")
                        }
                    }
                }),
            );

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        let handle = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        (port, shutdown_seen, handle)
    }

    #[tokio::test]
    async fn windows_stop_uses_api_shutdown_then_waits_for_healthz_down() {
        let (port, shutdown_seen, server) = fake_daemon_fixture("test-token").await;
        let dir = tempfile::tempdir().unwrap();
        let layout = Layout::new(dir.path());
        let client = reqwest::Client::new();

        let stop_task = tokio::spawn(async move { stop(&client, &layout, port, "test-token", Duration::from_secs(5)).await });
        for _ in 0..100 {
            if shutdown_seen.load(Ordering::SeqCst) {
                break;
            }
            tokio::time::sleep(Duration::from_millis(25)).await;
        }
        assert!(shutdown_seen.load(Ordering::SeqCst), "stop must call POST /api/shutdown with the bearer token");
        // The daemon honors the request and goes away: the port stops
        // answering, which is exactly what stop() is polling for.
        server.abort();
        tokio::time::sleep(Duration::from_millis(150)).await;
        stop_task.await.unwrap().expect("stop should succeed once healthz stops answering");
    }

    /// A listener bound in this process (and kept alive by the caller) so
    /// the port's owner is this test's pid.
    fn bound_port_in_this_process() -> (u16, TcpListener) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        (port, listener)
    }

    fn managed_record(layout: &Layout, pid: u32, exe_path: &str) {
        let record = ManagedRecord { pid, version: "0.7.0".to_string(), installed_at: "0".to_string(), exe_path: exe_path.to_string() };
        managed::save(&layout.state_path, &record).unwrap();
    }

    #[test]
    fn windows_stop_falls_back_to_terminate_only_with_identity_match() {
        // This process owns the port (the in-process listener) and is its
        // own recorded pid with a matching image: the fallback decision
        // must resolve to exactly this pid -- and nothing else can ever be
        // targeted instead. (stop() itself is not driven to the kill here:
        // the matched pid is the test process.)
        let dir = tempfile::tempdir().unwrap();
        let layout = Layout::new(dir.path());
        let (port, _listener) = bound_port_in_this_process();
        let me = std::process::id();
        let exe = std::env::current_exe().unwrap().display().to_string();
        managed_record(&layout, me, &exe);
        assert_eq!(check_termination_target(&layout, port), Ok(me));
    }

    #[test]
    fn windows_stop_refuses_to_terminate_unidentified_process() {
        let dir = tempfile::tempdir().unwrap();
        let layout = Layout::new(dir.path());
        let (port, _listener) = bound_port_in_this_process();
        let me = std::process::id();
        let exe = std::env::current_exe().unwrap().display().to_string();

        // No record at all: refuse.
        let err = check_termination_target(&layout, port).unwrap_err();
        assert!(err.contains("refusing"), "{err}");

        // Recorded image does not match the live owner's image: refuse.
        managed_record(&layout, me, r"C:\somewhere\else\smind.exe");
        let err = check_termination_target(&layout, port).unwrap_err();
        assert!(err.contains("refusing"), "{err}");

        // Recorded pid is not the live port owner (pid reuse / stale
        // record): refuse even with a matching recorded path.
        managed_record(&layout, me + 40_000_000, &exe);
        let err = check_termination_target(&layout, port).unwrap_err();
        assert!(err.contains("refusing"), "{err}");
    }
}
