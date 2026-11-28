/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { DevHarness, requireSelfSignedHarnessTrust } from '@smind/relay-client/src/__tests__/devHarness';
import { RelayStateError } from '../controller';
import { pairAndApprove, type Paired } from './harnessSupport';

let harness: DevHarness;
let paired: Paired | undefined;

beforeAll(async () => {
  requireSelfSignedHarnessTrust();
  harness = await DevHarness.start(0);
}, 90_000);

afterAll(() => {
  paired?.controller.close();
  harness?.stop();
});

describe('revoke', () => {
  it('revoked_device_gets_a_clear_state', { timeout: 60_000 }, async () => {
    paired = await pairAndApprove(harness, 'web it');
    const { controller, deviceId } = paired;
    await harness.mustCmd(`revoke ${deviceId}`);
    await vi.waitFor(() => expect(controller.state.kind).toBe('revoked'), { timeout: 30_000 });
    const err = await controller.socket().catch((e) => e);
    expect(err).toBeInstanceOf(RelayStateError);
    expect((err as RelayStateError).state.kind).toBe('revoked');
    expect((await harness.devices()).find((r) => r.id === deviceId)?.state).toBe('revoked');
  });
});
