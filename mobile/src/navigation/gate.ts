// gate.ts picks which Stack.Protected group of the app is reachable (ADR-0036
// decision 3). Kept free of React Native so the truth table is unit-tested.

import { ConnectionState } from '../relay/RelayConnection';

export type RouteGate = 'pairing' | 'status' | 'app';

/**
 * pairing: no connection yet. status: a connection exists but the session needs
 * the person (password, locked, pending, revoked, failed). app: a usable
 * session, including the ones that are still (re)connecting.
 */
export function routeGate(hasConn: boolean, state: ConnectionState | null): RouteGate {
  if (!hasConn) return 'pairing';
  switch (state?.kind) {
    case 'password':
    case 'locked':
    case 'pending':
    case 'revoked':
    case 'failed':
      return 'status';
    default:
      return 'app';
  }
}
