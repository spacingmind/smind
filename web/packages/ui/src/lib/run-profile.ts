import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import type { AgentProfile } from "@/lib/types";

/**
 * ADR-0041 S2w: the profile id a send should carry, or undefined for a
 * plain send. A send is "that profile" only while a profile was applied
 * (`baseAgentId`), it still exists, and every field applyProfileFields
 * (run-config-toolbar.tsx) seeds still equals the profile's stored value:
 * provider, permission mode, auto-accept, model ("" = provider default)
 * and thinking level (an empty stored level is not part of the profile).
 * A hand-edited field makes the run a plain one (copy semantics, ADR-0014).
 */
export function profileIdFor(
  state: Pick<RunConfigState, "baseAgentId" | "provider" | "permissionMode" | "autoAccept" | "thinkingLevel" | "model">,
  profiles: readonly AgentProfile[],
): number | undefined {
  if (state.baseAgentId === null) return undefined;
  const p = profiles.find((x) => String(x.ID) === state.baseAgentId);
  if (!p) return undefined;
  const same =
    state.provider === p.Provider &&
    state.permissionMode === p.PermissionMode &&
    state.autoAccept === p.AutoAccept &&
    state.model === (p.Model ?? "") &&
    (p.ThinkingLevel === "" || state.thinkingLevel === p.ThinkingLevel);
  return same ? p.ID : undefined;
}
