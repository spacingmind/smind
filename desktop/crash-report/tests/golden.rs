//! The redaction contract is the shared golden file
//! internal/crash/testdata/redact_vectors.json: every vector must redact to
//! exactly `want` in Rust as it does in Go.

use serde_json::Value;
use smind_crash_report::redact::redact;
use std::path::Path;

#[test]
fn redaction_matches_golden_vectors() {
    let raw = include_str!("../../../internal/crash/testdata/redact_vectors.json");
    let cases: Vec<Value> =
        serde_json::from_str(raw).expect("redact_vectors.json must be a JSON list");
    assert!(
        cases.len() >= 50,
        "expected at least 50 golden vectors, got {}",
        cases.len()
    );

    let mut mismatches = Vec::new();
    for case in &cases {
        let name = case["name"].as_str().unwrap_or("<unnamed>");
        let input = case["input"].as_str().unwrap_or_default();
        let home = case["home"].as_str().filter(|s| !s.is_empty()).map(Path::new);
        let smind_home = case["smindHome"]
            .as_str()
            .filter(|s| !s.is_empty())
            .map(Path::new);
        let want = case["want"].as_str().unwrap_or_default();
        let got = redact(input, home, smind_home);
        if got != want {
            mismatches.push(format!(
                "case {name:?}:\n  input: {input:?}\n  want:  {want:?}\n  got:   {got:?}"
            ));
        }
    }
    assert!(
        mismatches.is_empty(),
        "{} of {} golden vectors mismatched:\n{}",
        mismatches.len(),
        cases.len(),
        mismatches.join("\n")
    );
}
