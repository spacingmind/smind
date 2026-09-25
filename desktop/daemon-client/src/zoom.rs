//! Webview zoom stepping (quick-wins AC2 View menu) and the on-disk
//! persistence round-trip. Steps mirror common browser zoom levels so
//! "Zoom In" from 100% lands on a familiar 110%, not an arbitrary +10pt.

/// The selectable zoom levels, ascending. 1.0 (100%) is the default.
pub const STEPS: &[f64] = &[0.5, 0.67, 0.8, 0.9, 1.0, 1.1, 1.25, 1.5, 1.75, 2.0];

pub const DEFAULT: f64 = 1.0;

fn nearest_index(level: f64) -> usize {
    STEPS
        .iter()
        .enumerate()
        .min_by(|(_, a), (_, b)| (**a - level).abs().partial_cmp(&(**b - level).abs()).unwrap())
        .map(|(i, _)| i)
        .unwrap_or(0)
}

/// step_in returns the next zoom level up from `level`, clamped at the
/// top of `STEPS`.
pub fn step_in(level: f64) -> f64 {
    let i = nearest_index(level);
    STEPS[(i + 1).min(STEPS.len() - 1)]
}

/// step_out returns the next zoom level down from `level`, clamped at
/// the bottom of `STEPS`.
pub fn step_out(level: f64) -> f64 {
    let i = nearest_index(level);
    STEPS[i.saturating_sub(1)]
}

/// serialize/parse round-trip the persisted zoom level as plain text.
/// parse falls back to `DEFAULT` for anything that doesn't parse (a
/// missing or corrupt state file), never an error the caller must
/// handle.
pub fn serialize(level: f64) -> String {
    level.to_string()
}

pub fn parse(raw: &str) -> f64 {
    raw.trim().parse().unwrap_or(DEFAULT)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn steps_in_from_default() {
        assert_eq!(step_in(1.0), 1.1);
        assert_eq!(step_in(1.75), 2.0);
    }

    #[test]
    fn steps_out_from_default() {
        assert_eq!(step_out(1.0), 0.9);
        assert_eq!(step_out(0.67), 0.5);
    }

    #[test]
    fn clamps_at_top() {
        assert_eq!(step_in(2.0), 2.0);
        assert_eq!(step_in(3.5), 2.0); // above the table snaps to the nearest (top) step first
    }

    #[test]
    fn clamps_at_bottom() {
        assert_eq!(step_out(0.5), 0.5);
        assert_eq!(step_out(0.1), 0.5);
    }

    #[test]
    fn persistence_round_trip() {
        for level in [0.5, 1.0, 1.25, 2.0] {
            assert_eq!(parse(&serialize(level)), level);
        }
    }

    #[test]
    fn parse_falls_back_to_default_on_garbage() {
        assert_eq!(parse("not a number"), DEFAULT);
        assert_eq!(parse(""), DEFAULT);
    }
}
