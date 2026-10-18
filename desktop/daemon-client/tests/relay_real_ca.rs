//! Opt-in interop test of the desktop relay dial's CA mode (ADR-0031 P12c)
//! against the REAL Go relay serving a certificate from a private CA
//! (`smind relay --tls-cert/--tls-key`, Go PR #361). Skips cleanly unless
//! `SMIND_REAL_RELAY_BIN` points at a smind built from a tree that has those
//! flags:
//!
//! ```text
//! git worktree add --detach /tmp/p12c-go origin/feat/relay-ca-tls   # or any tree with #361
//! go build -C /tmp/p12c-go -o /tmp/p12c-smind ./cmd/smind
//! cd desktop/daemon-client
//! SMIND_REAL_RELAY_BIN=/tmp/p12c-smind cargo test --test relay_real_ca -- --nocapture
//! ```
//!
//! The test generates a private CA and a server certificate (SAN 127.0.0.1 and
//! localhost) with rcgen, enrols a relay host in a throwaway `SMIND_HOME`,
//! starts the real relay on free loopback ports and proves, with a real RPC
//! (the admission exchange) each time:
//!   (a) empty fingerprint + the test CA as roots  -> connects and admits,
//!   (b) empty fingerprint + the system roots      -> refused (unknown authority),
//!   (c) the correct leaf pin                      -> connects and admits,
//!   (d) a wrong pin                               -> refused.
//! Everything is loopback; the relay and its temp home are cleaned up.

use std::net::TcpListener;
use std::process::{Child, Command, Stdio};
use std::sync::Arc;
use std::time::Duration;

use rcgen::{BasicConstraints, CertificateParams, IsCa, Issuer, KeyPair, SanType};
use sha2::{Digest, Sha256};
use smind_daemon_client::relay::client as relay_client;
use smind_daemon_client::relay::relaypb::relay_client::RelayClient as GrpcRelayClient;

struct Relay {
    child: Child,
    addr: String,
    _home: tempfile::TempDir,
}

impl Drop for Relay {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port()
}

fn hex_decode(s: &str) -> Vec<u8> {
    (0..s.len()).step_by(2).map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap()).collect()
}

#[tokio::test]
async fn real_relay_ca_mode_and_pin_mode() {
    let Ok(bin) = std::env::var("SMIND_REAL_RELAY_BIN") else {
        eprintln!("relay_real_ca: SMIND_REAL_RELAY_BIN not set; skipping");
        return;
    };

    // --- a private CA and a server certificate for 127.0.0.1 / localhost ---
    let mut ca_params = CertificateParams::new(Vec::<String>::new()).unwrap();
    ca_params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    ca_params.distinguished_name.push(rcgen::DnType::CommonName, "p12c test CA");
    let ca_key = KeyPair::generate().unwrap();
    let ca_cert = ca_params.self_signed(&ca_key).unwrap();
    let mut leaf_params = CertificateParams::new(Vec::<String>::new()).unwrap();
    leaf_params.subject_alt_names = vec![SanType::IpAddress("127.0.0.1".parse().unwrap()), SanType::DnsName("localhost".try_into().unwrap())];
    let leaf_key = KeyPair::generate().unwrap();
    let leaf = leaf_params.signed_by(&leaf_key, &Issuer::from_params(&ca_params, &ca_key)).unwrap();
    let pin: String = Sha256::digest(leaf.der().as_ref()).iter().map(|b| format!("{b:02x}")).collect();
    let mut test_roots = rustls::RootCertStore::empty();
    test_roots.add(ca_cert.der().clone()).unwrap();
    let test_roots = Arc::new(test_roots);

    // --- enrol a relay host and start the real relay with that certificate ---
    let home = tempfile::tempdir().unwrap();
    let cert_path = home.path().join("relay.crt");
    let key_path = home.path().join("relay.key");
    std::fs::write(&cert_path, leaf.pem()).unwrap();
    std::fs::write(&key_path, leaf_key.serialize_pem()).unwrap();
    let enrolled = Command::new(&bin).args(["relay", "host", "new", "ws-1"]).env("SMIND_HOME", home.path()).output().unwrap();
    assert!(enrolled.status.success(), "{}", String::from_utf8_lossy(&enrolled.stderr));
    let secret_hex = String::from_utf8_lossy(&enrolled.stdout)
        .lines()
        .map(str::trim)
        .find(|l| l.len() == 64 && l.bytes().all(|b| b.is_ascii_hexdigit()))
        .expect("the host secret is printed")
        .to_string();
    let secret = hex_decode(&secret_hex);

    let (grpc_port, web_port) = (free_port(), free_port());
    let child = Command::new(&bin)
        .args(["relay", "--listen", &format!("127.0.0.1:{grpc_port}"), "--grpc-web-listen", &format!("127.0.0.1:{web_port}")])
        .arg("--tls-cert")
        .arg(&cert_path)
        .arg("--tls-key")
        .arg(&key_path)
        .env("SMIND_HOME", home.path())
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::inherit())
        .spawn()
        .expect("start the real relay");
    let relay = Relay { child, addr: format!("127.0.0.1:{grpc_port}"), _home: home };
    for _ in 0..100 {
        if std::net::TcpStream::connect(&relay.addr).is_ok() {
            break;
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }

    // A real RPC over the dialed channel: the admission exchange.
    async fn admits(channel: tonic::transport::Channel, secret: &[u8]) -> Result<String, String> {
        let mut client = GrpcRelayClient::new(channel);
        relay_client::admit(&mut client, "ws-1", "desktop-ca-test", secret).await
    }

    // (a) CA mode, the test CA as roots: connects and admits -- by IP and by name.
    let ch = relay_client::dial_with_roots(&relay.addr, "", Some(test_roots.clone())).await.expect("(a) CA mode dial");
    let admission = admits(ch, &secret).await.expect("(a) admission over a CA-verified channel");
    eprintln!("[relay_real_ca] (a) CA mode, test CA roots: admitted ({} hex chars)", admission.len());
    let by_name = format!("localhost:{grpc_port}");
    let ch = relay_client::dial_with_roots(&by_name, "", Some(test_roots.clone())).await.expect("(a2) CA mode by name");
    admits(ch, &secret).await.expect("(a2) admission");
    eprintln!("[relay_real_ca] (a2) CA mode by name localhost: admitted");

    // (b) CA mode with the SYSTEM roots: the private CA is not trusted.
    let err = relay_client::dial(&relay.addr, "").await.expect_err("(b) unknown authority must be refused");
    eprintln!("[relay_real_ca] (b) CA mode, system roots: refused: {err}");
    assert!(err.contains("UnknownIssuer"), "refused for the right reason: {err}");

    // (c) the correct leaf pin: connects and admits, no CA involved.
    let ch = relay_client::dial(&relay.addr, &pin).await.expect("(c) pinned dial");
    admits(ch, &secret).await.expect("(c) admission over a pinned channel");
    eprintln!("[relay_real_ca] (c) correct pin: admitted");

    // (d) a wrong pin is refused, even though the trusted CA signed the cert.
    let err = relay_client::dial_with_roots(&relay.addr, &"ab".repeat(32), Some(test_roots.clone())).await.expect_err("(d) wrong pin must be refused");
    eprintln!("[relay_real_ca] (d) wrong pin: refused: {err}");
    assert!(err.contains("does not match pin"), "refused for the right reason: {err}");

    // A malformed fingerprint never falls back to CA mode, even with trusted roots.
    let err = relay_client::dial_with_roots(&relay.addr, &pin[..62], Some(test_roots)).await.expect_err("malformed fingerprint");
    assert!(err.contains("bad fingerprint"), "{err}");
}
