import { describe, expect, it } from "vitest";

import { EM_DASH, formatCost, formatTokens, usageSince, usageSourceLabel } from "@/lib/usage-format";

describe("usage-format", () => {
  it("formatTokens renders null as an em dash, never 0", () => {
    expect(formatTokens(null)).toBe(EM_DASH);
    expect(formatTokens(undefined)).toBe(EM_DASH);
    expect(formatTokens(null, "compact")).toBe(EM_DASH);
    // A reported 0 is a real value and renders as 0.
    expect(formatTokens(0)).toBe("0");
  });

  it("formatTokens groups thousands in full mode and abbreviates in compact mode", () => {
    expect(formatTokens(12345)).toBe("12,345");
    expect(formatTokens(1234567)).toBe("1,234,567");

    expect(formatTokens(842, "compact")).toBe("842");
    expect(formatTokens(1234, "compact")).toBe("1.2k");
    expect(formatTokens(12345, "compact")).toBe("12.3k");
    expect(formatTokens(123000, "compact")).toBe("123k");
    expect(formatTokens(1300000, "compact")).toBe("1.3M");
    // A trailing ".0" is trimmed, never "1.0k".
    expect(formatTokens(1000, "compact")).toBe("1k");
    // Rounding up to 1000 of a unit promotes to the next unit, never "1000k".
    expect(formatTokens(999_950, "compact")).toBe("1M");
  });

  it("formatCost uses 4/3/2 decimals by magnitude, null is an em dash, a reported 0 is $0.00", () => {
    expect(formatCost(null)).toBe(EM_DASH);
    expect(formatCost(undefined)).toBe(EM_DASH);
    expect(formatCost(0)).toBe("$0.00");
    expect(formatCost(0.0042)).toBe("$0.0042");
    expect(formatCost(0.0099)).toBe("$0.0099");
    expect(formatCost(0.042)).toBe("$0.042");
    expect(formatCost(0.999)).toBe("$0.999");
    expect(formatCost(12.345)).toBe("$12.35");
    expect(formatCost(12.3)).toBe("$12.30");
  });

  it("usageSourceLabel maps every ADR-0020 source and falls back to the raw value", () => {
    expect(usageSourceLabel("claude_result")).toBe("Claude result");
    expect(usageSourceLabel("codex_token_usage")).toBe("Codex token usage");
    expect(usageSourceLabel("acp_prompt_usage")).toBe("ACP prompt usage");
    expect(usageSourceLabel("acp_usage_update")).toBe("ACP usage update");
    expect(usageSourceLabel("not_reported")).toBe("Not reported");
    expect(usageSourceLabel("acp_something_new")).toBe("acp_something_new");
  });

  it("usageSince: today is local midnight, 7d and 30d are rolling", () => {
    const now = new Date(2026, 9, 11, 15, 30, 45); // local time
    expect(usageSince("today", now)).toBe(new Date(2026, 9, 11, 0, 0, 0, 0).toISOString());

    const week = usageSince("7d", now);
    expect(week).toBe(new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString());
    const month = usageSince("30d", now);
    expect(month).toBe(new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000).toISOString());
  });
});
