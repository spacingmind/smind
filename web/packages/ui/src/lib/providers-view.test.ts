import { describe, expect, it } from "vitest";

import { buildProviderNav } from "@/lib/providers-view";
import {
  filterAccountRows,
  healthBadge,
  mostHeadroomAccountIds,
  sortAccountRows,
} from "@/lib/providers-view";
import type { HealthBadge } from "@/lib/providers-view";
import type { AccountUsage, UsageWindow } from "@/lib/account-usage/types";
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
    expect(items[2]).toMatchObject({ label: "GLM", kind: "other", accountProvider: undefined });
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

  it("relogin detail names the fix on the row, not a pointer back to Providers", () => {
    const usage = { report: { status: "unavailable", problem: { kind: "expired" }, fetchedAt: "" } } as Partial<AccountUsage>;
    const full = { ...baseUsage(), ...usage } as AccountUsage;
    const oauth = healthBadge({ usage: full, test: undefined, credentialType: "oauth", now, formatTime });
    expect(oauth.detail).toMatch(/^Login expired.* — use Re-login\.$/);
    expect(oauth.detail).not.toMatch(/Reconnect it in Providers/);
    const key = healthBadge({ usage: full, test: undefined, credentialType: "api_key", now, formatTime });
    expect(key.detail).toMatch(/ — use Update credential\.$/);
  });

  it("a usage fetch error is a warning, not an error", () => {
    const result = badge({ report: { status: "error", fetchedAt: "" } });
    expect(result).toMatchObject({ kind: "error", label: "Usage unavailable", tone: "warning" });
    expect(typeof result.detail).toBe("string");
    expect(badge({ report: { status: "error", fetchedAt: "" } }, { ok: false, detail: "boom" })).toMatchObject({
      kind: "error",
      label: "Error",
      tone: "danger",
      detail: "boom",
    });
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

describe("mostHeadroomAccountIds", () => {
  const active: HealthBadge = { kind: "active", label: "Active", detail: null, tone: "ok" };
  const cooling: HealthBadge = { kind: "cooldown", label: "Cooling", detail: null, tone: "warning" };

  function usageWith(windows: Partial<UsageWindow>[]): AccountUsage {
    return {
      ...baseUsage(),
      report: {
        status: "available",
        fetchedAt: "",
        windows: windows.map((window, i) => ({ id: `w${i}`, label: `w${i}`, ...window })),
      },
    };
  }
  const row = (accountId: number, provider: string, badge: HealthBadge, usage?: AccountUsage) => ({
    accountId,
    provider,
    badge,
    usage,
  });

  it("most headroom picks the best active account per provider", () => {
    const ids = mostHeadroomAccountIds({
      rows: [
        row(1, "anthropic", active, usageWith([{ remainingPct: 80 }, { remainingPct: 50 }])),
        row(2, "anthropic", active, usageWith([{ remainingPct: 70 }])),
        row(3, "openai", active, usageWith([{ usedPct: 90 }])),
        row(4, "openai", active, usageWith([{ remainingPct: 20 }])),
        row(5, "kimi", active, usageWith([{ remainingPct: 90 }])),
      ],
    });
    expect(ids).toEqual(new Set([2, 4]));
  });

  it("most headroom needs two candidates and skips unhealthy accounts", () => {
    const ids = mostHeadroomAccountIds({
      rows: [
        row(1, "anthropic", cooling, usageWith([{ remainingPct: 90 }])),
        row(2, "anthropic", active, usageWith([{ remainingPct: 10 }])),
        row(3, "openai", active, usageWith([{ remainingPct: 50 }])),
        row(4, "openai", active, undefined),
      ],
    });
    expect(ids).toEqual(new Set());
  });

  it("most headroom ignores reshaped windows", () => {
    const reshaped = (windows: unknown): AccountUsage => ({
      ...baseUsage(),
      report: { status: "available", fetchedAt: "", windows } as never,
    });
    const ids = mostHeadroomAccountIds({
      rows: [
        row(1, "anthropic", active, reshaped([null, 7])),
        row(2, "anthropic", active, reshaped({})),
        row(3, "anthropic", active, reshaped([null, { id: "w", label: "w", remainingPct: 40 }])),
        row(4, "anthropic", active, usageWith([{ remainingPct: 60 }])),
      ],
    });
    expect(ids).toEqual(new Set([4]));
  });

  it("most headroom is not marked when candidates tie", () => {
    const ids = mostHeadroomAccountIds({
      rows: [
        row(1, "anthropic", active, usageWith([{ remainingPct: 50 }])),
        row(2, "anthropic", active, usageWith([{ remainingPct: 50 }])),
        row(3, "openai", active, usageWith([{ remainingPct: 80 }])),
        row(4, "openai", active, usageWith([{ remainingPct: 20 }])),
      ],
    });
    expect(ids).toEqual(new Set([3]));
  });
});

describe("filter and sort account rows", () => {
  const active: HealthBadge = { kind: "active", label: "Active", detail: null, tone: "ok" };
  const relogin: HealthBadge = { kind: "relogin", label: "Needs re-login", detail: null, tone: "danger" };
  const untested: HealthBadge = { kind: "untested", label: "Not tested", detail: null, tone: "neutral" };

  const row = (label: string, badge: HealthBadge, usage?: Partial<AccountUsage>) => ({
    account: { id: 1, provider: "anthropic", label, credentialType: "oauth", createdAt: "", updatedAt: "" },
    badge,
    usage: usage === undefined ? undefined : ({ ...baseUsage(), ...usage } as AccountUsage),
  });

  it("filter matches label, display name and plan", () => {
    const rows = [
      row("Zebra", active),
      row("Work", active, { displayName: "Claude Max" }),
      row("Side", active, { report: { status: "available", planLabel: "Team plan", fetchedAt: "" } }),
    ];
    const names = (filtered: typeof rows) => filtered.map((r) => r.account.label);
    expect(names(filterAccountRows(rows, { query: "  zeb ", filter: "all" }))).toEqual(["Zebra"]);
    expect(names(filterAccountRows(rows, { query: "max", filter: "all" }))).toEqual(["Work"]);
    expect(names(filterAccountRows(rows, { query: "team", filter: "all" }))).toEqual(["Side"]);
  });

  it("filter by attention and active", () => {
    const rows = [row("A", active), row("B", relogin), row("C", untested)];
    const names = (filtered: typeof rows) => filtered.map((r) => r.account.label);
    expect(names(filterAccountRows(rows, { query: "", filter: "attention" }))).toEqual(["B"]);
    expect(names(filterAccountRows(rows, { query: "", filter: "active" }))).toEqual(["A"]);
    expect(names(filterAccountRows(rows, { query: "", filter: "all" }))).toEqual(["A", "B", "C"]);
  });

  it("sort by status puts attention first", () => {
    const rows = [row("Alpha", active), row("Beta", relogin), row("Gamma", untested)];
    const names = (sorted: typeof rows) => sorted.map((r) => r.account.label);
    expect(names(sortAccountRows(rows, "status"))).toEqual(["Beta", "Gamma", "Alpha"]);
    expect(names(sortAccountRows(rows, "name"))).toEqual(["Alpha", "Beta", "Gamma"]);
    expect(rows.map((r) => r.account.label)).toEqual(["Alpha", "Beta", "Gamma"].slice().reverse().reverse());
  });
});
