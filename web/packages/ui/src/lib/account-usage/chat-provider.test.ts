import { describe, expect, it } from "vitest";

import {
  chatPlanAccounts,
  hasPlanWindows,
  planProviderFor,
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

  it("keeps_a_percentless_window_only_when_its_status_limits", () => {
    const account = claudeAccount();
    const [five, weekly] = account.report!.windows!;
    const rejected = { id: "obs_a", label: "obs", status: "rejected", source: "observed" };
    const warning = { id: "obs_b", label: "obs", status: "allowed_warning", source: "observed" };
    const allowed = { id: "obs_c", label: "obs", status: "allowed", source: "observed" };
    const windows = [rejected, five, allowed, weekly, warning, null as never];
    const ids = (w: typeof windows) => summaryWindows({ ...account, report: { ...account.report!, windows: w } }).map((x) => x.id);
    expect(ids(windows)).toEqual(["obs_a", "five_hour", "weekly", "obs_b"]);
    // Without summary flags the first percent window stays the only meter, the status ones still show.
    const unflagged = windows.map((w) => (w ? { ...w, summary: false } : w));
    expect(ids(unflagged)).toEqual(["obs_a", "five_hour", "obs_b"]);
    expect(hasPlanWindows([{ ...account, report: { ...account.report!, windows: [rejected] } }])).toBe(true);
  });
});
