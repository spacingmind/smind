import { describe, expect, it } from "vitest";

import { clampRunes, DENY_REASON_COUNTER_FROM, DENY_REASON_MAX, denyReasonCounter, runeCount } from "@/lib/deny-reason";

describe("deny reason helpers", () => {
  it("counts runes like the daemon, not UTF-16 units", () => {
    expect(runeCount("abc")).toBe(3);
    expect(runeCount("😀")).toBe(1);
    expect("😀".length).toBe(2);
    expect(runeCount("é😀é")).toBe(3);
  });

  it("clamps by runes and never splits a surrogate pair", () => {
    const text = "😀".repeat(DENY_REASON_MAX + 5);
    const clamped = clampRunes(text);
    expect(runeCount(clamped)).toBe(DENY_REASON_MAX);
    expect(clamped.endsWith("😀")).toBe(true);
    expect(clampRunes("short")).toBe("short");
  });

  it("shows the counter from the threshold and flags the limit", () => {
    expect(denyReasonCounter("x".repeat(DENY_REASON_COUNTER_FROM - 1)).shown).toBe(false);
    const near = denyReasonCounter("x".repeat(DENY_REASON_COUNTER_FROM));
    expect(near).toEqual({ shown: true, used: DENY_REASON_COUNTER_FROM, max: DENY_REASON_MAX, atLimit: false });
    expect(denyReasonCounter("x".repeat(DENY_REASON_MAX)).atLimit).toBe(true);
  });
});
