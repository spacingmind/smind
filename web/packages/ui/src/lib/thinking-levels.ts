import { providerRunner, type ServedProvider } from "@/lib/provider-catalog";
import type { ModelInfo, ThinkingLevel } from "@/lib/types";

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
  id: string;
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

/** The slice of a provider.list row the reasoning levels read: the runner facts plus the served model catalogue. */
export type ReasoningProvider = ServedProvider & { models?: ModelInfo[] };

const EFFORT_LABELS: Readonly<Record<string, string>> = {
  none: "None",
  minimal: "Minimal",
  low: "Low",
  medium: "Medium",
  high: "High",
  xhigh: "Extra high",
  max: "Max",
  ultra: "Ultra",
};

export function effortLabel(id: string): string {
  return EFFORT_LABELS[id] ?? id.charAt(0).toUpperCase() + id.slice(1);
}

/** The codex model a run would use: the requested id, else the provider's own default model. */
function codexModel(provider: string, model: string, providers?: readonly ReasoningProvider[]): ModelInfo | undefined {
  const models = providers?.find((p) => p.id === provider)?.models;
  return (model ? models?.find((m) => m.id === model) : undefined) ?? (model ? undefined : models?.find((m) => m.default));
}

/** Whether id is the model's own default effort (the row the picker marks "(default)"). */
export function isDefaultEffort(model: Pick<ModelInfo, "defaultEffort"> | undefined, id: string): boolean {
  return !!model && model.defaultEffort === id;
}

/**
 * The reasoning levels a provider+model offers, i.e. the valid `thinkingLevel` values: Claude's fixed
 * vocabulary for a claude-native runner, a codex-native model's own advertised efforts (`[]` when it lists
 * none or the model is unknown), null for every other provider (no control at all).
 */
export function reasoningLevels(provider: string, model: string, providers?: readonly ReasoningProvider[]): ThinkingLevelInfo[] | null {
  if (supportsThinking(provider, providers)) return THINKING_LEVELS;
  if (providerRunner(provider, providers) !== "codex-native") return null;
  const m = codexModel(provider, model, providers);
  return (m?.efforts ?? []).map((e) => ({
    id: e.id,
    label: effortLabel(e.id),
    help: [e.description, isDefaultEffort(m, e.id) ? "(default)" : ""].filter(Boolean).join(" "),
  }));
}

/** The model's default effort as a label, for the "Default (medium)" unset row; "Default" when it names none. */
export function defaultEffortLabel(provider: string, model: string, providers?: readonly ReasoningProvider[]): string {
  const d = codexModel(provider, model, providers)?.defaultEffort;
  return d ? `Default (${effortLabel(d)})` : "Default";
}
