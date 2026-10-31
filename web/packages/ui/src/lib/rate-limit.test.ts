import { describe, expect, it } from "vitest";
import { describeRateLimit, describeRateLimitReset, formatResetClock, isRateLimitedRun, retryNotBefore, waitUntilLabel } from "@/lib/rate-limit";

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

describe("inferred reset marker", () => {
  const now = new Date(2026, 9, 23, 12, 0, 0);
  const same = new Date(2026, 9, 23, 17, 35);
  const next = new Date(2026, 9, 24, 9, 5);

  it("inferred reset shows ≈ in clock, describe, describeRateLimit, waitUntilLabel", () => {
    expect(formatResetClock(same, now, true)).toBe("≈17:35");
    expect(describeRateLimitReset(same.toISOString(), now, true)).toBe("resets ≈17:35");
    expect(describeRateLimit({ window: "5h", resetAt: same.toISOString(), inferred: true }, now)).toBe("Rate limited (5h window) · resets ≈17:35");
    expect(waitUntilLabel(same.toISOString(), now, true)).toBe("Wait until ≈17:35");
  });

  it("stated reset unchanged", () => {
    expect(formatResetClock(same, now)).toBe("17:35");
    expect(formatResetClock(same, now, false)).toBe("17:35");
    expect(describeRateLimitReset(same.toISOString(), now)).toBe("resets 17:35");
    expect(describeRateLimit({ resetAt: same.toISOString(), inferred: false }, now)).toBe("Rate limited · resets 17:35");
    expect(waitUntilLabel(same.toISOString(), now)).toBe("Wait until 17:35");
  });

  it("weekday prefix with ≈", () => {
    expect(formatResetClock(next, now, true)).toBe("Sat ≈09:05");
    expect(waitUntilLabel(next.toISOString(), now, true)).toBe("Wait until Sat ≈09:05");
  });
});

describe("plan limit kinds (run failure classes)", () => {
  it("treats plan_window_exhausted and the residual rate_limited as a rate-limited error run", () => {
    expect(isRateLimitedRun({ status: "error", failureKind: "plan_window_exhausted" })).toBe(true);
    expect(isRateLimitedRun({ status: "error", failureKind: "rate_limited" })).toBe(true);
  });
  it("does not treat a transient 429, auth, model or cli_exited failure as a plan limit", () => {
    for (const failureKind of ["transient_429", "auth_expired", "model_not_in_plan", "cli_exited", ""]) {
      expect(isRateLimitedRun({ status: "error", failureKind })).toBe(false);
    }
    expect(isRateLimitedRun({ status: "done", failureKind: "plan_window_exhausted" })).toBe(false);
  });
});
