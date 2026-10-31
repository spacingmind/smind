import { providerRunner, type ServedProvider } from "@/lib/provider-catalog";
import type { ThinkingLevel } from "@/lib/types";

/**
 * The one thinking-level vocabulary every surface shares (run-config IA
 * plan, mirroring lib/permission-modes.ts's single source for permission
 * modes) -- previously duplicated between run-config-toolbar.tsx and
 * profiles-section.tsx with slightly different id sets.
 *
 * Claude-only (see internal/taskrunner/thinking.go's doc comment): every
 * consuming surface gates this list behind `supportsThinking(provider)`,
 * so it isn't repeated here.
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

/**
 * Whether provider has a thinking level at all: a claude-native-runner-only (the base and its instances), pre-run
 * control (internal/taskrunner/thinking.go) -- every other provider takes
 * no thinking option, so the control is omitted rather than disabled.
 */
export function supportsThinking(provider: string, providers?: readonly ServedProvider[]): boolean {
  if (providers?.find((p) => p.id === provider)?.thinking === "hidden") return false;
  return providerRunner(provider, providers) === "claude-native";
}
