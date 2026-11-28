/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { DevHarness, requireSelfSignedHarnessTrust } from '@smind/relay-client/src/__tests__/devHarness';
import { WsClient } from '../../../ui/src/lib/ws-client';
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

describe('reconnect', () => {
  it('reconnect_resumes_after_relay_restart', { timeout: 120_000 }, async () => {
    paired = await pairAndApprove(harness, 'web it');
    const { controller, states } = paired;
    expect(await new WsClient(await controller.socket()).call('workspace.list')).toEqual([]);

    const before = states.length;
    await harness.mustCmd('relay-restart');
    await vi.waitFor(() => expect(states.slice(before).some((s) => s.kind === 'reconnecting')).toBe(true), { timeout: 60_000 });
    await vi.waitFor(() => expect(controller.state.kind).toBe('connected'), { timeout: 60_000 });

    const client = new WsClient(await controller.socket());
    expect(await client.call('workspace.list')).toEqual([]);
  });
});
