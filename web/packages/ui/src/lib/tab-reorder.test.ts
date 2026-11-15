import { describe, expect, it } from "vitest";

import { reorderIndicatorX, reorderSlotAtX } from "@/lib/tab-reorder";

// Three 100px tabs with 4px gaps: [0,100] [104,204] [208,308].
const spans = [
  { left: 0, right: 100 },
  { left: 104, right: 204 },
  { left: 208, right: 308 },
];

describe("reorderSlotAtX", () => {
  const cases: Array<[string, number, number]> = [
    ["left of everything", -20, 0],
    ["left half of the first tab", 40, 0],
    ["right half of the first tab", 60, 1],
    ["exactly on a midpoint goes after (slot of the next tab)", 50, 1],
    ["left half of the middle tab", 120, 1],
    ["right half of the middle tab", 180, 2],
    ["right half of the last tab", 300, 3],
    ["past the end", 900, 3],
  ];
  it.each(cases)("%s", (_name, x, want) => {
    expect(reorderSlotAtX(spans, x)).toBe(want);
  });
  it("no tabs: slot 0", () => {
    expect(reorderSlotAtX([], 10)).toBe(0);
  });
});

describe("reorderIndicatorX", () => {
  it("is the start of the first tab for slot 0, the end of the last for the last slot, the middle of the gap between", () => {
    expect(reorderIndicatorX(spans, 0)).toBe(0);
    expect(reorderIndicatorX(spans, 1)).toBe(102);
    expect(reorderIndicatorX(spans, 2)).toBe(206);
    expect(reorderIndicatorX(spans, 3)).toBe(308);
    expect(reorderIndicatorX(spans, 40)).toBe(308);
    expect(reorderIndicatorX(spans, -1)).toBe(0);
  });
  it("is null with no tabs", () => {
    expect(reorderIndicatorX([], 0)).toBeNull();
  });
});
