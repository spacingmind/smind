import { describe, expect, it } from "vitest";

import { formatTabTitle } from "@/lib/tab-title";

describe("formatTabTitle", () => {
  it("returns the base title unchanged when there's nothing unread", () => {
    expect(formatTabTitle("sMind", 0)).toBe("sMind");
  });

  it("prefixes the count when something is unread", () => {
    expect(formatTabTitle("sMind", 3)).toBe("(3) sMind");
  });

  it("prefixes a count of exactly one the same way", () => {
    expect(formatTabTitle("sMind", 1)).toBe("(1) sMind");
  });
});
