import { describe, expect, it } from 'vitest';
import { PendingTarget, releaseTarget, resolveNotificationTarget, targetHref } from '../target';

describe('notification target', () => {
  it('resolves a valid target', () => {
    expect(resolveNotificationTarget({ taskId: 12, chatId: 3 })).toEqual({ taskId: 12, chatId: 3 });
    expect(resolveNotificationTarget({ taskId: 12 })).toEqual({ taskId: 12 });
  });

  it('accepts digit strings and drops an invalid chatId', () => {
    expect(resolveNotificationTarget({ taskId: '12', chatId: '7' })).toEqual({ taskId: 12, chatId: 7 });
    expect(resolveNotificationTarget({ taskId: 12, chatId: 'x' })).toEqual({ taskId: 12 });
    expect(resolveNotificationTarget({ taskId: 12, chatId: -1 })).toEqual({ taskId: 12 });
  });

  it('rejects invalid task ids', () => {
    const bad: unknown[] = [
      null,
      undefined,
      [],
      [{ taskId: 1 }],
      'x',
      5,
      {},
      { taskId: 0 },
      { taskId: -1 },
      { taskId: 1.5 },
      { taskId: 'abc' },
      { taskId: '12abc' },
      { taskId: '' },
      { taskId: NaN },
      { taskId: {} },
      { taskId: Number.MAX_SAFE_INTEGER + 1 },
      { taskId: '99999999999999999999' },
    ];
    for (const b of bad) expect(resolveNotificationTarget(b), JSON.stringify(b)).toBeNull();
  });

  it('targetHref points at the task route with an empty title', () => {
    const h = targetHref({ taskId: 42 });
    expect(h.pathname).toBe('/task/[id]');
    expect(h.params.id).toBe('42');
    expect(h.params.title).toBe('');
  });
});

describe('PendingTarget', () => {
  it('a newer target replaces an older one', () => {
    const p = new PendingTarget();
    p.set({ taskId: 1 });
    p.set({ taskId: 2 });
    expect(p.peek()).toEqual({ taskId: 2 });
    p.set(null);
    expect(p.peek()).toBeNull();
  });

  it('take clears the target', () => {
    const p = new PendingTarget();
    p.set({ taskId: 1 });
    expect(p.take()).toEqual({ taskId: 1 });
    expect(p.take()).toBeNull();
    expect(p.peek()).toBeNull();
  });
});

describe('releaseTarget', () => {
  it('releaseTarget holds the target while the gate is pairing or status', () => {
    const p = new PendingTarget();
    p.set({ taskId: 5 });
    expect(releaseTarget('pairing', p)).toBeNull();
    expect(releaseTarget('status', p)).toBeNull();
    expect(p.peek()).toEqual({ taskId: 5 });
  });

  it('releaseTarget releases once when the gate becomes app', () => {
    const p = new PendingTarget();
    p.set({ taskId: 5 });
    expect(releaseTarget('app', p)).toEqual({ taskId: 5 });
    expect(releaseTarget('app', p)).toBeNull();
  });

  it('a target set after the gate is already app is released on the next call', () => {
    const p = new PendingTarget();
    expect(releaseTarget('app', p)).toBeNull();
    p.set({ taskId: 9, chatId: 1 });
    expect(releaseTarget('app', p)).toEqual({ taskId: 9, chatId: 1 });
  });
});
