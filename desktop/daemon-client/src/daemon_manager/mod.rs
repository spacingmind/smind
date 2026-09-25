//! The app-managed local daemon (ADR-0013 part D2): install, update and
//! restart a daemon the app itself started, on Windows+WSL2 and macOS
//! native. See `docs/plans/active/desktop-managed-daemon.md`.
//!
//! Pure, OS-agnostic logic (version comparison, checksum verification,
//! release asset naming, the managed/unmanaged decision table, WSL2 argv
//! building) lives here so it's unit-testable without a real daemon, a
//! real WSL distro, or a real macOS host. The thin OS-calling layer
//! (spawning `wsl.exe`, spawning the daemon process, actually hitting the
//! network) lives in `native`/`wsl` alongside the logic it drives, kept
//! separable so tests can inject fakes.

pub mod version;

pub use version::{classify, compare, Comparison, VersionKind};
