//! ADR-0031 D7 (b2): an SSH-tunnel connection (`ssh -N -L`). SCAFFOLD: the
//! command shape is fixed by the P5 contract.

use smind_daemon_client::proxy::Connection;

#[tauri::command]
pub async fn connections_add_ssh(
    _label: String,
    _host: String,
    _ssh_port: Option<u16>,
    _daemon_port: u16,
) -> Result<Connection, String> {
    Err("connections_add_ssh: not implemented yet".to_string())
}
