// api.ts is the whole fleet host client surface of the mobile app: one read, plus approve/reject of a pending
// host (ADR-0048 S13: no pairing, invite, grant editing or reverse toggles from a phone; the daemon refuses
// those over the relay anyway, being local-only admin). hostsSafety.test.ts scans the sources against
// HOST_METHODS.

import { RelayConnection } from '../relay/RelayConnection';
import { HostList } from './types';
import { normalizeSas } from './view';

/** Every RPC this module may send. hostsSafety.test.ts compares the source against it. */
export const HOST_METHODS = ['fleet.host.list', 'fleet.host.approve', 'fleet.host.reject'] as const;

/** A phone can only grant READ access; the human widens it later on a computer (ADR-0048 section 1: the narrowest grant — read-only, 0 live agents, 0 s of run time, $0). */
export const READ_ONLY_GRANT = {
  ops: ['read'],
  workspacesRead: { all: true },
  workspacesWrite: [],
  providers: [],
  maxLiveAgents: 0,
  maxWallSeconds: 0,
  maxCostUsd: 0,
};

export async function fetchHosts(conn: RelayConnection): Promise<HostList> {
  let res: { hosts?: unknown } | null;
  try {
    res = (await conn.call('fleet.host.list', {})) as { hosts?: unknown } | null;
  } catch (err) {
    // With fleet off the daemon answers method not found for every fleet method: that reads as "Fleet is
    // off", not as an error.
    if (err instanceof Error && /method not found/i.test(err.message)) return { off: true, hosts: [] };
    throw err;
  }
  return { off: false, hosts: Array.isArray(res?.hosts) ? res.hosts : [] };
}

/** The typed six digits travel with the call (spaces dropped) so the phone cannot confirm blindly. */
export async function approveHost(conn: RelayConnection, hostId: string, sas: string): Promise<unknown> {
  return conn.call('fleet.host.approve', { hostId, sas: normalizeSas(sas), grant: READ_ONLY_GRANT });
}

export async function rejectHost(conn: RelayConnection, hostId: string): Promise<unknown> {
  return conn.call('fleet.host.reject', { hostId });
}
