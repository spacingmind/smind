import { describe, expect, it } from 'vitest';
import { routeGate } from '../navigation/gate';
import { ConnectionState } from '../relay/RelayConnection';

const states: Record<ConnectionState['kind'], { state: ConnectionState; gate: 'status' | 'app' }> = {
  connecting: { state: { kind: 'connecting' }, gate: 'app' },
  connected: { state: { kind: 'connected' }, gate: 'app' },
  reconnecting: { state: { kind: 'reconnecting', attempt: 1, reason: 'x' }, gate: 'app' },
  closed: { state: { kind: 'closed' }, gate: 'app' },
  pending: { state: { kind: 'pending', deviceId: 'd', label: 'l' }, gate: 'status' },
  password: { state: { kind: 'password', reason: 'required' }, gate: 'status' },
  locked: { state: { kind: 'locked', retryAfter: 5, until: 1 }, gate: 'status' },
  revoked: { state: { kind: 'revoked', deviceId: 'd' }, gate: 'status' },
  failed: { state: { kind: 'failed', message: 'm' }, gate: 'status' },
};

describe('routeGate', () => {
  it('is pairing without a connection, whatever the state', () => {
    expect(routeGate(false, null)).toBe('pairing');
    for (const { state } of Object.values(states)) expect(routeGate(false, state)).toBe('pairing');
  });

  it('is app for a connection whose state is not known yet', () => {
    expect(routeGate(true, null)).toBe('app');
  });

  for (const [kind, { state, gate }] of Object.entries(states)) {
    it(`${kind} with a connection → ${gate}`, () => {
      expect(routeGate(true, state)).toBe(gate);
    });
  }
});
