import { describe, expect, it } from "vitest";
import { COOLDOWN_MODEL_MAX_CHARS, usageCopy } from "./copy";
import type { ActiveCooldown } from "./model";
import type { AccountUsage, UsageReport } from "./types";

const NOW = Date.parse("2026-10-01T12:00:00Z");

function usage(input: Partial<AccountUsage> = {}): AccountUsage {
  return {
    accountId: 1,
    key: "anthropic-1",
    provider: "anthropic",
    displayName: "Work",
    credentialType: "oauth",
    stale: false,
    nextRefreshAt: null,
    refreshBlocked: null,
    inFlight: false,
    lastError: null,
    report: null,
    ...input,
  };
}

function withReport(report: Partial<UsageReport>, rest: Partial<AccountUsage> = {}): AccountUsage {
  return usage({
    report: { status: "unavailable", fetchedAt: "2026-10-01T11:59:00Z", ...report },
    ...rest,
  });
}

describe("usageCopy.problem", () => {
  it.each([
    [
      { kind: "expired", expiresAt: "2026-10-01T10:00:00Z" },
      "Login expired 2h ago. Reconnect it in Providers.",
    ],
    [{ kind: "expired" }, "Login expired. Reconnect it in Providers."],
    [{ kind: "expired", expiresAt: "garbage" }, "Login expired. Reconnect it in Providers."],
    [
      { kind: "rejected", status: 401 },
      "Login rejected (HTTP 401). Reconnect it in Providers.",
    ],
    [{ kind: "rejected" }, "Login rejected. Reconnect it in Providers."],
    [{ kind: "no_quota", detail: "No active coding plan" }, "No active coding plan"],
    [{ kind: "no_quota" }, "No plan usage for this account."],
  ] as const)("problem %j", (problem, sentence) => {
    expect(usageCopy.problem(withReport({ problem }), NOW)).toBe(sentence);
  });

  it("says why an error report has no windows, with the retry time", () => {
    const soon = "2026-10-01T12:03:00Z";
    expect(
      usageCopy.problem(withReport({ status: "error", error: "timeout" }, { nextRefreshAt: soon }), NOW),
    ).toBe("Couldn't read usage: timeout, retry in 3 min");
    expect(
      usageCopy.problem(
        withReport({ status: "error", error: "timeout" }, { nextRefreshAt: "2026-10-01T12:00:45Z" }),
        NOW,
      ),
    ).toBe("Couldn't read usage: timeout, retry in 45 s");
    expect(
      usageCopy.problem(
        withReport({ status: "error", error: "timeout" }, { nextRefreshAt: "2026-10-01T14:00:00Z" }),
        NOW,
      ),
    ).toBe("Couldn't read usage: timeout, retry in 2 h");
  });

  it("omits the retry when the next refresh is past or unknown", () => {
    expect(
      usageCopy.problem(
        withReport({ status: "error", error: "timeout" }, { nextRefreshAt: "2026-10-01T11:00:00Z" }),
        NOW,
      ),
    ).toBe("Couldn't read usage: timeout");
    expect(usageCopy.problem(withReport({ status: "error", error: "timeout" }), NOW)).toBe(
      "Couldn't read usage: timeout",
    );
  });

  it("falls back from the report's error to lastError to unknown", () => {
    expect(usageCopy.problem(withReport({ status: "error" }, { lastError: "rate limited" }), NOW)).toBe(
      "Couldn't read usage: rate limited",
    );
    expect(usageCopy.problem(withReport({ status: "error" }), NOW)).toBe(
      "Couldn't read usage: unknown error",
    );
  });

  it("reads the same error sentence when there is no report yet", () => {
    expect(usageCopy.problem(usage({ lastError: "timeout" }), NOW)).toBe(
      "Couldn't read usage: timeout",
    );
  });

  it("has nothing to say for an available or absent report", () => {
    expect(usageCopy.problem(withReport({ status: "available" }), NOW)).toBeNull();
    expect(usageCopy.problem(usage(), NOW)).toBeNull();
  });
});

describe("usageCopy.refreshState", () => {
  const at = (refreshBlocked: AccountUsage["refreshBlocked"], nextRefreshAt: string | null) =>
    usageCopy.refreshState(usage({ refreshBlocked, nextRefreshAt }), NOW);

  it("explains every block", () => {
    expect(at("backoff", "2026-10-01T12:12:00Z")).toEqual({
      disabled: true,
      label: "Rate limited, retry in 12 min",
    });
    expect(at("floor", "2026-10-01T12:00:30Z")).toEqual({
      disabled: true,
      label: "Refreshed just now",
    });
    expect(at("budget", "2026-10-01T12:24:00Z")).toEqual({
      disabled: true,
      label: "Refresh limit reached, next in 24 min",
    });
    expect(at("rejected", null)).toEqual({
      disabled: true,
      label: "Reconnect this account to refresh",
    });
  });

  it("drops the time tail when there is no next refresh", () => {
    expect(at("backoff", null).label).toBe("Rate limited");
    expect(at("budget", null).label).toBe("Refresh limit reached");
  });

  it("is a plain enabled Refresh when nothing blocks it", () => {
    expect(at(null, null)).toEqual({ disabled: false, label: "Refresh" });
  });
});

describe("usageCopy strings", () => {
  it("keeps the fixed labels", () => {
    expect(usageCopy.daemonTooOld).toBe("Update the daemon to see plan usage");
    expect(usageCopy.refreshFailed).toBe("Unable to refresh usage");
    expect(usageCopy.updated).toBe("Updated");
    expect(usageCopy.loading).toBe("Loading plan usage…");
    expect(usageCopy.empty).toBe("No accounts with plan usage yet");
    expect(usageCopy.unavailableLabel).toBe("Unavailable");
    expect(usageCopy.errorLabel).toBe("Error");
    expect(usageCopy.disabled).toContain("accountUsage.enabled");
  });
});

describe("usageCopy.cooldown", () => {
  const noon = new Date(2026, 9, 12, 12, 0).getTime();
  const base: ActiveCooldown = { untilMs: noon + 65 * 60_000, scope: "account", reason: "rejected" };
  const say = (over: Partial<ActiveCooldown> = {}) => usageCopy.cooldown({ ...base, ...over }, noon, "en-GB");

  it("says when, and that it was seen on a proxied request", () => {
    expect(say()).toEqual({
      label: "Rate limited",
      until: "until 13:05",
      headline: "Rate limited until 13:05",
      rejected: null,
      note: "seen on a proxied request",
    });
  });

  it("adds the date when the cooldown ends on another day", () => {
    const later = usageCopy.cooldown({ ...base, untilMs: noon + 30 * 3_600_000 }, noon, "en-US");
    expect(later.headline).toMatch(/^Rate limited until Oct 13, 6:00\s?PM$/);
  });

  it("names the rejected window", () => {
    expect(say({ window: "five_hour" }).rejected).toBe("5-hour window");
    expect(say({ window: "weekly" }).rejected).toBe("weekly window");
    expect(say({ window: "weekly_model", scope: "model", model: "opus-4" }).rejected).toBe("weekly limit for opus-4");
    expect(say({ window: "weekly_model" }).rejected).toBe("weekly model limit");
    expect(say({ window: "unified" }).rejected).toBeNull();
  });

  it("a model scope without a window reads '<model> only'", () => {
    expect(say({ scope: "model", model: "opus-4" }).rejected).toBe("opus-4 only");
    expect(say({ scope: "model" }).rejected).toBeNull();
  });

  it("a fallback says the reset time is unknown", () => {
    expect(say({ reason: "fallback" }).rejected).toBe("reset time unknown");
    expect(say({ reason: "fallback", window: "five_hour" }).rejected).toBe("5-hour window · reset time unknown");
  });

  it("truncates a long model name", () => {
    const long = "m".repeat(200);
    const text = say({ scope: "model", model: long }).rejected ?? "";
    expect(text).toBe(`${"m".repeat(COOLDOWN_MODEL_MAX_CHARS - 1)}… only`);
  });
});

describe("usageCopy.windowStatus", () => {
  it("has a word for the two limiting statuses only", () => {
    expect(usageCopy.windowStatus("rejected")).toBe("Limit reached");
    expect(usageCopy.windowStatus("allowed_warning")).toBe("Near limit");
    expect(usageCopy.windowStatus("allowed")).toBeNull();
    expect(usageCopy.windowStatus(undefined)).toBeNull();
  });
});
