// providerGate.ts is the new-task form's view of the daemon's picker gates (internal/gate,
// docs/plans/active/picker-gate-reasons.md): hard gates block choosing a provider/model, advisory gates warn, a time
// gate whose end has passed is no gate, an unknown reason is hard and shows its action label. Pure (no react-native
// import), like providerCatalog.ts, so vitest covers it against plain data. The web picker's lib/provider-gate.ts is
// the twin; the daemon computes the gate, this only renders it.

import { ProviderGate, ProviderInfo } from './api';
import type { BadgeTone } from './ui/Badge';

const TIME_REASONS = new Set(['out_of_plan', 'cooling_down']);

/** Whether the daemon enforces the gate (no `advisory` flag). Unknown reasons count as hard. */
export function isHardGate(g: ProviderGate | undefined): boolean {
  return g !== undefined && !g.advisory;
}

/** The gate unless a time gate's `until` has already passed (the daemon re-publishes on a change, not on expiry). */
export function liveGate(g: ProviderGate | undefined, now: number = Date.now()): ProviderGate | undefined {
  if (!g) return undefined;
  if (TIME_REASONS.has(g.reason) && g.until) {
    const at = Date.parse(g.until);
    if (!Number.isNaN(at) && at <= now) return undefined;
  }
  return g;
}

const BADGES: Record<string, string> = {
  not_signed_in: 'Signed out',
  out_of_plan: 'Plan used up',
  cooling_down: 'Cooling down',
  unsupported_by_harness: 'Unsupported',
  model_not_in_plan: 'Not in plan',
};

/** The short reason (the badge, and the lower-cased label suffix). */
export function gateBadgeLabel(g: ProviderGate): string {
  return BADGES[g.reason] ?? 'Unavailable';
}

/** "resets 2h" / "resets 35m" for a window still counting down, null once due or unknown. */
function resetLabel(until: string | undefined, now: number): string | null {
  if (!until) return null;
  const diff = Date.parse(until) - now;
  if (!Number.isFinite(diff) || diff < 60_000) return null;
  const minutes = Math.floor(diff / 60_000);
  if (minutes >= 24 * 60) return `resets ${Math.floor(minutes / (24 * 60))}d`;
  if (minutes >= 60) return `resets ${Math.floor(minutes / 60)}h`;
  return `resets ${minutes}m`;
}

/** The full one-line reason: the badge sentence plus what the daemon says to do. */
export function gateText(g: ProviderGate, now: number = Date.now()): string {
  const action = g.action?.label;
  switch (g.reason) {
    case 'not_signed_in':
      return action ? `Signed out. ${action}` : 'Signed out';
    case 'unsupported_by_harness':
      return 'Not supported by this harness. Pick another provider';
    case 'model_not_in_plan':
      return 'Not in your plan. Pick another model';
    case 'out_of_plan': {
      const reset = resetLabel(g.until, now);
      return reset ? `Plan used up, ${reset}` : 'Plan used up';
    }
    case 'cooling_down': {
      const at = g.until ? Date.parse(g.until) : NaN;
      return Number.isNaN(at) ? 'Cooling down' : `Cooling down until ${new Date(at).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}`;
    }
    default:
      return action || g.reason;
  }
}

export type GateAction =
  | { kind: 'clear_cooldown'; label: string; provider: string }
  | { kind: 'hint'; text: string };

export interface GateNotice {
  badge: string;
  tone: BadgeTone;
  text: string;
  hard: boolean;
  /** A tap target (clear_cooldown) or a plain line saying where to act; undefined when there is nothing to run on a phone. */
  action?: GateAction;
}

/** What the notice under the provider/model fields shows. `canClear`: the session may call provider.clearCooldown (a human device). */
export function gateNotice(g: ProviderGate, now: number, canClear: boolean): GateNotice {
  const hard = isHardGate(g);
  const warn = g.advisory || TIME_REASONS.has(g.reason) || g.reason === 'model_not_in_plan';
  const notice: GateNotice = { badge: gateBadgeLabel(g), tone: warn ? 'warning' : 'destructive', text: gateText(g, now), hard };
  switch (g.action?.kind) {
    case 'sign_in':
      notice.action = { kind: 'hint', text: 'Sign in on the desktop app (Settings > Providers).' };
      break;
    case 'clear_cooldown':
      if (canClear && g.action.target) notice.action = { kind: 'clear_cooldown', label: 'Clear cooldown', provider: g.action.target };
      break;
    default:
      break;
  }
  return notice;
}

/** The gate that applies to (provider, model): the model's own, else the provider's (the default model '' inherits it). */
export function selectionGate(p: ProviderInfo | undefined, model: string, now: number): ProviderGate | undefined {
  if (!p) return undefined;
  return liveGate((model ? p.modelGates?.[model] : undefined) ?? p.gate, now);
}

/** Lower-cased suffix for an option label: "GLM — signed out". */
export function gateSuffix(g: ProviderGate | undefined): string {
  return g ? ` — ${gateBadgeLabel(g).toLowerCase()}` : '';
}
