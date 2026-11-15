//! The redaction contract is the shared golden file
//! internal/crash/testdata/redact_vectors.json: every vector must redact to
//! exactly `want` in Rust as it does in Go, and every rule of the redaction
//! has at least one vector that exercises it. The parsing is strict on purpose:
//! a missing or misspelt field must fail the test, not become an empty string
//! that passes vacuously.

use regex::Regex;
use serde_json::Value;
use smind_crash_report::redact::redact;
use std::collections::HashSet;
use std::path::Path;

const VECTORS: &str = include_str!("../../../internal/crash/testdata/redact_vectors.json");
const REDACT_RS: &str = include_str!("../src/redact.rs");

fn file() -> Value {
    serde_json::from_str(VECTORS).expect("redact_vectors.json must be valid JSON")
}

fn field<'a>(v: &'a Value, key: &str, what: &str) -> &'a str {
    v.get(key)
        .unwrap_or_else(|| panic!("{what}: missing field {key:?}"))
        .as_str()
        .unwrap_or_else(|| panic!("{what}: field {key:?} is not a string"))
}

fn vectors(f: &Value) -> &Vec<Value> {
    f.get("vectors")
        .expect("`vectors` missing")
        .as_array()
        .expect("`vectors` is not a list")
}

#[test]
fn redaction_matches_golden_vectors() {
    let f = file();
    let list = vectors(&f);
    let count = f
        .get("count")
        .expect("`count` missing")
        .as_u64()
        .expect("`count` is not a number") as usize;
    assert_eq!(
        count,
        list.len(),
        "the file's own count disagrees with its vectors"
    );
    assert!(count >= 80, "expected the full vector set, got {count}");

    let mut names = HashSet::new();
    let mut mismatches = Vec::new();
    for (i, case) in list.iter().enumerate() {
        let what = format!("vector #{i}");
        let name = field(case, "name", &what);
        assert!(
            names.insert(name.to_string()),
            "duplicate vector name {name:?}"
        );
        let input = field(case, "input", name);
        let home = field(case, "home", name);
        let smind_home = field(case, "smindHome", name);
        let want = field(case, "want", name);
        let got = redact(
            input,
            (!home.is_empty()).then(|| Path::new(home)),
            (!smind_home.is_empty()).then(|| Path::new(smind_home)),
        );
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
        list.len(),
        mismatches.join("\n")
    );
}

/// Every coverage entry (a rule, or one alternative of a rule, with the shape of input that
/// exercises it) matches the input of some vector that the redaction changes; and every rule
/// variable of redact.rs is claimed by some entry (the Go names are mapped RE_*: `reUnixUserDir`
/// is `RE_UNIX_USER_DIR`), so a rule added on this side without a vector fails.
#[test]
fn golden_vectors_cover_every_rule() {
    let f = file();
    let list = vectors(&f);
    let coverage = f
        .get("coverage")
        .expect("`coverage` missing")
        .as_array()
        .expect("`coverage` is not a list");
    assert!(
        coverage.len() >= 30,
        "coverage list is truncated: {}",
        coverage.len()
    );

    let mut claimed = HashSet::new();
    for c in coverage {
        let rule = field(c, "rule", "coverage entry");
        let shape = field(c, "shape", rule);
        claimed.insert(field(c, "var", rule).to_string());
        let re = Regex::new(shape).unwrap_or_else(|e| panic!("coverage {rule:?}: {e}"));
        let hit = list.iter().any(|v| {
            let input = field(v, "input", "vector");
            re.is_match(input) && input != field(v, "want", "vector")
        });
        assert!(hit, "rule {rule:?} (shape {shape}): no vector exercises it");
    }

    // Go `reFooBar` -> Rust `RE_FOO_BAR` (acronyms stay together: `reURLUserinfo` ->
    // `RE_URL_USERINFO`, `reSK` -> `RE_SK`; `GitHub` is one word on the Rust side).
    let to_rust = |go: &str| -> String {
        let chars: Vec<char> = go
            .trim_start_matches("re")
            .replace("GitHub", "Github")
            .chars()
            .collect();
        let mut out = String::from("RE_");
        for (i, &ch) in chars.iter().enumerate() {
            let prev_lower = i > 0 && chars[i - 1].is_ascii_lowercase();
            let acronym_end = i > 0
                && chars[i - 1].is_ascii_uppercase()
                && ch.is_ascii_uppercase()
                && chars.get(i + 1).is_some_and(|n| n.is_ascii_lowercase());
            if ch.is_ascii_uppercase() && (prev_lower || acronym_end) {
                out.push('_');
            }
            out.push(ch.to_ascii_uppercase());
        }
        out
    };
    let claimed_rust: HashSet<String> = claimed.iter().map(|g| to_rust(g)).collect();
    let rust_rules: Vec<String> = Regex::new(r"static (RE_[A-Z_]+):")
        .unwrap()
        .captures_iter(REDACT_RS)
        .map(|c| c[1].to_string())
        .collect();
    assert!(
        rust_rules.len() >= 15,
        "found only {} rules in redact.rs",
        rust_rules.len()
    );
    for rule in &rust_rules {
        assert!(
            claimed_rust.contains(rule),
            "redact.rs rule {rule} has no coverage entry in the shared file (add a vector and an entry on the Go side)"
        );
    }
}
