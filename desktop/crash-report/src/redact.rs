//! Port of internal/crash/redact.go. The golden vectors in
//! internal/crash/testdata/redact_vectors.json are the contract (tests/golden.rs).

use std::path::Path;

/// Removes home paths, other users' directories and secret shapes from `s`.
pub fn redact(_s: &str, _home: Option<&Path>, _smind_home: Option<&Path>) -> String {
    unimplemented!("step 1 of the brief")
}
