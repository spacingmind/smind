//! desktop-native-feel D4.4: the Windows taskbar overlay badge. Windows
//! has no numeric badge API, only a per-window overlay *image*, so the
//! attention count is rendered here as a small RGBA bitmap (a red disc
//! with a white digit, or "9+") with no image dependency. Pure and
//! platform-independent so it is unit-tested on any host; only the
//! `set_overlay_icon` call in `lifecycle` is Windows-only.

pub const SIZE: u32 = 32;

const RED: [u8; 4] = [0xE5, 0x48, 0x4D, 0xFF];
const WHITE: [u8; 4] = [0xFF, 0xFF, 0xFF, 0xFF];

/// 3x5 glyphs, one string per row; `#` is ink. Index 0-9 are digits, 10 is `+`.
const GLYPHS: [[&str; 5]; 11] = [
    ["###", "#.#", "#.#", "#.#", "###"],
    [".#.", "##.", ".#.", ".#.", "###"],
    ["###", "..#", "###", "#..", "###"],
    ["###", "..#", "###", "..#", "###"],
    ["#.#", "#.#", "###", "..#", "..#"],
    ["###", "#..", "###", "..#", "###"],
    ["###", "#..", "###", "#.#", "###"],
    ["###", "..#", "..#", "..#", "..#"],
    ["###", "#.#", "###", "#.#", "###"],
    ["###", "#.#", "###", "..#", "###"],
    ["...", ".#.", "###", ".#.", "..."],
];

/// The glyph indices drawn for `count`: one digit up to 9, "9+" beyond.
fn glyphs_for(count: usize) -> Vec<usize> {
    if count > 9 {
        vec![9, 10]
    } else {
        vec![count]
    }
}

/// overlay_icon_rgba renders the badge for `count` (callers only render
/// a badge for count > 0; 0 draws a bare disc). Returns (rgba, width, height).
pub fn overlay_icon_rgba(count: usize) -> (Vec<u8>, u32, u32) {
    let mut px = vec![0u8; (SIZE * SIZE * 4) as usize];
    let mut put = |x: u32, y: u32, c: [u8; 4]| {
        let i = ((y * SIZE + x) * 4) as usize;
        px[i..i + 4].copy_from_slice(&c);
    };

    let r = SIZE as f32 / 2.0;
    for y in 0..SIZE {
        for x in 0..SIZE {
            let (dx, dy) = (x as f32 + 0.5 - r, y as f32 + 0.5 - r);
            if dx * dx + dy * dy <= r * r {
                put(x, y, RED);
            }
        }
    }

    let glyphs = glyphs_for(count);
    if count == 0 {
        return (px, SIZE, SIZE);
    }
    // One glyph is drawn 3x; two share the disc at 2x.
    let scale = if glyphs.len() == 1 { 3 } else { 2 };
    let cols = glyphs.len() as u32 * 3 + (glyphs.len() as u32 - 1); // 1-col gap
    let (w, h) = (cols * scale, 5 * scale);
    let (x0, y0) = ((SIZE - w) / 2, (SIZE - h) / 2);
    for (gi, g) in glyphs.iter().enumerate() {
        for (row, line) in GLYPHS[*g].iter().enumerate() {
            for (col, ch) in line.bytes().enumerate() {
                if ch != b'#' {
                    continue;
                }
                let gx = x0 + (gi as u32 * 4 + col as u32) * scale;
                let gy = y0 + row as u32 * scale;
                for sy in 0..scale {
                    for sx in 0..scale {
                        put(gx + sx, gy + sy, WHITE);
                    }
                }
            }
        }
    }
    (px, SIZE, SIZE)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn at(px: &[u8], x: u32, y: u32) -> [u8; 4] {
        let i = ((y * SIZE + x) * 4) as usize;
        [px[i], px[i + 1], px[i + 2], px[i + 3]]
    }

    #[test]
    fn overlay_icon_pixels() {
        let (px, w, h) = overlay_icon_rgba(3);
        assert_eq!((w, h), (SIZE, SIZE));
        assert_eq!(px.len(), (w * h * 4) as usize);
        // The disc's edge is red, the bitmap corners are transparent.
        assert_eq!(at(&px, 2, SIZE / 2), RED);
        assert_eq!(at(&px, 0, 0)[3], 0);
        assert_eq!(at(&px, SIZE - 1, SIZE - 1)[3], 0);
        // The digit is white ink on the disc.
        assert!(px.chunks(4).any(|p| p == WHITE), "digit ink missing");
        // Different counts draw different glyphs; 10+ renders "9+" without panicking.
        assert_ne!(px, overlay_icon_rgba(7).0);
        assert_ne!(overlay_icon_rgba(12).0, overlay_icon_rgba(9).0);
        assert_eq!(overlay_icon_rgba(12).0, overlay_icon_rgba(99).0);
    }
}
