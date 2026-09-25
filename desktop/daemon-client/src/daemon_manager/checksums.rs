//! `checksums.txt` parsing and SHA-256 verification (AC2). The release
//! workflow (`.github/workflows/release-please.yml`) produces this with
//! plain `sha256sum <file...> | sort -k2 > checksums.txt`, so lines look
//! like `<hex>  <filename>` (two spaces) or, for a file `sha256sum` marks
//! as binary, `<hex> *<filename>` (one space, `*` prefix). Either form is
//! accepted; a line that doesn't match either shape is skipped rather than
//! failing the whole parse -- a stray blank line or trailing newline must
//! not break checksum lookup for every other file in the same release.

use std::collections::HashMap;

use sha2::{Digest, Sha256};

/// parse extracts a filename -> lowercase hex digest map from a
/// `checksums.txt`-shaped string.
pub fn parse(text: &str) -> HashMap<String, String> {
    let mut out = HashMap::new();
    for line in text.lines() {
        let line = line.trim_end();
        if line.is_empty() {
            continue;
        }
        let Some((hex, rest)) = line.split_once(char::is_whitespace) else {
            continue;
        };
        let hex = hex.trim();
        if hex.len() != 64 || !hex.bytes().all(|b| b.is_ascii_hexdigit()) {
            continue;
        }
        let name = rest.trim_start().trim_start_matches('*').trim();
        if name.is_empty() {
            continue;
        }
        out.insert(name.to_string(), hex.to_lowercase());
    }
    out
}

/// verify hashes `data` with SHA-256 and compares it (case-insensitively)
/// against `expected_hex`.
pub fn verify(data: &[u8], expected_hex: &str) -> bool {
    let mut hasher = Sha256::new();
    hasher.update(data);
    let digest = hasher.finalize();
    hex_eq(&hex_encode(&digest), expected_hex)
}

/// hex_eq compares two hex digests case-insensitively, trimming
/// whitespace -- used both by `verify` above (hash computed in-process,
/// e.g. the macOS/native path) and by the WSL2 path, which computes the
/// hash *inside* the distro (`sha256sum`) and only ever brings the
/// resulting hex string back across the `wsl.exe` boundary, never the
/// tarball's raw bytes.
pub fn hex_eq(a: &str, b: &str) -> bool {
    a.trim().eq_ignore_ascii_case(b.trim())
}

fn hex_encode(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_two_space_form() {
        let text = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  smind_0.7.0_linux_amd64.tar.gz\n\
                     bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  checksums.txt\n";
        let map = parse(text);
        assert_eq!(map.len(), 2);
        assert_eq!(
            map.get("smind_0.7.0_linux_amd64.tar.gz").unwrap(),
            "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        );
    }

    #[test]
    fn parse_binary_star_form() {
        let hex = "c".repeat(64);
        let text = format!("{hex} *smind_0.7.0_darwin_arm64.tar.gz\n");
        let map = parse(&text);
        assert_eq!(map.get("smind_0.7.0_darwin_arm64.tar.gz").unwrap(), &hex);
    }

    #[test]
    fn parse_skips_malformed_lines_without_failing_the_rest() {
        let text = "not a checksum line\n\
                     \n\
                     short 0000\n\
                     ddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd toolong.tar.gz\n\
                     eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee  ok.tar.gz\n";
        let map = parse(text);
        assert_eq!(map.len(), 1);
        assert!(map.contains_key("ok.tar.gz"));
    }

    #[test]
    fn verify_true_and_false() {
        let data = b"hello world";
        let mut hasher = Sha256::new();
        hasher.update(data);
        let expected = hex_encode(&hasher.finalize());
        assert!(verify(data, &expected));
        assert!(verify(data, &expected.to_uppercase()));
        assert!(!verify(data, "0000000000000000000000000000000000000000000000000000000000000000"));
        assert!(!verify(b"different", &expected));
    }

    #[test]
    fn hex_eq_case_and_whitespace_insensitive() {
        assert!(hex_eq("AbCd", " abcd \n"));
        assert!(!hex_eq("abcd", "abce"));
    }
}
