import type { ProviderInfo } from "@/lib/types";

/**
 * The offline provider catalog (ADR-0032 AC13, ADR-0042 AC31): the built-ins, used only while `provider.list`
 * has not answered or failed, so the composer and the agent form are never unusable because one fetch was lost.
 * Once the daemon answers, its list is the single source (instances and providers.custom entries included) and this
 * one is never merged in. Imported by `useProviderCatalog` -- never duplicated in a component.
 */
export const FALLBACK_PROVIDERS: ProviderInfo[] = [
  { id: "claude-native", runner: "claude-native" },
  { id: "codex-native", runner: "codex-native" },
  { id: "glm", runner: "acp" },
  { id: "kimi", runner: "acp" },
  { id: "opencode", runner: "acp" },
  { id: "copilot", runner: "acp" },
  { id: "cline", runner: "acp" },
  { id: "goose", runner: "acp" },
  { id: "cursor", runner: "acp" },
];
