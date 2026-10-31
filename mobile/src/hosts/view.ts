// view.ts turns fleet.host.list into what the Hosts screen draws, as plain data, so labels, tones, details
// and the approval gate are unit-testable without rendering. PURE — no react-native import (same constraint
// as icons.ts). Nothing here ever fills a SAS: the view only compares what the human typed against host.sas
// (the daemon supplies it, and only while pending).

import { FleetHost, FleetLinkState, HostList } from './types';

/** Drops the spaces a six-digit code is typed or shown with. */
export function normalizeSas(s: string): string {
  return s.replace(/\s+/g, '');
}

/** True for a six-digit short authentication string, spaces ignored. */
export function isValidSas(s: string): boolean {
  return /^\d{6}$/.test(normalizeSas(s));
}

/** "123 456" for display; anything that is not six digits is shown as it came. */
export function formatSas(s: string): string {
  const d = normalizeSas(s);
  return d.length === 6 ? `${d.slice(0, 3)} ${d.slice(3)}` : s;
}

/** The human label of a link state (the same words as the web Hosts page). */
export function linkLabel(link: FleetLinkState): string {
  switch (link) {
    case 'active':
      return 'Connected';
    case 'connecting':
      return 'Connecting';
    case 'down':
      return 'Unreachable';
    case 'refused':
      return 'Refused';
    case 'peer_disabled':
      return 'Peer turned this direction off';
    case 'disabled':
      return 'Off';
    case 'revoked':
      return 'Revoked';
  }
}

export type HostTone = 'ok' | 'warning' | 'danger' | 'neutral';

function linkTone(link: FleetLinkState): HostTone {
  switch (link) {
    case 'active':
      return 'ok';
    case 'connecting':
    case 'down':
      return 'warning';
    case 'refused':
    case 'revoked':
      return 'danger';
    case 'peer_disabled':
    case 'disabled':
      return 'neutral';
  }
}

export interface HostRowView {
  key: string;
  alias: string;
  label: string;
  status: string;
  tone: HostTone;
  detail: string | null;
  canApprove: boolean;
}

function rowDetail(host: FleetHost): string | null {
  if (host.link === 'refused') return host.linkGuidance || host.linkCode || null;
  if (host.pendingApproval) return 'Awaiting your approval';
  if (host.state === 'pending') return `Waiting for approval on ${host.alias}`;
  return null;
}

export function hostRowView(host: FleetHost): HostRowView {
  return {
    key: host.hostId,
    alias: host.alias,
    label: host.label,
    status: linkLabel(host.link),
    tone: linkTone(host.link),
    detail: rowDetail(host),
    canApprove: host.pendingApproval && !!host.sas,
  };
}

export type HostsView =
  | { kind: 'off'; title: string; text: string }
  | { kind: 'empty'; title: string; text: string }
  | { kind: 'list'; rows: HostRowView[] };

/** The Hosts screen's whole state: fleet off, nothing paired, or the rows. */
export function hostsView(list: HostList): HostsView {
  if (list.off) return { kind: 'off', title: 'Fleet is off', text: 'Turn fleet on in config.yaml on the computer running smind.' };
  if (list.hosts.length === 0) return { kind: 'empty', title: 'No hosts paired', text: 'Pair hosts from Settings -> Hosts on a computer.' };
  return { kind: 'list', rows: list.hosts.map(hostRowView) };
}

/** The Approve button lights up only when the typed code is the six digits the daemon sent for THIS pending host. */
export function approveEnabled(host: FleetHost, typed: string): boolean {
  return host.pendingApproval && !!host.sas && isValidSas(typed) && normalizeSas(typed) === host.sas;
}
