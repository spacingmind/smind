// listView.ts turns the daemon's automation.list into what the Automations list draws, as plain data, so the states are
// unit-testable without rendering. Names, reasons and signal text are human- or agent-authored: they are only ever
// strings drawn through <Text>, never interpreted.

import { AutomationInfo, AutomationListing, AutomationSignal } from './types';

export const AUTOMATIONS_OFF_TEXT = 'Automations are off on this daemon. Turn them on in config.yaml on the computer.';
export const AUTOMATIONS_EMPTY_TEXT = 'No automations are set up on this daemon.';
export const AUTOMATIONS_PAUSED_TEXT = 'All automations are paused in config.yaml on the computer.';
export const MANAGE_ON_COMPUTER_TEXT = 'Create and change automations on the computer: Settings -> Automations';

export type AutomationStatus = 'Active' | 'Paused' | 'Disabled';
export type StatusTone = 'success' | 'neutral' | 'warning';

/** Disabled (the human switch) beats paused (the engine's brake) beats active: one chip, the most important fact. */
export function automationStatus(a: Pick<AutomationInfo, 'enabled' | 'status'>): { label: AutomationStatus; tone: StatusTone } {
  if (!a.enabled) return { label: 'Disabled', tone: 'neutral' };
  if (a.status === 'paused') return { label: 'Paused', tone: 'warning' };
  return { label: 'Active', tone: 'success' };
}

/** 'cron: "17 19 * * *" UTC' style one-liner: cron is UTC; a CI signal names the workflow and branch. */
export function signalSummary(s: AutomationSignal): string {
  if (s.kind === 'cron') return `${s.cron ?? ''} UTC`;
  const workflow = s.workflow ?? '';
  return s.branch ? `${workflow} on ${s.branch}` : workflow;
}

/** '2026-10-21 19:17 UTC' from an RFC 3339 instant; anything unparseable is shown as it came. */
export function whenText(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  return `${new Date(t).toISOString().slice(0, 16).replace('T', ' ')} UTC`;
}

export interface AutomationRowView {
  id: number;
  name: string;
  status: AutomationStatus;
  tone: StatusTone;
  signal: string;
  lastFired: string;
  /** The daemon's reason while the row is paused (and enabled), else null. */
  pausedReason: string | null;
  /** True when the Pause brake is offered: enabled and not already paused. */
  canPause: boolean;
}

export type AutomationsListView =
  | { kind: 'off'; notice: string }
  | { kind: 'empty'; text: string; notice: string | null }
  | { kind: 'list'; rows: AutomationRowView[]; notice: string | null };

export function automationRow(a: AutomationInfo): AutomationRowView {
  const s = automationStatus(a);
  const paused = s.label === 'Paused';
  return {
    id: a.id,
    name: a.name || `Automation ${a.id}`,
    status: s.label,
    tone: s.tone,
    signal: signalSummary(a.signal),
    lastFired: a.lastFiredAt ? `Last fired ${whenText(a.lastFiredAt)}` : 'Never fired',
    pausedReason: paused && a.pausedReason ? a.pausedReason : null,
    canPause: a.enabled && !paused,
  };
}

export function automationsListView(l: AutomationListing): AutomationsListView {
  if (!l.enabled) return { kind: 'off', notice: AUTOMATIONS_OFF_TEXT };
  const notice = l.paused ? AUTOMATIONS_PAUSED_TEXT : null;
  if (l.automations.length === 0) return { kind: 'empty', text: AUTOMATIONS_EMPTY_TEXT, notice };
  return { kind: 'list', rows: l.automations.map(automationRow), notice };
}
