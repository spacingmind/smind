import { beforeEach, describe, expect, it } from "vitest";
import { STORAGE_KEYS } from "@/lib/storage";
import {
  DEFAULT_USAGE_PREFERENCES,
  effectiveUsagePins,
  isUsagePinned,
  parseUsagePreferences,
  readUsagePreferences,
  setUsageAccount,
  setUsageDisplayAs,
  setUsageInSidebar,
  toggleUsagePin,
  writeUsagePreferences,
} from "./preferences";
import type { AccountUsage, UsageWindow } from "./types";

const claudeFiveHour = { provider: "anthropic", windowId: "five_hour" };
const claudeWeekly = { provider: "anthropic", windowId: "weekly" };
const codexWeekly = { provider: "openai", windowId: "weekly" };

function account(provider: string, id: number, windows: UsageWindow[]): AccountUsage {
  return {
    accountId: id,
    key: `${provider}-${id}`,
    provider,
    displayName: `${provider} ${id}`,
    credentialType: "oauth",
    stale: false,
    nextRefreshAt: null,
    refreshBlocked: null,
    inFlight: false,
    lastError: null,
    report: { status: "available", windows, fetchedAt: "2026-10-02T00:00:00Z" },
  };
}

const reports = [
  account("anthropic", 1, [
    { id: "five_hour", label: "5-hour", summary: true, usedPct: 31 },
    { id: "weekly", label: "Weekly", summary: true, usedPct: 54 },
  ]),
  account("openai", 2, [{ id: "five_hour", label: "5-hour", summary: true, usedPct: 7 }]),
];

describe("usage preferences", () => {
  beforeEach(() => window.localStorage.clear());

  it("defaults the sidebar summary to off", () => {
    expect(DEFAULT_USAGE_PREFERENCES).toEqual({
      displayAs: "used",
      accountKey: null,
      inSidebar: false,
      pins: null,
    });
  });

  it("pins windows in the order the user pins them", () => {
    const pinned = toggleUsagePin(
      toggleUsagePin(DEFAULT_USAGE_PREFERENCES, [], codexWeekly),
      [],
      claudeFiveHour,
    );
    expect(pinned.pins).toEqual([codexWeekly, claudeFiveHour]);
    expect(isUsagePinned(pinned, { provider: "openai", windowId: "weekly" }, [])).toBe(true);
    expect(isUsagePinned(pinned, { provider: "openai", windowId: "five_hour" }, [])).toBe(false);
  });

  it("unpins a pinned window and keeps the others in order", () => {
    const three = [codexWeekly, claudeFiveHour, claudeWeekly];
    const unpinned = toggleUsagePin({ ...DEFAULT_USAGE_PREFERENCES, pins: three }, [], claudeFiveHour);
    expect(unpinned.pins).toEqual([codexWeekly, claudeWeekly]);
  });

  it("switches between used and remaining without touching pins", () => {
    const preferences = { ...DEFAULT_USAGE_PREFERENCES, pins: [codexWeekly] };
    expect(setUsageDisplayAs(preferences, "remaining")).toEqual({
      ...preferences,
      displayAs: "remaining",
    });
  });

  it("remembers the picked account without touching pins", () => {
    const preferences = { ...DEFAULT_USAGE_PREFERENCES, pins: [codexWeekly] };
    expect(setUsageAccount(preferences, "openai-2")).toEqual({
      ...preferences,
      accountKey: "openai-2",
    });
    expect(setUsageAccount(preferences, null).pins).toEqual([codexWeekly]);
  });

  it("turns the sidebar summary on and off without touching pins", () => {
    const preferences = { ...DEFAULT_USAGE_PREFERENCES, pins: [] };
    expect(setUsageInSidebar(preferences, true)).toEqual({ ...preferences, inSidebar: true });
  });

  it("reads preferences saved before the account pick existed as no pick", () => {
    expect(parseUsagePreferences({ displayAs: "remaining", pins: [codexWeekly] })).toEqual({
      displayAs: "remaining",
      accountKey: null,
      inSidebar: false,
      pins: [codexWeekly],
    });
  });

  it("reads default windows as pinned and the first toggle removes only that row", () => {
    const defaults = effectiveUsagePins(DEFAULT_USAGE_PREFERENCES, reports);
    expect(defaults).toEqual([claudeFiveHour, claudeWeekly, { provider: "openai", windowId: "five_hour" }]);
    for (const pin of defaults) expect(isUsagePinned(DEFAULT_USAGE_PREFERENCES, pin, reports)).toBe(true);

    const changed = toggleUsagePin(DEFAULT_USAGE_PREFERENCES, defaults, defaults[1]!);
    expect(isUsagePinned(changed, defaults[1]!, reports)).toBe(false);
    expect(isUsagePinned(changed, defaults[0]!, reports)).toBe(true);
    expect(isUsagePinned(changed, defaults[2]!, reports)).toBe(true);
  });

  it("keeps an explicit empty selection through a write and read", () => {
    const defaults = effectiveUsagePins(DEFAULT_USAGE_PREFERENCES, reports);
    let prefs = DEFAULT_USAGE_PREFERENCES;
    for (const pin of defaults) prefs = toggleUsagePin(prefs, defaults, pin);
    expect(prefs.pins).toEqual([]);

    writeUsagePreferences(prefs);
    const restored = readUsagePreferences();
    expect(restored.pins).toEqual([]);
    for (const pin of defaults) expect(isUsagePinned(restored, pin, reports)).toBe(false);
  });

  it("round-trips every field", () => {
    const prefs = { displayAs: "remaining" as const, accountKey: "openai-2", inSidebar: true, pins: [codexWeekly] };
    writeUsagePreferences(prefs);
    expect(readUsagePreferences()).toEqual(prefs);
  });

  it("falls back to defaults on garbage", () => {
    window.localStorage.setItem(STORAGE_KEYS.usage, "{not json");
    expect(readUsagePreferences()).toEqual(DEFAULT_USAGE_PREFERENCES);
    window.localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify([1, 2]));
    expect(readUsagePreferences()).toEqual(DEFAULT_USAGE_PREFERENCES);
    expect(readUsagePreferences()).toEqual(DEFAULT_USAGE_PREFERENCES);
    expect(parseUsagePreferences(null)).toEqual(DEFAULT_USAGE_PREFERENCES);
    expect(
      parseUsagePreferences({ displayAs: "bad", accountKey: 3, inSidebar: "yes", pins: "bad" }),
    ).toEqual(DEFAULT_USAGE_PREFERENCES);
    expect(parseUsagePreferences({ pins: [{ provider: "x" }] }).pins).toBeNull();
  });

  it("skips reports with no windows or an unusable report when defaulting", () => {
    const broken = { ...reports[0]!, report: null };
    const noWindows = account("anthropic", 3, []);
    expect(effectiveUsagePins(DEFAULT_USAGE_PREFERENCES, [broken, noWindows])).toEqual([]);
  });
});
