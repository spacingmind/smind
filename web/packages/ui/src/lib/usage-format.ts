import type { UsageRange } from "@/lib/types";

export const EM_DASH = "—";

/**
 * Formats a token count. null/undefined means "not reported" and renders
 * as an em dash -- never 0, which is a real reported value (ADR-0020).
 * "full" groups thousands en-US style; "compact" abbreviates (842, 1.2k,
 * 12.3k, 123k, 1.3M) with one decimal below 100 of a unit and none above,
 * promoting a rounded 1000 to the next unit (999,950 -> "1M", never "1000k").
 */
export function formatTokens(n: number | null | undefined, mode: "full" | "compact" = "full"): string {
  if (n === null || n === undefined) return EM_DASH;
  if (mode === "full") return n.toLocaleString("en-US");

  const units = ["", "k", "M", "B", "T"];
  let value = n;
  let unit = 0;
  while (unit < units.length - 1 && Math.round(value) >= 1000) {
    value /= 1000;
    unit += 1;
  }
  const rounded = Math.round(value);
  if (unit === 0 || rounded >= 100) return `${rounded}${units[unit]}`;
  const text = value.toFixed(1).replace(/\.0$/, "");
  return `${text}${units[unit]}`;
}

/** Formats a USD cost: 4 decimals below $0.01, 3 below $1, else 2. null is an em dash; a reported 0 is $0.00. */
export function formatCost(usd: number | null | undefined): string {
  if (usd === null || usd === undefined) return EM_DASH;
  if (usd === 0) return "$0.00";
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  if (usd < 1) return `$${usd.toFixed(3)}`;
  return `$${usd.toFixed(2)}`;
}

const SOURCE_LABELS: Record<string, string> = {
  claude_result: "Claude result",
  codex_token_usage: "Codex token usage",
  acp_prompt_usage: "ACP prompt usage",
  acp_usage_update: "ACP usage update",
  not_reported: "Not reported",
};

/** Human label for a RunUsage source (ADR-0020); unknown values fall back to the raw string. */
export function usageSourceLabel(source: string): string {
  return SOURCE_LABELS[source] ?? source;
}

/** The RFC3339 `since` for a usage range: "today" is local midnight, "7d"/"30d" are now minus N*24h. */
export function usageSince(range: UsageRange, now: Date = new Date()): string {
  if (range === "today") {
    return new Date(now.getFullYear(), now.getMonth(), now.getDate()).toISOString();
  }
  const days = range === "7d" ? 7 : 30;
  return new Date(now.getTime() - days * 24 * 60 * 60 * 1000).toISOString();
}
