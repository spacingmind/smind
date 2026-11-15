//! Test helper process for tests/hook.rs: installs the hook and panics in the way the mode
//! says, on a real main thread (a test harness thread is not named "main"). Not part of the
//! shell; nothing here is built into the app.
//!
//! usage: crash-child <mode> <crashes dir> [marker file]

use std::path::PathBuf;

use smind_crash_report::store::Config;

fn main() {
    let mut args = std::env::args().skip(1);
    let mode = args.next().unwrap_or_default();
    let dir = PathBuf::from(args.next().unwrap_or_default());
    let marker = args.next().map(PathBuf::from);
    match mode.as_str() {
        "main-panic" => {
            smind_crash_report::install(Config::new(&dir, "child-1"));
            panic!("SECRET-MAIN-TEXT");
        }
        "thread-panic" => {
            smind_crash_report::install(Config::new(&dir, "child-1"));
            let r = std::thread::spawn(|| -> () { panic!("SECRET-THREAD-TEXT") }).join();
            assert!(r.is_err());
        }
        "runtime-panic" => {
            // a runtime panic whose message is on the allow-list: kept
            smind_crash_report::install(Config::new(&dir, "child-1"));
            let v: Vec<u8> = Vec::new();
            let i = std::hint::black_box(7usize);
            let _ = std::thread::spawn(move || v[i]).join();
        }
        "previous-hook" => {
            let marker = marker.expect("marker file");
            std::panic::set_hook(Box::new(move |_| {
                let _ = std::fs::write(&marker, "previous hook ran");
            }));
            smind_crash_report::install(Config::new(&dir, "child-1"));
            let _ = std::thread::spawn(|| -> () { panic!("boom") }).join();
        }
        "unwritable" => {
            // the crashes dir sits under a regular file: every write fails; the process and the
            // previous hook must carry on
            let file = marker.expect("a regular file");
            smind_crash_report::install(Config::new(file.join("crashes"), "child-1"));
            let _ = std::thread::spawn(|| -> () { panic!("outer") }).join();
            println!("CHILD-SURVIVED");
        }
        "unwritable-slow" => {
            // as "unwritable", but the panic comes after the parent has closed its end of our
            // stderr pipe: the write-failure log line must not panic inside the hook
            let file = marker.expect("a regular file");
            smind_crash_report::install(Config::new(file.join("crashes"), "child-1"));
            std::thread::sleep(std::time::Duration::from_millis(600));
            let _ = std::thread::spawn(|| -> () { panic!("outer") }).join();
            println!("CHILD-SURVIVED");
        }
        "no-io" => {
            smind_crash_report::install(Config::new(&dir, "child-1"));
            println!("CHILD-INSTALLED exists={}", dir.exists());
        }
        other => {
            eprintln!("unknown mode {other}");
            std::process::exit(2);
        }
    }
}
