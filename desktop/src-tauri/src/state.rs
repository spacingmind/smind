//! Tauri-managed app state (AC5/AC7): the shared proxy (its registry is
//! the single source of truth for "which connection is selected"), the
//! path its connection list is persisted to, the fixed loopback proxy
//! origin every navigation happens within, and a handle to the
//! currently-running daemon-client watcher task so a connection switch
//! can restart it against the newly-selected connection.

use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use tauri::async_runtime::JoinHandle;
use url::Url;

use smind_daemon_client::proxy::ProxyState;
use smind_daemon_client::WorkspaceCache;

use crate::tray::Tray;

pub struct DesktopState {
    pub proxy: Arc<ProxyState>,
    pub connections_path: PathBuf,
    pub proxy_url: Url,
    pub cache: WorkspaceCache,
    pub tray: Arc<Tray>,
    /// The daemon-client watcher task following the selected connection
    /// (AC7). Replaced (old one aborted) on every `connections_select`.
    pub client_task: Mutex<Option<JoinHandle<()>>>,
    /// The default WSL distro name, detected once and cached (ADR-0013
    /// part D2) -- see `daemon_manager::resolve_distro`.
    pub daemon_manager_distro: Mutex<Option<String>>,
}
