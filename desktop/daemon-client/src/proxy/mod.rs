//! The desktop bundled-UI loopback proxy (ADR-0013): a Rust-side HTTP
//! server that serves the bundled `web/packages/ui` build and
//! reverse-proxies `/api/*` + `/ws` to whichever connection is
//! currently selected, gated by a per-launch secret cookie. See
//! `docs/decisions/0013-desktop-bundled-ui.md` and
//! `docs/plans/active/desktop-bundled-ui.md`.

pub mod connections;
pub mod secret;
pub mod server;

pub use connections::{Connection, ConnectionKind, Registry};
pub use server::{serve, AssetSource, ProxyState};
