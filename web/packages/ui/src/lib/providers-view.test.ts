import { describe, expect, it } from "vitest";

import { buildProviderNav } from "@/lib/providers-view";
import type { ProviderInfo } from "@/lib/types";

function provider(overrides: Partial<ProviderInfo> = {}): ProviderInfo {
  return { id: "p", ...overrides };
}

describe("buildProviderNav", () => {
  it("nav has one item per provider in order", () => {
    const items = buildProviderNav({
      providers: [
        provider({ id: "b", label: "Beta", credentialKind: "oauth", accountProvider: "beta" }),
        provider({ id: "a", label: "Alpha", credentialKind: "api-key", accountProvider: "alpha" }),
        provider({ id: "c" }),
      ],
      accounts: [],
      toneOf: () => undefined,
    });
    expect(items.map((item) => item.id)).toEqual(["b", "a", "c"]);
    expect(items[0]).toMatchObject({
      label: "Beta",
      accountProvider: "beta",
      kind: "oauth",
      accountIds: [],
      tone: "neutral",
      connected: false,
    });
    expect(items[1].kind).toBe("api-key");
    expect(items[2]).toMatchObject({ label: "c", kind: "other", accountProvider: undefined });
  });

  it("nav item tone is the worst account tone", () => {
    const items = buildProviderNav({
      providers: [provider({ id: "p", credentialKind: "oauth", accountProvider: "anthropic" })],
      accounts: [
        { id: 1, provider: "anthropic", label: "a", credentialType: "oauth", createdAt: "", updatedAt: "" },
        { id: 2, provider: "anthropic", label: "b", credentialType: "oauth", createdAt: "", updatedAt: "" },
        { id: 3, provider: "anthropic", label: "c", credentialType: "oauth", createdAt: "", updatedAt: "" },
      ],
      toneOf: (id) => (id === 1 ? "ok" : id === 2 ? "danger" : undefined),
    });
    expect(items[0]).toMatchObject({ accountIds: [1, 2, 3], tone: "danger", connected: true });
  });

  it("nav puts unmapped accounts in an other bucket", () => {
    const items = buildProviderNav({
      providers: [provider({ id: "p", credentialKind: "oauth", accountProvider: "anthropic" })],
      accounts: [
        { id: 5, provider: "xai", label: "x", credentialType: "oauth", createdAt: "", updatedAt: "" },
      ],
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
      providers: [provider({ id: "cli", kind: "cli", credentialKind: "oauth", accountProvider: "anthropic" })],
      accounts: [
        { id: 9, provider: "anthropic", label: "a", credentialType: "oauth", createdAt: "", updatedAt: "" },
      ],
      toneOf: () => "danger",
    });
    expect(items[0]).toMatchObject({ kind: "cli", accountIds: [], tone: "neutral", connected: false });
  });
});
