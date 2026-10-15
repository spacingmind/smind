//! ADR-0031 D7 (b): `connections_probe(url)` -- what a daemon URL answers,
//! without saving anything. SCAFFOLD: shape fixed by the P5 contract.

use serde::Serialize;

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProbeResult {
    pub reachable: bool,
    pub version: Option<String>,
    pub service: Option<String>,
    pub auth_required: bool,
    pub loopback: bool,
    pub error: Option<String>,
}

#[tauri::command]
pub async fn connections_probe(_url: String) -> Result<ProbeResult, String> {
    Err("connections_probe: not implemented yet".to_string())
}
