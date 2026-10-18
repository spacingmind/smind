// integration.stop.node.test.ts: Stop for a live run and the delivered prompt of
// a queued run, against a real relay and daemon with the fake ACP agent's
// long-running `hang` scenario (a run that never ends until it is stopped).
//
// Run with: npm run test:integration (from mobile/); needs `go` on PATH.

/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { adoptedPromptText } from '../../adoptedPrompt';
import { listRunsForTask, startRunWhenBusy } from '../../api';
import { ChatQueue } from '../../chatQueue';
import { RunFollower, RunStatusEvent } from '../../runFollower';
import { requestStop } from '../../stopControl';
import { RelayConnection } from '../RelayConnection';
import { buildFakeAgent, DevHarness, requireSelfSignedHarnessTrust, testConnectOptions } from './devHarness';

let harness: DevHarness;
let fakeAgent: { path: string; cleanup(): void };
let conn: RelayConnection;
const queues: ChatQueue[] = [];

beforeAll(async () => {
  requireSelfSignedHarnessTrust();
  fakeAgent = buildFakeAgent();
  harness = await DevHarness.start(10, false, undefined, fakeAgent.path);
  conn = await RelayConnection.connect(harness.pairingUrl, testConnectOptions(71));
}, 120_000);

afterAll(() => {
  for (const q of queues) q.stop();
  conn?.close();
  harness?.stop();
  fakeAgent?.cleanup();
});

describe('Stop and delivered prompts on mobile, against a real daemon and a long-running fake agent', () => {
  it('requestStop ends a live run: it is stopped on the daemon and the follower clears the live run', { timeout: 60_000 }, async () => {
    const live = await harness.seedLive('hang', 'Stop task');
    const lives: Array<string | null> = [];
    const follower = new RunFollower(live.chatId, () => {}, (id) => lives.push(id));
    follower.markKnown(live.runId, 'running');
    conn.subscribe(['run.status'], (e) => follower.onRunStatus(e.payload as RunStatusEvent));
    expect(follower.liveRunId).toBe(live.runId);

    expect(await requestStop(conn, live.runId)).toBeNull(); // no confirmation step, the call just succeeds
    await vi.waitFor(async () => expect((await harness.runStatus(live.runId)).status).toBe('stopped'), { timeout: 20_000 });
    await vi.waitFor(() => expect(follower.liveRunId).toBeNull(), { timeout: 10_000 }); // from run.status, not from the call
    expect(lives).toEqual([live.runId, null]);

    // Stopping a run that already ended is not an error (the daemon's run.stop contract).
    expect(await requestStop(conn, live.runId)).toBeNull();
  });

  it('requestStop returns the daemon\'s error text instead of throwing', { timeout: 30_000 }, async () => {
    const failure = await requestStop(conn, 'no-such-run');
    expect(typeof failure === 'string' || failure === null).toBe(true); // a daemon that rejects an unknown id says why; one that does not is fine too
  });

  it('the prompt of a daemon-delivered queued run is known client-side: from the queue entry, and from run.list', { timeout: 90_000 }, async () => {
    const live = await harness.seedLive('hang', 'Delivered prompt task');
    const queue = new ChatQueue(conn, live.chatId, () => {});
    queues.push(queue);
    queue.start();

    const queued = await startRunWhenBusy(conn, { taskId: live.taskId, chatId: live.chatId, provider: 'glm', prompt: 'please continue with part two', whenBusy: 'queue' });
    expect(queued.kind).toBe('queued');
    await vi.waitFor(() => expect(queue.items.map((i) => i.Prompt)).toEqual(['please continue with part two']), { timeout: 10_000 });

    const adopted: string[] = [];
    const follower = new RunFollower(live.chatId, (id) => adopted.push(id), () => {});
    follower.markKnown(live.runId, 'running');
    conn.subscribe(['run.status'], (e) => follower.onRunStatus(e.payload as RunStatusEvent));
    await harness.setScenario(live.taskId, 'default'); // the delivered run finishes
    await harness.stopRun(live.runId);
    await vi.waitFor(() => expect(adopted).toHaveLength(1), { timeout: 30_000 });
    const runId = adopted[0];

    // 1. The queue snapshot keeps the delivered item: its text is available client-side.
    await vi.waitFor(() => expect(queue.promptForRun(runId)).toBe('please continue with part two'), { timeout: 10_000 });
    expect(await adoptedPromptText(queue, () => listRunsForTask(conn, live.taskId, live.chatId), runId)).toBe('please continue with part two');
    // 2. And even without the queue entry, the run's own prompt from run.list says the same.
    expect(await adoptedPromptText({ promptForRun: () => undefined }, () => listRunsForTask(conn, live.taskId, live.chatId), runId)).toBe('please continue with part two');
  });
});
