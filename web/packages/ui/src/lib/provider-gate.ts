import {
  formatCooldownUntil,
  formatResetLabel,
} from "@/lib/account-usage/format";
import type { StatusBadgeStatus } from "@/components/ui/status-badge";
import type { ProviderGate } from "@/lib/types";

// Picker gate helpers (docs/plans/active/picker-gate-reasons.md). The daemon computes the gate; the client
// only renders it: hard gates block selection, advisory ones warn and stay selectable. An unknown reason
// (a newer daemon) is treated as hard and renders its action label.

/** Whether the daemon enforces the gate (no `advisory` flag). Unknown reasons count as hard. */
export function isHardGate(g: ProviderGate | undefined): boolean {
  return g !== undefined && !g.advisory;
}

const TIME_REASONS = new Set(["out_of_plan", "cooling_down"]);

/** The gate unless a time gate's `until` has already passed (the daemon only re-publishes on a change, not on expiry). */
export function liveGate(
  g: ProviderGate | undefined,
  now: number = Date.now(),
): ProviderGate | undefined {
  if (!g) return undefined;
  if (TIME_REASONS.has(g.reason) && g.until) {
    const at = Date.parse(g.until);
    if (!Number.isNaN(at) && at <= now) return undefined;
  }
  return g;
}

/** The one-line reason. Times use the usage meters' own formatters so both surfaces read the same. */
export function gateReasonText(
  g: ProviderGate,
  now: number = Date.now(),
): string {
  switch (g.reason) {
    case "not_signed_in":
      return "Signed out";
    case "unsupported_by_harness":
      return "Not supported by this harness";
    case "model_not_in_plan":
      return "Not in your plan";
    case "out_of_plan": {
      const reset = formatResetLabel(g.until, now);
      return reset ? `Plan used up · ${reset}` : "Plan used up";
    }
    case "cooling_down": {
      const at = g.until ? Date.parse(g.until) : NaN;
      return Number.isNaN(at)
        ? "Cooling down"
        : `Cooling down until ${formatCooldownUntil(at, now)}`;
    }
    default:
      return g.action?.label || g.reason;
  }
}

/** The short badge: advisory and time gates warn, setup gates (sign in, harness) read as problems. */
export function gateBadge(g: ProviderGate): {
  label: string;
  status: StatusBadgeStatus;
} {
  const labels: Record<string, string> = {
    not_signed_in: "Signed out",
    out_of_plan: "Out of plan",
    cooling_down: "Cooling down",
    unsupported_by_harness: "Unsupported",
    model_not_in_plan: "Not in plan",
  };
  const warn =
    g.advisory ||
    TIME_REASONS.has(g.reason) ||
    g.reason === "model_not_in_plan";
  return {
    label: labels[g.reason] ?? "Unavailable",
    status: warn ? "warning" : "danger",
  };
}

const ACTION_LABELS: Record<string, string> = {
  sign_in: "Sign in",
  clear_cooldown: "Clear",
  switch_profile: "Switch",
  pick_model: "Pick model",
  pick_provider: "Pick provider",
};

/** The control's short label, undefined when the picker has nothing to run for this action (wait_until, unknown kinds). */
export function gateActionLabel(g: ProviderGate): string | undefined {
  return g.action ? ACTION_LABELS[g.action.kind] : undefined;
}
