import { describe, expect, it } from "vitest";
import { formatAmount, formatCooldownUntil, formatDisplayPct, formatPct, formatResetLabel } from "./format";
import type { UsageWindow } from "./types";

const NOW = Date.parse("2026-10-01T12:00:00Z");

describe("formatPct", () => {
  it("rounds and clamps into 0-100", () => {
    expect(formatPct(30.6)).toBe("31%");
    expect(formatPct(-5)).toBe("0%");
    expect(formatPct(140)).toBe("100%");
    expect(formatPct(0)).toBe("0%");
  });

  it("renders an em dash instead of a guess", () => {
    expect(formatPct(null)).toBe("—");
    expect(formatPct(undefined)).toBe("—");
    expect(formatPct(Number.NaN)).toBe("—");
  });
});

describe("formatDisplayPct", () => {
  const window = (input: Partial<UsageWindow>): UsageWindow => ({
    id: "five_hour",
    label: "5-hour",
    ...input,
  });

  it("reads the share used or the share left", () => {
    expect(formatDisplayPct(window({ usedPct: 31 }), "used")).toBe("31%");
    expect(formatDisplayPct(window({ usedPct: 31 }), "remaining")).toBe("69% left");
    expect(formatDisplayPct(window({ remainingPct: 40 }), "remaining")).toBe("40% left");
    expect(formatDisplayPct(window({ usedPct: 50, remainingPct: 45 }), "remaining")).toBe("45% left");
  });

  it("renders an em dash when the window reports no percent", () => {
    expect(formatDisplayPct(window({}), "used")).toBe("—");
    expect(formatDisplayPct(window({}), "remaining")).toBe("—");
  });
});

describe("formatResetLabel", () => {
  it("names the unit that best fits the wait", () => {
    expect(formatResetLabel("2026-10-01T12:12:00Z", NOW)).toBe("resets 12m");
    expect(formatResetLabel("2026-10-01T14:00:00Z", NOW)).toBe("resets 2h");
    expect(formatResetLabel("2026-10-04T12:00:00Z", NOW)).toBe("resets 3d");
  });

  it("says now once the reset is due or nearly due", () => {
    expect(formatResetLabel("2026-10-01T11:59:00Z", NOW)).toBe("resetting now");
    expect(formatResetLabel("2026-10-01T12:00:30Z", NOW)).toBe("resetting now");
  });

  it("has no label when the reset is absent or unreadable", () => {
    expect(formatResetLabel(null, NOW)).toBeNull();
    expect(formatResetLabel(undefined, NOW)).toBeNull();
    expect(formatResetLabel("not a date", NOW)).toBeNull();
  });
});

describe("formatAmount", () => {
  it("formats dollars as the language writes currency", () => {
    expect(formatAmount(1234.5, "usd", "en")).toBe("$1,234.50");
    expect(formatAmount(1234.5, "usd", "fr").replace(/\u202f|\u00a0/g, " ")).toBe("1 234,50 $US");
  });

  it("compacts counts", () => {
    expect(formatAmount(12345, "credits", "en")).toBe("12.3K");
    expect(formatAmount(12345, "requests", "en")).toBe("12.3K");
    expect(formatAmount(1_234_567, "requests", "en")).toBe("1.2M");
  });

  it("abbreviates token counts", () => {
    expect(formatAmount(842, "tokens", "en")).toBe("842");
    expect(formatAmount(84_200, "tokens", "en")).toBe("84.2k");
    expect(formatAmount(1_300_000, "tokens", "en")).toBe("1.3M");
  });

  it("writes an unknown unit out plainly", () => {
    expect(formatAmount(5, "wombats", "en")).toBe("5 wombats");
  });
});

describe("formatCooldownUntil", () => {
  const noon = new Date(2026, 9, 12, 12, 0).getTime();

  it("is the time alone today", () => {
    expect(formatCooldownUntil(noon + 65 * 60_000, noon, "en-US")).toMatch(/^1:05\s?PM$/);
    expect(formatCooldownUntil(noon + 65 * 60_000, noon, "en-GB")).toBe("13:05");
  });

  it("adds the date on another day", () => {
    expect(formatCooldownUntil(noon + 26 * 3_600_000, noon, "en-US")).toMatch(/^Oct 13, 2:00\s?PM$/);
  });
});
