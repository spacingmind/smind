// Pure readiness helpers for the Get started card (E5-A): the words a
// provider's `provider.test` answer becomes, and whether any agent is ready.

import type { ProviderHealth } from "@/lib/model-browser";
import type { ProviderInfo } from "@/lib/types";

export type AgentStatus = "ready" | "not-installed" | "needs-sign-in" | "checking";

/** One provider's health answer as the card's vocabulary: ok, the CLI is missing, a login is missing, or no answer yet. */
export function agentStatus(health: ProviderHealth | undefined): AgentStatus {
  if (health === undefined) return "checking";
  if (health.ok) return "ready";
  if (health.detail.includes("not found on PATH")) return "not-installed";
  return "needs-sign-in";
}

const STATUS_LABELS: Record<AgentStatus, string> = {
  ready: "Ready",
  "not-installed": "Not installed",
  "needs-sign-in": "Sign-in needed",
  checking: "Checking",
};

export function agentStatusLabel(status: AgentStatus): string {
  return STATUS_LABELS[status];
}

/**
 * Whether the daemon's own provider list can start a run: "ready" when any
 * provider tested ok; "unknown" while any provider has no answer (or its RPC
 * itself failed -- never a verdict on the provider); "none" only once every
 * provider was tested and none is ok. An empty list is "unknown" (nothing
 * tested, the catalog may still be loading).
 */
export function agentsReady(
  providers: ProviderInfo[],
  health: Record<string, ProviderHealth | undefined>,
  rpcFailed?: ReadonlySet<string>,
): "ready" | "none" | "unknown" {
  if (providers.length === 0) return "unknown";
  let untested = false;
  for (const provider of providers) {
    const entry = health[provider.id];
    if (entry?.ok) return "ready";
    if (entry === undefined || rpcFailed?.has(provider.id)) untested = true;
  }
  return untested ? "unknown" : "none";
}
