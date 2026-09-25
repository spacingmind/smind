//! The bundled UI's static assets (AC1), embedded into the binary at
//! compile time from `desktop/ui/app/` (the `web/packages/ui` production
//! build, built with `VITE_SMIND_DESKTOP=1` by `beforeBuildCommand`/
//! `beforeDevCommand` -- see `tauri.conf.json`). `desktop/ui/app/` is
//! committed empty (`.gitkeep`; real contents gitignored, like
//! `internal/server/dist`), so this embed -- and therefore `cargo
//! build`/`cargo test` -- succeeds with zero files when the web build
//! hasn't run.
//!
//! Kept here rather than in `smind-daemon-client`, which stays free of
//! anything that only makes sense once this crate's own build layout is
//! known -- the pure-logic crate only needs the `AssetSource` trait it
//! already declares.

use rust_embed::Embed;

use smind_daemon_client::proxy::AssetSource;

#[derive(Embed)]
#[folder = "../ui/app/"]
struct BundledUi;

pub struct EmbeddedAssets;

impl AssetSource for EmbeddedAssets {
    fn get(&self, path: &str) -> Option<(Vec<u8>, String)> {
        let file = BundledUi::get(path)?;
        Some((file.data.into_owned(), file.metadata.mimetype().to_string()))
    }
}
