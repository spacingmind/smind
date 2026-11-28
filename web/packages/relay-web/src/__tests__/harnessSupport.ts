// Shared by the *.node.test.ts integration tests: a RelayController over
// fake-indexeddb and Node's webcrypto, paired with the real Go devharness.

import { expect, vi } from 'vitest';
import { IDBFactory } from 'fake-indexeddb';
import { DevHarness } from '@smind/relay-client/src/__tests__/devHarness';
import { RelayController, type ControllerState } from '../controller';
import { openRelayStore } from '../store';

export const timings = {
  reconnect: { pendingPollMs: 300, baseMs: 100, maxMs: 1_000, resumeProbeMs: 8_000, handshakeMs: 15_000 },
  keepalive: { intervalMs: 300, deadAfterMs: 2_000, probeTimeoutMs: 2_000 },
  liveness: { pendingStaleMs: 1_500, idleStaleMs: 3_000, checkMs: 250 },
};

export interface Paired {
  controller: RelayController;
  deviceId: string;
  states: ControllerState[];
}

/** Pairs a fresh browser profile with the harness and approves it on the daemon. */
export async function pairAndApprove(harness: DevHarness, label: string): Promise<Paired> {
  const store = await openRelayStore(new IDBFactory());
  const controller = new RelayController({ store, timings });
  const states: ControllerState[] = [];
  controller.onState((s) => states.push(s));
  await controller.pair(harness.pairingUrl, label);
  await vi.waitFor(() => expect(controller.state.kind).toBe('pending'), { timeout: 20_000 });
  const st = controller.state;
  if (st.kind !== 'pending') throw new Error('unreachable');
  await harness.mustCmd(`approve ${st.deviceId}`);
  await controller.whenConnected();
  return { controller, deviceId: st.deviceId, states };
}
