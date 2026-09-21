// taskAttention.test.ts covers Item 2's Test Scenarios: the sort
// function's ordering/tie-breaking as a pure unit, and the
// permission.pending subscribe wiring against the shared relay harness
// (same style as RelayConnection.test.ts's pushNotification pattern).

import { describe, expect, it } from 'vitest';
import { Task } from '../api';
import { makeRelayHarness } from '../relayHarness';
import { PendingApprovalSet, sortTasksByAttention, subscribeToPendingApprovals } from '../taskAttention';

function task(id: number, status: string): Task {
  return { ID: id, WorkspaceID: 1, SpaceID: null, Title: `task ${id}`, Status: status, CreatedAt: '', UpdatedAt: '' };
}

describe('sortTasksByAttention', () => {
  it('sorts pending-approval first, then running, then everything else, preserving original order within each tier', () => {
    const tasks = [task(1, 'done'), task(2, 'running'), task(3, 'created'), task(4, 'running'), task(5, 'error')];
    const pending = new Set([5]); // task 5 has a live permission.pending hit despite being "error"
    expect(sortTasksByAttention(tasks, pending).map((t) => t.ID)).toEqual([5, 2, 4, 1, 3]);
  });

  it('an empty pending set falls back to running-first, ties preserving original order', () => {
    const tasks = [task(1, 'created'), task(2, 'running'), task(3, 'done')];
    expect(sortTasksByAttention(tasks, new Set()).map((t) => t.ID)).toEqual([2, 1, 3]);
  });
});

describe('PendingApprovalSet', () => {
  it('markPending adds a taskId; reset() clears the set back to empty', () => {
    const set = new PendingApprovalSet();
    expect(set.has(7)).toBe(false);
    set.markPending(7);
    expect(set.has(7)).toBe(true);
    set.reset();
    expect(set.has(7)).toBe(false);
    expect(set.ids.size).toBe(0);
  });
});

describe('subscribeToPendingApprovals', () => {
  it('subscribes to permission.pending, delivers the event taskId, and unsubscribes cleanly', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const seen: number[] = [];
    const unsubscribe = subscribeToPendingApprovals(conn, (taskId) => seen.push(taskId));

    const [subReq] = await daemon.waitForRequests(1);
    expect(subReq.method).toBe('events.subscribe');
    expect(subReq.params).toEqual({ topics: ['permission.pending'] });
    daemon.reply(subReq.id!, { topics: ['permission.pending'] });

    daemon.pushNotification('permission.pending', { runId: 'r1', taskId: 42, requestId: 'req1', summary: 'run a command', options: [] });
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual([42]);

    unsubscribe();
    const unsubReq = (await daemon.waitForRequests(2))[1];
    expect(unsubReq.method).toBe('events.unsubscribe');
    daemon.reply(unsubReq.id!, { topics: [] });

    daemon.pushNotification('permission.pending', { runId: 'r2', taskId: 43, requestId: 'req2', summary: 'run another', options: [] });
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual([42]); // unchanged -- no leaked handler after unsubscribe
  });
});
