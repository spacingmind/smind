import type { ProviderInfo } from "@/lib/types";

/**
 * The one built-in provider catalog (ADR-0032 AC13): used until
 * `provider.list` answers (and kept if it fails), so the composer and the
 * agent form are never unusable because one fetch was lost. Imported by
 * `useProviderCatalog` -- never duplicated in a component.
 */
export const FALLBACK_PROVIDERS: ProviderInfo[] = [{ id: "claude-native" }, { id: "glm" }];
