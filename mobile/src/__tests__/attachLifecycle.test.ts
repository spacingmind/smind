// attachLifecycle.test.ts covers Item 3's detach Test Scenario at the
// connection layer, over the shared in-memory E2EE pipe (relayHarness.ts):
// cancelling a run.attach-style call sends the daemon's task.cancel wire
// shape, rejects only that call, leaves the connection fully usable for
// further calls, and delivers no further interim events to the cancelled
// call's onEvent (no leaked handler).

import { describe, expect, it } from 'vitest';
import { makeRelayHarness } from '../relayHarness';

describe('call cancellation (run.attach detach)', () => {
  it('cancel() sends task.cancel on the wire, rejects only that call, and stops event delivery to its onEvent', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const seen: string[] = [];
    const attach = conn.call('run.attach', { runId: 'r1' }, { onEvent: (e) => seen.push(e) });
    const [attachReq] = await daemon.waitForRequests(1);

    daemon.emitRequestEvent(attachReq.id!, 'chunk', { text: 'streaming...' });
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual(['chunk']);

    attach.cancel();
    // Attach the rejection expectation before anything else so the
    // cancel-triggered rejection is always handled.
    const rejected = expect(attach).rejects.toThrow('relay: call cancelled');

    const cancelReq = (await daemon.waitForRequests(2))[1];
    expect(cancelReq.method).toBe('task.cancel');
    expect(cancelReq.id).toBeUndefined(); // no envelope id, per conn.go's handleCancel
    expect((cancelReq.params as { id: string }).id).toBe(attachReq.id);
    await rejected;

    // A late interim event for the cancelled id has no handler anymore.
    daemon.emitRequestEvent(attachReq.id!, 'chunk', { text: 'too late' });
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual(['chunk']); // unchanged -- no leaked handler

    // The connection itself is still perfectly usable.
    const followUp = conn.call('workspace.list');
    const followReq = (await daemon.waitForRequests(3))[2];
    daemon.reply(followReq.id!, []);
    await expect(followUp).resolves.toEqual([]);
  });

  it('cancel() after the call resolved is a no-op', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const p = conn.call('workspace.list');
    const [req] = await daemon.waitForRequests(1);
    daemon.reply(req.id!, [1, 2]);
    await expect(p).resolves.toEqual([1, 2]);
    expect(() => p.cancel()).not.toThrow();
    await new Promise((r) => setTimeout(r, 10));
    expect(daemon.received()).toHaveLength(1); // no spurious task.cancel
  });
});
