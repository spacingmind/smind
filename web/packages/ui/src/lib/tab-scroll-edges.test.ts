import { describe, expect, it } from "vitest";

import { disambiguatedParts, disambiguatedTitles, revealScrollLeft, TAB_REVEAL_PADDING_PX, tabScrollEdges } from "@/lib/tab-scroll-edges";

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

describe("revealScrollLeft", () => {
  const view = { clientWidth: 400, scrollWidth: 1600 };
  const P = TAB_REVEAL_PADDING_PX;
  const cases: Array<[string, number, number, number, number]> = [
    ["already visible with room on both sides: unchanged", 300, 400, 500, 300],
    ["cut off at the right: scrolled until the chip and the fade's clear space fit", 0, 380, 480, 480 - 400 + P],
    ["flush against the right edge would sit under the fade: still moved by the padding", 0, 280, 400, 400 - 400 + P],
    ["cut off at the left: scrolled back with the padding", 500, 460, 560, 460 - P],
    ["the last tab: clamped to the end of the strip", 0, 1500, 1600, 1200],
    ["the first tab: clamped to the start", 800, 0, 100, 0],
    ["a chip wider than the viewport shows its left end", 0, 100, 700, 700 - 400 + P],
  ];
  it.each(cases)("%s", (_name, scrollLeft, itemLeft, itemRight, want) => {
    expect(revealScrollLeft({ ...view, scrollLeft, itemLeft, itemRight })).toBe(want);
  });

  it("the revealed chip is always fully inside the visible strip minus the clear space (unless clamped at an end)", () => {
    for (let left = 0; left <= 1500; left += 70) {
      const next = revealScrollLeft({ ...view, scrollLeft: 0, itemLeft: left, itemRight: left + 100 });
      const visibleLeft = next + (next > 0 ? P : 0);
      const visibleRight = next + view.clientWidth - (next < view.scrollWidth - view.clientWidth ? P : 0);
      expect(left).toBeGreaterThanOrEqual(visibleLeft - (next === 0 ? 0 : 0));
      expect(left + 100).toBeLessThanOrEqual(visibleRight);
    }
  });
});

describe("disambiguatedTitles", () => {
  it("numbers repeats in tab order and leaves unique titles alone", () => {
    const m = disambiguatedTitles([
      { key: "a", title: "Chat" },
      { key: "b", title: "Diff" },
      { key: "c", title: "Chat" },
      { key: "d", title: "Chat" },
      { key: "e", title: "Terminal" },
      { key: "f", title: "Terminal" },
    ]);
    expect([...m.entries()]).toEqual([["a", "Chat"], ["b", "Diff"], ["c", "Chat 2"], ["d", "Chat 3"], ["e", "Terminal"], ["f", "Terminal 2"]]);
  });
});

describe("disambiguatedParts", () => {
  it("is the one source of the numbers: titles are derived from it", () => {
    const entries = [{ key: "a", title: "Chat" }, { key: "b", title: "Chat" }, { key: "c", title: "Diff" }, { key: "d", title: "Chat" }];
    const parts = disambiguatedParts(entries);
    expect([...parts.entries()]).toEqual([["a", { base: "Chat", n: 1 }], ["b", { base: "Chat", n: 2 }], ["c", { base: "Diff", n: 1 }], ["d", { base: "Chat", n: 3 }]]);
    const titles = disambiguatedTitles(entries);
    for (const [k, { base, n }] of parts) expect(titles.get(k)).toBe(n === 1 ? base : `${base} ${n}`);
  });
});
