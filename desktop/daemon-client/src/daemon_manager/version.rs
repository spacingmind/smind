//! Version classification and comparison (AC1). A version is either a
//! clean `Release(major, minor, patch)`, a `Dev` build (bare `"dev"` or
//! anything containing `-dev`, matching the Taskfile's
//! `<manifest>-dev+<shortsha>[.dirty]` shape), or `Unknown` (unparseable).
//! Comparison only ever orders two `Release`s -- anything else is
//! `Unknown`, which is the "never nag in a loop" rule: a `dev` daemon next
//! to a packaged app (or vice versa) never triggers the "older" banner.

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VersionKind {
    Release(u64, u64, u64),
    Dev,
    Unknown,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Comparison {
    Older,
    Same,
    Newer,
    Unknown,
}

/// classify strips a leading `v` (release tags are `v0.7.0`; `/healthz`
/// and `tauri.conf.json` already report the bare form, but this tolerates
/// either), then checks for a dev build before trying to parse three
/// dot-separated numeric components. Anything else -- extra components,
/// non-numeric parts, empty string -- is `Unknown`, never a panic.
pub fn classify(raw: &str) -> VersionKind {
    let trimmed = raw.trim();
    let stripped = trimmed.strip_prefix('v').unwrap_or(trimmed);

    if stripped == "dev" || stripped.contains("-dev") {
        return VersionKind::Dev;
    }

    let parts: Vec<&str> = stripped.split('.').collect();
    if parts.len() != 3 {
        return VersionKind::Unknown;
    }
    let mut nums = [0u64; 3];
    for (i, p) in parts.iter().enumerate() {
        match p.parse::<u64>() {
            Ok(n) => nums[i] = n,
            Err(_) => return VersionKind::Unknown,
        }
    }
    VersionKind::Release(nums[0], nums[1], nums[2])
}

/// compare orders `app` against `daemon` from the daemon's point of view:
/// `Older` means the daemon is older than the app. `Unknown` unless both
/// sides classify as `Release`.
pub fn compare(app: &str, daemon: &str) -> Comparison {
    match (classify(app), classify(daemon)) {
        (VersionKind::Release(a1, a2, a3), VersionKind::Release(d1, d2, d3)) => {
            let a = (a1, a2, a3);
            let d = (d1, d2, d3);
            if d < a {
                Comparison::Older
            } else if d > a {
                Comparison::Newer
            } else {
                Comparison::Same
            }
        }
        _ => Comparison::Unknown,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn classify_release() {
        assert_eq!(classify("0.7.0"), VersionKind::Release(0, 7, 0));
        assert_eq!(classify("v0.7.0"), VersionKind::Release(0, 7, 0));
        assert_eq!(classify("1.2.3"), VersionKind::Release(1, 2, 3));
    }

    #[test]
    fn classify_dev() {
        assert_eq!(classify("dev"), VersionKind::Dev);
        assert_eq!(classify("0.7.0-dev+abc123"), VersionKind::Dev);
        assert_eq!(classify("0.7.0-dev+abc123.dirty"), VersionKind::Dev);
    }

    #[test]
    fn classify_unknown() {
        assert_eq!(classify(""), VersionKind::Unknown);
        assert_eq!(classify("foo"), VersionKind::Unknown);
        assert_eq!(classify("1.2"), VersionKind::Unknown);
        assert_eq!(classify("1.2.3.4"), VersionKind::Unknown);
        assert_eq!(classify("1.2.x"), VersionKind::Unknown);
    }

    #[test]
    fn compare_ordering() {
        assert_eq!(compare("0.7.0", "0.6.0"), Comparison::Older);
        assert_eq!(compare("0.6.0", "0.7.0"), Comparison::Newer);
        assert_eq!(compare("0.7.0", "0.7.0"), Comparison::Same);
        assert_eq!(compare("1.0.0", "0.9.9"), Comparison::Older);
        assert_eq!(compare("0.7.1", "0.7.0"), Comparison::Older);
    }

    #[test]
    fn compare_unknown_when_either_side_is_dev_or_unknown() {
        assert_eq!(compare("dev", "0.7.0"), Comparison::Unknown);
        assert_eq!(compare("0.7.0", "dev"), Comparison::Unknown);
        assert_eq!(compare("0.7.0-dev+abc123", "0.7.0"), Comparison::Unknown);
        assert_eq!(compare("0.7.0", "0.7.0-dev+abc123"), Comparison::Unknown);
        assert_eq!(compare("foo", "0.7.0"), Comparison::Unknown);
        assert_eq!(compare("dev", "dev"), Comparison::Unknown);
    }

    #[test]
    fn compare_tolerates_v_prefix_either_side() {
        assert_eq!(compare("v0.7.0", "0.6.0"), Comparison::Older);
        assert_eq!(compare("0.7.0", "v0.7.0"), Comparison::Same);
    }
}
