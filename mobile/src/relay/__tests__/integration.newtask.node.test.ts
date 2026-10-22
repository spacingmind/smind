// integration.newtask.node.test.ts is M6's mobile end-to-end test: the real
// TypeScript relay client's create-task path (listProviders, createTask,
// startFirstRun) against a real Go relay + daemon hub with the repo's fake
// ACP agent: the provider catalog arrives, a task is created in the seeded
// workspace, its first prompt's run finishes done, and a task created
// without a prompt stays run-less.
//
// Run with: npm run test:integration (from mobile/); needs `go` on PATH.

/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { createTask, listProviders, listRunsForTask, startFirstRun } from '../../api';
import { RelayConnection } from '../RelayConnection';
import { buildFakeAgent, DevHarness, requireSelfSignedHarnessTrust, testConnectOptions } from './devHarness';

let harness: DevHarness;
let fakeAgent: { path: string; cleanup(): void };
let conn: RelayConnection;

beforeAll(async () => {
  requireSelfSignedHarnessTrust();
  fakeAgent = buildFakeAgent();
  harness = await DevHarness.start(10, false, undefined, fakeAgent.path); // a window: the test device is approved at once
  conn = await RelayConnection.connect(harness.pairingUrl, testConnectOptions(71));
}, 120_000);

afterAll(() => {
  conn?.close();
  harness?.stop();
  fakeAgent?.cleanup();
});

describe('creating a task from the phone, against a real daemon', () => {
  it('provider.list includes the harness provider glm', async () => {
    const providers = await listProviders(conn);
    expect(providers.some((p) => p.id === 'glm')).toBe(true);
  });

  it('task.create + a first prompt whose run finishes done', { timeout: 90_000 }, async () => {
    const seeded = await harness.seedRun('default', 'M6 seed task'); // the seeded workspace the new task joins
    const task = await createTask(conn, { workspaceId: seeded.workspaceId, spaceId: null, title: 'M6 created task' });
    expect(task.Title).toBe('M6 created task');
    expect(task.WorkspaceID).toBe(seeded.workspaceId);

    const runId = await startFirstRun(conn, { taskId: task.ID, provider: 'glm', prompt: 'do the thing' });
    expect(runId).toBeTruthy();
    await vi.waitFor(async () => {
      const runs = await listRunsForTask(conn, task.ID);
      expect(runs.find((r) => r.ID === runId)?.Status).toBe('done');
    }, { timeout: 30_000 });
  });

  it('task.create without a prompt leaves the task with no runs', async () => {
    const seeded = await harness.seedRun('default', 'M6 seed task two');
    const task = await createTask(conn, { workspaceId: seeded.workspaceId, spaceId: null, title: 'M6 quiet task' });
    expect(await listRunsForTask(conn, task.ID)).toEqual([]);
  });
});
