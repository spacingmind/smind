import type { ThinkingLevel } from "@/lib/types";

/**
 * The one thinking-level vocabulary every surface shares (run-config IA
 * plan, mirroring lib/approval-policies.ts's unification of the approval
 * tiers) -- previously duplicated between run-config-toolbar.tsx and
 * profiles-section.tsx with slightly different id sets.
 *
 * Claude-only (see internal/taskrunner/thinking.go's doc comment): every
 * consuming surface already gates this list behind
 * `provider === "claude-native"` on its own, so it isn't repeated here.
 */
export interface ThinkingLevelInfo {
  id: Exclude<ThinkingLevel, "">;
  label: string;
  help: string;
}

export const THINKING_LEVELS: ThinkingLevelInfo[] = [
  { id: "off", label: "Off", help: "No extended thinking -- responds immediately." },
  { id: "standard", label: "Standard", help: "The model adapts how much it thinks to the turn." },
  {
    id: "extended",
    label: "Extended",
    help: "A large fixed thinking budget, for turns that need to reason at length before acting.",
  },
];

/** "" (unset) reads as "Standard" everywhere -- that's already the adaptive default in effect, just via no Option at all (see ThinkingLevelUnspecified's own doc comment on the Go side). */
export function thinkingLevelLabel(level: ThinkingLevel): string {
  return THINKING_LEVELS.find((t) => t.id === (level || "standard"))?.label ?? "Standard";
}
