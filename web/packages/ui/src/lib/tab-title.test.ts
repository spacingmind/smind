import { describe, expect, it } from "vitest";

import { formatTabTitle } from "@/lib/tab-title";

describe("formatTabTitle", () => {
  it("returns the base title unchanged when there's nothing unread", () => {
    expect(formatTabTitle("smind", 0)).toBe("smind");
  });

  it("prefixes the count when something is unread", () => {
    expect(formatTabTitle("smind", 3)).toBe("(3) smind");
  });

  it("prefixes a count of exactly one the same way", () => {
    expect(formatTabTitle("smind", 1)).toBe("(1) smind");
  });
});
