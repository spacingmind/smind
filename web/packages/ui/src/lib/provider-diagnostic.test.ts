import { describe, expect, it } from "vitest";

import { buildProviderDiagnostic } from "@/lib/provider-diagnostic";
import { accountStatus, type AccountStatus } from "@/lib/account-status";
import { healthBadge } from "@/lib/providers-view";
import { claudeAccount } from "@/test/account-usage-fixtures";
import type { AccountRowData } from "@/hooks/use-account-rows";
import type { ProviderInfo, ProviderTestResult } from "@/lib/types";

const NOW = Date.parse("2026-10-12T12:00:00Z");

function provider(over: Partial<ProviderInfo> = {}): ProviderInfo {
  return {
    id: "claude-native",
    label: "Claude Code",
    credentialKind: "oauth",
    models: [
      { id: "sonnet", label: "Sonnet" },
      { id: "opus", label: "Opus", default: true },
    ],
    modelsDiscovered: true,
    ...over,
  };
}

function row(over: Partial<AccountRowData> & { extra?: Record<string, unknown> } = {}): AccountRowData {
  const account = over.account ?? {
    id: 1,
    provider: "anthropic",
    label: "Work account",
    credentialType: "oauth",
    createdAt: "",
    updatedAt: "",
  };
  const usage = "usage" in over ? over.usage : claudeAccount({ accountId: account.id }, NOW);
  const test = "test" in over ? over.test : undefined;
  return {
    account,
    usage,
    test,
    status: accountStatus({ account, usage, test }),
    badge: healthBadge({ usage, test, now: NOW }),
    ...over,
  } as AccountRowData;
}

describe("buildProviderDiagnostic", () => {
  it("diagnostic lists provider, models and per-account health", () => {
    const text = buildProviderDiagnostic({
      provider: provider(),
      label: "Claude Code",
      navKind: "oauth",
      rows: [row(), row({ account: { id: 2, provider: "anthropic", label: "Other", credentialType: "api-key", createdAt: "", updatedAt: "" }, usage: undefined, test: { ok: false, detail: "401 unauthorized" } as ProviderTestResult })],
      now: NOW,
      generatedAt: "2026-10-12T12:00:00.000Z",
    });
    expect(text).toContain("Provider: Claude Code (claude-native)");
    expect(text).toContain("Kind: oauth   Credentials: oauth");
    expect(text).toContain("Models: 2 (reported by the agent)");
    expect(text).toContain("Accounts: 2");
    expect(text).toContain("health=");
    expect(text).toContain("usage: available");
    expect(text).toContain("test: failed 401 unauthorized");
  });

  it("diagnostic never includes credential-looking fields", () => {
    const r = row();
    const secret = {
      credentialData: "sk-super-secret",
      token: "tok_abc123",
      email: "user@example.com",
      identity: "oauth-identity-blob",
    };
    const text = buildProviderDiagnostic({
      provider: provider(),
      label: "Claude Code",
      navKind: "oauth",
      rows: [{ ...r, account: { ...r.account, ...secret } } as AccountRowData],
      now: NOW,
    });
    for (const value of Object.values(secret)) {
      expect(text).not.toContain(value);
    }
  });

  it("diagnostic handles a provider with no accounts or models", () => {
    const text = buildProviderDiagnostic({
      provider: null,
      label: "Other",
      navKind: "other",
      rows: [],
      now: NOW,
      generatedAt: "2026-10-12T12:00:00.000Z",
    });
    expect(text).toContain("Provider: Other (unknown)");
    expect(text).toContain("Kind: other   Credentials: none");
    expect(text).toContain("Models: 0 (built-in catalog)");
    expect(text).toContain("Accounts: 0");
  });
});
