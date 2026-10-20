import { describe, expect, it, vi } from 'vitest';
import { dropConnection, performResetIdentity, replaceConnection } from '../connection/connectionActions';

describe('performResetIdentity', () => {
  it('disconnects once, and only after the identity was reset', async () => {
    const calls: string[] = [];
    const disconnect = vi.fn(() => calls.push('disconnect'));
    const reset = async () => {
      await Promise.resolve();
      calls.push('reset');
    };
    expect(await performResetIdentity(reset, disconnect)).toBe(true);
    expect(disconnect).toHaveBeenCalledTimes(1);
    expect(calls).toEqual(['reset', 'disconnect']);
  });

  it('stays connected when the reset fails', async () => {
    const disconnect = vi.fn();
    expect(await performResetIdentity(async () => { throw new Error('keychain'); }, disconnect)).toBe(false);
    expect(disconnect).not.toHaveBeenCalled();
  });
});

const fake = () => ({ close: vi.fn() });

describe('replaceConnection', () => {
  it('keeps the first connection without closing anything', () => {
    const next = fake();
    expect(replaceConnection(null, next)).toBe(next);
    expect(next.close).not.toHaveBeenCalled();
  });
  it('closes a different previous connection and keeps the new one', () => {
    const prev = fake();
    const next = fake();
    expect(replaceConnection(prev, next)).toBe(next);
    expect(prev.close).toHaveBeenCalledTimes(1);
    expect(next.close).not.toHaveBeenCalled();
  });
  it('leaves the same connection alone', () => {
    const same = fake();
    expect(replaceConnection(same, same)).toBe(same);
    expect(same.close).not.toHaveBeenCalled();
  });
});

describe('dropConnection', () => {
  it('closes the current connection once and keeps none', () => {
    const cur = fake();
    expect(dropConnection(cur)).toBeNull();
    expect(cur.close).toHaveBeenCalledTimes(1);
  });
  it('is a no-op without a connection', () => {
    expect(dropConnection(null)).toBeNull();
  });
});
