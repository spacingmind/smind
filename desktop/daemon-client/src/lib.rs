//! Pure-logic daemon client for the smind desktop shell: daemon URL
//! resolution, wsapi wire parsing, notification shaping, and reconnect
//! backoff. GUI-free so `cargo test` runs on machines without
//! webkit2gtk; the tauri crate wires these into the connection loop.

pub mod backoff;
pub mod config;
pub mod protocol;

pub use backoff::Backoff;
pub use config::{daemon_url, Config};
pub use protocol::{Notification, ServerMessage};
