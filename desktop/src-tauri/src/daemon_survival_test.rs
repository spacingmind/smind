//! Opt-in end-to-end test for `daemon-survives-app-update` (U7 of
//! `docs/plans/active/desktop-auto-update.md`) against a REAL `smind serve`.
//! Skips cleanly unless `SMIND_REAL_DAEMON_BIN` points at a built smind:
//!
//! ```text
//! T=$(mktemp -d)
//! (cd <repo> && go build -o $T/smind ./cmd/smind)
//! cd desktop/src-tauri
//! SMIND_REAL_DAEMON_BIN=$T/smind cargo test daemon_survives_app_update -- --nocapture
//! ```
//!
//! The "app" is a stub: this test binary re-invoked as a child process in
//! its own process group (`daemon_survival_stub_app`, a no-op unless
//! `SMIND_STUB_APP_ROLE` is set). The stub starts the daemon with the real
//! `native::spawn_detached` (what `daemon_manager.rs` calls) and records it
//! as managed. The "update" kills the stub's WHOLE process group with
//! SIGKILL (the worst case of the app quitting), swaps the fake bundle's
//! files, and launches a fresh stub, which must find the same daemon pid on
//! the same port with the managed record still classifying as `Managed`.
//! A terminal opened before the update must still be listed and still answer
//! after it. Isolation: its own `SMIND_HOME`, a free loopback port in
//! `SMIND_LISTEN` (the stub refuses to run without it, so it can never touch
//! :4648), a temp base dir; the only processes started (daemon, stub) are
//! killed by pid at the end.
#![cfg(unix)]
// The stub app and the daemon are deliberately never waited on: the test's
// point is that one is SIGKILLed and the other outlives it; `Rig::drop` cleans
// both up by pid.
#![allow(clippy::zombie_processes)]

use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use serde_json::{json, Value};
use smind_daemon_client::daemon_manager::managed::{self, ManagedState};
use smind_daemon_client::daemon_manager::native::{self, Layout};
use std::os::unix::process::CommandExt;
use tokio_tungstenite::tungstenite::Message;

const ROLE: &str = "SMIND_STUB_APP_ROLE";

fn port_from_env() -> u16 {
    let listen = std::env::var("SMIND_LISTEN").expect("the stub app never runs without SMIND_LISTEN (it must not touch :4648)");
    listen.rsplit(':').next().unwrap().parse().unwrap()
}

async fn healthz(port: u16) -> Option<Value> {
    reqwest::get(format!("http://127.0.0.1:{port}/healthz")).await.ok()?.json().await.ok()
}

/// The stub "app": launch (or re-find) the managed daemon, write what it saw
/// to `$STUB_OUT`, then stay alive until killed.
#[tokio::test]
async fn daemon_survival_stub_app() {
    let Ok(role) = std::env::var(ROLE) else { return };
    let port = port_from_env();
    let base = PathBuf::from(std::env::var("STUB_BASE").unwrap());
    let layout = Layout::new(&base);
    let record = managed::load(&layout.state_path);
    let owner = native::find_port_owner(port).unwrap();
    let state = managed::classify(record.as_ref(), owner);
    if state == ManagedState::NotRunning {
        let child = tokio::task::spawn_blocking({
            let layout = layout.clone();
            move || native::spawn_detached(&layout)
        })
        .await
        .unwrap()
        .expect("spawn the daemon");
        let mut up = false;
        for _ in 0..100 {
            if healthz(port).await.is_some() {
                up = true;
                break;
            }
            tokio::time::sleep(Duration::from_millis(100)).await;
        }
        assert!(up, "the daemon did not come up");
        let pid = native::find_port_owner(port).unwrap().expect("port owner");
        assert_eq!(pid, child.id());
        managed::save(&layout.state_path, &managed::take_over(pid, "stub", &layout.bin_path.display().to_string(), "now")).unwrap();
    }
    let record = managed::load(&layout.state_path);
    let owner = native::find_port_owner(port).unwrap();
    let seen = json!({
        "role": role,
        "stubPid": std::process::id(),
        "firstSightState": format!("{state:?}"),
        "state": format!("{:?}", managed::classify(record.as_ref(), owner)),
        "daemonPid": owner,
        "healthy": healthz(port).await.is_some(),
    });
    std::fs::write(base.join(format!("{role}.json")), seen.to_string()).unwrap();
    std::thread::sleep(Duration::from_secs(600));
}

struct Rig {
    base: PathBuf,
    _home: tempfile::TempDir,
    _base_dir: tempfile::TempDir,
    port: u16,
    daemon_pid: Option<u32>,
    stub_pgid: Option<u32>,
    token: PathBuf,
}

impl Drop for Rig {
    fn drop(&mut self) {
        // Only the processes this test started, by pid / process group.
        if let Some(g) = self.stub_pgid {
            let _ = Command::new("kill").args(["-KILL", "--", &format!("-{g}")]).status();
        }
        if let Some(p) = self.daemon_pid {
            let _ = Command::new("kill").args(["-KILL", &p.to_string()]).status();
        }
    }
}

fn launch_stub(rig: &mut Rig, role: &str) -> Child {
    let child = Command::new(std::env::current_exe().unwrap())
        .args(["--exact", "daemon_survival_test::daemon_survival_stub_app", "--nocapture", "--test-threads=1"])
        .env(ROLE, role)
        .env("STUB_BASE", &rig.base)
        .env("SMIND_HOME", rig._home.path())
        .env("SMIND_LISTEN", format!("127.0.0.1:{}", rig.port))
        .env_remove("SMIND_PASSWORD")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .process_group(0)
        .spawn()
        .unwrap();
    rig.stub_pgid = Some(child.id());
    child
}

async fn wait_seen(base: &Path, role: &str) -> Value {
    let f = base.join(format!("{role}.json"));
    for _ in 0..300 {
        if let Ok(s) = std::fs::read_to_string(&f) {
            return serde_json::from_str(&s).unwrap();
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
    panic!("the {role} stub never reported");
}

/// One request over a fresh /ws connection (a new client, like the
/// relaunched app's webview); returns the terminal `result`.
async fn rpc(rig: &Rig, method: &str, params: Value) -> Value {
    let token = std::fs::read_to_string(&rig.token).unwrap().trim().to_string();
    let (mut ws, _) = tokio_tungstenite::connect_async(format!("ws://127.0.0.1:{}/ws?token={token}", rig.port)).await.unwrap();
    ws.send(Message::Text(json!({"id": "1", "method": method, "params": params}).to_string().into())).await.unwrap();
    loop {
        let msg = tokio::time::timeout(Duration::from_secs(10), ws.next()).await.expect("rpc timeout").unwrap().unwrap();
        if let Message::Text(t) = msg {
            let v: Value = serde_json::from_str(&t).unwrap();
            if v["id"] == "1" && v.get("event").is_none() {
                assert!(v.get("error").is_none() || v["error"].is_null(), "{method}: {v}");
                return v["result"].clone();
            }
        }
    }
}

#[tokio::test]
async fn daemon_survives_app_update() {
    let Ok(bin) = std::env::var("SMIND_REAL_DAEMON_BIN") else {
        eprintln!("daemon_survives_app_update: SMIND_REAL_DAEMON_BIN not set; skipping");
        return;
    };
    let port = TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port();
    assert_ne!(port, 4648);
    let home = tempfile::tempdir().unwrap();
    let base_dir = tempfile::tempdir().unwrap();
    let base = base_dir.path().to_path_buf();
    // The managed install layout the app uses: base/bin/smind.
    std::fs::create_dir_all(base.join("bin")).unwrap();
    std::fs::copy(&bin, base.join("bin/smind")).unwrap();
    // The fake bundle ("app 0.10.0").
    let bundle = base.join("smind.app");
    std::fs::create_dir_all(bundle.join("Contents")).unwrap();
    std::fs::write(bundle.join("Contents/Info.plist"), "0.10.0").unwrap();
    // A git repo for a workspace/task/terminal.
    let repo = base.join("repo");
    std::fs::create_dir_all(&repo).unwrap();
    for args in [vec!["init", "-q", "-b", "main"], vec!["-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]] {
        assert!(Command::new("git").args(&args).current_dir(&repo).status().unwrap().success());
    }
    let mut rig = Rig { token: home.path().join("token"), base, _home: home, _base_dir: base_dir, port, daemon_pid: None, stub_pgid: None };

    // --- app 0.10.0 starts the managed daemon ---------------------------
    let _app_old = launch_stub(&mut rig, "old");
    let old = wait_seen(&rig.base, "old").await;
    assert_eq!(old["state"], "Managed", "{old}");
    let daemon_pid = old["daemonPid"].as_u64().unwrap() as u32;
    rig.daemon_pid = Some(daemon_pid);
    assert_ne!(daemon_pid as u64, old["stubPid"].as_u64().unwrap());

    // Work in flight: a terminal running a long command in a real task.
    let ws = rpc(&rig, "workspace.create", json!({"path": repo.display().to_string()})).await;
    let task = rpc(&rig, "task.create", json!({"workspaceId": ws["ID"], "title": "survive"})).await;
    let task_id = task["ID"].as_i64().unwrap();
    let term = rpc(&rig, "terminal.create", json!({"taskId": task_id})).await;
    let term_id = term["terminalId"].as_str().unwrap().to_string();
    // Started before the update, finishes after it: it only completes if the
    // daemon (and so the PTY) lives through the whole sequence.
    let marker = rig.base.join("alive.txt");
    let after = rig.base.join("after.txt");
    rpc(&rig, "terminal.write", json!({"terminalId": term_id, "data": format!("echo before > {}; sleep 8; echo after > {}\n", marker.display(), after.display())})).await;
    for _ in 0..100 {
        if marker.exists() {
            break;
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
    assert!(marker.exists(), "the terminal ran its command");
    assert!(!after.exists(), "the long command is still in flight");

    // --- the app update: kill the whole app group, swap the bundle ---------
    let pgid = rig.stub_pgid.take().unwrap();
    assert!(Command::new("kill").args(["-KILL", "--", &format!("-{pgid}")]).status().unwrap().success());
    tokio::time::sleep(Duration::from_millis(500)).await;
    std::fs::write(rig.base.join("smind.app/Contents/Info.plist.new"), "0.10.1").unwrap();
    std::fs::rename(bundle.join("Contents/Info.plist.new"), bundle.join("Contents/Info.plist")).unwrap();
    assert_eq!(std::fs::read_to_string(bundle.join("Contents/Info.plist")).unwrap(), "0.10.1");
    assert!(native::is_pid_alive(daemon_pid), "the daemon outlived the app group's SIGKILL");

    // --- relaunch (only the shell) ----------------------------------------
    let _app_new = launch_stub(&mut rig, "new");
    let new = wait_seen(&rig.base, "new").await;
    assert_eq!(new["firstSightState"], "Managed", "the relaunched app adopts, not reinstalls: {new}");
    assert_eq!(new["daemonPid"].as_u64().unwrap() as u32, daemon_pid, "same daemon pid");
    assert_eq!(new["healthy"], true);
    assert_eq!(native::find_port_owner(rig.port).unwrap(), Some(daemon_pid), "same port owner");

    // The terminal opened before the update is still there and still runs.
    let listed = rpc(&rig, "terminal.list", json!({"taskId": task_id})).await;
    assert!(listed.to_string().contains(&term_id), "terminal survived: {listed}");
    for _ in 0..200 {
        if after.exists() {
            break;
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
    assert!(after.exists(), "the in-flight terminal command finished after the update");
}
