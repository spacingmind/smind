import type { ProviderInfo } from "@/lib/types";

/**
 * Resolves a taskrunner-vocabulary provider id (AgentProfile.Provider,
 * ProviderInfo.id) to whichever id provider.test actually expects --
 * accounts-vocabulary (e.g. "anthropic") for a credential-backed
 * provider, the same taskrunner id for a cli-kind one. Shared by
 * profiles-section.tsx's per-agent health dot and app-sidebar.tsx's
 * footer healthy-count -- both resolve provider.test's `provider` param
 * the same way, per ProviderInfo.accountProvider's own doc comment in
 * lib/types.ts and internal/wsapi/handlers.go's handleProviderTest.
 */
export function accountHealthTestKey(providerId: string, providers: ProviderInfo[]): string {
  const info = providers.find((p) => p.id === providerId);
  if (!info || info.kind === "cli") return providerId;
  return info.accountProvider ?? providerId;
}
