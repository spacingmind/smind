import { afterEach, describe, expect, it } from "vitest";

import { DIFF_PREFS_STORAGE_KEY, loadDiffPrefs, saveDiffPrefs } from "@/lib/diff-prefs";

function setWidth(width: number): void {
  Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: width });
}

afterEach(() => {
  window.localStorage.clear();
  setWidth(1024);
});

describe("loadDiffPrefs defaults", () => {
  it("defaults to side by side on a desktop-width window", () => {
    setWidth(1280);
    expect(loadDiffPrefs()).toEqual({ view: "by-file", format: "side-by-side" });
  });

  it("defaults to unified on a phone-width window", () => {
    setWidth(390);
    expect(loadDiffPrefs()).toEqual({ view: "by-file", format: "line-by-line" });
  });

  it("an explicit stored choice wins at any width", () => {
    setWidth(390);
    saveDiffPrefs({ view: "whole", format: "side-by-side" });
    expect(loadDiffPrefs()).toEqual({ view: "whole", format: "side-by-side" });
    setWidth(1280);
    saveDiffPrefs({ view: "by-file", format: "line-by-line" });
    expect(loadDiffPrefs()).toEqual({ view: "by-file", format: "line-by-line" });
  });

  it("an unreadable or partial stored value falls back to the width's default for the missing field", () => {
    setWidth(390);
    window.localStorage.setItem(DIFF_PREFS_STORAGE_KEY, JSON.stringify({ view: "whole" }));
    expect(loadDiffPrefs()).toEqual({ view: "whole", format: "line-by-line" });
    window.localStorage.setItem(DIFF_PREFS_STORAGE_KEY, "{not json");
    expect(loadDiffPrefs()).toEqual({ view: "by-file", format: "line-by-line" });
  });
});
