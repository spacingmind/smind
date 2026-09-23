import { describe, expect, it } from "vitest";

import type { TabEntry } from "@/components/tab-registry";
import {
  clampNormalizedSizes,
  collectAllPanes,
  createDefaultLayout,
  detachTabFromTree,
  getTreeDepth,
  moveTabToPaneInLayout,
  normalizeLayout,
  removeTabFromTree,
  splitPaneInLayout,
  type SplitNode,
  type TaskLayout,
} from "@/lib/split-tree";

function tab(key: string, title = key): TabEntry {
  return { kind: "file", key, taskId: 1, title };
}

function pane(id: string, tabs: TabEntry[], activeKey: string | null = tabs[0]?.key ?? null): SplitNode {
  return { kind: "pane", pane: { id, tabs, activeKey } };
}

function group(id: string, direction: "horizontal" | "vertical", children: SplitNode[], sizes: number[]): SplitNode {
  return { kind: "group", group: { id, direction, children, sizes } };
}

function makeCreateNodeId() {
  let counter = 0;
  return (prefix: "pane" | "group") => `${prefix}-${(counter += 1)}`;
}

describe("splitPaneInLayout", () => {
  it("splits a single-pane tree to the right into a 2-child horizontal group", () => {
    const t = tab("t1");
    const layout: TaskLayout = { root: pane("primary", [t], t.key), focusedPaneId: "primary" };

    const result = splitPaneInLayout({
      layout,
      tabKey: t.key,
      targetPaneId: "primary",
      position: "right",
      createNodeId: makeCreateNodeId(),
      maxTreeDepth: 5,
    });

    expect(result).not.toBeNull();
    const root = result!.layout.root;
    expect(root.kind).toBe("group");
    if (root.kind !== "group") throw new Error("unreachable");
    expect(root.group.direction).toBe("horizontal");
    expect(root.group.children).toHaveLength(2);
    expect(root.group.sizes).toEqual([0.5, 0.5]);

    const newPane = root.group.children[1];
    expect(newPane?.kind).toBe("pane");
    if (newPane?.kind !== "pane") throw new Error("unreachable");
    expect(newPane.pane.tabs.map((entry) => entry.key)).toEqual([t.key]);
    expect(result!.paneId).toBe(newPane.pane.id);
    expect(result!.layout.focusedPaneId).toBe(newPane.pane.id);
  });

  it("splitting twice in the same direction reuses the existing group instead of nesting", () => {
    const tA = tab("tA");
    const tB = tab("tB");
    const layout: TaskLayout = { root: pane("primary", [tA, tB], tA.key), focusedPaneId: "primary" };
    const createNodeId = makeCreateNodeId();

    const first = splitPaneInLayout({
      layout,
      tabKey: tA.key,
      targetPaneId: "primary",
      position: "right",
      createNodeId,
      maxTreeDepth: 5,
    });
    expect(first).not.toBeNull();

    const second = splitPaneInLayout({
      layout: first!.layout,
      tabKey: tB.key,
      targetPaneId: "primary",
      position: "right",
      createNodeId,
      maxTreeDepth: 5,
    });
    expect(second).not.toBeNull();

    const root = second!.layout.root;
    expect(root.kind).toBe("group");
    if (root.kind !== "group") throw new Error("unreachable");
    expect(root.group.children).toHaveLength(3);
    expect(root.group.sizes.reduce((sum, size) => sum + size, 0)).toBeCloseTo(1);
  });

  it("returns null when the split would exceed maxTreeDepth", () => {
    // 5-level tree: group -> group -> group -> group -> pane(with the tab)/pane
    const t = tab("deep");
    const deepPane = pane("deep-pane", [t], t.key);
    const root = group(
      "g1",
      "horizontal",
      [
        pane("p1", []),
        group(
          "g2",
          "vertical",
          [
            pane("p2", []),
            group(
              "g3",
              "horizontal",
              [pane("p3", []), group("g4", "vertical", [pane("p4", []), deepPane], [0.5, 0.5])],
              [0.5, 0.5],
            ),
          ],
          [0.5, 0.5],
        ),
      ],
      [0.5, 0.5],
    );
    const layout: TaskLayout = { root, focusedPaneId: "deep-pane" };
    expect(getTreeDepth(root)).toBe(5);

    const result = splitPaneInLayout({
      layout,
      tabKey: t.key,
      targetPaneId: "deep-pane",
      position: "right",
      createNodeId: makeCreateNodeId(),
      maxTreeDepth: 5,
    });
    expect(result).toBeNull();
  });
});

describe("pane removal (closing a pane's last tab)", () => {
  it("collapses a split-created pane into its sibling when its last tab closes", () => {
    const t = tab("t1");
    const layout: TaskLayout = { root: pane("primary", [t], t.key), focusedPaneId: "primary" };
    const split = splitPaneInLayout({
      layout,
      tabKey: t.key,
      targetPaneId: "primary",
      position: "right",
      createNodeId: makeCreateNodeId(),
      maxTreeDepth: 5,
    });
    expect(split).not.toBeNull();
    const newPaneId = split!.paneId;

    const nextRoot = removeTabFromTree(split!.layout.root, t.key);
    expect(nextRoot.kind).toBe("pane");
    if (nextRoot.kind !== "pane") throw new Error("unreachable");
    expect(nextRoot.pane.id).toBe("primary");
    expect(nextRoot.pane.id).not.toBe(newPaneId);
  });

  it("leaves the sole root pane in place, empty, when its last tab closes", () => {
    const t = tab("t1");
    const root = pane("only-root", [t], t.key);

    const nextRoot = removeTabFromTree(root, t.key);
    expect(nextRoot.kind).toBe("pane");
    if (nextRoot.kind !== "pane") throw new Error("unreachable");
    expect(nextRoot.pane.id).toBe("only-root");
    expect(nextRoot.pane.tabs).toEqual([]);
    expect(nextRoot.pane.activeKey).toBeNull();
  });

  it("collapses primary into its sibling on close, rather than keeping it alive-but-empty, when a sibling pane exists (detachTabFromTree with a dynamic preserveEmptyPaneId)", () => {
    const primaryTab = tab("primary-tab");
    const sideTab = tab("side-tab");
    const layout: TaskLayout = { root: pane("primary", [primaryTab], primaryTab.key), focusedPaneId: "primary" };
    const split = splitPaneInLayout({
      layout,
      tabKey: primaryTab.key,
      targetPaneId: "primary",
      position: "right",
      createNodeId: makeCreateNodeId(),
      maxTreeDepth: 5,
    });
    expect(split).not.toBeNull();

    // Reconstruct: primary now empty, pane-1 holds primaryTab. Open a
    // second tab in primary so both siblings are non-empty.
    const rootWithBothNonEmpty: SplitNode = {
      kind: "group",
      group: {
        id: (split!.layout.root as { group: { id: string } }).group.id,
        direction: "horizontal",
        children: [pane("primary", [sideTab], sideTab.key), pane(split!.paneId, [primaryTab], primaryTab.key)],
        sizes: [0.5, 0.5],
      },
    };

    const { root } = detachTabFromTree(rootWithBothNonEmpty, { tabKey: sideTab.key, preserveEmptyPaneId: null });
    expect(root.kind).toBe("pane");
    if (root.kind !== "pane") throw new Error("unreachable");
    expect(root.pane.id).toBe(split!.paneId);
    expect(root.pane.tabs.map((t) => t.key)).toEqual([primaryTab.key]);
  });
});

describe("moveTabToPaneInLayout", () => {
  it("moves a tab across two arbitrary pane ids, updates focusedPaneId, leaves the rest untouched", () => {
    const tX = tab("tX");
    const tY = tab("tY");
    const root = group("g1", "horizontal", [pane("alpha", [tX, tY], tX.key), pane("beta", [])], [0.6, 0.4]);
    const layout: TaskLayout = { root, focusedPaneId: "alpha" };

    const next = moveTabToPaneInLayout({ layout, tabKey: tX.key, toPaneId: "beta" });
    expect(next).not.toBeNull();
    expect(next!.focusedPaneId).toBe("beta");

    const nextRoot = next!.root;
    expect(nextRoot.kind).toBe("group");
    if (nextRoot.kind !== "group") throw new Error("unreachable");
    expect(nextRoot.group.children).toHaveLength(2);
    expect(nextRoot.group.sizes).toEqual([0.6, 0.4]);

    const [alpha, beta] = nextRoot.group.children;
    expect(alpha?.kind === "pane" && alpha.pane.tabs.map((entry) => entry.key)).toEqual([tY.key]);
    expect(beta?.kind === "pane" && beta.pane.tabs.map((entry) => entry.key)).toEqual([tX.key]);
  });
});

describe("clampNormalizedSizes", () => {
  it("always sums to 1", () => {
    const sizes = clampNormalizedSizes([0.5, 0.3, 0.2]);
    expect(sizes.reduce((sum, size) => sum + size, 0)).toBeCloseTo(1);
  });

  it("never drives a child below MIN_SPLIT_SIZE, even when the raw input is extreme", () => {
    const sizes = clampNormalizedSizes([0.98, 0.01, 0.01]);
    for (const size of sizes) {
      expect(size).toBeGreaterThanOrEqual(0.1 - 1e-9);
    }
    expect(sizes.reduce((sum, size) => sum + size, 0)).toBeCloseTo(1);
  });

  it("splits evenly when three panes would each be pushed below the minimum", () => {
    const sizes = clampNormalizedSizes([0.97, 0.02, 0.01]);
    expect(sizes.every((size) => size >= 0.1 - 1e-9)).toBe(true);
    expect(sizes.reduce((sum, size) => sum + size, 0)).toBeCloseTo(1);
  });
});

describe("normalizeLayout", () => {
  it("repairs a dangling focusedPaneId to a real pane", () => {
    const raw = {
      root: { kind: "pane", pane: { id: "p1", tabs: [], activeKey: null } },
      focusedPaneId: "does-not-exist",
    };
    const normalized = normalizeLayout(raw);
    expect(normalized.focusedPaneId).toBe("p1");
  });

  it("collapses a group left with a single child", () => {
    const raw = {
      root: {
        kind: "group",
        group: {
          id: "g1",
          direction: "horizontal",
          children: [{ kind: "pane", pane: { id: "p1", tabs: [], activeKey: null } }],
          sizes: [1],
        },
      },
      focusedPaneId: "p1",
    };
    const normalized = normalizeLayout(raw);
    expect(normalized.root.kind).toBe("pane");
    if (normalized.root.kind !== "pane") throw new Error("unreachable");
    expect(normalized.root.pane.id).toBe("p1");
  });

  it("falls back to createDefaultLayout() for an unsalvageable root", () => {
    expect(normalizeLayout({ root: "garbage", focusedPaneId: "dangling" })).toEqual(createDefaultLayout());
    expect(normalizeLayout(null)).toEqual(createDefaultLayout());
  });
});

describe("getTreeDepth", () => {
  it("returns 5 for a 5-level nested fixture", () => {
    const root = group(
      "g1",
      "horizontal",
      [
        pane("p1", []),
        group(
          "g2",
          "vertical",
          [
            pane("p2", []),
            group(
              "g3",
              "horizontal",
              [pane("p3", []), group("g4", "vertical", [pane("p4", []), pane("p5", [])], [0.5, 0.5])],
              [0.5, 0.5],
            ),
          ],
          [0.5, 0.5],
        ),
      ],
      [0.5, 0.5],
    );
    expect(getTreeDepth(root)).toBe(5);
    expect(collectAllPanes(root)).toHaveLength(5);
  });
});
