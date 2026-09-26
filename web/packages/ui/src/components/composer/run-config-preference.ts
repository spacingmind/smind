import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import type { ApprovalPolicy, Provider, ThinkingLevel } from "@/lib/types";

/** localStorage key for one task's run-config toolbar state -- same per-task-key shape as use-composer-draft.ts's draftStorageKey (docs/design.md §9). */
export function runConfigStorageKey(taskId: number): string {
  return `smind:run-config:${taskId}`;
}

const PROVIDERS: readonly Provider[] = ["claude-native", "glm", "kimi", "codex-native"];
const APPROVAL_POLICIES: readonly ApprovalPolicy[] = ["manual", "auto-safe", "full-access"];
const THINKING_LEVEL_VALUES: readonly ThinkingLevel[] = ["", "off", "standard", "extended"];

function isRunConfigState(value: unknown): value is RunConfigState {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    (v.baseAgentId === null || typeof v.baseAgentId === "string") &&
    typeof v.custom === "boolean" &&
    PROVIDERS.includes(v.provider as Provider) &&
    APPROVAL_POLICIES.includes(v.approvalPolicy as ApprovalPolicy) &&
    THINKING_LEVEL_VALUES.includes(v.thinkingLevel as ThinkingLevel)
  );
}

/** Reads taskId's persisted run-config, or null if unset/corrupt/thrown/no task -- run-config-toolbar.tsx falls back to EMPTY_STATE (or the ★ default agent) in that case, never a crash. */
export function readRunConfigPreference(taskId: number | null): RunConfigState | null {
  if (taskId === null || typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(runConfigStorageKey(taskId));
    if (raw === null) return null;
    const parsed: unknown = JSON.parse(raw);
    return isRunConfigState(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

/** Best-effort persistence -- a write failure never stops the toolbar applying the change for the rest of the session. */
export function writeRunConfigPreference(taskId: number | null, state: RunConfigState): void {
  if (taskId === null || typeof window === "undefined") return;
  try {
    window.localStorage.setItem(runConfigStorageKey(taskId), JSON.stringify(state));
  } catch {
    // Best-effort only.
  }
}
