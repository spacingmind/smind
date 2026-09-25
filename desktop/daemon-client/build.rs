// Generates the relay.v1.Relay gRPC client from the same relay.proto the
// Go daemon/relay/mobile all share (internal/relay/relaypb/relay.proto),
// so no protobuf schema is hand-duplicated in Rust. Client-only: this
// crate never runs a relay server.
fn main() -> Result<(), Box<dyn std::error::Error>> {
    // prost-build (via tonic-prost-build) shells out to `protoc`; a
    // vendored binary means this builds on a bare CI runner (notably
    // Windows, which has no system protoc) without an extra install
    // step, rather than depending on whatever happens to be on PATH.
    // Safe to always set: this build script is the only thing in the
    // process that cares about PROTOC.
    unsafe {
        std::env::set_var("PROTOC", protoc_bin_vendored::protoc_bin_path()?);
    }

    let proto_dir = "../../internal/relay/relaypb";
    let proto_file = format!("{proto_dir}/relay.proto");
    println!("cargo:rerun-if-changed={proto_file}");
    tonic_prost_build::configure()
        .build_server(false)
        .build_client(true)
        .compile_protos(&[proto_file], &[proto_dir.to_string()])?;
    Ok(())
}
