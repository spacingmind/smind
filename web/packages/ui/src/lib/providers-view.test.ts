import { describe, expect, it } from "vitest";

import { buildProviderNav } from "@/lib/providers-view";
import type { Account, ProviderInfo } from "@/lib/types";

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
