//! Local, redacted, bounded crash reports for the smind desktop shell: the Rust half of
//! `internal/crash` (docs/plans/active/crash-capture.md and crash-capture-desktop.md).
//! Reports stay on this machine; this crate has no network code and spawns no process.
//!
//! [`install`] stores the configuration and sets a panic hook; installing does no disk I/O. The
//! hook records the panic (the directory is created by the first report), then calls the hook
//! that was installed before it, so the default message, Tauri's own handling and any test
//! harness keep working. The hook does real work on the panicking thread: it captures a
//! backtrace (which resolves symbols), redacts it and writes one small file. That is bounded
//! (the stack is cut before redaction, the report is capped, a second report arriving meanwhile
//! is dropped), not zero.

// A panic inside a panic hook aborts the process, so the code the hook runs must not be able to
// panic: these lints are the guard (tests are exempt; the static regexes are validated by the
// golden test and carry an explicit allow).
#![cfg_attr(
    not(test),
    deny(
        clippy::unwrap_used,
        clippy::expect_used,
        clippy::indexing_slicing,
        clippy::panic,
        clippy::unreachable,
        clippy::todo,
        clippy::unimplemented
    )
)]

pub mod redact;
pub mod report;
pub mod store;

use std::panic::{self, AssertUnwindSafe, PanicHookInfo};
use std::path::PathBuf;
use std::sync::{Arc, Mutex, Once};

use store::{Config, Recorder};

static RECORDER: Mutex<Option<Arc<Recorder>>> = Mutex::new(None);
static HOOK: Once = Once::new();

/// A panic raised INSIDE a panic hook aborts the process ("thread panicked while processing
/// panic"); `catch_unwind` cannot stop that. So nothing on the hook path may panic: the crate
/// denies `unwrap`, `expect`, indexing and `panic!` outside tests (see the lint attributes
/// below), and the static regexes are checked by the golden test.
///
/// Installs the panic hook with `cfg`. Safe to call more than once (the latest configuration
/// wins; the hook is set once). No I/O, never panics.
pub fn install(cfg: Config) {
    let _ = panic::catch_unwind(AssertUnwindSafe(|| {
        // Compile every regex now, outside any hook (a panic in a hook aborts the process).
        redact::warm_up();
        report::warm_up();
        redact::prime(cfg.home.as_deref(), cfg.smind_home.as_deref());
        let rec = Arc::new(Recorder::new(cfg));
        *RECORDER.lock().unwrap_or_else(|e| e.into_inner()) = Some(rec);
        HOOK.call_once(|| {
            let prev = panic::take_hook();
            panic::set_hook(Box::new(move |info| {
                record_from_hook(info);
                prev(info);
            }));
        });
    }));
}

/// [`install`] for the shell: the reports go to the app data crashes directory
/// ([`report::default_dir`]); `smind_home` for redaction is `$SMIND_HOME` or `~/.smind`.
/// When no usable directory can be derived (no home directory, only relative bases) nothing is
/// installed and one line says so on stderr: a shell that silently records nothing would look
/// like a shell that never crashes.
pub fn install_default(version: &str) {
    if install_default_with(report::default_dir(), version).is_err() {
        // `eprintln!` panics on a closed stderr; this runs at startup but keep the rule: write
        // and ignore the result.
        let _ = std::io::Write::write_all(
            &mut std::io::stderr(),
            b"smind-crash-report: no usable application data directory (is HOME set?); crash reports are off\n",
        );
    }
}

/// [`install_default`] with the directory given: `Err` when there is none.
pub fn install_default_with(dir: Option<PathBuf>, version: &str) -> Result<(), &'static str> {
    let dir = dir.ok_or("no usable application data directory")?;
    let mut cfg = Config::new(dir, version);
    cfg.smind_home = std::env::var_os("SMIND_HOME")
        .map(PathBuf::from)
        .or_else(|| cfg.home.as_ref().map(|h| h.join(".smind")));
    install(cfg);
    Ok(())
}

fn record_from_hook(info: &PanicHookInfo<'_>) {
    let _ = panic::catch_unwind(AssertUnwindSafe(|| {
        let Some(rec) = RECORDER.lock().unwrap_or_else(|e| e.into_inner()).clone() else {
            return;
        };
        // The site is a constant: never the thread name or any panic text.
        let site = if std::thread::current().name() == Some("main") {
            "desktop.main"
        } else {
            "desktop.thread"
        };
        let payload = info.payload();
        let (panic_type, message) = if let Some(s) = payload.downcast_ref::<&str>() {
            ("&str", Some((*s).to_string()))
        } else if let Some(s) = payload.downcast_ref::<String>() {
            ("String", Some(s.clone()))
        } else {
            ("other", None)
        };
        let stack = std::backtrace::Backtrace::force_capture().to_string();
        rec.record(site, panic_type, message.as_deref(), &stack);
    }));
}
