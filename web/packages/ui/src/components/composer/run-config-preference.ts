import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import type { Provider, ThinkingLevel } from "@/lib/types";

/** localStorage key for one chat's run-config toolbar state (ADR-0016 P3: `smind:run-config:${taskId}:${chatId}`, replacing the old per-task-only key) -- same per-key shape as use-composer-draft.ts's draftStorageKey (docs/design.md §9). */
export function runConfigStorageKey(taskId: number, chatId: number): string {
  return `smind:run-config:${taskId}:${chatId}`;
}

/** The pre-ADR-0016 per-task-only key, read once to migrate a task's default chat onto the new per-chat key (see readRunConfigPreference). */
function legacyRunConfigStorageKey(taskId: number): string {
  return `smind:run-config:${taskId}`;
}

const PROVIDERS: readonly Provider[] = ["claude-native", "glm", "kimi", "codex-native"];
const THINKING_LEVEL_VALUES: readonly ThinkingLevel[] = ["", "off", "standard", "extended"];

/**
 * Validates a stored value, migrating the pre-ADR-0019 shape on the way:
 * a legacy `approvalPolicy` (manual/auto-safe/full-access) is dropped, and
 * the state loads with the provider's own default mode ("") and
 * autoAccept off -- never mapped, since a stale choice can only narrow
 * back to asking a human, never widen (W2). Any other invalid field still
 * rejects the whole value.
 */
function toRunConfigState(value: unknown): RunConfigState | null {
  if (typeof value !== "object" || value === null) return null;
  const v = value as Record<string, unknown>;
  const legacy = "approvalPolicy" in v;
  const valid =
    (v.baseAgentId === null || typeof v.baseAgentId === "string") &&
    typeof v.custom === "boolean" &&
    PROVIDERS.includes(v.provider as Provider) &&
    THINKING_LEVEL_VALUES.includes(v.thinkingLevel as ThinkingLevel) &&
    (legacy || (typeof v.permissionMode === "string" && typeof v.autoAccept === "boolean"));
  if (!valid) return null;
  return {
    baseAgentId: v.baseAgentId as string | null,
    custom: v.custom as boolean,
    provider: v.provider as Provider,
    permissionMode: legacy ? "" : (v.permissionMode as string),
    autoAccept: legacy ? false : (v.autoAccept as boolean),
    thinkingLevel: v.thinkingLevel as ThinkingLevel,
  };
}

function readKey(key: string): RunConfigState | null {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw === null) return null;
    const parsed: unknown = JSON.parse(raw);
    return toRunConfigState(parsed);
  } catch {
    return null;
  }
}

/**
 * Reads chatId's persisted run-config, or null if unset/corrupt/thrown/no
 * chat -- run-config-toolbar.tsx falls back to EMPTY_STATE (or the ★
 * default agent) in that case, never a crash.
 *
 * isDefaultChat migrates the pre-ADR-0016 per-task-only key (plan's P3
 * AC2: "migrate the old per-task key into the default chat"): only the
 * task's default chat ever falls back to it, since every other chat is
 * new and never had a legacy key to inherit.
 */
export function readRunConfigPreference(
  taskId: number | null,
  chatId: number | null,
  isDefaultChat: boolean,
): RunConfigState | null {
  if (taskId === null || chatId === null || typeof window === "undefined") return null;
  const own = readKey(runConfigStorageKey(taskId, chatId));
  if (own) return own;
  return isDefaultChat ? readKey(legacyRunConfigStorageKey(taskId)) : null;
}

/** Best-effort persistence -- a write failure never stops the toolbar applying the change for the rest of the session. */
export function writeRunConfigPreference(taskId: number | null, chatId: number | null, state: RunConfigState): void {
  if (taskId === null || chatId === null || typeof window === "undefined") return;
  try {
    window.localStorage.setItem(runConfigStorageKey(taskId, chatId), JSON.stringify(state));
  } catch {
    // Best-effort only.
  }
}
