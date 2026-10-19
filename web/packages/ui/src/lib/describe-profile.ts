import { describePermission } from "@/lib/permission-modes";
import { providerLabel } from "@/lib/provider-catalog";
import { supportsThinking, thinkingLevelLabel } from "@/lib/thinking-levels";
import type { AgentProfile, ProviderInfo, ThinkingLevel } from "@/lib/types";

/** "<provider> · <model> · <mode> · <thinking>" -- the composer picker's per-row metadata and the Settings → Agents row share this (Paseo's profile summary order). The model is shown only when the profile pins one (ADR-0025). The mode is the provider's own label (ADR-0019). Thinking is omitted for a non-Claude profile, mirroring the Thinking control's own visibility rule. */
export function describeProfile(p: AgentProfile, providers: ProviderInfo[]): string {
  const parts = [providerLabel(p.Provider, providers)];
  if (p.Model) parts.push(providers.find((candidate) => candidate.id === p.Provider)?.models?.find((m) => m.id === p.Model)?.label ?? p.Model);
  parts.push(describePermission(providers, p.Provider, p.PermissionMode, p.AutoAccept));
  if (supportsThinking(p.Provider)) parts.push(thinkingLevelLabel(p.ThinkingLevel as ThinkingLevel));
  return parts.join(" · ");
}
