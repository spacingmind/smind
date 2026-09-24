import type { ApprovalPolicy } from "@/lib/types";

/**
 * Ported from `refs/paseo/packages/app/src/composer/agent-controls/mode.ts`'s
 * `resolveNextAgentModeId`: the next option after `current` in `options`,
 * wrapping past the end back to the first. `current` not being in the list
 * (nothing selected yet, or a stale value) starts from the first option
 * rather than treating it as an error -- Shift+Tab should always advance
 * to *some* real policy.
 */
export function resolveNextApprovalPolicy(
  options: readonly { id: ApprovalPolicy }[],
  current: ApprovalPolicy,
): ApprovalPolicy | null {
  if (options.length < 2) return null;
  const currentIndex = options.findIndex((option) => option.id === current);
  const startIndex = currentIndex >= 0 ? currentIndex : 0;
  const nextIndex = (startIndex + 1) % options.length;
  return options[nextIndex]?.id ?? null;
}
