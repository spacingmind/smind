import { describe, expect, it } from "vitest";

import { formatRelativeTime } from "@/lib/relative-time";

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
});
