//! Local, redacted, bounded crash reports for the smind desktop shell: the Rust half of
//! `internal/crash` (docs/plans/active/crash-capture.md and crash-capture-desktop.md).
//! Reports stay on this machine; this crate has no network code and spawns no process.

pub mod redact;
