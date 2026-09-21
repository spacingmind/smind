// followUpPrompt.test.ts covers Milestone 3 Item 1's Test Scenarios at
// the logic layer, in the codebase's two established styles: fake-
// connection tests (api.test.ts's scripted call()) for the send flow's
// RPC sequence and failure rollback, and relay-harness tests
// (attachLifecycle.test.ts's in-memory E2EE pipe) for the new run's
// live attach, including navigate-away cancel sending task.cancel for
// the run.start-then-attach pair -- not task.prompt, whose cancellation
// would stop the run.

import { describe, expect, it } from 'vitest';
import { sendFollowUpPrompt } from '../followUpPrompt';
import { CloseableStream, RelayConnection } from '../relay/RelayConnection';
import { PairingOffer } from '../relay/pairing';
import { makeRelayHarness } from '../relayHarness';
import { TimelineLine } from '../runTimeline';

const dummyOffer: PairingOffer = {
  daemonId: '',
  publicKey: new Uint8Array(),
  relay: '',
  relayFingerprint: '',
  secret: new Uint8Array(),
  workspaceId: '',
};

/** A RelayConnection whose call() is fully scripted per test, recording traffic. */
function fakeConnection(
  handler: (method: string, params: unknown) => unknown,
): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => unknown }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}

/** Collects append/remove/track callbacks the screen would wire to its state. */
function recordingDeps() {
  const appended: TimelineLine[] = [];
  const removed: TimelineLine[] = [];
  const tails: Array<{ cancel(): void }> = [];
  return {
    appended,
    removed,
    tails,
    deps: {
      appendLines: (lines: TimelineLine[]) => appended.push(...lines),
      removeLines: (lines: TimelineLine[]) => removed.push(...lines),
      trackTail: (tail: { cancel(): void }) => tails.push(tail),
    },
  };
}

function seqFrom(start: number) {
  let n = start;
  return () => ++n;
}

describe('sendFollowUpPrompt (fake connection)', () => {
  it('renders the user line immediately, then calls run.start with the prior run\'s provider and attach to the new runId', async () => {
    const { conn, calls } = fakeConnection((method, params) => {
      if (method === 'run.start') return { runId: 'run-new' };
      if (method === 'run.attach') return {};
      throw new Error(`unexpected method ${method}`);
    });
    const rec = recordingDeps();
    const seq = seqFrom(0);

    const pending = sendFollowUpPrompt(conn, rec.deps, 42, 'claude/gpt-5.4', 'and now fix the lint error', seq);
    // The optimistic user line landed before any await could resolve.
    expect(rec.appended).toEqual([{ key: 'f1', role: 'user', text: 'and now fix the lint error' }]);
    await pending;

    expect(calls).toEqual([
      ['run.start', { taskId: 42, provider: 'claude/gpt-5.4', prompt: 'and now fix the lint error' }],
      ['run.attach', { runId: 'run-new' }],
    ]);
    expect(rec.tails).toHaveLength(1); // the attach is tracked for navigate-away cancel
    expect(rec.removed).toEqual([]); // nothing rolled back
  });

  it('a run.start failure rolls the optimistic user line back and surfaces the error (draft preserved for retry)', async () => {
    const { conn, calls } = fakeConnection((method) => {
      if (method === 'run.start') throw new Error('relay: connection is closed');
      throw new Error(`unexpected method ${method}`);
    });
    const rec = recordingDeps();

    await expect(sendFollowUpPrompt(conn, rec.deps, 42, 'claude/gpt-5.4', 'try again later', seqFrom(0))).rejects.toThrow(
      'relay: connection is closed',
    );
    expect(rec.appended).toEqual([{ key: 'f1', role: 'user', text: 'try again later' }]);
    expect(rec.removed).toEqual([{ key: 'f1', role: 'user', text: 'try again later' }]);
    expect(rec.tails).toEqual([]);
    expect(calls.map(([m]) => m)).toEqual(['run.start']); // no attach after a failed start
  });
});

describe('sendFollowUpPrompt (relay harness)', () => {
  it('streams the new run\'s events into the same appendLines timeline, not a fresh one', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const rec = recordingDeps();
    const seq = seqFrom(0);

    const pending = sendFollowUpPrompt(conn, rec.deps, 7, 'codex/gpt-5.4', 'run the tests', seq);
    const [startReq] = await daemon.waitForRequests(1);
    expect(startReq.method).toBe('run.start');
    expect(startReq.params).toEqual({ taskId: 7, provider: 'codex/gpt-5.4', prompt: 'run the tests' });
    daemon.reply(startReq.id!, { runId: 'run-2' });
    await pending;
    const attachReq = (await daemon.waitForRequests(2))[1];
    expect(attachReq.method).toBe('run.attach');
    expect(attachReq.params).toEqual({ runId: 'run-2' });

    daemon.emitRequestEvent(attachReq.id!, 'chunk', { text: 'running them now' });
    daemon.emitRequestEvent(attachReq.id!, 'done', { stopReason: 'end_turn' });
    await new Promise((r) => setTimeout(r, 10));
    expect(rec.appended).toEqual([
      { key: 'f1', role: 'user', text: 'run the tests' }, // the sent prompt, first
      { key: 'e2', role: 'assistant', text: 'running them now' }, // appended live...
      { key: 'e3', role: '', text: 'done (end_turn)' }, // ...to the same list
    ]);
  });

  it('cancelling the tracked tail (navigate away) sends task.cancel naming the attach\'s id and stops its event delivery', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const rec = recordingDeps();
    const seq = seqFrom(0);

    const pending = sendFollowUpPrompt(conn, rec.deps, 7, 'codex/gpt-5.4', 'run the tests', seq);
    const [startReq] = await daemon.waitForRequests(1);
    daemon.reply(startReq.id!, { runId: 'run-2' });
    await pending;
    const attachReq = (await daemon.waitForRequests(2))[1];
    expect(attachReq.method).toBe('run.attach');
    expect(rec.tails).toHaveLength(1);

    daemon.emitRequestEvent(attachReq.id!, 'chunk', { text: 'before detach' });
    await new Promise((r) => setTimeout(r, 10));

    rec.tails[0].cancel(); // the screen's navigate-away cleanup
    const cancelReq = (await daemon.waitForRequests(3))[2];
    expect(cancelReq.method).toBe('task.cancel');
    expect(cancelReq.id).toBeUndefined(); // conn.go's no-id cancellation envelope
    expect((cancelReq.params as { id: string }).id).toBe(attachReq.id);

    daemon.emitRequestEvent(attachReq.id!, 'chunk', { text: 'after detach' });
    await new Promise((r) => setTimeout(r, 10));
    expect(rec.appended.filter((l) => l.text === 'after detach')).toEqual([]); // no leaked handler
  });
});
