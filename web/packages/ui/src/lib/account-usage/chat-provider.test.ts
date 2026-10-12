import { describe, expect, it } from "vitest";

import {
  chatPlanAccounts,
  hasPlanWindows,
  planProviderFor,
  planProviderName,
  summaryWindows,
} from "@/lib/account-usage/chat-provider";
import { claudeAccount, codexAccount, problemAccount } from "@/test/account-usage-fixtures";

describe("planProviderFor", () => {
  it("provider_map_claude_native_to_anthropic_codex_native_to_openai_others_none", () => {
    expect(planProviderFor("claude-native")).toBe("anthropic");
    expect(planProviderFor("codex-native")).toBe("openai");
    for (const other of ["glm", "kimi", "acp-gemini", "", null, undefined]) {
      expect(planProviderFor(other)).toBeNull();
    }
    expect(planProviderName("anthropic")).toBe("Claude");
    expect(planProviderName("openai")).toBe("Codex");
  });
});

describe("chatPlanAccounts", () => {
  const second = claudeAccount({ accountId: 3, key: "anthropic-3", displayName: "Work" });
  const apiKey = claudeAccount({ accountId: 4, key: "anthropic-4", credentialType: "api_key" });

  it("keeps every OAuth account of the provider and nothing else", () => {
    const got = chatPlanAccounts([claudeAccount(), codexAccount(), second, apiKey], "anthropic", null);
    expect(got.map((a) => a.key)).toEqual(["anthropic-1", "anthropic-3"]);
  });

  it("puts the filter-selected account first and keeps the rest in order", () => {
    const got = chatPlanAccounts([claudeAccount(), second], "anthropic", "anthropic-3");
    expect(got.map((a) => a.key)).toEqual(["anthropic-3", "anthropic-1"]);
  });

  it("a filter on another provider's account changes nothing", () => {
    const got = chatPlanAccounts([claudeAccount(), second, codexAccount()], "anthropic", "openai-2");
    expect(got.map((a) => a.key)).toEqual(["anthropic-1", "anthropic-3"]);
  });
});

describe("summaryWindows", () => {
  it("uses the summary-flagged windows, else the first with a percent, never throws on a reshaped report", () => {
    expect(summaryWindows(claudeAccount()).map((w) => w.id)).toEqual(["five_hour", "weekly"]);
    const account = claudeAccount();
    const windows = account.report!.windows!.map((w) => ({ ...w, summary: false }));
    expect(summaryWindows({ ...account, report: { ...account.report!, windows } }).map((w) => w.id)).toEqual(["five_hour"]);
    expect(summaryWindows(problemAccount("expired"))).toEqual([]);
    expect(summaryWindows({ ...account, report: { ...account.report!, windows: null as never } })).toEqual([]);
    expect(hasPlanWindows([problemAccount("error"), claudeAccount()])).toBe(true);
    expect(hasPlanWindows([problemAccount("error")])).toBe(false);
  });
});
