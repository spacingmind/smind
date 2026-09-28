import type { ModeInfo, Provider, ProviderInfo } from "@/lib/types";

/**
 * Provider-native permission modes (ADR-0019): every surface -- composer,
 * agent form, header pill, mid-run control -- renders the catalog the
 * daemon serves on provider.list (ProviderInfo.modes), with the
 * provider's own labels. smind has no tiers of its own any more.
 *
 * STATIC_MODES only covers the window before provider.list answers (or if
 * it fails), mirroring internal/taskrunner's static catalogs so the
 * composer is never unusable; the daemon's answer always wins.
 */
const STATIC_MODES: Record<Provider, { modes: ModeInfo[]; defaultMode: string }> = {
  "claude-native": {
    defaultMode: "acceptEdits",
    modes: [
      { id: "acceptEdits", label: "Accept File Edits", description: "Automatically approves edit-focused tools without prompting" },
      { id: "default", label: "Always Ask", description: "Prompts for permission the first time a tool is used" },
      { id: "plan", label: "Plan Mode", description: "Analyze the codebase without executing tools or edits" },
      { id: "bypassPermissions", label: "Bypass", description: "Skip all permission prompts (use with caution)", autoApproves: true },
    ],
  },
  "codex-native": {
    defaultMode: "auto",
    modes: [
      { id: "auto", label: "Default Permissions", description: "Edit files and run commands with Codex's default approval flow." },
      { id: "full-access", label: "Full Access", description: "Edit files, run commands, and access the network without additional prompts.", autoApproves: true },
    ],
  },
  glm: { defaultMode: "default", modes: [{ id: "default", label: "Default", description: "The agent's own starting mode" }] },
  kimi: { defaultMode: "default", modes: [{ id: "default", label: "Default", description: "The agent's own starting mode" }] },
};

const ACP_PROVIDERS: ReadonlySet<string> = new Set(["glm", "kimi"]);

function infoFor(providers: ProviderInfo[], provider: string): ProviderInfo | undefined {
  return providers.find((p) => p.id === provider);
}

/** provider's permission mode catalog, in display order. */
export function providerModes(providers: ProviderInfo[], provider: string): ModeInfo[] {
  const served = infoFor(providers, provider)?.modes;
  if (served && served.length > 0) return served;
  return STATIC_MODES[provider as Provider]?.modes ?? [];
}

/** The mode a run of provider gets when it names none. */
export function defaultModeFor(providers: ProviderInfo[], provider: string): string {
  const info = infoFor(providers, provider);
  if (info?.defaultMode) return info.defaultMode;
  return STATIC_MODES[provider as Provider]?.defaultMode ?? providerModes(providers, provider)[0]?.id ?? "";
}

/** The mode a stored/draft value actually means: "" resolves to the provider default. */
export function effectiveMode(providers: ProviderInfo[], provider: string, mode: string): string {
  return mode || defaultModeFor(providers, provider);
}

/** A mode id's label for provider (the provider's own words), falling back to the raw id for an unknown one. */
export function permissionModeLabel(providers: ProviderInfo[], provider: string, mode: string): string {
  const id = effectiveMode(providers, provider, mode);
  return providerModes(providers, provider).find((m) => m.id === id)?.label ?? id;
}

/** Whether provider takes autoAccept (ACP: approve every permission prompt). */
export function supportsAutoAccept(providers: ProviderInfo[], provider: string): boolean {
  return infoFor(providers, provider)?.supportsAutoAccept ?? ACP_PROVIDERS.has(provider);
}

/** Whether a running run of provider can switch mode mid-run (Codex can't). */
export function supportsLiveModeSwitch(providers: ProviderInfo[], provider: string): boolean {
  return infoFor(providers, provider)?.liveModeSwitch ?? provider !== "codex-native";
}

export const AUTO_ACCEPT_LABEL = "Auto-accept";
export const AUTO_ACCEPT_HELP = "Automatically approves every permission prompt from this agent.";

/** The run-config summary for a mode (+ auto-accept), e.g. "Plan Mode" or "Default · Auto-accept". */
export function describePermission(providers: ProviderInfo[], provider: string, mode: string, autoAccept: boolean): string {
  const label = permissionModeLabel(providers, provider, mode);
  return autoAccept ? `${label} · ${AUTO_ACCEPT_LABEL}` : label;
}
