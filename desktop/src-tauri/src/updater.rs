//! ADR-0023: desktop auto-update plumbing (Rust side). The shell checks,
//! downloads and installs; the webview can only ask Rust to check and to
//! install the update Rust already found. Linux (deb-only) has no updater.

#[cfg(test)]
const TAURI_CONF: &str = include_str!("../tauri.conf.json");
#[cfg(test)]
const UPDATER_CONF: &str = include_str!("../tauri.updater.conf.json");

#[cfg(test)]
mod tests {
    use super::*;
    use base64::Engine;
    use serde_json::Value;

    #[test]
    fn updater_config_pins_key_and_endpoint() {
        let conf: Value = serde_json::from_str(TAURI_CONF).expect("tauri.conf.json parses");
        let updater = &conf["plugins"]["updater"];
        let pubkey = updater["pubkey"].as_str().expect("pubkey is a string");
        assert!(!pubkey.is_empty());
        let decoded = base64::engine::general_purpose::STANDARD
            .decode(pubkey)
            .expect("pubkey base64-decodes");
        let text = String::from_utf8(decoded).expect("pubkey decodes to UTF-8 text");
        assert!(
            text.starts_with("untrusted comment:"),
            "pubkey is a minisign public key"
        );
        let endpoints: Vec<&str> = updater["endpoints"]
            .as_array()
            .expect("endpoints array")
            .iter()
            .filter_map(Value::as_str)
            .collect();
        assert_eq!(
            endpoints,
            vec!["https://github.com/spacingmind/smind/releases/latest/download/latest.json"],
            "exactly one endpoint, the stable-channel latest.json"
        );
        assert!(
            updater.get("dangerousInsecureTransportProtocol").is_none(),
            "dangerousInsecureTransportProtocol must never be committed"
        );
    }

    #[test]
    fn create_updater_artifacts_opt_in_only() {
        let conf: Value = serde_json::from_str(TAURI_CONF).expect("tauri.conf.json parses");
        let create = conf["bundle"]["createUpdaterArtifacts"].clone();
        assert!(
            create.is_null() || create == Value::Bool(false),
            "base config must not ask for updater artifacts (keyless builds)"
        );
        for (name, src) in [
            ("tauri.macos.conf.json", include_str!("../tauri.macos.conf.json")),
            ("tauri.windows.conf.json", include_str!("../tauri.windows.conf.json")),
        ] {
            let c: Value = serde_json::from_str(src).expect("{name} parses");
            let create = c["bundle"]["createUpdaterArtifacts"].clone();
            assert!(
                create.is_null() || create == Value::Bool(false),
                "{name} must not ask for updater artifacts"
            );
        }
        let updater_conf: Value =
            serde_json::from_str(UPDATER_CONF).expect("tauri.updater.conf.json parses");
        assert_eq!(
            updater_conf["bundle"]["createUpdaterArtifacts"],
            Value::Bool(true),
            "the opt-in merge config asks for updater artifacts"
        );
    }
}
