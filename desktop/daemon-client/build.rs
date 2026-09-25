// Generates the relay.v1.Relay gRPC client from the same relay.proto the
// Go daemon/relay/mobile all share (internal/relay/relaypb/relay.proto),
// so no protobuf schema is hand-duplicated in Rust. Client-only: this
// crate never runs a relay server.
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_dir = "../../internal/relay/relaypb";
    let proto_file = format!("{proto_dir}/relay.proto");
    println!("cargo:rerun-if-changed={proto_file}");
    tonic_prost_build::configure()
        .build_server(false)
        .build_client(true)
        .compile_protos(&[proto_file], &[proto_dir.to_string()])?;
    Ok(())
}
