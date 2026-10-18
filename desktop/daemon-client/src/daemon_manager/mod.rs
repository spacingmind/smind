//! The app-managed local daemon (ADR-0013 part D2, ADR-0030): install,
//! update and restart a daemon the app itself started, natively on
//! Windows and macOS. See `docs/plans/completed/desktop-managed-daemon.md`.
//!
//! Pure, OS-agnostic logic (version comparison, checksum verification,
//! release asset naming, the managed/unmanaged decision table) lives here
//! so it's unit-testable without a real daemon or a real macOS/Windows
//! host. The thin OS-calling layer (spawning the daemon process, actually
//! hitting the network) lives in `native`/`windows_native` alongside the
//! logic it drives, kept separable so tests can inject fakes.

pub mod checksums;
pub mod managed;
pub mod native;
pub mod release;
pub mod version;
#[cfg(windows)]
pub mod windows_native;

pub use managed::{ManagedRecord, ManagedState};
pub use release::{ReleaseError, ReleaseUrls};
pub use version::{classify, compare, Comparison, VersionKind};
