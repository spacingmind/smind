import { describe, expect, it } from "vitest";

import { buildProviderNav } from "@/lib/providers-view";
import { healthBadge } from "@/lib/providers-view";
import type { AccountUsage } from "@/lib/account-usage/types";
import type { Account, ProviderInfo, ProviderTestResult } from "@/lib/types";

function provider(overrides: Partial<ProviderInfo> = {}): ProviderInfo {
  return { id: "glm" as ProviderInfo["id"], ...overrides };
}

function account(overrides: Partial<Account> = {}): Account {
  return { id: 1, provider: "anthropic", label: "a", credentialType: "oauth", createdAt: "", updatedAt: "", ...overrides };
}

describe("buildProviderNav", () => {
  it("nav has one item per provider in order", () => {
    const items = buildProviderNav({
      providers: [
        provider({ id: "codex-native", label: "Beta", credentialKind: "oauth", accountProvider: "beta" }),
        provider({ id: "claude-native", label: "Alpha", credentialKind: "api-key", accountProvider: "alpha" }),
        provider({ id: "glm" }),
      ],
      accounts: [],
      toneOf: () => undefined,
    });
    expect(items.map((item) => item.id)).toEqual(["codex-native", "claude-native", "glm"]);
    expect(items[0]).toMatchObject({
      label: "Beta",
      accountProvider: "beta",
      kind: "oauth",
      accountIds: [],
      tone: "neutral",
      connected: false,
    });
    expect(items[1].kind).toBe("api-key");
    expect(items[2]).toMatchObject({ label: "glm", kind: "other", accountProvider: undefined });
  });

  it("nav item tone is the worst account tone", () => {
    const items = buildProviderNav({
      providers: [provider({ id: "glm", credentialKind: "oauth", accountProvider: "anthropic" })],
      accounts: [account({ id: 1, label: "a" }), account({ id: 2, label: "b" }), account({ id: 3, label: "c" })],
      toneOf: (id) => (id === 1 ? "ok" : id === 2 ? "danger" : undefined),
    });
    expect(items[0]).toMatchObject({ accountIds: [1, 2, 3], tone: "danger", connected: true });
  });

  it("nav puts unmapped accounts in an other bucket", () => {
    const items = buildProviderNav({
      providers: [provider({ id: "glm", credentialKind: "oauth", accountProvider: "anthropic" })],
      accounts: [account({ id: 5, provider: "xai", label: "x" })],
      toneOf: () => "warning",
    });
    expect(items[1]).toMatchObject({
      id: "other",
      label: "Other accounts",
      kind: "other",
      accountIds: [5],
      tone: "warning",
      connected: true,
    });
  });

  it("nav treats cli providers as accountless", () => {
    const items = buildProviderNav({
      providers: [provider({ id: "glm", kind: "cli", credentialKind: "oauth", accountProvider: "anthropic" })],
      accounts: [account({ id: 9, label: "a" })],
      toneOf: () => "danger",
    });
    expect(items[0]).toMatchObject({ kind: "cli", accountIds: [], tone: "neutral", connected: false });
  });
});

describe("healthBadge", () => {
  const now = Date.UTC(2026, 0, 1, 12, 0, 0);
  const formatTime = (ms: number) => `T${ms - now}`;
  const badge = (usage: Partial<AccountUsage> | undefined, test?: ProviderTestResult) =>
    healthBadge({
      usage: usage === undefined ? undefined : ({ ...baseUsage(), ...usage } as AccountUsage),
      test,
      now,
      formatTime,
    });

  it("health badge order: relogin beats cooldown beats error beats active", () => {
    const expired = badge({ report: { status: "error", problem: { kind: "expired" }, fetchedAt: "" }, cooldown: { until: "2026-01-01T13:00:00Z", scope: "account", reason: "rejected" } }, { ok: false, detail: "boom" });
    expect(expired.kind).toBe("relogin");
    expect(expired.tone).toBe("danger");

    const cooling = badge(
      { cooldown: { until: "2026-01-01T13:00:00Z", scope: "account", reason: "rejected" } },
      { ok: false, detail: "boom" },
    );
    expect(cooling.kind).toBe("cooldown");
    expect(cooling.tone).toBe("warning");

    const erroring = badge({}, { ok: false, detail: "boom" });
    expect(erroring).toMatchObject({ kind: "error", tone: "danger", detail: "boom" });

    const active = badge({ report: { status: "available", fetchedAt: "" } }, { ok: true, detail: "" });
    expect(active).toMatchObject({ kind: "active", tone: "ok", detail: null });
  });

  it("health badge cooldown label shows the end time", () => {
    const untilMs = Date.UTC(2026, 0, 1, 13, 0, 0);
    const result = badge({ cooldown: { until: "2026-01-01T13:00:00Z", scope: "account", reason: "rejected" } });
    expect(result.label).toBe(`Cooling down until ${formatTime(untilMs)}`);
    expect(result.detail).toMatch(/^Rate limited until /);
  });

  it("health badge untested without data", () => {
    expect(badge(undefined, undefined)).toEqual({
      kind: "untested",
      label: "Not tested",
      tone: "neutral",
      detail: null,
    });
  });
});

function baseUsage(): AccountUsage {
  return {
    accountId: 1,
    key: "anthropic-1",
    provider: "anthropic",
    displayName: "Claude",
    credentialType: "oauth",
    stale: false,
    nextRefreshAt: null,
    refreshBlocked: null,
    inFlight: false,
    lastError: null,
    report: null,
  };
}
