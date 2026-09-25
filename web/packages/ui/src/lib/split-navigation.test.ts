import { describe, expect, it } from "vitest";

import { findAdjacentPane } from "@/lib/split-navigation";
import type { SplitNode } from "@/lib/split-tree";

function pane(id: string): SplitNode {
  return { kind: "pane", pane: { id, tabs: [], activeKey: null } };
}

function group(id: string, direction: "horizontal" | "vertical", children: SplitNode[], sizes: number[]): SplitNode {
  return { kind: "group", group: { id, direction, children, sizes } };
}

describe("findAdjacentPane", () => {
  it("finds the pane to the right in a simple horizontal split", () => {
    const root = group("g1", "horizontal", [pane("left"), pane("right")], [0.5, 0.5]);
    expect(findAdjacentPane(root, "left", "right")).toBe("right");
    expect(findAdjacentPane(root, "right", "left")).toBe("left");
  });

  it("returns null when there is nothing in that direction", () => {
    const root = group("g1", "horizontal", [pane("left"), pane("right")], [0.5, 0.5]);
    expect(findAdjacentPane(root, "left", "left")).toBeNull();
    expect(findAdjacentPane(root, "right", "right")).toBeNull();
    expect(findAdjacentPane(root, "left", "up")).toBeNull();
    expect(findAdjacentPane(root, "left", "down")).toBeNull();
  });

  it("finds the pane below in a vertical split", () => {
    const root = group("g1", "vertical", [pane("top"), pane("bottom")], [0.5, 0.5]);
    expect(findAdjacentPane(root, "top", "down")).toBe("bottom");
    expect(findAdjacentPane(root, "bottom", "up")).toBe("top");
  });

  it("returns null for an unknown focused pane", () => {
    const root = group("g1", "horizontal", [pane("left"), pane("right")], [0.5, 0.5]);
    expect(findAdjacentPane(root, "nowhere", "right")).toBeNull();
  });

  it("navigates a nested tree: a 2x2 grid picks the geometrically nearest pane, not tree order", () => {
    // +----------+----------+
    // | top-left | top-right|
    // +----------+----------+
    // | bottom-  | bottom-  |
    // | left     | right    |
    // +----------+----------+
    const root = group(
      "g1",
      "horizontal",
      [
        group("g-left", "vertical", [pane("top-left"), pane("bottom-left")], [0.5, 0.5]),
        group("g-right", "vertical", [pane("top-right"), pane("bottom-right")], [0.5, 0.5]),
      ],
      [0.5, 0.5],
    );

    expect(findAdjacentPane(root, "top-left", "right")).toBe("top-right");
    expect(findAdjacentPane(root, "top-left", "down")).toBe("bottom-left");
    expect(findAdjacentPane(root, "bottom-right", "left")).toBe("bottom-left");
    expect(findAdjacentPane(root, "bottom-right", "up")).toBe("top-right");
  });

  it("prefers the pane with the most row/column overlap over a merely-closer center", () => {
    // A wide top pane over two narrower bottom panes -- moving down from
    // the top pane's right half should land on bottom-right, the pane it
    // actually overlaps most, not bottom-left.
    const root = group(
      "g1",
      "vertical",
      [pane("top"), group("g-bottom", "horizontal", [pane("bottom-left"), pane("bottom-right")], [0.3, 0.7])],
      [0.5, 0.5],
    );
    expect(findAdjacentPane(root, "top", "down")).toBe("bottom-right");
  });

  it("a single-pane tree has no adjacent pane in any direction", () => {
    const root = pane("only");
    for (const direction of ["left", "right", "up", "down"] as const) {
      expect(findAdjacentPane(root, "only", direction)).toBeNull();
    }
  });
});
