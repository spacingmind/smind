// newTask.test.ts covers M6's submit pipeline against a fake connection
// (records every {method, params}): validation issues, clamping, the
// create-then-optionally-start call order, both failure kinds (a partial
// failure retries run.start only), and singleFlight's double-tap guard.

import { describe, expect, it } from 'vitest';
import { ProviderInfo, Task } from '../api';
import {
  clampPrompt,
  clampTitle,
  NewTaskInput,
  retryFirstRun,
  singleFlight,
  submitNewTask,
  validateNewTask,
} from '../newTask';
import { RelayConnection } from '../relay/RelayConnection';
import { PairingOffer } from '../relay/pairing';

const dummyOffer: PairingOffer = {
  daemonId: '',
  publicKey: new Uint8Array(),
  relay: '',
  relayFingerprint: '',
  secret: new Uint8Array(),
  workspaceId: '',
};

function fakeConnection(handler: (method: string, params: unknown) => unknown): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}

const providers: ProviderInfo[] = [
  {
    id: 'glm',
    label: 'GLM',
    defaultMode: 'default',
    modes: [
      { id: 'default', label: 'Default' },
      { id: 'acceptEdits', label: 'Accept edits', autoApproves: true },
    ],
    models: [
      { id: 'glm-4.7', label: 'GLM 4.7', default: true },
      { id: 'glm-air', label: 'GLM Air' },
    ],
  },
];

const created: Task = { ID: 11, WorkspaceID: 3, SpaceID: null, Title: 'T', Status: 'created', CreatedAt: '', UpdatedAt: '' };

function input(over: Partial<NewTaskInput> = {}): NewTaskInput {
  return { workspaceId: 3, spaceId: null, title: 'Fix crash', prompt: '', provider: 'glm', model: '', mode: 'default', start: { kind: 'head' }, ...over };
}

describe('clampTitle / clampPrompt', () => {
  it('slices to the max', () => {
    expect(clampTitle('x'.repeat(500))).toHaveLength(200);
    expect(clampPrompt('y'.repeat(100_005))).toHaveLength(100_000);
    expect(clampTitle('short')).toBe('short');
  });
});

describe('validateNewTask', () => {
  it('no issues for a valid input, with or without a prompt', () => {
    expect(validateNewTask(input())).toEqual([]);
    expect(validateNewTask(input({ prompt: 'do it' }))).toEqual([]);
  });

  it('blank or whitespace-only title -> title-empty; over-long is fine (clamped, not an issue)', () => {
    expect(validateNewTask(input({ title: '   ' }))).toEqual(['title-empty']);
    expect(validateNewTask(input({ title: 'x'.repeat(500) }))).toEqual([]);
  });

  it('workspaceId <= 0 -> workspace-missing', () => {
    expect(validateNewTask(input({ workspaceId: 0 }))).toEqual(['workspace-missing']);
    expect(validateNewTask(input({ workspaceId: -1, title: '' }))).toContain('workspace-missing');
  });

  it('provider-missing only when a prompt is non-empty', () => {
    expect(validateNewTask(input({ provider: '' }))).toEqual([]);
    expect(validateNewTask(input({ provider: '', prompt: 'hi' }))).toEqual(['provider-missing']);
  });
});

describe('submitNewTask', () => {
  it('(a) title only: exactly one task.create, no run.start; the title is trimmed and clamped', async () => {
    const { conn, calls } = fakeConnection(() => created);
    const result = await submitNewTask(conn, input({ title: '  padded  ' }), providers);
    expect(result).toEqual({ kind: 'ok', task: created });
    expect(calls).toEqual([['task.create', { workspaceId: 3, title: 'padded' }]]);
  });

  it('(b) title+prompt: task.create then run.start with the created taskId, model/mode per wireModelMode', async () => {
    const { conn, calls } = fakeConnection((method) => (method === 'task.create' ? created : { runId: 'run-1' }));
    const result = await submitNewTask(conn, input({ prompt: 'do it', model: 'glm-air', mode: 'acceptEdits' }), providers);
    expect(result).toEqual({ kind: 'ok', task: created, run: { kind: 'started', runId: 'run-1' } });
    expect(calls).toEqual([
      ['task.create', { workspaceId: 3, title: 'Fix crash' }],
      ['run.start', { taskId: 11, provider: 'glm', prompt: 'do it', whenBusy: 'queue', model: 'glm-air', permissionMode: 'acceptEdits' }],
    ]);
  });

  it('(c) task.create rejects -> create-failed, no run.start', async () => {
    const { conn, calls } = fakeConnection(() => {
      throw new Error('daemon says no');
    });
    const result = await submitNewTask(conn, input({ prompt: 'do it' }), providers);
    expect(result).toEqual({ kind: 'create-failed', message: 'daemon says no' });
    expect(calls.map(([m]) => m)).toEqual(['task.create']);
  });

  it('(d) run.start rejects -> run-failed with the created task; retryFirstRun calls only run.start and returns ok', async () => {
    let fail = true;
    const { conn, calls } = fakeConnection((method) => {
      if (method === 'task.create') return created;
      if (fail) throw new Error('run refused');
      return { runId: 'run-2' };
    });
    const in_ = input({ prompt: 'do it' });
    const result = await submitNewTask(conn, in_, providers);
    expect(result).toEqual({ kind: 'run-failed', task: created, message: 'run refused' });

    fail = false;
    const retried = await retryFirstRun(conn, created, in_, providers);
    expect(retried).toEqual({ kind: 'ok', task: created, run: { kind: 'started', runId: 'run-2' } });
    expect(calls.filter(([m]) => m === 'task.create')).toHaveLength(1); // no second task
    expect(calls.filter(([m]) => m === 'run.start')).toHaveLength(2);
  });

  it('a queued outcome (setup script running) flows through as run: {kind: queued, queueItemId}', async () => {
    const { conn, calls } = fakeConnection((method) => (method === 'task.create' ? created : { queued: true, queueItemId: 7 }));
    const result = await submitNewTask(conn, input({ prompt: 'during setup' }), providers);
    expect(result).toEqual({ kind: 'ok', task: created, run: { kind: 'queued', queueItemId: 7 } });
    expect(calls).toEqual([
      ['task.create', { workspaceId: 3, title: 'Fix crash' }],
      ['run.start', { taskId: 11, provider: 'glm', prompt: 'during setup', whenBusy: 'queue' }],
    ]);
  });

  it('(e) invalid input makes no calls at all', async () => {
    const { conn, calls } = fakeConnection(() => {
      throw new Error('should not be called');
    });
    const result = await submitNewTask(conn, input({ title: '' }), providers);
    expect(result).toEqual({ kind: 'invalid', issues: ['title-empty'] });
    expect(calls).toEqual([]);
  });
});

describe('singleFlight', () => {
  it('two overlapping calls run the wrapped fn once; the second resolves undefined', async () => {
    let runs = 0;
    let release: (() => void) | undefined;
    const fn = singleFlight(async (n: number) => {
      runs += 1;
      await new Promise<void>((r) => {
        release = r;
      });
      return n * 2;
    });
    const first = fn(2);
    const second = fn(3);
    release!();
    await expect(first).resolves.toBe(4);
    await expect(second).resolves.toBeUndefined();
    expect(runs).toBe(1);
  });

  it('after the flight ends, a new call runs again', async () => {
    const fn = singleFlight(async () => 'ok');
    await expect(fn()).resolves.toBe('ok');
    await expect(fn()).resolves.toBe('ok');
  });
});
