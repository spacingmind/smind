import { describe, expect, it } from "vitest";

import { tabScrollEdges } from "@/lib/tab-scroll-edges";

describe("tabScrollEdges", () => {
  const cases: Array<[string, number, number, number, { start: boolean; end: boolean }]> = [
    ["not scrollable: no fades", 0, 400, 400, { start: false, end: false }],
    ["scrollable at the start: only the right side has more", 0, 400, 900, { start: false, end: true }],
    ["in the middle: both sides", 200, 400, 900, { start: true, end: true }],
    ["at the end: only the left side has more", 500, 400, 900, { start: true, end: false }],
    ["sub-pixel overflow is not scrollable", 0, 400, 400.6, { start: false, end: false }],
    ["sub-pixel off the start counts as the start", 0.6, 400, 900, { start: false, end: true }],
    ["sub-pixel short of the end counts as the end", 499.4, 400, 900, { start: true, end: false }],
    ["one real pixel of overflow scrolls", 0, 400, 402, { start: false, end: true }],
  ];
  it.each(cases)("%s", (_name, left, client, scroll, want) => {
    expect(tabScrollEdges(left, client, scroll)).toEqual(want);
  });
});
