import { describe, expect, it } from "vitest";

import { accountStatus } from "@/lib/account-status";
import {
  FIXTURE_NOW,
  claudeAccount,
  problemAccount,
} from "@/test/account-usage-fixtures";
import type { Account } from "@/lib/types";

const NOW = FIXTURE_NOW;

const ACCOUNT: Account = {
  id: 1,
  provider: "anthropic",
  label: "main",
  credentialType: "oauth",
  createdAt: "2024-01-01T00:00:00Z",
  updatedAt: "2024-01-01T00:00:00Z",
};

const OK_TEST = { ok: true, detail: "token is valid" };
const FAILED_TEST = { ok: false, detail: "invalid token" };

/** The cooldown the model keeps while `until` is ahead of `now`. */
function withCooldown(overrides: Record<string, unknown> = {}) {
  return claudeAccount(
    { cooldown: { until: new Date(NOW + 60_000).toISOString(), scope: "account", reason: "rejected", ...overrides } },
    NOW,
  );
}

describe("accountStatus", () => {
  it("prefers the usage report's displayName as the account name", () => {
    const status = accountStatus({
      account: ACCOUNT,
      usage: claudeAccount({ displayName: "Claude" }, NOW),
      test: undefined,
      now: NOW,
    });
    expect(status.name).toBe("Claude");
  });

  it("falls back to account.label when there is no usage report", () => {
    const status = accountStatus({ account: ACCOUNT, usage: undefined, test: undefined, now: NOW });
    expect(status.name).toBe("main");
  });

  it("an expired login is danger, with the usage copy as the reason", () => {
    const status = accountStatus({
      account: ACCOUNT,
      usage: problemAccount("expired", {}, NOW),
      test: undefined,
      now: NOW,
    });
    expect(status.tone).toBe("danger");
    expect(status.label).toBe("Login expired");
    expect(status.reason).toMatch(/Login expired.*Reconnect it in Providers\./);
  });

  it("a rejected login is danger", () => {
    const status = accountStatus({
      account: ACCOUNT,
      usage: problemAccount("rejected", {}, NOW),
      test: undefined,
      now: NOW,
    });
    expect(status.tone).toBe("danger");
    expect(status.label).toBe("Login rejected");
    expect(status.reason).toContain("Reconnect it in Providers.");
  });

  it("a failing provider.test is danger with the test detail", () => {
    const status = accountStatus({ account: ACCOUNT, usage: undefined, test: FAILED_TEST, now: NOW });
    expect(status).toMatchObject({ tone: "danger", label: "Connection failed", reason: "invalid token" });
  });

  it("an active cooldown is warning with the cooldown headline", () => {
    const status = accountStatus({ account: ACCOUNT, usage: withCooldown(), test: undefined, now: NOW });
    expect(status.tone).toBe("warning");
    expect(status.label).toBe("Rate limited");
    expect(status.reason).toContain("Rate limited until");
  });

  it("an error-status report is warning 'Usage unavailable'", () => {
    const status = accountStatus({
      account: ACCOUNT,
      usage: problemAccount("error", {}, NOW),
      test: undefined,
      now: NOW,
    });
    expect(status.tone).toBe("warning");
    expect(status.label).toBe("Usage unavailable");
    expect(status.reason).toContain("Couldn't read usage");
  });

  it("a passing provider.test is ok with the test detail", () => {
    const status = accountStatus({ account: ACCOUNT, usage: undefined, test: OK_TEST, now: NOW });
    expect(status).toMatchObject({ tone: "ok", label: "Connection ok", reason: "token is valid" });
  });

  it("an available report with no test is ok 'Connected'", () => {
    const status = accountStatus({ account: ACCOUNT, usage: claudeAccount({}, NOW), test: undefined, now: NOW });
    expect(status).toMatchObject({ tone: "ok", label: "Connected", reason: null });
  });

  it("nothing known is neutral 'Not tested yet'", () => {
    const status = accountStatus({ account: ACCOUNT, usage: undefined, test: undefined, now: NOW });
    expect(status).toMatchObject({ tone: "neutral", label: "Not tested yet", reason: null });
  });

  it("an expired report beats a passing provider.test", () => {
    const status = accountStatus({
      account: ACCOUNT,
      usage: problemAccount("expired", {}, NOW),
      test: OK_TEST,
      now: NOW,
    });
    expect(status.tone).toBe("danger");
    expect(status.label).toBe("Login expired");
  });

  it("a failing provider.test beats no signal at all", () => {
    const status = accountStatus({ account: ACCOUNT, usage: undefined, test: FAILED_TEST, now: NOW });
    expect(status.tone).toBe("danger");
  });

  it("a no_quota report is not a failure and falls through to the local test", () => {
    const status = accountStatus({
      account: ACCOUNT,
      usage: problemAccount("no_quota", {}, NOW),
      test: OK_TEST,
      now: NOW,
    });
    expect(status).toMatchObject({ tone: "ok", label: "Connection ok" });
  });

  it("a no_quota report with no test falls through to 'Not tested yet'", () => {
    const status = accountStatus({
      account: ACCOUNT,
      usage: problemAccount("no_quota", {}, NOW),
      test: undefined,
      now: NOW,
    });
    expect(status.tone).toBe("neutral");
  });
});
