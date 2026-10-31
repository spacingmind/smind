import { normalizeSas, type FleetGrant } from "@/lib/fleet-types";

/**
 * One quiet line for what a grant allows (ADR-0048 section 1's table, as text): the ops, the live-agent
 * cap, the per-run wall limit in minutes and the rolling 24 h cost cap. The default reverse grant is the
 * common case and reads better as its name than as "read · 0 live agents · …".
 */
export function grantSummary(g: FleetGrant): string {
  if (g.ops.length === 1 && g.ops[0] === "read") return "Read only";
  const agents = `${g.maxLiveAgents} live agent${g.maxLiveAgents === 1 ? "" : "s"}`;
  return `${g.ops.join(", ")} · ${agents} · up to ${Math.round(g.maxWallSeconds / 60)} min per run · $${g.maxCostUsd.toFixed(2)} per 24 h`;
}

/** The six-digit SAS as the human compares it: two groups of three. */
export function formatSas(s: string): string {
  const digits = normalizeSas(s);
  return digits.length === 6 ? `${digits.slice(0, 3)} ${digits.slice(3)}` : s;
}
