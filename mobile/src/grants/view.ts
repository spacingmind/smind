// view.ts: the pure words and rules of the saved-grant UI (card control and list). No react-native import. The
// copy mirrors web/packages/ui/src/lib/grant-copy.ts (a test reads that file, so the two cannot drift); the class
// is the daemon's call, these only name it.

import type { GrantOffer, SavedGrant } from './types';

export const GRANT_BUTTON_LABEL = 'Always allow in this project';
export const GRANT_NEEDS_OPT_IN_HINT = 'Tick the box above to save this command.';
export const GRANT_OPT_IN_FALLBACK = "Also allow running this project's code (the agent can change it)";

/** True when saving needs the project-code opt-in: the CLASS decides (anything but the known read-only class is a runner); the daemon's flag can neither lower it nor raise it for a read-only command. */
export function grantNeedsOptIn(offer: Pick<GrantOffer, 'class'>): boolean {
  return offer.class !== 'readonly';
}

export function grantClassLabel(klass: string): string {
  return klass === 'readonly' ? 'Read-only' : 'Runs project code';
}

export function grantStatusLabel(status: string): string {
  switch (status) {
    case 'active':
      return 'Active';
    case 'expired':
      return 'Expired';
    case 'void':
      return 'Void: tool setup changed';
    case 'revoked':
      return 'Revoked';
    default:
      return status;
  }
}

export function grantStatusHint(g: Pick<SavedGrant, 'status'>): string {
  switch (g.status) {
    case 'expired':
    case 'revoked':
      return 'Not applied; this command asks again.';
    case 'void':
      return "The project's MCP servers changed, so this grant is not applied; this command asks again.";
    default:
      return '';
  }
}

/** Whether the save control is enabled: the opt-in must be ticked first for a runner, and nothing saves while busy. */
export function grantSaveEnabled(offer: Pick<GrantOffer, 'class'>, optIn: boolean, busy: boolean): boolean {
  return !busy && (!grantNeedsOptIn(offer) || optIn);
}

/** Whether the save sends allowProjectCode: only a runner with its opt-in ticked. */
export function grantAllowProjectCode(offer: Pick<GrantOffer, 'class'>, optIn: boolean): boolean {
  return grantNeedsOptIn(offer) && optIn;
}

/** "Expires in 12d" / "Expires in 3h" / "Expired 2d ago". */
export function grantExpiryText(expiresAt: string, now: number): string {
  const at = new Date(expiresAt).getTime();
  if (Number.isNaN(at)) return expiresAt;
  const diff = at - now;
  if (diff <= 0) return `Expired ${agoText(now - at)}`;
  const days = Math.floor(diff / 86_400_000);
  if (days >= 1) return `Expires in ${days}d`;
  return `Expires in ${Math.max(1, Math.floor(diff / 3_600_000))}h`;
}

function agoText(ms: number): string {
  if (ms < 60_000) return 'just now';
  const days = Math.floor(ms / 86_400_000);
  if (days >= 1) return `${days}d ago`;
  const hours = Math.floor(ms / 3_600_000);
  if (hours >= 1) return `${hours}h ago`;
  return `${Math.floor(ms / 60_000)}m ago`;
}

export function grantAgo(iso: string, now: number): string {
  const t = new Date(iso).getTime();
  return Number.isNaN(t) ? iso : agoText(now - t);
}

/** One row of the list, every line a plain string. */
export interface GrantRowView {
  id: number;
  command: string;
  status: string;
  statusLabel: string;
  classLabel: string;
  isRunner: boolean;
  optInLine: string | null;
  expiry: string;
  folderLine: string;
  usageLine: string;
  hint: string;
  canRevoke: boolean;
}

export function grantRows(grants: readonly SavedGrant[], now: number): GrantRowView[] {
  return grants.map((g) => ({
    id: g.id,
    command: g.command,
    status: g.status,
    statusLabel: grantStatusLabel(g.status),
    classLabel: grantClassLabel(g.class),
    isRunner: g.class !== 'readonly',
    optInLine: g.class === 'readonly' ? null : g.runnerOptIn ? "You allowed this to run the project's code (the agent can change it)." : 'No project-code opt-in: this is never applied.',
    expiry: grantExpiryText(g.expiresAt, now),
    folderLine: `Project folder: ${g.folder || 'unknown'}`,
    usageLine: `Used ${g.useCount} ${g.useCount === 1 ? 'time' : 'times'}${g.lastUsedAt ? `, last ${grantAgo(g.lastUsedAt, now)}` : ''}. Saved ${grantAgo(g.createdAt, now)}.`,
    hint: grantStatusHint(g),
    canRevoke: g.status !== 'revoked',
  }));
}

export function grantRevokeCopy(command: string): string {
  return `${command} will ask again in this project.`;
}
