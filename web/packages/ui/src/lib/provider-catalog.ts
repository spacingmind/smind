import type { ProviderRunner } from "@/lib/types";

/** The label lookup only needs the daemon's own row shape; ProviderInfo satisfies it. */
export interface ServedProvider {
  id: string;
  label?: string;
  accountProvider?: string;
  runner?: ProviderRunner;
  extends?: string;
}

/** The four built-ins' runners: the offline answer for `providerRunner` until/unless `provider.list` describes them. */
const BUILTIN_RUNNERS: Readonly<Record<string, ProviderRunner>> = {
  "claude-native": "claude-native",
  "codex-native": "codex-native",
  glm: "acp",
  kimi: "acp",
  opencode: "acp",
  copilot: "acp",
  cline: "acp",
  goose: "acp",
  cursor: "acp",
};

/**
 * How a provider id runs: the catalog's `runner` wins (an instance or a providers.custom entry is only known
 * there), else the built-in table, else "acp" (any other id is a generic ACP agent). Never a switch on the id
 * at the call site: every capability check (thinking, auto-accept, live mode switch) asks this.
 */
export function providerRunner(id: string, providers?: readonly ServedProvider[]): ProviderRunner {
  return providers?.find((p) => p.id === id)?.runner ?? BUILTIN_RUNNERS[id] ?? "acp";
}

/** The provider an id is a flavour of: an instance's `extends`, else the id itself (the mark and the env field follow it). */
export function providerFamilyId(id: string, providers?: readonly ServedProvider[]): string {
  return providers?.find((p) => p.id === id)?.extends ?? id;
}

/**
 * The well-known provider identities (ADR-0032 AC17): one row per provider
 * family the UI can name without a provider.list entry. `runtimeId` is the
 * taskrunner vocabulary (claude-native/codex-native/glm/kimi),
 * `accountProvider` the internal/accounts one (anthropic/openai/kimi), and
 * the bare family word ("claude", "codex") is what the glyph code accepted.
 */
export interface ProviderIdentity {
  key: "claude" | "codex" | "glm" | "kimi" | "opencode" | "copilot" | "cline" | "goose" | "cursor";
  runtimeId: string;
  accountProvider?: string;
  label: string;
  shortName: string;
  /** Which mark the family wears: its own brand SVG, or the registry's agent glyph (what GLM and Kimi always wore). */
  mark: "claude" | "codex" | "agent";
}

const IDENTITIES: readonly ProviderIdentity[] = [
  { key: "claude", runtimeId: "claude-native", accountProvider: "anthropic", label: "Claude Code", shortName: "Claude", mark: "claude" },
  { key: "codex", runtimeId: "codex-native", accountProvider: "openai", label: "Codex", shortName: "Codex", mark: "codex" },
  { key: "glm", runtimeId: "glm", label: "GLM", shortName: "GLM", mark: "agent" },
  { key: "kimi", runtimeId: "kimi", accountProvider: "kimi", label: "Kimi", shortName: "Kimi", mark: "agent" },
  { key: "opencode", runtimeId: "opencode", label: "OpenCode", shortName: "OpenCode", mark: "agent" },
  { key: "copilot", runtimeId: "copilot", label: "GitHub Copilot", shortName: "GitHub Copilot", mark: "agent" },
  { key: "cline", runtimeId: "cline", label: "Cline", shortName: "Cline", mark: "agent" },
  { key: "goose", runtimeId: "goose", label: "Goose", shortName: "Goose", mark: "agent" },
  { key: "cursor", runtimeId: "cursor", label: "Cursor", shortName: "Cursor", mark: "agent" },
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
export function providerLabel(id: string, providers?: readonly ServedProvider[]): string {
  const served = providers?.find((p) => p.id === id);
  return served?.label ?? providerIdentity(id)?.label ?? id;
}

/** Label for an account-vocabulary id (anthropic/openai/...): daemon label, else the table, else the raw id. */
export function accountProviderLabel(accountProvider: string, providers?: readonly ServedProvider[]): string {
  const served = providers?.find((p) => p.accountProvider === accountProvider);
  return served?.label ?? providerIdentity(accountProvider)?.label ?? accountProvider;
}

/** "Claude" / "Codex" / "GLM" / "Kimi" for either vocabulary, else the raw id. */
export function providerShortName(id: string): string {
  return providerIdentity(id)?.shortName ?? id;
}
