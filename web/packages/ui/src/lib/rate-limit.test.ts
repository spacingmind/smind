import { describe, expect, it } from "vitest";
import { retryNotBefore, waitUntilLabel } from "@/lib/rate-limit";

describe("waitUntilLabel", () => {
  const now = new Date(2026, 9, 23, 12, 0, 0);

  it("reads Wait until HH:MM on the same day", () => {
    expect(waitUntilLabel(new Date(2026, 9, 23, 17, 35).toISOString(), now)).toBe("Wait until 17:35");
  });

  it("prefixes the weekday on another day", () => {
    expect(waitUntilLabel(new Date(2026, 9, 24, 9, 5).toISOString(), now)).toBe("Wait until Sat 09:05");
  });

  it("is null when the reset is absent, unparseable or past", () => {
    expect(waitUntilLabel(undefined, now)).toBeNull();
    expect(waitUntilLabel("", now)).toBeNull();
    expect(waitUntilLabel("nope", now)).toBeNull();
    expect(waitUntilLabel(new Date(2026, 9, 23, 11, 0).toISOString(), now)).toBeNull();
    expect(waitUntilLabel(now.toISOString(), now)).toBeNull();
  });
});

describe("retryNotBefore", () => {
  it("is the reset plus 30 s as RFC 3339", () => {
    expect(retryNotBefore("2026-10-23T10:35:00Z")).toBe("2026-10-23T10:35:30.000Z");
  });

  it("is null when the reset is absent or unparseable", () => {
    expect(retryNotBefore(undefined)).toBeNull();
    expect(retryNotBefore("nope")).toBeNull();
  });
});
