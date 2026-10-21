// integration.queue.node.test.ts is ADR-0021 §9's mobile end-to-end test: the
// real TypeScript relay client and its queue logic (ChatQueue, RunFollower,
// startRunWhenBusy) against a real Go relay and daemon hub, with the repo's
// fake ACP agent as a LONG-RUNNING agent (scenario `hang` never ends until the
// run is stopped): a busy chat queues prompts on the daemon, the queue view
// shows and cancels them, the queue survives an app restart (it is server
// state), a stopped run delivers the next prompt as a daemon-started run that
// the client adopts, and `interrupt` stops the live run and goes first.
//
// Run with: npm run test:integration (from mobile/); needs `go` on PATH.

/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { ChatQueue } from '../../chatQueue';
import { defaultChatId, listRunsForTask, startRunWhenBusy } from '../../api';
import { RunFollower, RunStatusEvent } from '../../runFollower';
import { RelayConnection } from '../RelayConnection';
import { buildFakeAgent, DevHarness, requireSelfSignedHarnessTrust, testConnectOptions } from './devHarness';

let harness: DevHarness;
let fakeAgent: { path: string; cleanup(): void };
const connections: RelayConnection[] = [];
const queues: ChatQueue[] = [];

async function connect(): Promise<RelayConnection> {
  // The same device identity every time: a reopened app is the same device.
  const conn = await RelayConnection.connect(harness.pairingUrl, testConnectOptions(61));
  connections.push(conn);
  return conn;
}

function queueFor(conn: RelayConnection, chatId: number): ChatQueue {
  const q = new ChatQueue(conn, chatId, () => {});
  queues.push(q);
  q.start();
  return q;
}

const prompts = (q: ChatQueue) => q.items.map((i) => i.Prompt);

/**
 * seed-live returns as soon as the run is `running`, which can be a moment BEFORE the fake agent has read its
 * scenario file (it reads it when the prompt starts). Overwriting the file for the runs that follow before that
 * makes the "hanging" run read `default` and finish `done` on a slow machine (seen on CI). The `hang` scenario
 * streams "before hang" first, so wait for that chunk, then it is safe to switch the scenario.
 */
async function waitForHangToStream(conn: RelayConnection, runId: string): Promise<void> {
  await vi.waitFor(
    async () => {
      const logs = (await conn.call('run.logs', { runId })) as { events?: Array<{ type: string; text?: string }> };
      expect((logs.events ?? []).some((e) => e.type === 'chunk' && e.text === 'before hang')).toBe(true);
    },
    { timeout: 15_000, interval: 100 },
  );
}

beforeAll(async () => {
  requireSelfSignedHarnessTrust();
  fakeAgent = buildFakeAgent();
  harness = await DevHarness.start(10, false, undefined, fakeAgent.path); // a window: the test device is approved at once
}, 120_000);

afterAll(() => {
  for (const q of queues) q.stop();
  for (const c of connections) c.close();
  harness?.stop();
  fakeAgent?.cleanup();
});

describe(`the daemon prompt queue on mobile, against a real daemon and a long-running fake agent`, () => {
  it('an idle chat starts the run at once under whenBusy queue', { timeout: 60_000 }, async () => {
    const conn = await connect();
    const seeded = await harness.seedRun('end_turn', 'Idle task');
    const chatId = await defaultChatId(conn, seeded.taskId);
    const result = await startRunWhenBusy(conn, { taskId: seeded.taskId, chatId, provider: 'glm', prompt: 'hello again', whenBusy: 'queue' });
    expect(result.kind).toBe('started');
    await vi.waitFor(async () => expect((await harness.runStatus((result as { runId: string }).runId)).status).toBe('done'), { timeout: 20_000 });
  });

  describe('a busy chat', () => {
    let conn: RelayConnection;
    let taskId: number;
    let chatId: number;
    let liveRunId: string;

    beforeAll(async () => {
      conn = await connect();
      const live = await harness.seedLive('hang', 'Busy task');
      taskId = live.taskId;
      chatId = live.chatId;
      liveRunId = live.runId;
    }, 60_000);

    it('queues prompts on the daemon, shows them in order, and cancel removes one', { timeout: 60_000 }, async () => {
      const queue = queueFor(conn, chatId);
      await vi.waitFor(() => expect(prompts(queue)).toEqual([]));

      const second = await startRunWhenBusy(conn, { taskId, chatId, provider: 'glm', prompt: 'second', whenBusy: 'queue' });
      const third = await startRunWhenBusy(conn, { taskId, chatId, provider: 'glm', prompt: 'third', whenBusy: 'queue' });
      expect(second.kind).toBe('queued');
      expect(third.kind).toBe('queued');
      await vi.waitFor(() => expect(prompts(queue)).toEqual(['second', 'third']), { timeout: 10_000 });
      expect(queue.items.every((i) => i.Priority === 0 && i.Status === 'queued')).toBe(true);

      await queue.cancel(queue.items[0].ID);
      await vi.waitFor(() => expect(prompts(queue)).toEqual(['third']), { timeout: 10_000 }); // gone when the snapshot arrives
      expect((await harness.queueList(chatId)).find((i) => i.prompt === 'second')?.status).toBe('cancelled');

      // Cancelling something that is no longer queued is swallowed and the list stays the truth.
      await expect(queue.cancel(queue.items[0].ID + 9999)).resolves.toBeUndefined();
      expect(prompts(queue)).toEqual(['third']);
    });

    it('the queue survives an app restart: it is server state', { timeout: 60_000 }, async () => {
      conn.close(); // the app goes away
      const reopened = await connect(); // and comes back
      const queue = queueFor(reopened, chatId);
      await vi.waitFor(() => expect(prompts(queue)).toEqual(['third']), { timeout: 10_000 });
      conn = reopened;
    });

    it('a stopped run delivers the next prompt as a daemon-started run that the client adopts once', { timeout: 90_000 }, async () => {
      const queue = queueFor(conn, chatId);
      await vi.waitFor(() => expect(prompts(queue)).toEqual(['third']), { timeout: 10_000 });

      const adopted: string[] = [];
      const lives: Array<string | null> = [];
      const follower = new RunFollower(chatId, (id) => adopted.push(id), (live) => lives.push(live));
      follower.markKnown(liveRunId, 'running');
      conn.subscribe(['run.status'], (e) => follower.onRunStatus(e.payload as RunStatusEvent));

      await waitForHangToStream(conn, liveRunId);
      await harness.setScenario(taskId, 'default'); // delivered runs finish
      await harness.stopRun(liveRunId);

      await vi.waitFor(() => expect(adopted).toHaveLength(1), { timeout: 30_000 });
      await vi.waitFor(() => expect(prompts(queue)).toEqual([]), { timeout: 15_000 }); // delivered items vanish
      const runs = await listRunsForTask(conn, taskId);
      const delivered = runs.find((r) => r.ID === adopted[0])!;
      expect(delivered.Prompt).toBe('third');
      expect(delivered.ChatID).toBe(chatId);
      await vi.waitFor(async () => expect((await harness.runStatus(adopted[0])).status).toBe('done'), { timeout: 20_000 });
      await vi.waitFor(() => expect(follower.liveRunId).toBeNull(), { timeout: 10_000 });
      expect(adopted).toHaveLength(1); // adopted exactly once, however many statuses arrived
      expect(lives).toContain(adopted[0]); // it became the live run...
      expect(lives.at(-1)).toBeNull(); // ...and then ended, leaving none live
    });
  });

  it('interrupt stops the live run and its prompt goes ahead of earlier queued ones', { timeout: 90_000 }, async () => {
    const conn = await connect();
    const live = await harness.seedLive('hang', 'Interrupt task');
    await waitForHangToStream(conn, live.runId);
    await harness.setScenario(live.taskId, 'default'); // the delivered runs finish; the live one is already running `hang`
    const queue = queueFor(conn, live.chatId);

    const later = await startRunWhenBusy(conn, { taskId: live.taskId, chatId: live.chatId, provider: 'glm', prompt: 'later', whenBusy: 'queue' });
    expect(later.kind).toBe('queued');
    const urgent = await startRunWhenBusy(conn, { taskId: live.taskId, chatId: live.chatId, provider: 'glm', prompt: 'urgent', whenBusy: 'interrupt' });
    expect(urgent.kind).toBe('queued'); // interrupt also goes through the queue (priority 1)

    await vi.waitFor(async () => expect((await harness.runStatus(live.runId)).status).toBe('stopped'), { timeout: 20_000 });
    // Both end up delivered, the interrupting prompt first.
    await vi.waitFor(() => expect(prompts(queue)).toEqual([]), { timeout: 30_000 });
    const runs = (await listRunsForTask(conn, live.taskId)).filter((r) => r.ID !== live.runId);
    const order = runs.sort((a, b) => a.StartedAt.localeCompare(b.StartedAt)).map((r) => r.Prompt);
    expect(order).toEqual(['urgent', 'later']);
  });
});
