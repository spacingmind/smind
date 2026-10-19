import { describe, expect, it, vi } from 'vitest';
import { performResetIdentity } from '../connection/connectionActions';

describe('performResetIdentity', () => {
  it('disconnects once after the identity was reset', async () => {
    const disconnect = vi.fn();
    expect(await performResetIdentity(async () => {}, disconnect)).toBe(true);
    expect(disconnect).toHaveBeenCalledTimes(1);
  });

  it('stays connected when the reset fails', async () => {
    const disconnect = vi.fn();
    expect(await performResetIdentity(async () => { throw new Error('keychain'); }, disconnect)).toBe(false);
    expect(disconnect).not.toHaveBeenCalled();
  });
});
