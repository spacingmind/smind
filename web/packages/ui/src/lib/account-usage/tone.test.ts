import { describe, expect, it } from "vitest";
import { windowTone } from "./tone";
import type { UsageWindow } from "./types";

function window(usedPct: number | null): UsageWindow {
  return { id: "five_hour", label: "5-hour", usedPct };
}

describe("windowTone", () => {
  it("derives the tone from how much of the window is used", () => {
    expect(windowTone(window(69.9))).toBe("ok");
    expect(windowTone(window(70))).toBe("warning");
    expect(windowTone(window(90))).toBe("warning");
    expect(windowTone(window(90.1))).toBe("danger");
  });

  it("is default when the window has no percent", () => {
    expect(windowTone(window(null))).toBe("default");
  });

  it("the report's own tone wins", () => {
    expect(windowTone({ ...window(31), tone: "warning" })).toBe("warning");
    expect(windowTone({ ...window(95), tone: "ok" })).toBe("ok");
  });

  it("ignores an unknown tone string", () => {
    expect(windowTone({ ...window(31), tone: "loud" as UsageWindow["tone"] })).toBe("ok");
  });
});
