import type { ProviderInfo } from "@/lib/types";

/**
 * The well-known provider identities (ADR-0032 AC17): one row per provider
 * family the UI can name without a provider.list entry. `runtimeId` is the
 * taskrunner vocabulary (claude-native/codex-native/glm/kimi),
 * `accountProvider` the internal/accounts one (anthropic/openai/kimi), and
 * the bare family word ("claude", "codex") is what the glyph code accepted.
 */
export interface ProviderIdentity {
  key: "claude" | "codex" | "glm" | "kimi";
  runtimeId: string;
  accountProvider?: string;
  label: string;
  shortName: string;
}

const IDENTITIES: readonly ProviderIdentity[] = [
  { key: "claude", runtimeId: "claude-native", accountProvider: "anthropic", label: "Claude Code", shortName: "Claude" },
  { key: "codex", runtimeId: "codex-native", accountProvider: "openai", label: "Codex", shortName: "Codex" },
  { key: "glm", runtimeId: "glm", label: "GLM", shortName: "GLM" },
  { key: "kimi", runtimeId: "kimi", accountProvider: "kimi", label: "Kimi", shortName: "Kimi" },
];

/** The identity for either id vocabulary (runtime, account, or bare family word), case-insensitive. */
export function providerIdentity(id: string | null | undefined): ProviderIdentity | undefined {
  if (id == null) return undefined;
  const needle = id.toLowerCase();
  for (const identity of IDENTITIES) {
    const keys = [identity.key, identity.runtimeId, identity.accountProvider];
    if (keys.includes(needle)) return identity;
  }
  return undefined;
}

/** Label for a runtime-vocabulary id: the daemon's own ProviderInfo.label wins, else the table, else the raw id. */
export function providerLabel(id: string, providers?: ProviderInfo[]): string {
  const served = providers?.find((p) => p.id === id);
  return served?.label ?? providerIdentity(id)?.label ?? id;
}

/** Label for an account-vocabulary id (anthropic/openai/...): daemon label, else the table, else the raw id. */
export function accountProviderLabel(accountProvider: string, providers?: ProviderInfo[]): string {
  const served = providers?.find((p) => p.accountProvider === accountProvider);
  return served?.label ?? providerIdentity(accountProvider)?.label ?? accountProvider;
}

/** "Claude" / "Codex" / "GLM" / "Kimi" for either vocabulary, else the raw id. */
export function providerShortName(id: string): string {
  return providerIdentity(id)?.shortName ?? id;
}
