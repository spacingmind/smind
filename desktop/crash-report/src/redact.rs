//! Port of internal/crash/redact.go. The golden vectors in
//! internal/crash/testdata/redact_vectors.json are the contract (tests/golden.rs).

use std::path::Path;

/// Removes home paths, other users' directories and secret shapes from `s`.
///
/// STUB until the redaction port lands: it returns the input unchanged, so nothing that
/// depends on real redaction may be asserted before then.
pub fn redact(s: &str, _home: Option<&Path>, _smind_home: Option<&Path>) -> String {
    s.to_string()
}
