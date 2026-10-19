import { describe, expect, it } from "vitest";

import { formatRelativeTime, formatRelativeTimeShort } from "@/lib/relative-time";

const NOW = new Date("2024-06-01T12:00:00Z").getTime();

describe("formatRelativeTime", () => {
  it("reports 'just now' for anything under a minute", () => {
    expect(formatRelativeTime("2024-06-01T11:59:30Z", NOW)).toBe("just now");
  });

  it("reports minutes", () => {
    expect(formatRelativeTime("2024-06-01T11:55:00Z", NOW)).toBe("5m ago");
  });

  it("reports hours", () => {
    expect(formatRelativeTime("2024-06-01T09:00:00Z", NOW)).toBe("3h ago");
  });

  it("reports days", () => {
    expect(formatRelativeTime("2024-05-29T12:00:00Z", NOW)).toBe("3d ago");
  });

  it("reports months", () => {
    expect(formatRelativeTime("2024-03-01T12:00:00Z", NOW)).toBe("3mo ago");
  });

  it("reports years", () => {
    expect(formatRelativeTime("2022-06-01T12:00:00Z", NOW)).toBe("2y ago");
  });

  it("falls back to the raw string for an unparseable timestamp", () => {
    expect(formatRelativeTime("not-a-date", NOW)).toBe("not-a-date");
  });

  it("takes a Date or epoch ms as well as an ISO string, and a Date for now (the run footer's callers)", () => {
    const iso = "2024-06-01T11:55:00Z";
    expect(formatRelativeTime(new Date(iso), new Date(NOW))).toBe("5m ago");
    expect(formatRelativeTime(Date.parse(iso), NOW)).toBe("5m ago");
  });

  it("shortDateAfterDays switches to a short date once the age passes that many days (the run footer's rule)", () => {
    const opts = { shortDateAfterDays: 7 };
    expect(formatRelativeTime("2024-05-29T12:00:00Z", NOW, opts)).toBe("3d ago");
    expect(formatRelativeTime("2024-05-25T12:00:00Z", NOW, opts)).toMatch(/^May \d{1,2}$/);
    // Without the option the same age stays relative.
    expect(formatRelativeTime("2024-05-25T12:00:00Z", NOW)).toBe("7d ago");
  });
});

describe("formatRelativeTimeShort", () => {
  it.each([
    ["2024-06-01T11:59:30Z", "now"],
    ["2024-06-01T11:11:00Z", "49m"],
    ["2024-06-01T09:00:00Z", "3h"],
    ["2024-05-29T12:00:00Z", "3d"],
    ["2024-03-01T12:00:00Z", "3mo"],
    ["2022-06-01T12:00:00Z", "2y"],
  ])("%s -> %s", (iso, expected) => {
    expect(formatRelativeTimeShort(iso, NOW)).toBe(expected);
  });

  it("is empty for an unparseable timestamp rather than echoing it into a 24px slot", () => {
    expect(formatRelativeTimeShort("garbage", NOW)).toBe("");
  });
});
