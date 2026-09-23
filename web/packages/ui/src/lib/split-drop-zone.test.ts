import { describe, expect, it } from "vitest";

import { resolveSplitDropPosition } from "@/lib/split-drop-zone";

describe("resolveSplitDropPosition", () => {
  it("resolves the exact center of a rect to \"center\"", () => {
    expect(resolveSplitDropPosition({ width: 400, height: 300, x: 200, y: 150 })).toBe("center");
  });

  it("resolves a point in the outer-left 15% to \"left\" even though it's also inside the vertical center band -- the edge checks run before the center-band-on-both-axes result, and the X-axis edge check alone fails the center-band test", () => {
    // x=10 is well inside the left edge threshold (400 * 0.15 = 60), and
    // y=150 is the rect's vertical center -- inside the *vertical* center
    // band (300 * 0.4 = 120 tall, centered -> [90, 210]). Because the
    // horizontal center band ([120, 280]) excludes x=10, the "center"
    // check (which requires both axes inside their band) fails first, and
    // the left-edge check below it wins.
    expect(resolveSplitDropPosition({ width: 400, height: 300, x: 10, y: 150 })).toBe("left");
  });

  it("resolves a point in neither the center band nor any edge band to the nearest edge by raw pixel distance", () => {
    // x=100, y=75 on a 400x300 rect: outside the center band on both axes
    // ([120, 280] x [90, 210]) and outside the edge thresholds on both
    // axes (<=60 or >=340 for x; <=45 or >=255 for y) -- the ambiguous
    // ring the edge/center checks don't resolve directly. Distances to
    // each edge: left=100, right=300, top=75, bottom=225 -- top is nearest.
    expect(resolveSplitDropPosition({ width: 400, height: 300, x: 100, y: 75 })).toBe("top");
  });
});
